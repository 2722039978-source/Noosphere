package evalrunner

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// Trusted probes intentionally bypass model AST validation only in this test file.
// They still use the production compiler containment and the actual WASI runner.
func probe(t *testing.T, r *Runner, file string) ModuleArtifact {
	t.Helper()
	source, err := os.ReadFile(filepath.Join("testdata", file))
	if err != nil {
		t.Fatal(err)
	}
	wasm, err := r.compileFiles(context.Background(), string(source), "")
	if err != nil {
		t.Fatal(err)
	}
	return r.seal(wasm, "string")
}

func TestRejectsInitImportsAndGoDirectives(t *testing.T) {
	r := newTestRunner(t)
	for _, source := range []string{
		"package main; import \"os\"; func Solve(x int) int{return x}",
		"package main; func init(){}; func Solve(x int) int{return x}",
		"package main; var secret=1; func Solve(x int) int{return x}",
		"package changed; func Solve(x int) int{return x}",
		"package main\n//go:linkname Solve runtime.exit\nfunc Solve(x int) int{return x}",
		"package main\n//line other.go:1\nfunc Solve(x int) int{return x}",
		"package main; func Solve(x int) int{go Solve(x);return x}",
		"package main; func Solve(x int) int{println(x);return x}",
		"package main; func Solve(x int) int{ f:=func()int{return x};return f()}",
	} {
		_, err := r.Compile(context.Background(), CompileSpec{Source: source, FunctionName: "Solve", InputType: "int", OutputType: "int"})
		if err == nil {
			t.Errorf("accepted forbidden source: %s", source)
		}
	}
	for _, typ := range []string{"map[string]int", "any", "int; func main(){}", "[][]int", "*int"} {
		_, err := r.Compile(context.Background(), CompileSpec{Source: "package main;func Solve(x int)int{return x}", FunctionName: "Solve", InputType: typ, OutputType: "int"})
		if err == nil {
			t.Errorf("accepted arbitrary type %q", typ)
		}
	}
}

