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
	"image"
	_ "image/jpeg"
	_ "image/png"
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
	return State{
		Available:   true,
		Installed:   err == nil,
		Version:     Version,
		SHA256:      fmt.Sprintf("%x", sha256.Sum256(b)),
		Size:        len(b),
		SizeBytes:   len(b),
		Description: "内置离线文档工具包；支持主题、多栏卡片、KPI 指标卡、原生表格、嵌入工作区图片与自动渲染预览图，可生成 PPTX、DOCX、PDF、PNG 和静态 HTML。",
		Formats:     []string{"pptx", "docx", "pdf", "png", "html"},
		Renderer:    "PNG/PDF 与 Office 内置多页预览缩略图均通过跨平台字体栅格化生成；Office 文件生成完整可编辑 OOXML 包并校验结构。",
	}
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

type Metric struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Note  string `json:"note,omitempty"`
}

type Table struct {
	Headers []string   `json:"headers"`
	Rows    [][]string `json:"rows"`
}

type Column struct {
	Heading    string   `json:"heading,omitempty"`
	Paragraphs []string `json:"paragraphs,omitempty"`
	Bullets    []string `json:"bullets,omitempty"`
}

type Section struct {
	Heading    string   `json:"heading"`
	Subtitle   string   `json:"subtitle,omitempty"`
	Layout     string   `json:"layout,omitempty"`
	Paragraphs []string `json:"paragraphs,omitempty"`
	Bullets    []string `json:"bullets,omitempty"`
	Columns    []Column `json:"columns,omitempty"`
	Metrics    []Metric `json:"metrics,omitempty"`
	Table      *Table   `json:"table,omitempty"`
	ImagePath  string   `json:"image_path,omitempty"`
	ImageAlt   string   `json:"image_alt,omitempty"`
	Notes      string   `json:"notes,omitempty"`
}

type Spec struct {
	Path         string    `json:"path"`
	ExpectedHash string    `json:"expected_hash,omitempty"`
	Title        string    `json:"title"`
	Subtitle     string    `json:"subtitle,omitempty"`
	Author       string    `json:"author,omitempty"`
	Theme        string    `json:"theme,omitempty"`
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

// Reader is optionally implemented by workspace adapters (such as host.Draft)
// to allow embedding verified draft images inside generated documents.
type Reader interface {
	Stat(context.Context, string) (int64, string, error)
	Read(context.Context, string, int64, int) ([]byte, error)
}

type embeddedImage struct {
	Bytes  []byte
	Ext    string
	MIME   string
	Width  int
	Height int
}

type profile struct {
	Font       string `json:"font"`
	Ink        string `json:"ink"`
	Accent     string `json:"accent"`
	Background string `json:"background"`
	Muted      string `json:"muted"`
	Surface    string `json:"surface,omitempty"`
	SoftAccent string `json:"soft_accent,omitempty"`
	Border     string `json:"border,omitempty"`
	Accent2    string `json:"accent2,omitempty"`
	OnAccent   string `json:"on_accent,omitempty"`
}

func themeProfile(base profile, name string) profile {
	fontName := base.Font
	if fontName == "" {
		fontName = "Microsoft YaHei"
	}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "slate":
		return profile{
			Font: fontName, Background: "F8FAFC", Surface: "FFFFFF", Border: "CBD5E1",
			Ink: "0F172A", Muted: "475569", Accent: "2563EB", Accent2: "0EA5E9",
			SoftAccent: "EFF6FF", OnAccent: "FFFFFF",
		}
	case "warm":
		return profile{
			Font: fontName, Background: "FCFBF7", Surface: "FFFFFF", Border: "E6DEC8",
			Ink: "2C241B", Muted: "6B5B4B", Accent: "B45309", Accent2: "0F766E",
			SoftAccent: "FEF3C7", OnAccent: "FFFFFF",
		}
	case "dark":
		return profile{
			Font: fontName, Background: "0F172A", Surface: "1E293B", Border: "334155",
			Ink: "F8FAFC", Muted: "CBD5E1", Accent: "38BDF8", Accent2: "2DD4BF",
			SoftAccent: "16263B", OnAccent: "0F172A",
		}
	default:
		// Default "noosphere" theme: clean, flat, rounded, high-contrast teal & slate.
		return profile{
			Font: fontName, Background: "F5F9F9", Surface: "FFFFFF", Border: "D4E3E4",
			Ink: "1B2F44", Muted: "4A6074", Accent: "007A73", Accent2: "2C6E91",
			SoftAccent: "E6F4F2", OnAccent: "FFFFFF",
		}
	}
}

