//go:build !windows && !linux && !darwin

package agent

import (
	"fmt"
	"os"
)

const fileReadFlags = 0

func directoryIdentity(*os.File) (string, error) {
	return "", fmt.Errorf("此平台不支持校验项目目录身份")
}
