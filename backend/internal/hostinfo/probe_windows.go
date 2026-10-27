//go:build windows

package hostinfo

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

//go:embed inventory_windows.ps1
var inventoryScript string

type boundedOutput struct{ data []byte }

func (b *boundedOutput) Write(p []byte) (int, error) {
	if len(p) > maxOutputBytes-len(b.data) {
		return 0, fmt.Errorf("inventory response too large")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}
func platformProbe(ctx context.Context) ([]byte, error) {
	dir, err := windows.GetSystemDirectory()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "WindowsPowerShell", "v1.0", "powershell.exe")
	cmd := exec.CommandContext(ctx, path, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", inventoryScript)
	// Do not inherit credentials, user module search paths, or profile settings.
	windowsDir := filepath.Dir(dir)
	cmd.Env = []string{"SystemRoot=" + windowsDir, "WINDIR=" + windowsDir, "SystemDrive=" + filepath.VolumeName(dir), "PATH=" + dir, "TEMP=" + os.TempDir(), "TMP=" + os.TempDir(), "PSModulePath=" + filepath.Join(dir, "WindowsPowerShell", "v1.0", "Modules")}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.WaitDelay = time.Second
	var output boundedOutput
	cmd.Stdout = &output
	// Stderr is intentionally discarded: CIM errors can contain private paths.
	if err = cmd.Run(); err != nil {
		return nil, err
	}
	return output.data, nil
}
