package render

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pdfFixture() []byte {
	content := "1 0 0 rg 0 0 200 100 re f\n"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Count 1 /Kids [3 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources <<>> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content)}
	var b bytes.Buffer
	fmt.Fprint(&b, "%PDF-1.4\n")
	offsets := []int{0}
	for i, obj := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := b.Len()
	fmt.Fprint(&b, "xref\n0 5\n0000000000 65535 f \n")
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size 5 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)
	return b.Bytes()
}

func TestPDFPageIsRasterizedFromDrawingCommands(t *testing.T) {
	preview, e := pdfPreview(context.Background(), bytes.NewReader(pdfFixture()))
	if e != nil {
		t.Fatal(e)
	}
	im, e := png.Decode(bytes.NewReader(preview.PNG))
	if e != nil {
		t.Fatal(e)
	}
	if im.Bounds().Dx() != 2*im.Bounds().Dy() || !strings.Contains(preview.Check, "共 1 页") || preview.Metadata.PageCount != 1 || len(preview.Metadata.SampledPages) != 1 || preview.Metadata.SampledPages[0] != 1 {
		t.Fatal(im.Bounds(), preview.Check)
	}
	r, g, b, _ := im.At(im.Bounds().Dx()/2, im.Bounds().Dy()/2).RGBA()
	if r < 60000 || g > 1000 || b > 1000 {
		t.Fatalf("wanted red center, got %d %d %d", r, g, b)
	}
}

func TestOfficeEmbeddedThumbnailIsLabelled(t *testing.T) {
	var data bytes.Buffer
	z := zip.NewWriter(&data)
	w, e := z.Create("docProps/thumbnail.png")
	if e != nil {
		t.Fatal(e)
	}
	im := image.NewRGBA(image.Rect(0, 0, 8, 8))
	im.Set(1, 1, color.Black)
	if e = png.Encode(w, im); e != nil {
		t.Fatal(e)
	}
	if e = z.Close(); e != nil {
		t.Fatal(e)
	}
	result, e := embeddedThumbnail(bytes.NewReader(data.Bytes()), int64(data.Len()))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = png.Decode(bytes.NewReader(result)); e != nil {
		t.Fatal(e)
	}
}

func TestCancelledRenderStops(t *testing.T) {
	f, e := os.Create(filepath.Join(t.TempDir(), "page.pdf"))
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if _, e = f.Write(pdfFixture()); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e = Document(ctx, f, "page.pdf", "")
	if e == nil {
		t.Fatal("cancelled render succeeded")
	}
}
