package toolpack

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type line struct {
	text       string
	x, y, size int
	rgb        color.RGBA
}
type page struct{ lines []line }

const pageWidth = 1024
const pageHeight = 1448

func loadFont(spec Spec) (*opentype.Font, error) {
	needsUnicode := func(s string) bool {
		for _, r := range s {
			if r > 255 {
				return true
			}
		}
		return false
	}
	needs := needsUnicode(spec.Title)
	for _, s := range spec.Sections {
		needs = needs || needsUnicode(s.Heading)
		for _, p := range s.Paragraphs {
			needs = needs || needsUnicode(p)
		}
	}
	if needs {
		paths := []string{"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc", "/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc", "/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc"}
		if runtime.GOOS == "windows" {
			dir := filepath.Join(os.Getenv("SystemRoot"), "Fonts")
			paths = []string{filepath.Join(dir, "msyh.ttc"), filepath.Join(dir, "NotoSansSC-VF.ttf"), filepath.Join(dir, "simhei.ttf")}
		}
		for _, p := range paths {
			info, err := os.Stat(p)
			if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<20 {
				continue
			}
			b, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			collection, err := opentype.ParseCollection(b)
			if err != nil {
				continue
			}
			f, err := collection.Font(0)
			if err == nil {
				return f, nil
			}
		}
	}
	return opentype.Parse(goregular.TTF)
}
func readColor(hex string) color.RGBA {
	var r, g, b uint8
	fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	return color.RGBA{R: r, G: g, B: b, A: 255}
}
func render(ctx context.Context, style profile, spec Spec, pdf bool) ([]byte, int, error) {
	f, err := loadFont(spec)
	if err != nil {
		return nil, 0, err
	}
	faces := map[int]font.Face{}
	defer func() {
		for _, face := range faces {
			face.Close()
		}
	}()
	for _, size := range []int{48, 34, 25} {
		face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: float64(size), DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			return nil, 0, err
		}
		faces[size] = face
	}
	pages := []page{{}}
	y := 102
	write := func(text string, size, gap int, ink color.RGBA) error {
		face := faces[size]
		output := func(s string) error {
			if y > pageHeight-85 {
				if len(pages) >= 64 || !pdf {
					return fmt.Errorf("正文超出单张图片或 64 页 PDF 的版面，请拆分内容")
				}
				pages = append(pages, page{})
				y = 102
			}
			n := len(pages) - 1
			pages[n].lines = append(pages[n].lines, line{s, 72, y, size, ink})
			y += size * 3 / 2
			return ctx.Err()
		}
		for _, paragraph := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
			var buf strings.Builder
			var advance fixed.Int26_6
			for _, r := range paragraph {
				if r == '\t' {
					r = ' '
				}
				w, ok := face.GlyphAdvance(r)
				if !ok {
					return fmt.Errorf("当前字体不支持字符 %q，请安装支持该语言的系统字体或生成可编辑文档", r)
				}
				if advance+w > fixed.I(pageWidth-144) && buf.Len() > 0 {
					if err := output(buf.String()); err != nil {
						return err
					}
					buf.Reset()
					advance = 0
				}
				buf.WriteRune(r)
				advance += w
			}
			if err := output(buf.String()); err != nil {
				return err
			}
		}
		y += gap
		return nil
	}
	if err = write(spec.Title, 48, 35, readColor(style.Ink)); err != nil {
		return nil, 0, err
	}
	for _, s := range spec.Sections {
		if s.Heading != "" {
			if err = write(s.Heading, 34, 10, readColor(style.Accent)); err != nil {
				return nil, 0, err
			}
		}
		for _, p := range s.Paragraphs {
			if err = write(p, 25, 16, readColor(style.Muted)); err != nil {
				return nil, 0, err
			}
		}
		y += 12
	}
	var images [][]byte
	for _, p := range pages {
		if err = ctx.Err(); err != nil {
			return nil, 0, err
		}
		canvas := image.NewRGBA(image.Rect(0, 0, pageWidth, pageHeight))
		draw.Draw(canvas, canvas.Bounds(), image.White, image.Point{}, draw.Src)
		draw.Draw(canvas, image.Rect(72, 35, 175, 42), image.NewUniform(readColor(style.Accent)), image.Point{}, draw.Src)
		for _, row := range p.lines {
			d := font.Drawer{Dst: canvas, Face: faces[row.size], Src: image.NewUniform(row.rgb), Dot: fixed.P(row.x, row.y)}
			d.DrawString(row.text)
		}
		var buf bytes.Buffer
		if !pdf {
			err = png.Encode(&buf, canvas)
			return buf.Bytes(), 1, err
		}
		if err = jpeg.Encode(&buf, canvas, &jpeg.Options{Quality: 90}); err != nil {
			return nil, 0, err
		}
		images = append(images, buf.Bytes())
	}
	return pdfPages(pages, images), len(pages), nil
}

