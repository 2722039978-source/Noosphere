// Package render builds raster previews from immutable artifact bytes.
package render

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"strings"
	"time"

	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
)

const maxDocument = 32 << 20

var compileCache = wazero.NewCompilationCache()
var rasterSlot = make(chan struct{}, 1)

func pdfPreview(ctx context.Context, file io.Reader) (Preview, error) {
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	select {
	case rasterSlot <- struct{}{}:
		defer func() { <-rasterSlot }()
	case <-ctx.Done():
		return Preview{}, ctx.Err()
	}
	content, err := io.ReadAll(io.LimitReader(file, maxDocument+1))
	if err != nil {
		return Preview{}, err
	}
	if len(content) > maxDocument {
		return Preview{}, fmt.Errorf("PDF 栅格预览限 32 MiB")
	}
	config := wazero.NewRuntimeConfig().WithCoreFeatures(api.CoreFeaturesV2 | experimental.CoreFeaturesExceptionHandling).
		WithCompilationCache(compileCache).WithCloseOnContextDone(true).WithMemoryLimitPages(4096)
	// A nil FSConfig would mount the entire host drive in the upstream package.
	pool, err := webassembly.Init(webassembly.Config{Context: ctx, MaxIdle: 1, MaxTotal: 1, RuntimeConfig: config, FSConfig: wazero.NewFSConfig(), Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		return Preview{}, err
	}
	defer pool.Close()
	inst, err := pool.GetInstanceWithContext(ctx)
	if err != nil {
		return Preview{}, err
	}
	defer inst.Close()
	doc, err := inst.OpenDocument(&requests.OpenDocument{File: &content})
	if err != nil {
		return Preview{}, err
	}
	defer inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})
	count, err := inst.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc.Document})
	if err != nil {
		return Preview{}, err
	}
	if count.PageCount < 1 || count.PageCount > 20000 {
		return Preview{}, fmt.Errorf("PDF 页数超出预览范围")
	}
	pages := []int{0}
	if count.PageCount > 2 {
		pages = append(pages, (count.PageCount-1)/2)
	}
	if count.PageCount > 1 {
		pages = append(pages, count.PageCount-1)
	}
	var images []image.Image
	var labels []string
	for _, i := range pages {
		size, err := inst.FPDF_GetPageSizeByIndex(&requests.FPDF_GetPageSizeByIndex{Document: doc.Document, Index: i})
		if err != nil {
			return Preview{}, err
		}
		if !(size.Width >= 1 && size.Height >= 1 && size.Width <= 14400 && size.Height <= 14400) || math.Max(size.Width, size.Height)/math.Min(size.Width, size.Height) > 200 {
			return Preview{}, fmt.Errorf("PDF 页面尺寸无效")
		}
		page, err := inst.RenderPageInPixels(&requests.RenderPageInPixels{Page: requests.Page{ByIndex: &requests.PageByIndex{Document: doc.Document, Index: i}}, Width: 900, Height: 900})
		if err != nil {
			return Preview{}, err
		}
		// The returned pixel buffer belongs to the WASM instance.
		im := image.NewRGBA(page.Result.RenderedImage.Bounds())
		draw.Draw(im, im.Bounds(), page.Result.RenderedImage, im.Bounds().Min, draw.Src)
		page.Cleanup()
		images = append(images, im)
		labels = append(labels, fmt.Sprint(i+1))
	}
	preview, err := sheet(images)
	p, err := rasterPreview(preview, "pdfium", fmt.Sprintf("PDFium 已渲染第 %s 页，共 %d 页", strings.Join(labels, "、"), count.PageCount), err)
	if err != nil {
		return Preview{}, err
	}
	p.Metadata.PageCount = count.PageCount
	for _, n := range pages {
		p.Metadata.SampledPages = append(p.Metadata.SampledPages, n+1)
	}
	return p, nil
}

func sheet(images []image.Image) ([]byte, error) {
	width, height := 0, 0
	for _, im := range images {
		width = max(width, im.Bounds().Dx())
		height += im.Bounds().Dy()
	}
	height += 8 * (len(images) - 1)
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.RGBA{235, 235, 235, 255}), image.Point{}, draw.Src)
	y := 0
	for _, im := range images {
		b := im.Bounds()
		x := (width - b.Dx()) / 2
		draw.Draw(canvas, image.Rect(x, y, x+b.Dx(), y+b.Dy()), im, b.Min, draw.Src)
		y += b.Dy() + 8
	}
	var data bytes.Buffer
	err := png.Encode(&data, canvas)
	if err == nil && data.Len() > 8<<20 {
		return nil, fmt.Errorf("预览图片超过 8 MiB")
	}
	return data.Bytes(), err
}

func officePreview(ctx context.Context, file io.Reader) (Preview, error) {
	p, err := pdfPreview(ctx, file)
	if err != nil {
		return Preview{}, err
	}
	p.Check = "LibreOffice 排版；" + p.Check
	p.Metadata.Renderer = "libreoffice-pdfium"
	return p, nil
}
