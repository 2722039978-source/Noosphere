package render

import (
	"bytes"
	"fmt"
	"image/png"
	"slices"
	"testing"
)

func TestPDFiumRendersPageContent(t *testing.T) {
	const drawing = "1 0 0 rg\n0 0 200 200 re f\n"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(drawing), drawing),
	}
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, object := range objects {
		offsets[i] = pdf.Len()
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Root 1 0 R /Size %d >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)

	preview, err := pdfPreview(t.Context(), &pdf)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Metadata.Renderer != "pdfium" || preview.Metadata.PageCount != 1 || !slices.Equal(preview.Metadata.SampledPages, []int{1}) {
		t.Fatalf("unexpected PDF preview metadata: %+v", preview.Metadata)
	}
	img, err := png.Decode(bytes.NewReader(preview.PNG))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, a := img.At(img.Bounds().Dx()/2, img.Bounds().Dy()/2).RGBA()
	if r != 0xffff || g != 0 || b != 0 || a != 0xffff {
		t.Fatalf("PDF content did not render red: rgba(%d, %d, %d, %d)", r, g, b, a)
	}
}
