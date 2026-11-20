//go:build linux || darwin

package agent

import (
	"fmt"
	"os"
	"syscall"
)

const fileReadFlags = syscall.O_NONBLOCK

func directoryIdentity(file *os.File) (string, error) {
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("无法识别项目目录")
	}
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino), nil
}
