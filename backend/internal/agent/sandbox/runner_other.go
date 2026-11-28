//go:build !windows && !linux

package sandbox

import (
	"context"
	"errors"
	"io"
)

func launch(context.Context, string, string, string, []string, io.Writer, Limits) (int, error) {
	_, err := resolveProgram("", "")
	return -1, err
}

func resolveProgram(string, string) (string, error) {
	return "", errors.New("本平台尚未启用通用隔离命令")
}
