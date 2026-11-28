package toolpack

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ledongthuc/pdf"
)

type testWriter map[string][]byte

func (w testWriter) Replace(_ context.Context, path, hash string, data []byte) error {
	if _, exists := w[path]; exists && hash == "" {
		return errors.New("existing file")
	}
	w[path] = bytes.Clone(data)
	return nil
}
func TestGenerateInstalledPackFormats(t *testing.T) {
	dir := t.TempDir()
	s, err := Install(dir)
	if err != nil || !s.Installed || s.SHA256 == "" {
		t.Fatal(s, err)
	}
	files := testWriter{}
	for _, ext := range s.Formats {
		spec := Spec{Path: "report." + ext, Title: "Design Review", Sections: []Section{{Heading: "Results", Paragraphs: []string{"Visible report & <ordinary> text"}}}}
		out, err := Generate(t.Context(), dir, files, spec)
		if err != nil {
			t.Fatalf("%s: %v", ext, err)
		}
		raw := files[spec.Path]
		if out.Size != len(raw) || out.SHA256 == "" || len(out.Checks) == 0 {
			t.Fatal(out)
		}
		switch ext {
		case "docx", "pptx":
			z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
			if err != nil {
				t.Fatal(err)
			}
			body := false
			for _, part := range z.File {
				r, err := part.Open()
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(r)
				r.Close()
				if err != nil {
					t.Fatal(err)
				}
				if !strings.HasSuffix(part.Name, ".xml") && !strings.HasSuffix(part.Name, ".rels") {
					continue
				}
				parser := xml.NewDecoder(bytes.NewReader(data))
				for {
					tok, err := parser.Token()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatalf("%s: %v", part.Name, err)
					}
					if text, ok := tok.(xml.CharData); ok && strings.Contains(string(text), "& <ordinary>") {
						body = true
					}
				}
			}
			if !body {
				t.Fatal("editable content missing", ext)
			}
		case "pdf":
			doc, err := pdf.NewReader(bytes.NewReader(raw), int64(len(raw)))
			if err != nil || doc.NumPage() != 1 {
				t.Fatal(err, out.Pages)
			}
			text, err := doc.Page(1).GetPlainText(nil)
			if err != nil || !strings.Contains(text, "Design Review") {
				t.Fatal(text, err)
			}
		case "png":
			img, err := png.Decode(bytes.NewReader(raw))
			if err != nil || img.Bounds().Dx() != pageWidth {
				t.Fatal(err)
			}
			ink := false
			for y := 65; y < 145; y++ {
				for x := 72; x < 512; x++ {
					r, _, _, _ := img.At(x, y).RGBA()
					if r < 0x9000 {
						ink = true
					}
				}
			}
			if !ink {
				t.Fatal("missing rasterized title")
			}
		case "html":
			if strings.Contains(string(raw), "<ordinary>") || !strings.Contains(string(raw), "&lt;ordinary&gt;") {
				t.Fatal("unescaped HTML")
			}
		}
	}
}
func TestTamperedPackCannotGenerateAndReinstallsAtomically(t *testing.T) {
	dir := t.TempDir()
	if _, err := Install(dir); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(packDir(dir), packageName())
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	b[4] ^= 1
	if err = os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
	if Status(dir).Installed {
		t.Fatal("accepted corrupt resources")
	}
	if _, err = Generate(t.Context(), dir, testWriter{}, Spec{Path: "out.pdf", Title: "hello"}); err == nil {
		t.Fatal("corrupt pack generated output")
	}
	if s, err := Install(dir); err != nil || !s.Installed {
		t.Fatal(s, err)
	}
}
func TestLongPDFPaginatesAndImageRejectsOverflow(t *testing.T) {
	s := Spec{Title: "Results", Sections: []Section{{Heading: "A"}}}
	for i := 0; i < 55; i++ {
		s.Sections[0].Paragraphs = append(s.Sections[0].Paragraphs, fmt.Sprint("Line number ", i))
	}
	dir := t.TempDir()
	if _, err := Install(dir); err != nil {
		t.Fatal(err)
	}
	files := testWriter{}
	s.Path = "long.pdf"
	out, err := Generate(t.Context(), dir, files, s)
	if err != nil || out.Pages <= 1 {
		t.Fatal(out, err)
	}
	s.Path = "long.png"
	if _, err = Generate(t.Context(), dir, files, s); err == nil || files[s.Path] != nil {
		t.Fatal("image clipped")
	}
}
