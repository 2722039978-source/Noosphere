// Package sandbox launches project processes in an OS-enforced private filesystem and network context.
package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Result struct {
	LogID        string
	LogBytes     int64
	LogTruncated bool
	Output       string
	ExitCode     int
	Truncated    bool
}
type limitedOutput struct {
	bytes.Buffer
	clipped bool
}

func (o *limitedOutput) Write(p []byte) (int, error) {
	n := len(p)
	left := maxLogBytes - o.Len()
	if n > left {
		o.clipped = true
		p = p[:left]
	}
	_, e := o.Buffer.Write(p)
	return n, e
}
func capture(out *limitedOutput) io.Writer { return struct{ io.Writer }{out} }

// CheckCommand validates a model request without starting a process or modifying the draft.
func CheckCommand(work, cwd, program string, args []string, projectRoot string) error {
	if err := validateRequest(work, cwd, program, args); err != nil {
		return err
	}
	_, err := resolveProgram(program, projectRoot)
	return err
}

func validateRequest(work, cwd, program string, args []string) error {
	if strings.HasPrefix(program, "noosphere-render-") {
		return errors.New("此入口只供宿主生成预览")
	}
	if cwd == "" {
		cwd = "."
	}
	if !fs.ValidPath(cwd) || strings.ContainsAny(cwd, "\\:\x00") || !filepath.IsAbs(work) {
		return errors.New("命令目录必须在工作副本内")
	}
	base := filepath.Join(work, filepath.FromSlash(cwd))
	resolved, e := filepath.EvalSymlinks(base)
	if e != nil || filepath.Clean(resolved) != filepath.Clean(base) {
		return errors.New("命令目录已改变或包含链接")
	}
	info, e := os.Stat(resolved)
	if e != nil || !info.IsDir() {
		return errors.New("命令目录不存在")
	}
	if len(args) > 128 {
		return errors.New("命令参数过多")
	}
	for _, arg := range args {
		if strings.ContainsRune(arg, 0) || len(arg) > 32<<10 {
			return errors.New("无效命令参数")
		}
	}
	return nil
}

func Run(ctx context.Context, work, cwd, program string, args []string, limits ...Limits) (Result, error) {
	if cwd == "" {
		cwd = "."
	}
	if err := validateRequest(work, cwd, program, args); err != nil {
		return Result{}, err
	}
	l := Limits{Timeout: 2 * time.Minute, DiskBytes: 2 << 30}
	if len(limits) > 0 {
		l.ProjectRoot = limits[0].ProjectRoot
		l.CacheDir = limits[0].CacheDir
		if limits[0].Timeout > 0 {
			l.Timeout = limits[0].Timeout
		}
		if limits[0].DiskBytes > 0 {
			l.DiskBytes = limits[0].DiskBytes
		}
	}
	ctx, deadlineCancel := context.WithTimeout(ctx, l.Timeout)
	defer deadlineCancel()
	ctx, cancel := context.WithCancelCause(ctx)
	if err := scopedDiskUsage(ctx, work, l.DiskBytes); err != nil {
		cancel(err)
		return Result{}, err
	}
	stopped := monitorDisk(ctx, cancel, work, l.DiskBytes)
	defer func() { cancel(nil); <-stopped }()
	var output limitedOutput
	code, e := launch(ctx, work, cwd, program, args, capture(&output), l)
	result, logErr := persistLog(work, &output)
	result.ExitCode = code
	if logErr != nil {
		return result, logErr
	}
	if ctx.Err() != nil {
		return result, context.Cause(ctx)
	}
	if e != nil {
		return result, fmt.Errorf("隔离执行：%w", e)
	}
	if err := scopedDiskUsage(ctx, work, l.DiskBytes); err != nil {
		return result, err
	}
	return result, nil
}
