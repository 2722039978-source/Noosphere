package evalrunner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
)

func newTestRunner(t *testing.T) *Runner {
	t.Helper()
	binary := filepath.Join(runtime.GOROOT(), "bin", "go.exe")
	if runtime.GOOS != "windows" {
		binary = filepath.Join(runtime.GOROOT(), "bin", "go")
	}
	r, err := New(Config{GoBinary: binary, TempRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	return r
}

func TestRunCaseCancellationAtPreparationBoundary(t *testing.T) {
	r := newTestRunner(t)
	module, err := r.Compile(context.Background(), fixtureSpec(t, "solution.go.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	for _, mode := range []string{"caller", "runner-close"} {
		t.Run(mode, func(t *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			var once sync.Once
			var prepared, guestCalls int
			closed := make(chan error, 1)
			factory := experimental.FunctionListenerFactoryFunc(func(def api.FunctionDefinition) experimental.FunctionListener {
				if def.GoFunction() != nil {
					return nil
				}
				prepared++
				once.Do(func() {
					if mode == "caller" {
						// Preparation may finish after two seconds. Its caller
						// cancellation must not be replaced by an execution deadline.
						time.Sleep(CaseTimeout + 20*time.Millisecond)
						cancel()
					} else {
						go func() { closed <- r.Close() }()
						<-r.ctx.Done()
					}
				})
				return experimental.FunctionListenerFunc(func(context.Context, api.Module, api.FunctionDefinition, []uint64, experimental.StackIterator) {
					guestCalls++
				})
			})
			result, err := r.RunCase(experimental.WithFunctionListenerFactory(parent, factory), module, []byte(`[1]`))
			if mode == "runner-close" {
				if closeErr := <-closed; closeErr != nil {
					t.Fatal(closeErr)
				}
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("preparation cancellation lost: %v", err)
			}
			if prepared == 0 {
				t.Fatal("test did not reach real Wasm preparation")
			}
			if guestCalls != 0 || len(result.Output) != 0 || result.ExecutionDuration != 0 {
				t.Fatalf("cancelled preparation started guest: calls=%d result=%+v", guestCalls, result)
			}
			if result.PreparationDuration <= 0 || result.TotalDuration < result.PreparationDuration || result.Duration != result.TotalDuration {
				t.Fatalf("preparation cancellation timings missing/inconsistent: %+v", result)
			}
		})
	}
}

func fixtureSpec(t *testing.T, file string) CompileSpec {
	t.Helper()
	source, err := os.ReadFile(filepath.Join("testdata", file))
	if err != nil {
		t.Fatal(err)
	}
	return CompileSpec{Source: string(source), FunctionName: "Solve", InputType: "[]int", OutputType: "[]int"}
}

func TestWASIExecutesTrustedGoFixture(t *testing.T) {
	r := newTestRunner(t)
	started := time.Now()
	module, err := r.Compile(context.Background(), fixtureSpec(t, "solution.go.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	t.Logf("cold independent-cache compile %s; Wasm module %d bytes", time.Since(started), len(module.state.wasm))
	for _, tc := range []struct{ input, want string }{{`[3,-2,7]`, `[6,-4,14]`}, {`[]`, `[]`}} {
		got, err := r.RunCase(context.Background(), module, []byte(tc.input))
		if err != nil {
			t.Fatal(err)
		}
		if string(got.Output) != tc.want {
			t.Fatalf("output %s; want %s", got.Output, tc.want)
		}
		t.Logf("case input=%s output=%s duration=%s", tc.input, got.Output, got.Duration)
	}
	startup := probe(t, r, "startup-memory.go.txt")
	defer startup.Close()
	stats, err := r.RunCase(context.Background(), startup, []byte(`""`))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("actual guest Go runtime startup: %s; duration=%s", stats.Output, stats.Duration)
}

func TestLoopCancelledAndMemoryBounded(t *testing.T) {
	r := newTestRunner(t)
	spec := CompileSpec{Source: "package main\nfunc Solve(x int) int { for {} }", FunctionName: "Solve", InputType: "int", OutputType: "int"}
	module, err := r.Compile(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	loopResult, err := r.RunCase(context.Background(), module, []byte(`1`))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("infinite loop must hit timeout: %v", err)
	}
	if loopResult.ExecutionDuration < 1800*time.Millisecond || loopResult.ExecutionDuration > 3*time.Second || loopResult.TotalDuration < loopResult.PreparationDuration+loopResult.ExecutionDuration || loopResult.Duration != loopResult.TotalDuration {
		t.Fatalf("execution deadline/timing contract violated: %+v", loopResult)
	}
	spec.Source = "package main\nfunc Solve(x int) int { a := make([]int, x); a[x-1] = 7; return a[x-1] }"
	memory, err := r.Compile(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	if result, err := r.RunCase(context.Background(), memory, []byte(`100000000`)); err == nil || !strings.Contains(result.Stderr, "out of memory") {
		t.Fatalf("800MB allocation must exhaust linear memory: %v; stderr: %s", err, result.Stderr)
	} else {
		var runnerErr *Error
		if !errors.As(err, &runnerErr) || runnerErr.Code != "OUT_OF_MEMORY" {
			t.Fatalf("memory exhaustion must preserve a resource verdict: %v; stderr: %s", err, result.Stderr)
		}
	}
	result, err := r.RunCase(context.Background(), memory, []byte(`3`))
	if err != nil || string(result.Output) != "7" {
		t.Fatalf("sandbox not reusable after failure: %+v %v", result, err)
	}
	spec.Source = `package main
func Solve(x int) int {panic("runtime: out of memory: cannot allocate 800063488-byte block (393216 in use)\nfatal error: out of memory\n")}`
	spoof, err := r.Compile(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	defer spoof.Close()
	_, err = r.RunCase(context.Background(), spoof, []byte(`1`))
	var runnerErr *Error
	if !errors.As(err, &runnerErr) || runnerErr.Code != "SANDBOX_VIOLATION" {
		t.Fatalf("model panic text mistaken for OOM: %v", err)
	}
}
