//go:build linux

package render

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/noosphere/backend/internal/agent/sandbox"
	"github.com/noosphere/backend/internal/ownedprocess"
)

// Native layout engines see the one document and an empty output directory.
// The private namespace excludes network interfaces, user homes and the project.
func hostPreview(ctx context.Context, source io.Reader, ext, _ string) (Preview, error) {
	var program string
	var args []string
	var output string
	switch ext {
	case ".docx", ".pptx":
		program = "/usr/bin/libreoffice"
		output = "input.pdf"
		args = []string{"-env:UserInstallation=file:///tmp/home/office", "--headless", "--norestore", "--convert-to", "pdf", "--outdir", "/out", "/in/input" + ext}
	case ".html", ".htm":
		program = "/usr/bin/chromium"
		if st, e := os.Stat("/opt/google/chrome/chrome"); e == nil && st.Mode().IsRegular() {
			program = "/opt/google/chrome/chrome"
		}
		output = "page.png"
		args = []string{"--headless", "--disable-gpu", "--disable-extensions", "--disable-background-networking", "--disable-breakpad", "--blink-settings=scriptEnabled=false", "--no-first-run", "--user-data-dir=/tmp/home/chromium", "--window-size=1280,900", "--screenshot=/out/page.png", "file:///in/input" + ext}
	default:
		return Preview{}, fmt.Errorf("不支持的排版文件格式")
	}
	for _, exe := range []string{"/usr/bin/bwrap", "/usr/bin/prlimit", program} {
		if st, e := os.Stat(exe); e != nil || !st.Mode().IsRegular() {
			return Preview{}, fmt.Errorf("未安装隔离渲染组件 %s", filepath.Base(exe))
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	dir, e := os.MkdirTemp("", "noosphere-preview-")
	if e != nil {
		return Preview{}, e
	}
	defer os.RemoveAll(dir)
	input := filepath.Join(dir, "input"+ext)
	f, e := os.OpenFile(input, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return Preview{}, e
	}
	_, e = io.Copy(f, io.LimitReader(source, maxDocument+1))
	closeErr := f.Close()
	if e != nil {
		return Preview{}, e
	}
	if closeErr != nil {
		return Preview{}, closeErr
	}
	scratch := filepath.Join(dir, "scratch")
	tmpDir, outDir := filepath.Join(scratch, "tmp"), filepath.Join(scratch, "out")
	localMask := filepath.Join(dir, "local")
	for _, path := range []string{tmpDir, outDir, localMask} {
		if e := os.MkdirAll(path, 0700); e != nil {
			return Preview{}, e
		}
	}
	opts := []string{"--unshare-all", "--die-with-parent", "--new-session", "--clearenv", "--cap-drop", "ALL", "--ro-bind", "/usr", "/usr", "--ro-bind", localMask, "/usr/local"}
	for _, p := range []string{"/bin", "/lib", "/lib64", "/etc/fonts"} {
		if _, err := os.Stat(p); err == nil {
			opts = append(opts, "--ro-bind", p, p)
		}
	}
	if program == "/opt/google/chrome/chrome" {
		opts = append(opts, "--dir", "/opt", "--dir", "/opt/google", "--ro-bind", "/opt/google/chrome", "/opt/google/chrome")
	}
	opts = append(opts, "--proc", "/proc", "--dev", "/dev")
	opts = append(opts, sandbox.BubblewrapScratch(tmpDir, "/tmp", 128<<20)...)
	opts = append(opts, sandbox.BubblewrapScratch(outDir, "/out", maxDocument)...)
	opts = append(opts, "--dir", "/tmp/home", "--dir", "/in", "--ro-bind", input, "/in/input"+ext, "--chdir", "/in", "--setenv", "HOME", "/tmp/home", "--setenv", "LANG", "C.UTF-8", "--setenv", "PATH", "/usr/bin:/bin", "--", "/usr/bin/prlimit", "--cpu=25", "--fsize=33554432", "--nofile=256", "--", "/bin/sh", "-c", `set -e; out="$1"; shift; "$@" >/dev/null; test -f "$out"; test ! -L "$out"; exec /bin/cat "$out"`, "render", "/out/"+output, program)
	cmd := exec.Command("/usr/bin/bwrap", append(opts, args...)...)
	cmd.Env = []string{"LANG=C.UTF-8"}
	var stdout renderOutput
	cmd.Stdout = &stdout
	e = sandbox.WithDirectoryBudget(ctx, scratch, 160<<20, func(ctx context.Context) error {
		return ownedprocess.Run(ctx, cmd, nil)
	})
	if e != nil {
		return Preview{}, fmt.Errorf("隔离排版失败：%w", e)
	}
	generated := bytes.NewReader(stdout.data.Bytes())
	if output == "input.pdf" {
		return officePreview(ctx, generated)
	}
	raw, e := io.ReadAll(io.LimitReader(generated, (8<<20)+1))
	if e != nil {
		return Preview{}, e
	}
	if len(raw) > 8<<20 {
		return Preview{}, fmt.Errorf("HTML 预览图片过大")
	}
	png, e := normalizeImage(raw)
	return rasterPreview(png, "chromium", "Chromium 已离线渲染 1280 × 900 首屏，JavaScript 已关闭", e)
}

// Use a named field so os/exec cannot bypass the limit via bytes.Buffer.ReadFrom.
type renderOutput struct{ data bytes.Buffer }

func (w *renderOutput) Write(p []byte) (int, error) {
	if len(p) > maxDocument-w.data.Len() {
		return 0, fmt.Errorf("渲染器输出过大")
	}
	return w.data.Write(p)
}
