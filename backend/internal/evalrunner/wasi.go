package evalrunner

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

func (r *Runner) RunCase(parent context.Context, module ModuleArtifact, input []byte) (result ExecutionResult, err error) {
	start := time.Now()
	defer func() { result.TotalDuration = time.Since(start); result.Duration = result.TotalDuration }()
	if module.state == nil {
		return result, failure("ARTIFACT_MISSING", fmt.Errorf("empty module artifact"))
	}
	module.state.mu.RLock()
	if module.state.owner != r || module.state.closed {
		module.state.mu.RUnlock()
		return result, failure("ARTIFACT_MISSING", fmt.Errorf("foreign or released module artifact"))
	}
	wasm, inType, outType := module.state.wasm, module.state.inputType, module.state.outputType
	module.state.mu.RUnlock()
	// Admission and preparation inherit caller/Runner.Close cancellation, but the
	// two-second instance budget begins only immediately before instantiation.
	ctx, done, err := r.admit(parent, r.runGate, 0)
	if err != nil {
		return result, err
	}
	defer done()
	prepareStart := time.Now()
	defer func() {
		if result.PreparationDuration == 0 {
			result.PreparationDuration = time.Since(prepareStart)
		}
	}()
	canonical, err := decodeJSON(input, inType)
	if err != nil {
		return result, failure("INVALID_OUTPUT", err)
	}
	if err = r.caseContextError(ctx); err != nil {
		return result, failure("RUNNER_TIMEOUT", err)
	}
	// Interpreter avoids native AOT preparation. Decode/Validate and interpreter
	// preparation still run synchronously: cancellation is checked before/after,
	// and Close waits for completion rather than leaving background work alive.
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigInterpreter().WithMemoryLimitPages(MemoryLimitPages).WithCloseOnContextDone(true))
	defer runtime.Close(context.Background())
	if _, err = wasi_snapshot_preview1.Instantiate(ctx, runtime); err != nil {
		return result, failure("RUNNER_INFRASTRUCTURE", err)
	}
	if err = r.caseContextError(ctx); err != nil {
		return result, failure("RUNNER_TIMEOUT", err)
	}
	compiled, err := runtime.CompileModule(ctx, wasm)
	if compiled != nil {
		defer compiled.Close(context.Background())
	}
	if contextErr := r.caseContextError(ctx); contextErr != nil {
		return result, failure("RUNNER_TIMEOUT", contextErr)
	}
	if err != nil {
		return result, failure("SANDBOX_VIOLATION", err)
	}
	for _, imported := range compiled.ImportedFunctions() {
		name, _, _ := imported.Import()
		if name != wasi_snapshot_preview1.ModuleName {
			return result, failure("SANDBOX_VIOLATION", fmt.Errorf("forbidden host import %s", name))
		}
	}
	var stdout, stderr boundedBuffer
	// No FS/preopens, environment, args, sockets, or custom host modules. Wazero
	// defaults grant no inherited host directory, network or environment access.
	config := wazero.NewModuleConfig().WithName("").WithStdin(bytes.NewReader(canonical)).WithStdout(&stdout).WithStderr(&stderr)
	if err = r.caseContextError(ctx); err != nil {
		return result, failure("RUNNER_TIMEOUT", err)
	}
	result.PreparationDuration = time.Since(prepareStart)
	executionCtx, cancelExecution := context.WithTimeout(ctx, CaseTimeout)
	defer cancelExecution()
	if err = r.caseContextError(executionCtx); err != nil {
		return result, failure("RUNNER_TIMEOUT", err)
	}
	executionStart := time.Now()
	instance, runErr := runtime.InstantiateModule(executionCtx, compiled, config)
	result.ExecutionDuration = time.Since(executionStart)
	if instance != nil {
		instance.Close(context.Background())
	}
	result.Stderr = stderr.String()
	if err = r.caseContextError(executionCtx); err != nil {
		return result, failure("RUNNER_TIMEOUT", err)
	}
	if runErr != nil {
		if exit, ok := runErr.(*sys.ExitError); ok && exit.ExitCode() == 2 && guestOOM.MatchString(string(stderr.data)) {
			return result, failure("OUT_OF_MEMORY", runErr)
		}
		if exit, ok := runErr.(*sys.ExitError); !ok || exit.ExitCode() != 0 {
			return result, failure("SANDBOX_VIOLATION", runErr)
		}
	}
	if stdout.overflow || stderr.overflow {
		return result, failure("BUDGET_EXCEEDED", fmt.Errorf("stdout/stderr exceeded %d bytes", MaxIOBytes))
	}
	result.Output, err = decodeJSON(stdout.data, outType)
	if err != nil {
		return result, failure("INVALID_OUTPUT", err)
	}
	return result, nil
}

func (r *Runner) caseContextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Observe Close synchronously even if the AfterFunc linking r.ctx to the
	// preparation context has not yet been scheduled.
	return r.ctx.Err()
}

// Go 1.26.5's fatal allocator failure is distinguishable from a model panic:
// panic output always begins with "panic:". Match only the runtime's first
// line(s), never arbitrary occurrences inside model-controlled panic text.
var guestOOM = regexp.MustCompile(`^(?:runtime: out of memory: cannot allocate [0-9]+-byte block \([0-9]+ in use\)\n)?fatal error: out of memory\n`)
