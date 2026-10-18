package agent

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var testCommands = map[string]string{"go": "go test -timeout 60s ./...", "python": "python -m unittest discover -v", "node": "node --test"}

func testDirectory(root, name string) (string, error) {
	if root == "" || !fs.ValidPath(name) || strings.ContainsAny(name, "\\:") {
		return "", fmt.Errorf("无效测试目录")
	}
	handle, err := os.OpenRoot(root)
	if err != nil {
		return "", fmt.Errorf("项目目录不可用")
	}
	defer handle.Close()
	if name != "." {
		parts := strings.Split(name, "/")
		for i, part := range parts {
			if strings.HasPrefix(part, ".") || excluded[strings.ToLower(part)] {
				return "", fmt.Errorf("不允许此测试目录")
			}
			info, e := handle.Lstat(strings.Join(parts[:i+1], "/"))
			if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("测试目录不可用或为链接")
			}
		}
	}
	return filepath.Join(root, filepath.FromSlash(name)), nil
}

type NativeExecutor struct {
	Runtimes map[string]string
	Services bool
}

func NewNativeExecutor(pythonPath string, services bool) *NativeExecutor {
	e := &NativeExecutor{Runtimes: map[string]string{}, Services: services}
	for _, name := range []string{"go", "python", "node"} {
		if p, err := exec.LookPath(name); err == nil && filepath.IsAbs(p) {
			e.Runtimes[name] = p
		}
	}
	if filepath.IsAbs(pythonPath) {
		if info, err := os.Stat(pythonPath); err == nil && !info.IsDir() {
			e.Runtimes["python"] = pythonPath
		}
	}
	executable, _ := os.Executable()
	dir := filepath.Dir(executable)
	for i := 0; i < 5; i++ {
		for name, relative := range map[string]string{"go": ".local/tools/go/bin/go.exe", "python": ".local/venv/Scripts/python.exe"} {
			if e.Runtimes[name] == "" {
				p := filepath.Join(dir, relative)
				if info, err := os.Stat(p); err == nil && !info.IsDir() {
					e.Runtimes[name] = p
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return e
}

type boundedOutput struct{ buffer bytes.Buffer }

func (b *boundedOutput) Len() int       { return b.buffer.Len() }
func (b *boundedOutput) String() string { return b.buffer.String() }

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if left := 32*1024 - b.Len(); left > 0 {
		b.buffer.Write(p[:min(n, left)])
	}
	return n, nil
}
func executeCommand(ctx context.Context, dir, exe string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = dir
	cmd.WaitDelay = 2 * time.Second
	// Project test code is executable; do not inherit the desktop's local API credential.
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(key)
		if strings.Contains(upper, "TOKEN") || strings.Contains(upper, "API_KEY") || strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "SECRET") {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	var output boundedOutput
	cmd.Stdout = &output
	cmd.Stderr = &output
	cleanup, err := prepareActionProcess(cmd)
	if err != nil {
		return "", err
	}
	defer cleanup()
	if err = cmd.Start(); err != nil {
		return "", err
	}
	if err = containActionProcess(cmd); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return "", err
	}
	err = cmd.Wait()
	text := output.String()
	if output.Len() >= 32*1024 {
		text += "\n[output truncated at 32 KiB]"
	}
	return text, err
}
func (e *NativeExecutor) ServiceState(ctx context.Context, name string) (string, error) {
	if !e.Services || !serviceName.MatchString(name) {
		return "", fmt.Errorf("服务操作不可用")
	}
	sc := filepath.Join(os.Getenv("SystemRoot"), "System32", "sc.exe")
	output, err := executeCommand(ctx, "", sc, "query", name)
	if err != nil {
		return "", fmt.Errorf("无法查询服务状态：%w", err)
	}
	// sc uses localized labels but the state number is stable across Windows locales.
	re := regexp.MustCompile(`(?m):\s*([1-7])\s+(?:STOPPED|RUNNING|START_PENDING|STOP_PENDING|CONTINUE_PENDING|PAUSE_PENDING|PAUSED)\b`)
	match := re.FindStringSubmatch(output)
	if len(match) != 2 {
		return "", fmt.Errorf("无法识别服务状态")
	}
	switch match[1] {
	case "1":
		return "stopped", nil
	case "4":
		return "running", nil
	default:
		return "pending", nil
	}
}
func (e *NativeExecutor) Execute(ctx context.Context, a Action) (string, error) {
	if a.Kind == "test" {
		dir, err := testDirectory(a.Root, a.Path)
		if err != nil {
			return "", err
		}
		exe := e.Runtimes[a.Runner]
		if exe == "" {
			return "", fmt.Errorf("未找到 %s 运行环境，请安装或配置后重新提出测试", a.Runner)
		}
		args := map[string][]string{"go": {"test", "-timeout", "60s", "./..."}, "python": {"-m", "unittest", "discover", "-v"}, "node": {"--test"}}[a.Runner]
		if len(args) == 0 {
			return "", fmt.Errorf("未知测试运行器")
		}
		return executeCommand(ctx, dir, exe, args...)
	}
	if a.Kind != "service" || !e.Services || !serviceName.MatchString(a.Service) {
		return "", fmt.Errorf("操作不可用")
	}
	verb := map[string]string{"running": "start", "stopped": "stop"}[a.Target]
	if verb == "" {
		return "", fmt.Errorf("无效服务目标")
	}
	output, err := executeCommand(ctx, "", filepath.Join(os.Getenv("SystemRoot"), "System32", "sc.exe"), verb, a.Service)
	if err != nil {
		return output, fmt.Errorf("服务操作失败（可能需要权限），不会自动提权：%w", err)
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for attempts := 0; attempts < 80; attempts++ {
		state, err := e.ServiceState(ctx, a.Service)
		if err != nil {
			return output, err
		}
		if state == a.Target {
			return output + "\nVerified service state: " + state, nil
		}
		select {
		case <-ctx.Done():
			return output, ctx.Err()
		case <-ticker.C:
		}
	}
	return output, fmt.Errorf("服务在 20 秒内未达到目标状态")
}
