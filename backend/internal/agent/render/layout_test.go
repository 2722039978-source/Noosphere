//go:build linux

package render

import (
	"bytes"
	"context"
	"github.com/noosphere/backend/internal/agent/toolpack"
	"github.com/noosphere/backend/internal/ownedprocess"
	"image/png"
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) { ownedprocess.RunSupervisor(); os.Exit(m.Run()) }

func TestInstalledBrowserRendersCSSWithoutJavaScript(t *testing.T) {
	if os.Getenv("NOOSPHERE_TEST_BROWSER_RENDER") != "1" {
		t.Skip("requires machine Chromium in the OS sandbox")
	}
	html := "<style>.swatch{background-color:#bc3418;width:1100px;height:800px}</style><div class=\"swatch\">Sample</div><script>document.body.innerHTML='script executed'</script>"
	p, e := hostPreview(context.Background(), strings.NewReader(html), ".html", os.Getenv("NOOSPHERE_TEST_RENDER_DATA"))
	if e != nil {
		t.Fatal(e)
	}
	im, e := png.Decode(bytes.NewReader(p.PNG))
	if e != nil {
		t.Fatal(e)
	}
	r, g, b, _ := im.At(im.Bounds().Dx()/2, im.Bounds().Dy()/2).RGBA()
	if r>>8 != 188 || g>>8 != 52 || b>>8 != 24 {
		t.Fatalf("CSS/script isolation: %d %d %d", r>>8, g>>8, b>>8)
	}
}

type documentFixture struct{ bytes.Buffer }

func (d *documentFixture) Replace(_ context.Context, _, _ string, b []byte) error {
	_, err := d.Write(b)
	return err
}

func TestInstalledOfficeRendersDocumentAndSlide(t *testing.T) {
	if os.Getenv("NOOSPHERE_TEST_OFFICE_RENDER") != "1" {
		t.Skip("requires an installed isolated LibreOffice")
	}
	data := t.TempDir()
	if _, err := toolpack.Install(data); err != nil {
		t.Fatal(err)
	}
	for _, ext := range []string{".docx", ".pptx"} {
		t.Run(ext[1:], func(t *testing.T) {
			var doc documentFixture
			_, err := toolpack.Generate(t.Context(), data, &doc, toolpack.Spec{Path: "layout" + ext, Title: "Noosphere layout", Sections: []toolpack.Section{{Heading: "Actual typography", Paragraphs: []string{"This is text in the rendered Office document."}}}})
			if err != nil {
				t.Fatal(err)
			}
			preview, err := hostPreview(t.Context(), &doc, ext, os.Getenv("NOOSPHERE_TEST_RENDER_DATA"))
			if err != nil {
				t.Fatal(err)
			}
			if preview.Metadata.Renderer != "libreoffice-pdfium" || preview.Metadata.PageCount == 0 {
				t.Fatalf("%+v", preview.Metadata)
			}
			im, err := png.Decode(bytes.NewReader(preview.PNG))
			if err != nil {
				t.Fatal(err)
			}
			dark, light := 0, 0
			for y := 0; y < im.Bounds().Dy(); y += 2 {
				for x := 0; x < im.Bounds().Dx(); x += 2 {
					r, g, b, _ := im.At(x, y).RGBA()
					if r+g+b < 3*32000 {
						dark++
					}
					if r+g+b > 3*56000 {
						light++
					}
				}
			}
			if dark < 12 || light < 12 {
				t.Fatalf("empty or uniform layout: dark=%d, light=%d", dark, light)
			}
		})
	}
}
