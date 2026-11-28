package render

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"io"
	"os"
	"path"
	"strings"

	"golang.org/x/image/draw"
)

type Metadata struct {
	Renderer     string `json:"renderer"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	PageCount    int    `json:"page_count,omitempty"`
	SampledPages []int  `json:"sampled_pages,omitempty"`
}

type Preview struct {
	PNG      []byte
	Check    string
	Metadata Metadata
}

func rasterPreview(png []byte, renderer, check string, err error) (Preview, error) {
	if err != nil {
		return Preview{}, err
	}
	dimensions, _, err := image.DecodeConfig(bytes.NewReader(png))
	if err != nil {
		return Preview{}, err
	}
	return Preview{PNG: png, Check: check, Metadata: Metadata{Renderer: renderer, Width: dimensions.Width, Height: dimensions.Height}}, nil
}

// Document runs only against the immutable object opened by the artifact registry.
func Document(ctx context.Context, f *os.File, name, dataDir string) (Preview, error) {
	info, err := f.Stat()
	if err != nil {
		return Preview{}, err
	}
	if info.Size() > maxDocument {
		return Preview{}, fmt.Errorf("当前页面预览限 32 MiB")
	}
	ext := strings.ToLower(path.Ext(name))
	source := io.NewSectionReader(f, 0, info.Size())
	if ext == ".pdf" {
		return pdfPreview(ctx, source)
	}
	external, err := hostPreview(ctx, source, ext, dataDir)
	if ctx.Err() != nil {
		return Preview{}, ctx.Err()
	}
	if err == nil {
		return external, nil
	}
	if ext == ".pptx" || ext == ".docx" {
		thumb, thumbErr := embeddedThumbnail(f, info.Size())
		if thumbErr == nil {
			return rasterPreview(thumb, "embedded-thumbnail", "Office 文件内保存的缩略图；版面未重新渲染", nil)
		}
	}
	return Preview{}, err
}

func embeddedThumbnail(input io.ReaderAt, size int64) ([]byte, error) {
	z, e := zip.NewReader(input, size)
	if e != nil {
		return nil, e
	}
	for _, f := range z.File {
		switch strings.ToLower(f.Name) {
		case "docprops/thumbnail.jpeg", "docprops/thumbnail.jpg", "docprops/thumbnail.png":
		default:
			continue
		}
		if f.UncompressedSize64 > 8<<20 {
			return nil, fmt.Errorf("Office 缩略图过大")
		}
		r, e := f.Open()
		if e != nil {
			return nil, e
		}
		raw, e := io.ReadAll(io.LimitReader(r, (8<<20)+1))
		r.Close()
		if e != nil {
			return nil, e
		}
		return normalizeImage(raw)
	}
	return nil, errors.New("文件没有内置缩略图")
}

func normalizeImage(raw []byte) ([]byte, error) {
	cfg, _, e := image.DecodeConfig(bytes.NewReader(raw))
	if e != nil {
		return nil, e
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 12000 || cfg.Height > 12000 || int64(cfg.Width)*int64(cfg.Height) > 16_000_000 {
		return nil, fmt.Errorf("预览图像尺寸无效")
	}
	im, _, e := image.Decode(bytes.NewReader(raw))
	if e != nil {
		return nil, e
	}
	if max(cfg.Width, cfg.Height) > 1000 {
		w, h := max(1, cfg.Width*1000/max(cfg.Width, cfg.Height)), max(1, cfg.Height*1000/max(cfg.Width, cfg.Height))
		scaled := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.ApproxBiLinear.Scale(scaled, scaled.Bounds(), im, im.Bounds(), draw.Src, nil)
		im = scaled
	}
	return sheet([]image.Image{im})
}
