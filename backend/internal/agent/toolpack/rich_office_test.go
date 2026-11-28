package toolpack

import (
	"archive/zip"
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noosphere/backend/internal/agent/artifact"
	"github.com/noosphere/backend/internal/agent/host"
	"golang.org/x/image/font/gofont/goregular"
)

func TestParseFontFileSupportsSingleTTF(t *testing.T) {
	f, err := parseFontFile(goregular.TTF)
	if err != nil || f == nil {
		t.Fatalf("expected single TTF font to parse cleanly, got %v", err)
	}
	if len(fontCandidatePaths("darwin")) == 0 || len(fontCandidatePaths("windows")) == 0 || len(fontCandidatePaths("linux")) == 0 {
		t.Fatal("missing font candidate paths")
	}
}

func TestRichPPTXAndDOCXIncludeTablesImagesAndWindowsPreview(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Create a valid sample PNG inside the project so PPTX can embed it.
	img := image.NewRGBA(image.Rect(0, 0, 120, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 120; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 0, G: 122, B: 115, A: 255})
		}
	}
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "chart.png"), pngBuf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	if _, err := Install(dataDir); err != nil {
		t.Fatal(err)
	}
	d, err := host.Open(context.Background(), dataDir, "rich-doc-run", root, "binding")
	if err != nil {
		t.Fatal(err)
	}

	spec := Spec{
		Path:     "deck.pptx",
		Title:    "Noosphere Agent v2 升级汇报",
		Subtitle: "从只读审批走向授权自治交付",
		Author:   "Noosphere Team",
		Theme:    "noosphere",
		Sections: []Section{
			{
				Heading:  "核心指标",
				Subtitle: "关键吞吐与体验提升",
				Layout:   "metrics",
				Metrics: []Metric{
					{Label: "单轮步数上限", Value: "48 轮", Note: "支持自动压缩与续跑"},
					{Label: "工具链复用", Value: "毫秒级", Note: "告别重复全量拷贝"},
					{Label: "预览覆盖", Value: "100%", Note: "内置多页高清缩略图"},
				},
				Bullets: []string{"支持自动模式与审阅模式切换"},
				Notes:   "强调工具链缓存和真实预览能力的提升。",
			},
			{
				Heading: "架构对比",
				Layout:  "table",
				Table: &Table{
					Headers: []string{"维度", "旧版 v1", "新版 v2"},
					Rows: [][]string{
						{"文件编辑", "64KB 纯文本提案", "多处编辑 + 二进制产物"},
						{"回滚机制", "单条动作", "整轮快照一键撤销"},
					},
				},
			},
			{
				Heading: "双栏能力拆解",
				Layout:  "two_column",
				Columns: []Column{
					{Heading: "内核层", Bullets: []string{"事件日志持久化", "三档策略管线"}},
					{Heading: "宿主层", Bullets: []string{"LPAC / bwrap 隔离", "依赖只读缓存"}},
				},
			},
			{
				Heading:   "配图展示",
				Bullets:   []string{"自动嵌入工作区真实图片", "保持原始宽高比不拉伸"},
				ImagePath: "chart.png",
				ImageAlt:  "示例图表",
			},
		},
	}

	out, err := Generate(context.Background(), dataDir, d, spec)
	if err != nil {
		t.Fatalf("Generate rich PPTX failed: %v", err)
	}
	if out.Pages != 5 {
		t.Fatalf("expected 5 slides (cover + 4 sections), got %d", out.Pages)
	}

	// Verify OOXML contains table, embedded image, speaker notes, and embedded thumbnail.
	size, _, err := d.Stat(context.Background(), "deck.pptx")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := d.Read(context.Background(), "deck.pptx", 0, int(size))
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	hasTable := false
	for _, f := range z.File {
		seen[f.Name] = true
		if strings.HasPrefix(f.Name, "ppt/slides/slide") && strings.HasSuffix(f.Name, ".xml") {
			r, _ := f.Open()
			body, _ := io.ReadAll(r)
			r.Close()
			if bytes.Contains(body, []byte("<a:tbl>")) {
				hasTable = true
			}
		}
	}
	for _, want := range []string{"docProps/thumbnail.png", "ppt/media/image1.png", "ppt/notesSlides/notesSlide2.xml"} {
		if !seen[want] {
			t.Fatalf("missing expected PPTX part %q", want)
		}
	}
	if !hasTable {
		t.Fatal("expected native <a:tbl> inside generated PPTX")
	}

	// Verify artifact.Present produces a real PNG preview even on Windows (via embedded thumbnail).
	item, err := artifact.Present(context.Background(), d, "deck.pptx", dataDir)
	if err != nil {
		t.Fatalf("artifact.Present failed: %v", err)
	}
	if !item.Preview || item.PreviewMIME != "image/png" || item.PreviewHash == "" {
		t.Fatalf("expected generated PPTX to have a real preview, got: %+v", item)
	}

	// Also verify DOCX with table and thumbnail preview.
	spec.Path = "report.docx"
	if _, err := Generate(context.Background(), dataDir, d, spec); err != nil {
		t.Fatalf("Generate rich DOCX failed: %v", err)
	}
	docItem, err := artifact.Present(context.Background(), d, "report.docx", dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if !docItem.Preview || docItem.PreviewMIME != "image/png" {
		t.Fatalf("expected generated DOCX to have preview, got: %+v", docItem)
	}
}