func loadSectionImages(ctx context.Context, d Writer, sections []Section) (map[int]embeddedImage, error) {
	media := map[int]embeddedImage{}
	reader, canRead := d.(Reader)
	for i, s := range sections {
		if strings.TrimSpace(s.ImagePath) == "" {
			continue
		}
		if !canRead {
			return nil, errors.New("当前宿主不支持嵌入工作区图片")
		}
		size, _, err := reader.Stat(ctx, s.ImagePath)
		if err != nil {
			return nil, fmt.Errorf("读取配图 %s 失败: %w", s.ImagePath, err)
		}
		if size <= 0 || size > 8<<20 {
			return nil, fmt.Errorf("配图 %s 必须为 1 字节至 8 MiB 的图片文件", s.ImagePath)
		}
		raw, err := reader.Read(ctx, s.ImagePath, 0, int(size))
		if err != nil {
			return nil, fmt.Errorf("读取配图 %s 失败: %w", s.ImagePath, err)
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
		if err != nil || (format != "png" && format != "jpeg") {
			return nil, fmt.Errorf("配图 %s 仅支持有效的 PNG 或 JPEG 图片", s.ImagePath)
		}
		if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 8192 || cfg.Height > 8192 || int64(cfg.Width)*int64(cfg.Height) > 32_000_000 {
			return nil, fmt.Errorf("配图 %s 尺寸超出允许范围", s.ImagePath)
		}
		ext := "png"
		mimeType := "image/png"
		if format == "jpeg" {
			ext = "jpg"
			mimeType = "image/jpeg"
		}
		media[i] = embeddedImage{Bytes: raw, Ext: ext, MIME: mimeType, Width: cfg.Width, Height: cfg.Height}
	}
	return media, nil
}

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
		if len(s.Paragraphs) > 100 || len(s.Bullets) > 100 || len(s.Heading) > 1024 || len(s.Subtitle) > 1024 || len(s.Notes) > 8192 {
			return Output{}, errors.New("章节标题、正文或备注超出限制")
		}
		if len(s.Columns) > 4 || len(s.Metrics) > 6 {
			return Output{}, errors.New("单节最多支持 4 栏或 6 项指标卡")
		}
		if s.Table != nil && (len(s.Table.Headers) == 0 || len(s.Table.Headers) > 12 || len(s.Table.Rows) > 60) {
			return Output{}, errors.New("表格需含 1–12 列且最多 60 行")
		}
	}
	var baseStyle profile
	if err = json.Unmarshal([]byte(assets["profile.json"]), &baseStyle); err != nil {
		return Output{}, err
	}
	style := themeProfile(baseStyle, spec.Theme)
	media, err := loadSectionImages(ctx, d, spec.Sections)
	if err != nil {
		return Output{}, err
	}
	var raw []byte
	out := Output{Path: spec.Path}
	switch strings.ToLower(path.Ext(spec.Path)) {
	case ".docx":
		raw, err = word(ctx, assets, style, spec, media)
		out.Checks = []string{"可编辑 WordprocessingML 包（含表格/列表与内置缩略图）"}
	case ".pptx":
		raw, out.Pages, err = slides(ctx, assets, style, spec, media)
		out.Checks = []string{"可编辑幻灯片、母版、版式、主题、关系与内置多页缩略图"}
	case ".html":
		raw, err = webPage(style, spec, media)
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
