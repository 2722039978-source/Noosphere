//go:build !linux && !windows

package render

import (
	"context"
	"fmt"
	"io"
)

func hostPreview(_ context.Context, _ io.Reader, ext, _ string) (Preview, error) {
	return Preview{}, fmt.Errorf("当前平台未配置 %s 的隔离排版渲染器", ext)
}