// The visible PDF content is precisely the rasterized page. A hidden Unicode
// text layer preserves selection and search across common PDF viewers.
func pdfPages(pages []page, images [][]byte) []byte {
	objects := [][]byte{nil, nil}
	add := func(b []byte) int { objects = append(objects, b); return len(objects) }
	ref := func(s string) int { return add([]byte(s)) }
	stream := func(dict string, b []byte) int {
		var out bytes.Buffer
		fmt.Fprintf(&out, "<< %s /Length %d >>\nstream\n", dict, len(b))
		out.Write(b)
		out.WriteString("\nendstream")
		return add(out.Bytes())
	}
	codes := map[rune]int{}
	letters := []rune{}
	for _, p := range pages {
		for _, line := range p.lines {
			for _, r := range line.text {
				if _, exists := codes[r]; !exists {
					letters = append(letters, r)
					codes[r] = len(letters)
				}
			}
		}
	}
	var mapping, widths strings.Builder
	mapping.WriteString("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n/CMapName /NoosphereUnicode def\n/CMapType 2 def\n1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n")
	for start := 0; start < len(letters); start += 100 {
		batch := letters[start:min(start+100, len(letters))]
		fmt.Fprintf(&mapping, "%d beginbfchar\n", len(batch))
		for _, r := range batch {
			fmt.Fprintf(&mapping, "<%04X> <", codes[r])
			for _, v := range utf16.Encode([]rune{r}) {
				fmt.Fprintf(&mapping, "%04X", v)
			}
			mapping.WriteString(">\n")
			if r > 255 {
				widths.WriteString("1000 ")
			} else {
				widths.WriteString("550 ")
			}
		}
		mapping.WriteString("endbfchar\n")
	}
	mapping.WriteString("endcmap\nCMapName currentdict /CMap defineresource pop\nend end\n")
	cmap := stream("", []byte(mapping.String()))
	embedded := stream(fmt.Sprintf("/Length1 %d", len(goregular.TTF)), goregular.TTF)
	descriptor := ref(fmt.Sprintf("<< /Type /FontDescriptor /FontName /GoRegular /Flags 32 /FontBBox [-200 -300 1300 1100] /Ascent 940 /Descent -240 /CapHeight 740 /ItalicAngle 0 /StemV 80 /FontFile2 %d 0 R >>", embedded))
	glyphs := stream("", make([]byte, 2*(len(letters)+1)))
	cid := ref(fmt.Sprintf("<< /Type /Font /Subtype /CIDFontType2 /BaseFont /GoRegular /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /DW 1000 /W [1 [%s]] /CIDToGIDMap %d 0 R /FontDescriptor %d 0 R >>", widths.String(), glyphs, descriptor))
	fontID := ref(fmt.Sprintf("<< /Type /Font /Subtype /Type0 /BaseFont /GoRegular /Encoding /Identity-H /DescendantFonts [%d 0 R] /ToUnicode %d 0 R >>", cid, cmap))
	var kids strings.Builder
	for i, p := range pages {
		imageObj := stream(fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode", pageWidth, pageHeight), images[i])
		var content strings.Builder
		fmt.Fprintf(&content, "q 595 0 0 842 0 0 cm /PageImage Do Q\n")
		for _, row := range p.lines {
			fmt.Fprintf(&content, "BT /F1 %.2f Tf 3 Tr 1 0 0 1 %.2f %.2f Tm <", float64(row.size)*595/pageWidth, float64(row.x)*595/pageWidth, 842-float64(row.y)*842/pageHeight)
			for _, r := range row.text {
				fmt.Fprintf(&content, "%04X", codes[r])
			}
			content.WriteString("> Tj ET\n")
		}
		contents := stream("", []byte(content.String()))
		obj := ref(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /XObject << /PageImage %d 0 R >> /Font << /F1 %d 0 R >> >> /Contents %d 0 R >>", imageObj, fontID, contents))
		fmt.Fprintf(&kids, "%d 0 R ", obj)
	}
	objects[0] = []byte("<< /Type /Catalog /Pages 2 0 R >>")
	objects[1] = []byte(fmt.Sprintf("<< /Type /Pages /Count %d /Kids [%s] >>", len(pages), kids.String()))
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	offsets := []int{0}
	for i, obj := range objects {
		offsets = append(offsets, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n", i+1)
		out.Write(obj)
		out.WriteString("\nendobj\n")
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&out, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return out.Bytes()
}
