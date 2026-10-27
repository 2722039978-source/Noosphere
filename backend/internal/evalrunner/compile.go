package evalrunner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (r *Runner) Compile(ctx context.Context, spec CompileSpec) (ModuleArtifact, error) {
	if err := ctx.Err(); err != nil {
		return ModuleArtifact{}, err
	}
	if err := validateSource(spec); err != nil {
		return ModuleArtifact{}, failure("INVALID_OUTPUT", err)
	}
	// A separate trusted wrapper file prevents model comments from swallowing it.
	// Only finite validated type/name choices are interpolated.
	wrapper := fmt.Sprintf(`package main
import ("encoding/json"; "os"; "io")
func main() {
 var input %s
 decoder := json.NewDecoder(os.Stdin)
 if err := decoder.Decode(&input); err != nil { os.Exit(2) }
 if err := decoder.Decode(new(any)); err != io.EOF { os.Exit(2) }
 output := %s(input)
 if err := json.NewEncoder(os.Stdout).Encode(output); err != nil { os.Exit(3) }
}
`, spec.InputType, spec.FunctionName)
	wasm, err := r.compileFiles(ctx, spec.Source, wrapper)
	if err != nil {
		return ModuleArtifact{}, err
	}
	module := r.seal(wasm, spec.OutputType)
	module.state.inputType = spec.InputType
	return module, nil
}

func (r *Runner) compileFiles(parent context.Context, source, wrapper string) ([]byte, error) {
	ctx, done, err := r.admit(parent, r.compileGate, CompileTimeout)
	if err != nil {
		return nil, err
	}
	defer done()
	dir, err := os.MkdirTemp(r.root, "compile-")
	if err != nil {
		return nil, failure("RUNNER_INFRASTRUCTURE", err)
	}
	defer os.RemoveAll(dir)
	if err = os.WriteFile(filepath.Join(dir, "solution.go"), []byte(source), 0600); err != nil {
		return nil, err
	}
	args := []string{"build", "-p=1", "-trimpath", "-buildvcs=false", "-o", filepath.Join(dir, "module.wasm"), "solution.go"}
	if wrapper != "" {
		if err = os.WriteFile(filepath.Join(dir, "wrapper.go"), []byte(wrapper), 0600); err != nil {
			return nil, err
		}
		args = append(args, "wrapper.go")
	}
	cmd := exec.CommandContext(ctx, r.binary, args...)
	cmd.Dir = dir
	cmd.Env = r.compilerEnv()
	var output boundedBuffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	cmd.WaitDelay = 2 * CaseTimeout
	process, err := prepareCompiler(cmd)
	if err != nil {
		return nil, failure("RUNNER_INFRASTRUCTURE", err)
	}
	defer process.close()
	if err = cmd.Start(); err != nil {
		return nil, failure("RUNNER_INFRASTRUCTURE", err)
	}
	if err = process.contain(cmd); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return nil, failure("RUNNER_INFRASTRUCTURE", err)
	}
	err = cmd.Wait()
	if ctx.Err() != nil {
		return nil, failure("RUNNER_TIMEOUT", ctx.Err())
	}
	if err != nil {
		return nil, failure("COMPILE_ERROR", fmt.Errorf("%w: %s", err, output.String()))
	}
	if output.overflow {
		return nil, failure("BUDGET_EXCEEDED", fmt.Errorf("compiler output exceeded limit"))
	}
	path := filepath.Join(dir, "module.wasm")
	info, err := os.Stat(path)
	if err != nil {
		return nil, failure("RUNNER_INFRASTRUCTURE", err)
	}
	if info.Size() > MaxModuleBytes {
		return nil, failure("BUDGET_EXCEEDED", fmt.Errorf("module exceeds %d bytes", MaxModuleBytes))
	}
	wasm, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(wasm) < 8 || string(wasm[:8]) != "\x00asm\x01\x00\x00\x00" {
		return nil, failure("SANDBOX_VIOLATION", fmt.Errorf("compiler output is not Wasm v1"))
	}
	return wasm, nil
}

func (r *Runner) compilerEnv() []string {
	// Never append os.Environ: flags, proxies, credentials, HOME, Go env files and
	// shared caches cannot influence this source-only offline compiler.
	env := []string{
		"GOOS=wasip1", "GOARCH=wasm", "GOTOOLCHAIN=local", "GOWORK=off", "GOENV=off",
		"CGO_ENABLED=0", "GOPROXY=off", "GOSUMDB=off", "GO111MODULE=off", "GOFLAGS=",
		"GOMAXPROCS=2", "GOMEMLIMIT=1536MiB", "GOTELEMETRY=off",
		"GOCACHE=" + filepath.Join(r.root, "cache"), "GOMODCACHE=" + filepath.Join(r.root, "modcache"),
		"TMP=" + filepath.Join(r.root, "tmp"), "TEMP=" + filepath.Join(r.root, "tmp"), "TMPDIR=" + filepath.Join(r.root, "tmp"),
		"HOME=" + filepath.Join(r.root, "home"), "USERPROFILE=" + filepath.Join(r.root, "home"),
		"PATH=" + filepath.Dir(r.binary),
	}
	// The Windows loader needs its system directory; no other caller environment
	// is inherited. This path is system configuration, not an application secret.
	if root := os.Getenv("SystemRoot"); root != "" {
		env = append(env, "SystemRoot="+root, "WINDIR="+root)
	}
	return env
}

// boundedBuffer retains a bounded prefix and drains the rest without blocking
// child exit/cancellation. The compiler shares this writer for both streams.
type boundedBuffer struct {
	data     []byte
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	room := MaxIOBytes - len(b.data)
	if n > room {
		b.overflow = true
		p = p[:room]
	}
	b.data = append(b.data, p...)
	return n, nil
}
func (b *boundedBuffer) String() string { return strings.TrimSpace(string(b.data)) }
