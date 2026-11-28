package render

import (
	"archive/zip"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsOfficePreviewUsesEmbeddedThumbnail(t *testing.T) {
	for _, ext := range []string{".docx", ".pptx"} {
		t.Run(ext[1:], func(t *testing.T) {
			f, err := os.Create(filepath.Join(t.TempDir(), "document"+ext))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if _, err = Document(t.Context(), f, f.Name(), ""); err == nil || !strings.Contains(err.Error(), "内置缩略图") {
				t.Fatalf("expected a missing-thumbnail error, got %v", err)
			}
			z := zip.NewWriter(f)
			thumb, err := z.Create("docProps/thumbnail.png")
			if err != nil {
				t.Fatal(err)
			}
			img := image.NewRGBA(image.Rect(0, 0, 2, 3))
			img.SetRGBA(1, 1, color.RGBA{R: 200, A: 255})
			if err = png.Encode(thumb, img); err != nil {
				t.Fatal(err)
			}
			if err = z.Close(); err != nil {
				t.Fatal(err)
			}
			preview, err := Document(t.Context(), f, f.Name(), "")
			if err != nil {
				t.Fatal(err)
			}
			if preview.Metadata.Renderer != "embedded-thumbnail" || preview.Metadata.Width != 2 || preview.Metadata.Height != 3 {
				t.Fatalf("unexpected thumbnail metadata: %+v", preview.Metadata)
			}
		})
	}
}