func TestCompilerOfflineAndNoNativeExecution(t *testing.T) {
	r := newTestRunner(t)
	for key, value := range map[string]string{"GOFLAGS": "-toolexec=must-not-execute", "GOTOOLCHAIN": "go999.0.0", "GOPROXY": "https://127.0.0.1:1", "GOENV": "invalid", "CGO_ENABLED": "1"} {
		t.Setenv(key, value)
	}
	spec := CompileSpec{Source: "package main;func Solve(x int)int{for{}}", FunctionName: "Solve", InputType: "int", OutputType: "int"}
	module, err := r.Compile(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	if len(module.state.wasm) < 8 || string(module.state.wasm[:4]) != "\x00asm" {
		t.Fatal("compiler did not produce Wasm")
	}
}

func TestStrictJSONAndUnforgeableArtifact(t *testing.T) {
	r := newTestRunner(t)
	module, err := r.Compile(context.Background(), CompileSpec{Source: "package main;func Solve(x int)int{return x}", FunctionName: "Solve", InputType: "int", OutputType: "int"})
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	for _, input := range []string{`1 2`, `"PASS"`, `null`, `1.5`, `9223372036854775808`, strings.Repeat(" ", 32769)} {
		if _, err := r.RunCase(context.Background(), module, []byte(input)); err == nil {
			t.Errorf("accepted invalid input %q", input[:min(len(input), 40)])
		}
	}
	other := newTestRunner(t)
	for _, bad := range []ModuleArtifact{{}, module} {
		if _, err := other.RunCase(context.Background(), bad, []byte(`1`)); err == nil {
			t.Fatal("accepted forged/foreign artifact")
		}
	}
	for _, file := range []string{"extra-output.go.txt", "wrong-output.go.txt", "large-output.go.txt", "stderr-output.go.txt", "exit-output.go.txt"} {
		m := probe(t, r, file)
		if _, err := r.RunCase(context.Background(), m, []byte(`"x"`)); err == nil {
			t.Errorf("accepted %s", file)
		}
		m.Close()
	}
	module.Close()
	if _, err := r.RunCase(context.Background(), module, []byte(`1`)); err == nil {
		t.Fatal("released module accepted by open runner")
	}
}

func TestCancellationReleaseAndOwnedCleanup(t *testing.T) {
	parent := t.TempDir()
	binary := filepath.Join(runtime.GOROOT(), "bin", "go.exe")
	if runtime.GOOS != "windows" {
		binary = filepath.Join(runtime.GOROOT(), "bin", "go")
	}
	r, err := New(Config{GoBinary: binary, TempRoot: parent})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	marker := filepath.Join(parent, "keep")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	spec := CompileSpec{Source: "package main;func Solve(x int)int{for{}}", FunctionName: "Solve", InputType: "int", OutputType: "int"}
	module, err := r.Compile(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Compile(ctx, spec); !errors.Is(err, context.Canceled) {
		t.Fatalf("compile cancel: %v", err)
	}
	if _, err := r.RunCase(ctx, module, []byte(`1`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("run cancel: %v", err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	for range 2 {
		go func() {
			defer wg.Done()
			_, err := r.RunCase(context.Background(), module, []byte(`1`))
			if err == nil {
				t.Error("closed runner still executed")
			}
		}()
	}
	time.Sleep(100 * time.Millisecond)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("caller temp root destroyed: %v", err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 1 {
		t.Fatalf("owned temp leaked: %v %v", entries, err)
	}
	module.Close()
	if _, err := r.RunCase(context.Background(), module, []byte(`1`)); err == nil {
		t.Fatal("released module accepted")
	}
}

func TestCleanupRemainsAnchoredAfterWorkingDirectoryChange(t *testing.T) {
	base := t.TempDir()
	t.Chdir(base)
	if err := os.Mkdir("owned-parent", 0700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(runtime.GOROOT(), "bin", "go.exe")
	if runtime.GOOS != "windows" {
		binary = filepath.Join(runtime.GOROOT(), "bin", "go")
	}
	r, err := New(Config{GoBinary: binary, TempRoot: "owned-parent"})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ownedPath := filepath.Join(base, "owned-parent", filepath.Base(r.root))
	elsewhere := t.TempDir()
	innocentPath := filepath.Join(elsewhere, "owned-parent", filepath.Base(r.root))
	if err := os.MkdirAll(innocentPath, 0700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(elsewhere)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(innocentPath); err != nil {
		t.Fatalf("cleanup deleted unrelated directory after cwd change: %v", err)
	}
	if _, err := os.Stat(ownedPath); !os.IsNotExist(err) {
		t.Fatalf("owned directory leaked after cwd change: %v", err)
	}
}

func TestNoHostFilesystemOrNetwork(t *testing.T) {
	r := newTestRunner(t)
	t.Setenv("EVALRUNNER_SECRET", "credential-must-not-leak")
	sentinel := filepath.Join(t.TempDir(), "host-secret.txt")
	if err := os.WriteFile(sentinel, []byte("host-data"), 0600); err != nil {
		t.Fatal(err)
	}
	module := probe(t, r, "capabilities.go.txt")
	defer module.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	control, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	control.Close()
	accepted.Close()
	payload, _ := json.Marshal(sentinel + "\n" + listener.Addr().String())
	got, err := r.RunCase(context.Background(), module, payload)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Output) != `"no-fs,no-network,no-env"` {
		t.Fatalf("capability probe: %s", got.Output)
	}
	data, err := os.ReadFile(sentinel)
	if err != nil || string(data) != "host-data" {
		t.Fatalf("host file changed: %q %v", data, err)
	}
	listener.(*net.TCPListener).SetDeadline(time.Now().Add(50 * time.Millisecond))
	if connection, err := listener.Accept(); err == nil {
		connection.Close()
		t.Fatal("Wasm opened a host network connection")
	}
}
