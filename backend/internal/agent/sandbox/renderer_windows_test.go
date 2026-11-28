package sandbox

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestLPACStaticCSSPreview(t *testing.T) {
	exe := os.Getenv("NOOSPHERE_TEST_HTML_TOOL")
	if exe == "" {
		t.Skip("supply the locked official static HTML renderer")
	}
	work := t.TempDir()
	html := []byte("<style>.swatch{background-color:#bc3418;width:1100px;height:800px}</style><div class=\"swatch\">Sample</div><script>document.body.innerHTML='script executed'</script>")
	if e := os.WriteFile(filepath.Join(work, "input.html"), html, 0600); e != nil {
		t.Fatal(e)
	}
	if e := renderWithProgram(context.Background(), work, ".html", exe); e != nil {
		t.Fatal(e)
	}
	f, e := os.Open(filepath.Join(work, "page.png"))
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	im, e := png.Decode(f)
	if e != nil {
		t.Fatal(e)
	}
	r, g, b, _ := im.At(200, 200).RGBA()
	if r>>8 != 188 || g>>8 != 52 || b>>8 != 24 {
		t.Fatal(r>>8, g>>8, b>>8)
	}
}
