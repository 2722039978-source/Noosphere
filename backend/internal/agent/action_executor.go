package agent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"time"

	"github.com/noosphere/backend/internal/ownedprocess"
)

type NativeExecutor struct {
	Services bool
}

func NewNativeExecutor(services bool) *NativeExecutor {
	return &NativeExecutor{Services: services && runtime.GOOS == "windows"}
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
func executeCommand(ctx context.Context, cmd *exec.Cmd, directory *os.File) (string, error) {
	var output boundedOutput
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := ownedprocess.Run(ctx, cmd, directory)
	text := output.String()
	if output.Len() >= 32*1024 {
		text += "\n[output truncated at 32 KiB]"
	}
	return text, err
}

func serviceCommand(ctx context.Context, verb, name string) (string, error) {
	systemRoot := os.Getenv("SystemRoot")
	if !filepath.IsAbs(systemRoot) {
		return "", fmt.Errorf("Windows 系统目录不可用")
	}
	cmd := exec.Command(filepath.Join(systemRoot, "System32", "sc.exe"), verb, name)
	cmd.Env = ownedprocess.BaseEnv()
	return executeCommand(ctx, cmd, nil)
}

func (e *NativeExecutor) ServiceState(ctx context.Context, name string) (string, error) {
	if !e.Services {
		return "", fmt.Errorf("此平台或配置未启用系统服务写操作")
	}
	if !serviceName.MatchString(name) {
		return "", fmt.Errorf("无效服务名")
	}
	output, err := serviceCommand(ctx, "query", name)
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
		return executeTest(ctx, a)
	}
	if a.Kind != "service" || !e.Services || !serviceName.MatchString(a.Service) {
		return "", fmt.Errorf("操作不可用")
	}
	verb := map[string]string{"running": "start", "stopped": "stop"}[a.Target]
	if verb == "" {
		return "", fmt.Errorf("无效服务目标")
	}
	output, err := serviceCommand(ctx, verb, a.Service)
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
