// Package evalrunner compiles constrained pure Go functions and executes them in
// WASI. It has no workspace actions, providers, or application credentials.
package evalrunner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	MaxSourceBytes   = 64 << 10
	MaxIOBytes       = 32 << 10
	MaxModuleBytes   = 32 << 20
	MemoryLimitPages = 4096
	CompileTimeout   = 60 * time.Second
	CaseTimeout      = 2 * time.Second
)

// Config is trusted application configuration, never model-provided input.
// GoBinary names the installed, trusted Go toolchain by absolute path.
type Config struct{ GoBinary, TempRoot string }

// CompileSpec accepts no wrapper text, filenames, compiler flags, or manifests.
type CompileSpec struct{ Source, FunctionName, InputType, OutputType string }

// ModuleArtifact is an opaque Wasm capability owned by one Runner. Its zero value
// and artifacts from another Runner cannot be executed.
type ModuleArtifact struct{ state *moduleState }
type moduleState struct {
	mu                    sync.RWMutex
	owner                 *Runner
	wasm                  []byte
	inputType, outputType string
	closed                bool
}

// Close releases the artifact and disallows subsequent runs. An admitted RunCase
// retains its own reference until execution ends.
func (m ModuleArtifact) Close() error {
	if m.state != nil {
		m.state.mu.Lock()
		m.state.closed = true
		m.state.wasm = nil
		m.state.mu.Unlock()
	}
	return nil
}

// ExecutionResult is strictly type-validated JSON, not a score. The caller must
// compare Output with trusted expected results.
type ExecutionResult struct {
	Output json.RawMessage
	Stderr string
	// Duration aliases TotalDuration for existing callers. Total includes queue,
	// preparation, execution, host output validation and synchronous cleanup.
	Duration      time.Duration
	TotalDuration time.Duration
	// PreparationDuration covers host input validation and synchronous Wasm
	// decode/validation/interpreter preparation after acquiring the case gate.
	// It has no independent two-second deadline and cannot be hard interrupted.
	PreparationDuration time.Duration
	// ExecutionDuration covers InstantiateModule, including guest initialization
	// and start-function execution, under CaseTimeout (or earlier cancellation).
	ExecutionDuration time.Duration
}
type Error struct {
	Code string
	Err  error
}

func (e *Error) Error() string             { return e.Code + ": " + e.Err.Error() }
func (e *Error) Unwrap() error             { return e.Err }
func failure(code string, err error) error { return &Error{Code: code, Err: err} }

// Runner admits one compiler and one Wasm case concurrently. Close cancels work,
// waits for release, and deletes only the child directory created by New.
type Runner struct {
	binary, root         string
	ctx                  context.Context
	cancel               context.CancelFunc
	compileGate, runGate chan struct{}
	mu                   sync.Mutex
	closed               bool
	work                 sync.WaitGroup
	closeOnce            sync.Once
	closeErr             error
}

func New(config Config) (*Runner, error) {
	if !filepath.IsAbs(config.GoBinary) {
		return nil, fmt.Errorf("GoBinary must be an absolute trusted toolchain path")
	}
	info, err := os.Stat(config.GoBinary)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("GoBinary must be a regular file: %v", err)
	}
	parent := config.TempRoot
	if parent == "" {
		parent = os.TempDir()
	}
	parent, err = filepath.Abs(parent)
	if err != nil {
		return nil, err
	}
	root, err := os.MkdirTemp(parent, "evalrunner-")
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"cache", "modcache", "tmp", "home"} {
		if err = os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			os.RemoveAll(root)
			return nil, err
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Runner{binary: config.GoBinary, root: root, ctx: ctx, cancel: cancel, compileGate: make(chan struct{}, 1), runGate: make(chan struct{}, 1)}, nil
}
func (r *Runner) Close() error {
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closed = true
		r.cancel()
		r.mu.Unlock()
		r.work.Wait()
		r.closeErr = os.RemoveAll(r.root)
	})
	return r.closeErr
}
func (r *Runner) admit(parent context.Context, gate chan struct{}, timeout time.Duration) (context.Context, func(), error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, nil, errors.New("runner closed")
	}
	r.work.Add(1)
	r.mu.Unlock()
	var ctx context.Context
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(parent, timeout)
	} else {
		ctx, cancel = context.WithCancel(parent)
	}
	stop := context.AfterFunc(r.ctx, cancel)
	finish := func() { stop(); cancel(); r.work.Done() }
	if err := ctx.Err(); err != nil {
		finish()
		return nil, nil, err
	}
	select {
	case gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-gate
			finish()
			return nil, nil, err
		}
		return ctx, func() { <-gate; finish() }, nil
	case <-ctx.Done():
		finish()
		return nil, nil, ctx.Err()
	}
}
func (r *Runner) seal(wasm []byte, outputType string) ModuleArtifact {
	return ModuleArtifact{state: &moduleState{owner: r, wasm: wasm, outputType: outputType, inputType: "string"}}
}
