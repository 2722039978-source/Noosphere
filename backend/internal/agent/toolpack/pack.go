// Package toolpack installs the first-party document templates shipped with
// Noosphere. Every executable operation is compiled Go code; the package
// carries only declarative resources, pinned to the build.
package toolpack

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

const Version = "1.0.0"

//go:embed resources/*
var templates embed.FS

var compiled = sync.OnceValues(func() ([]byte, error) {
	files, err := templates.ReadDir("resources")
	if err != nil {
		return nil, err
	}
	assets := map[string]string{}
	for _, f := range files {
		data, err := templates.ReadFile("resources/" + f.Name())
		if err != nil {
			return nil, err
		}
		assets[f.Name()] = strings.ReplaceAll(string(data), "\r\n", "\n")
	}
	return json.Marshal(assets)
})

type State struct {
	Available   bool     `json:"available"`
	Installed   bool     `json:"installed"`
	Version     string   `json:"version"`
	SHA256      string   `json:"sha256"`
	Size        int      `json:"size"`
	SizeBytes   int      `json:"size_bytes"`
	Description string   `json:"description"`
	Formats     []string `json:"formats"`
	Renderer    string   `json:"renderer"`
}

func packDir(dataDir string) string { return filepath.Join(dataDir, "agent-toolpacks") }
func packageName() string           { return "documents-" + Version + ".json" }
func installed(dataDir string) (map[string]string, error) {
	if dataDir == "" {
		return nil, errors.New("未配置工具包目录")
	}
	root, err := os.OpenRoot(packDir(dataDir))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat(packageName())
	if err != nil {
		return nil, err
	}
	expected, err := compiled()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() != int64(len(expected)) {
		return nil, errors.New("文档工具包大小或类型异常，请重新安装")
	}
	f, err := root.Open(packageName())
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(len(expected)+1)))
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(data, expected) {
		return nil, errors.New("文档工具包完整性校验失败，请重新安装")
	}
	var assets map[string]string
	err = json.Unmarshal(data, &assets)
	return assets, err
}
func Status(dataDir string) State {
	b, _ := compiled()
	_, err := installed(dataDir)
	return State{Available: true, Installed: err == nil, Version: Version, SHA256: fmt.Sprintf("%x", sha256.Sum256(b)), Size: len(b), SizeBytes: len(b), Description: "内置离线文档模板；按需安装后可在草稿中生成 Office 文件、PDF、图片和静态网页。", Formats: []string{"pptx", "docx", "pdf", "png", "html"}, Renderer: "PNG 使用字体栅格化；PDF 嵌入实际渲染页。Office 文件生成可编辑文本并检查包结构。"}
}
func Install(dataDir string) (State, error) {
	if dataDir == "" || !filepath.IsAbs(dataDir) {
		return State{}, errors.New("无效的工具包目录")
	}
	if state := Status(dataDir); state.Installed {
		return state, nil
	}
	err := os.MkdirAll(packDir(dataDir), 0700)
	if err != nil {
		return State{}, err
	}
	data, err := compiled()
	if err != nil {
		return State{}, err
	}
	file, err := os.CreateTemp(packDir(dataDir), ".pack-")
	if err != nil {
		return State{}, err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return State{}, err
	}
	if err = os.Rename(file.Name(), filepath.Join(packDir(dataDir), packageName())); err != nil {
		if state := Status(dataDir); state.Installed {
			return state, nil
		}
		return State{}, err
	}
	state := Status(dataDir)
	if !state.Installed {
		return state, errors.New("安装后工具包校验失败")
	}
	return state, nil
}
func RegisterRoutes(mux *http.ServeMux, dataDir string) {
	mux.HandleFunc("GET /api/agent/toolpack", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Status(dataDir))
	})
	mux.HandleFunc("POST /api/agent/toolpack", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		state, err := Install(dataDir)
		if err != nil {
			w.WriteHeader(500)
			json.NewEncoder(w).Encode(map[string]string{"error": "内置工具包安装失败，请检查本地应用目录"})
			return
		}
		json.NewEncoder(w).Encode(state)
	})
}

type Section struct {
	Heading    string   `json:"heading"`
	Paragraphs []string `json:"paragraphs"`
}
type Spec struct {
	Path         string    `json:"path"`
	ExpectedHash string    `json:"expected_hash,omitempty"`
	Title        string    `json:"title"`
	Sections     []Section `json:"sections"`
}
type Output struct {
	Path   string   `json:"path"`
	SHA256 string   `json:"sha256"`
	Size   int      `json:"size"`
	Pages  int      `json:"pages,omitempty"`
	Checks []string `json:"checks"`
}
type Writer interface {
	Replace(context.Context, string, string, []byte) error
}
type profile struct{ Font, Ink, Accent, Background, Muted string }

func Generate(ctx context.Context, dataDir string, d Writer, spec Spec) (Output, error) {
	assets, err := installed(dataDir)
	if err != nil {
		return Output{}, fmt.Errorf("请在设置中安装文档工具包: %w", err)
	}
	if err = ctx.Err(); err != nil {
		return Output{}, err
	}
	input, _ := json.Marshal(spec)
	if len(input) > 1<<20 || strings.TrimSpace(spec.Title) == "" || len(spec.Title) > 1024 || len(spec.Sections) > 80 {
		return Output{}, errors.New("文档需要标题，最多 80 节、1 MiB 内容")
	}
	for _, s := range spec.Sections {
		if len(s.Paragraphs) > 100 || len(s.Heading) > 1024 {
			return Output{}, errors.New("章节标题或正文超出限制")
		}
	}
	var style profile
	if err = json.Unmarshal([]byte(assets["profile.json"]), &style); err != nil {
		return Output{}, err
	}
	var raw []byte
	out := Output{Path: spec.Path}
	switch strings.ToLower(path.Ext(spec.Path)) {
	case ".docx":
		raw, err = word(assets, style, spec)
		out.Checks = []string{"可编辑 WordprocessingML 包"}
	case ".pptx":
		raw, out.Pages, err = slides(assets, style, spec)
		out.Checks = []string{"可编辑幻灯片、母版、版式、主题与关系"}
	case ".html":
		raw, err = webPage(assets, spec)
		out.Checks = []string{"静态 HTML；用户内容已转义"}
	case ".png", ".pdf":
		raw, out.Pages, err = render(ctx, style, spec, strings.EqualFold(path.Ext(spec.Path), ".pdf"))
		out.Checks = []string{"正文已排版并渲染为像素"}
	default:
		return Output{}, errors.New("工具包支持 pptx、docx、pdf、png、html")
	}
	if err != nil {
		return Output{}, err
	}
	if err = ctx.Err(); err != nil {
		return Output{}, err
	}
	if err = d.Replace(ctx, spec.Path, spec.ExpectedHash, raw); err != nil {
		return Output{}, err
	}
	out.SHA256 = fmt.Sprintf("%x", sha256.Sum256(raw))
	out.Size = len(raw)
	return out, nil
}
