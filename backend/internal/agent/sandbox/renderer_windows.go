package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/noosphere/backend/internal/agent/renderpack"
	"golang.org/x/sys/windows"
)

const browserRenderer = "noosphere-render-browser"

func stageHTMLRenderer(ctx context.Context, exe, private string) (string, []string, error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	in, err := os.Open(exe)
	if err != nil {
		return "", nil, err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return "", nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 32<<20 {
		return "", nil, fmt.Errorf("静态网页排版器文件无效")
	}
	dir := filepath.Join(private, "renderer")
	if err = os.Mkdir(dir, 0700); err != nil {
		return "", nil, err
	}
	target := filepath.Join(dir, filepath.Base(exe))
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", nil, err
	}
	hash := sha256.New()
	_, err = io.Copy(io.MultiWriter(out, hash), io.LimitReader(in, (32<<20)+1))
	closeErr := out.Close()
	if err != nil {
		return "", nil, err
	}
	if closeErr != nil {
		return "", nil, closeErr
	}
	if hex.EncodeToString(hash.Sum(nil)) != renderpack.HTMLHash() {
		return "", nil, fmt.Errorf("静态网页排版器哈希冲突，请重新安装")
	}
	return target, nil, nil
}

// RenderStaticHTML receives only the disposable input copy and fixed arguments.
func RenderStaticHTML(ctx context.Context, work, ext, dataDir string) error {
	exe, err := renderpack.Locate(dataDir, "html")
	if err != nil {
		return err
	}
	return renderWithProgram(ctx, work, ext, exe)
}

func renderWithProgram(ctx context.Context, work, ext, exe string) error {
	if ext != ".html" && ext != ".htm" {
		return fmt.Errorf("不支持的静态网页格式")
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	// The renderer treats file://C:/ as a remote host. An absolute path on
	// its current drive has an empty authority.
	local := filepath.Join(work, "input"+ext)
	in := (&url.URL{Scheme: "file", Path: filepath.ToSlash(strings.TrimPrefix(local, filepath.VolumeName(local)))}).String()
	args := []string{"--allow", work, "--width", "1280", "--height", "900", "--format", "png", "-o", "page.png", in}
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return err
	}
	var out limitedOutput
	code, err := launchResolved(ctx, work, ".", browserRenderer, exe, system, args, capture(&out), Limits{})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("受控渲染器退出 0x%x：%.380s", uint32(code), out.String())
	}
	return nil
}
