// Package renderpack installs pinned optional layout engines independently of the base application.
package renderpack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Info struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Size        int64  `json:"size_bytes"`
	Available   bool   `json:"available"`
	Installed   bool   `json:"installed"`
	Description string `json:"description"`
}
type specification struct {
	Info
	URL, Hash, Executable string
}

func spec(kind string) (specification, bool) {
	switch kind {
	case "html":
		s := specification{Info: Info{Kind: kind, Name: "静态网页排版器", Version: "0.2.6", Description: "在独立沙箱中渲染 HTML 与受支持的静态 CSS，生成首屏图片。"}, Executable: "gowkhtmltoimage.exe"}
		switch runtime.GOARCH {
		case "amd64":
			s.Size = 17293312
			s.Hash = "a83f732f3ace27babed04a4b53b6c7b4dd38598a1603e6d51d21bafa0b4c9e59"
		case "arm64":
			s.Size = 15991808
			s.Hash = "f4ab47dfaa85f4ac0f3304680e8791803b362f06d9ec242ad5c8e6187fe5fd2b"
		}
		s.Available = runtime.GOOS == "windows" && s.Hash != ""
		s.URL = fmt.Sprintf("https://github.com/chinmay-sawant/gowkhtmltopdf/releases/download/v%s/gowkhtmltoimage_%s_windows_%s.exe", s.Version, s.Version, runtime.GOARCH)
		return s, true
	}
	return specification{}, false
}
func installDir(dataDir string, s specification) string {
	return filepath.Join(dataDir, "agent-renderers", s.Kind+"-"+s.Version+"-"+runtime.GOARCH)
}

func Status(dataDir string) []Info {
	var result []Info
	s, _ := spec("html")
	if s.Available {
		_, err := Locate(dataDir, "html")
		s.Installed = err == nil
		result = append(result, s.Info)
	}
	office := Info{Kind: "office", Name: "Office 预览", Description: "Office 文件可预览其自带缩略图。"}
	if runtime.GOOS == "linux" {
		office.Description = "Office 页面可通过系统安装的 LibreOffice 和 bubblewrap 隔离排版。"
	}
	result = append(result, office)
	return result
}

// All inputs are fixed by release metadata. Only the single output path is returned to the caller.
func Locate(dataDir, kind string) (string, error) {
	s, ok := spec(kind)
	if !ok || !s.Available || dataDir == "" {
		return "", errors.New("此平台没有对应的受管排版器")
	}
	path := filepath.Join(installDir(dataDir, s), filepath.FromSlash(s.Executable))
	resolved, e := filepath.EvalSymlinks(path)
	if e != nil || !strings.EqualFold(filepath.Clean(resolved), filepath.Clean(path)) {
		return "", fmt.Errorf("请在设置中安装%s", s.Name)
	}
	st, e := os.Stat(resolved)
	if e != nil || !st.Mode().IsRegular() {
		return "", fmt.Errorf("排版器安装无效")
	}
	return resolved, nil
}
func HTMLHash() string { s, _ := spec("html"); return s.Hash }

var installLock sync.Mutex

func Install(ctx context.Context, dataDir, kind string) error {
	s, ok := spec(kind)
	if !ok || !s.Available || !filepath.IsAbs(dataDir) {
		return fmt.Errorf("当前平台不支持此排版器")
	}
	if !installLock.TryLock() {
		return fmt.Errorf("已有排版器正在安装")
	}
	defer installLock.Unlock()
	if _, e := Locate(dataDir, kind); e == nil {
		return nil
	}
	parent := filepath.Join(dataDir, "agent-renderers")
	if e := os.MkdirAll(parent, 0700); e != nil {
		return e
	}
	resolved, e := filepath.EvalSymlinks(parent)
	if e != nil || !strings.EqualFold(resolved, parent) {
		return fmt.Errorf("渲染器目录已改变")
	}
	stage, e := os.MkdirTemp(parent, ".install-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	bundle := filepath.Join(stage, "bundle")
	f, e := os.OpenFile(bundle, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	e = download(ctx, s, f)
	closed := f.Close()
	if e != nil {
		return e
	}
	if closed != nil {
		return closed
	}
	tree := filepath.Join(stage, "tree")
	if e = os.Mkdir(tree, 0700); e != nil {
		return e
	}
	if e = os.Rename(bundle, filepath.Join(tree, s.Executable)); e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	return os.Rename(tree, installDir(dataDir, s))
}

func download(ctx context.Context, s specification, out *os.File) error {
	client := http.Client{Timeout: 10 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 8 || req.URL.Scheme != "https" || req.URL.User != nil {
			return fmt.Errorf("排版器镜像重定向无效")
		}
		return nil
	}}
	req, e := http.NewRequestWithContext(ctx, "GET", s.URL, nil)
	if e != nil {
		return e
	}
	response, e := client.Do(req)
	if e != nil {
		return e
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("下载排版器失败（HTTP %d）", response.StatusCode)
	}
	h := sha256.New()
	n, e := io.Copy(io.MultiWriter(out, h), io.LimitReader(response.Body, s.Size+1))
	if e != nil {
		return e
	}
	if n != s.Size || hex.EncodeToString(h.Sum(nil)) != s.Hash {
		return fmt.Errorf("下载文件大小或 SHA-256 与官方锁定版本不符")
	}
	return out.Sync()
}
