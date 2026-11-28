// Package artifact describes immutable deliverables using inspected file bytes.
package artifact

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/xml"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/noosphere/backend/internal/agent/host"
	"github.com/noosphere/backend/internal/agent/render"
)

type Item struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Path        string           `json:"path"`
	MIME        string           `json:"mime"`
	Size        int64            `json:"size"`
	SHA256      string           `json:"sha256"`
	Checks      []string         `json:"checks"`
	Preview     bool             `json:"preview"`
	PreviewMIME string           `json:"preview_mime,omitempty"`
	PreviewHash string           `json:"preview_hash,omitempty"`
	PreviewInfo *render.Metadata `json:"preview_info,omitempty"`
}

func Present(ctx context.Context, d *host.Draft, name, dataDir string) (Item, error) {
	var a Item
	hash, e := d.Archive(ctx, name)
	if e != nil {
		return a, e
	}
	file, e := d.OpenBlob(ctx, hash)
	if e != nil {
		return a, e
	}
	defer file.Close()
	info, e := file.Stat()
	if e != nil {
		return a, e
	}
	a = Item{ID: fmt.Sprintf("%x", sha256.Sum256([]byte(name))), Name: path.Base(name), Path: name, Size: info.Size(), SHA256: hash, Checks: []string{"已归档并核对 SHA-256"}}
	var first [512]byte
	n, _ := file.ReadAt(first[:], 0)
	a.MIME = http.DetectContentType(first[:n])
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".jpg", ".jpeg":
		cfg, format, e := image.DecodeConfig(file)
		if e != nil || cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 48_000_000 {
			return a, fmt.Errorf("图片尺寸或文件格式无效")
		}
		file.Seek(0, io.SeekStart)
		if _, _, err := image.Decode(file); err != nil {
			return a, fmt.Errorf("图片解码：%w", err)
		}
		a.MIME = "image/" + format
		a.Preview = a.Size <= 8<<20
		a.PreviewMIME = a.MIME
		a.Checks = append(a.Checks, fmt.Sprintf("已解码 %d × %d 像素", cfg.Width, cfg.Height))
	case ".docx", ".pptx":
		presentation := strings.HasSuffix(strings.ToLower(name), ".pptx")
		count, e := checkOffice(ctx, file, a.Size, presentation)
		if e != nil {
			return a, e
		}
		a.MIME = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
		item := "正文"
		if presentation {
			a.MIME = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
			item = "幻灯片"
		}
		a.Checks = append(a.Checks, fmt.Sprintf("OOXML 文档结构有效，%d 个%s部件", count, item))
	case ".pdf":
		if !strings.HasPrefix(string(first[:n]), "%PDF-") {
			return a, fmt.Errorf("PDF 文件头无效")
		}
		tail := make([]byte, min(int64(2048), a.Size))
		_, _ = file.ReadAt(tail, a.Size-int64(len(tail)))
		if !strings.Contains(string(tail), "%%EOF") {
			return a, fmt.Errorf("PDF 文件结尾不完整")
		}
		a.MIME = "application/pdf"
		a.Checks = append(a.Checks, "PDF 文件头与结束标记有效")
	default:
		if strings.HasPrefix(a.MIME, "text/") && a.Size <= 1<<20 {
			raw, e := io.ReadAll(io.LimitReader(file, (1<<20)+1))
			if e == nil && utf8.Valid(raw) {
				a.Preview = true
				a.PreviewMIME = "text/plain; charset=utf-8"
				a.Checks = append(a.Checks, "UTF-8 源文预览")
			}
		}
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".pdf", ".docx", ".pptx", ".html", ".htm":
		preview, err := render.Document(ctx, file, name, dataDir)
		if ctx.Err() != nil {
			return a, ctx.Err()
		}
		if err != nil {
			a.Checks = append(a.Checks, "页面预览未完成："+err.Error())
		} else {
			hash, err := d.StorePreview(ctx, preview.PNG)
			if err != nil {
				return a, err
			}
			a.Preview = true
			a.PreviewMIME = "image/png"
			a.PreviewHash = hash
			a.PreviewInfo = &preview.Metadata
			a.Checks = append(a.Checks, preview.Check)
		}
	}
	return a, nil
}
func checkOffice(ctx context.Context, f *os.File, size int64, slides bool) (int, error) {
	z, e := zip.NewReader(f, size)
	if e != nil {
		return 0, e
	}
	if len(z.File) > 20000 {
		return 0, fmt.Errorf("OOXML 部件过多")
	}
	required := map[string]bool{"[Content_Types].xml": false, "_rels/.rels": false}
	main := "word/document.xml"
	if slides {
		main = "ppt/presentation.xml"
	}
	required[main] = false
	seen := map[string]bool{}
	count := 0
	var expanded uint64
	for _, p := range z.File {
		if e := ctx.Err(); e != nil {
			return 0, e
		}
		if !fs.ValidPath(strings.TrimSuffix(p.Name, "/")) || strings.ContainsAny(p.Name, "\\:") || seen[p.Name] {
			return 0, fmt.Errorf("无效的 OOXML 部件名")
		}
		seen[p.Name] = true
		if _, ok := required[p.Name]; ok {
			required[p.Name] = true
		}
		if !strings.HasSuffix(p.Name, ".xml") && !strings.HasSuffix(p.Name, ".rels") {
			continue
		}
		if p.UncompressedSize64 > 16<<20 || expanded > 128<<20 {
			return 0, fmt.Errorf("OOXML 展开超过检查预算")
		}
		expanded += p.UncompressedSize64
		r, e := p.Open()
		if e != nil {
			return 0, e
		}
		dec := xml.NewDecoder(r)
		roots, depth := 0, 0
		for {
			t, err := dec.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				r.Close()
				return 0, err
			}
			switch t.(type) {
			case xml.StartElement:
				if depth == 0 {
					roots++
				}
				depth++
			case xml.EndElement:
				depth--
			}
		}
		r.Close()
		if roots != 1 || depth != 0 {
			return 0, fmt.Errorf("OOXML 部件结构无效")
		}
		if slides && path.Dir(p.Name) == "ppt/slides" && strings.HasPrefix(path.Base(p.Name), "slide") || !slides && p.Name == main {
			count++
		}
	}
	for name, found := range required {
		if !found {
			return 0, fmt.Errorf("缺少必要部件 %s", name)
		}
	}
	if count == 0 {
		return 0, fmt.Errorf("文档没有内容部件")
	}
	return count, nil
}
