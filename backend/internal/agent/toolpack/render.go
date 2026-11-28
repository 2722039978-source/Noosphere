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

	xdraw "golang.org/x/image/draw"
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

// parseFontFile parses both single TTF/OTF fonts and TTC/OTC collections.
func parseFontFile(data []byte) (*opentype.Font, error) {
	if len(data) >= 4 && string(data[:4]) == "ttcf" {
		collection, err := opentype.ParseCollection(data)
		if err != nil {
			return nil, err
		}
		return collection.Font(0)
	}
	f, err := opentype.Parse(data)
	if err == nil {
		return f, nil
	}
	collection, colErr := opentype.ParseCollection(data)
	if colErr != nil {
		return nil, err
	}
	return collection.Font(0)
}

func fontCandidatePaths(goos string) []string {
	switch goos {
	case "windows":
		dir := filepath.Join(os.Getenv("SystemRoot"), "Fonts")
		return []string{
			filepath.Join(dir, "msyh.ttc"),
			filepath.Join(dir, "msyh.ttf"),
			filepath.Join(dir, "Deng.ttf"),
			filepath.Join(dir, "simhei.ttf"),
			filepath.Join(dir, "simsun.ttc"),
			filepath.Join(dir, "NotoSansSC-VF.ttf"),
		}
	case "darwin":
		return []string{
			"/System/Library/Fonts/PingFang.ttc",
			"/System/Library/Fonts/STHeiti Medium.ttc",
			"/System/Library/Fonts/Hiragino Sans GB.ttc",
			"/Library/Fonts/Arial Unicode.ttf",
		}
	default:
		return []string{
			"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
			"/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc",
			"/usr/share/fonts/opentype/noto/NotoSansSC-Regular.otf",
			"/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc",
			"/usr/share/fonts/truetype/wqy/wqy-microhei.ttc",
		}
	}
}

func specNeedsUnicode(spec Spec) bool {
	hasNonASCII := func(s string) bool {
		for _, r := range s {
			if r > 255 {
				return true
			}
		}
		return false
	}
	if hasNonASCII(spec.Title) || hasNonASCII(spec.Subtitle) || hasNonASCII(spec.Author) {
		return true
	}
	for _, s := range spec.Sections {
		if hasNonASCII(s.Heading) || hasNonASCII(s.Subtitle) {
			return true
		}
		for _, p := range s.Paragraphs {
			if hasNonASCII(p) {
				return true
			}
		}
		for _, b := range s.Bullets {
			if hasNonASCII(b) {
				return true
			}
		}
		for _, c := range s.Columns {
			if hasNonASCII(c.Heading) {
				return true
			}
			for _, p := range c.Paragraphs {
				if hasNonASCII(p) {
					return true
				}
			}
			for _, b := range c.Bullets {
				if hasNonASCII(b) {
					return true
				}
			}
		}
		for _, m := range s.Metrics {
			if hasNonASCII(m.Label) || hasNonASCII(m.Value) || hasNonASCII(m.Note) {
				return true
			}
		}
		if s.Table != nil {
			for _, h := range s.Table.Headers {
				if hasNonASCII(h) {
					return true
				}
			}
			for _, row := range s.Table.Rows {
				for _, cell := range row {
					if hasNonASCII(cell) {
						return true
					}
				}
			}
		}
	}
	return false
}

func loadFont(spec Spec) (*opentype.Font, error) {
	return loadFontForUnicode(specNeedsUnicode(spec))
}

func loadFontForUnicode(needs bool) (*opentype.Font, error) {
	if needs {
		for _, p := range fontCandidatePaths(runtime.GOOS) {
			info, err := os.Stat(p)
			if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<20 {
				continue
			}
			b, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			if f, err := parseFontFile(b); err == nil {
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
	if err = write(spec.Title, 48, 22, readColor(style.Ink)); err != nil {
		return nil, 0, err
	}
	if strings.TrimSpace(spec.Subtitle) != "" {
		if err = write(spec.Subtitle, 25, 22, readColor(style.Muted)); err != nil {
			return nil, 0, err
		}
	}
	for _, s := range spec.Sections {
		if s.Heading != "" {
			if err = write(s.Heading, 34, 10, readColor(style.Accent)); err != nil {
				return nil, 0, err
			}
		}
		if strings.TrimSpace(s.Subtitle) != "" {
			if err = write(s.Subtitle, 25, 10, readColor(style.Muted)); err != nil {
				return nil, 0, err
			}
		}
		for _, p := range s.Paragraphs {
			if err = write(p, 25, 16, readColor(style.Muted)); err != nil {
				return nil, 0, err
			}
		}
		for _, b := range s.Bullets {
			if err = write("• "+b, 25, 12, readColor(style.Ink)); err != nil {
				return nil, 0, err
			}
		}
		for _, m := range s.Metrics {
			row := m.Label + ": " + m.Value
			if m.Note != "" {
				row += " (" + m.Note + ")"
			}
			if err = write(row, 25, 12, readColor(style.Accent)); err != nil {
				return nil, 0, err
			}
		}
		for _, col := range s.Columns {
			if col.Heading != "" {
				if err = write(col.Heading, 25, 8, readColor(style.Ink)); err != nil {
					return nil, 0, err
				}
			}
			for _, p := range col.Paragraphs {
				if err = write(p, 25, 10, readColor(style.Muted)); err != nil {
					return nil, 0, err
				}
			}
			for _, b := range col.Bullets {
				if err = write("• "+b, 25, 10, readColor(style.Muted)); err != nil {
					return nil, 0, err
				}
			}
		}
		if s.Table != nil {
			if err = write(strings.Join(s.Table.Headers, " | "), 25, 8, readColor(style.Ink)); err != nil {
				return nil, 0, err
			}
			for _, r := range s.Table.Rows {
				if err = write(strings.Join(r, " | "), 25, 8, readColor(style.Muted)); err != nil {
					return nil, 0, err
				}
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
		draw.Draw(canvas, canvas.Bounds(), image.NewUniform(readColor(style.Background)), image.Point{}, draw.Src)
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

// slidePreviewCard describes one logical slide for rasterizing a crisp embedded OOXML thumbnail.
type slidePreviewCard struct {
	Cover    bool
	Title    string
	Subtitle string
	Footer   string
	PageNum  string
	Bullets  []string
	Metrics  []Metric
	Columns  []Column
	Table    *Table
	Image    *embeddedImage
}

func fillRect(dst *image.RGBA, r image.Rectangle, c color.RGBA) {
	draw.Draw(dst, r.Intersect(dst.Bounds()), image.NewUniform(c), image.Point{}, draw.Src)
}

func strokeRect(dst *image.RGBA, r image.Rectangle, c color.RGBA, thickness int) {
	fillRect(dst, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+thickness), c)
	fillRect(dst, image.Rect(r.Min.X, r.Max.Y-thickness, r.Max.X, r.Max.Y), c)
	fillRect(dst, image.Rect(r.Min.X, r.Min.Y, r.Min.X+thickness, r.Max.Y), c)
	fillRect(dst, image.Rect(r.Max.X-thickness, r.Min.Y, r.Max.X, r.Max.Y), c)
}

func drawSafeText(dst *image.RGBA, face font.Face, x, y, maxW int, c color.RGBA, text string) int {
	var clean strings.Builder
	var adv fixed.Int26_6
	limit := fixed.I(maxW)
	for _, r := range text {
		if r == '\n' || r == '\r' || r == '\t' {
			r = ' '
		}
		w, ok := face.GlyphAdvance(r)
		if !ok {
			r = '?'
			w, _ = face.GlyphAdvance(r)
		}
		if adv+w > limit {
			break
		}
		clean.WriteRune(r)
		adv += w
	}
	d := font.Drawer{Dst: dst, Face: face, Src: image.NewUniform(c), Dot: fixed.P(x, y)}
	d.DrawString(clean.String())
	return y
}

func drawWrappedSafe(dst *image.RGBA, face font.Face, x, y, maxW, lineH, maxLines int, c color.RGBA, text string) int {
	drawn := 0
	for _, para := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if drawn >= maxLines {
			break
		}
		var lineBuf strings.Builder
		var adv fixed.Int26_6
		limit := fixed.I(maxW)
		for _, r := range para {
			if r == '\t' {
				r = ' '
			}
			w, ok := face.GlyphAdvance(r)
			if !ok {
				r = '?'
				w, _ = face.GlyphAdvance(r)
			}
			if adv+w > limit && lineBuf.Len() > 0 {
				drawSafeText(dst, face, x, y, maxW, c, lineBuf.String())
				y += lineH
				drawn++
				if drawn >= maxLines {
					return y
				}
				lineBuf.Reset()
				adv = 0
			}
			lineBuf.WriteRune(r)
			adv += w
		}
		if lineBuf.Len() > 0 && drawn < maxLines {
			drawSafeText(dst, face, x, y, maxW, c, lineBuf.String())
			y += lineH
			drawn++
		}
	}
	return y
}

func renderSlideTile(faceTitle, faceSub, faceBody, faceSmall font.Face, style profile, card slidePreviewCard) *image.RGBA {
	const w, h = 600, 338
	tile := image.NewRGBA(image.Rect(0, 0, w, h))
	bg := readColor(style.Background)
	surf := readColor(style.Surface)
	border := readColor(style.Border)
	ink := readColor(style.Ink)
	muted := readColor(style.Muted)
	accent := readColor(style.Accent)
	soft := readColor(style.SoftAccent)
	onAccent := readColor(style.OnAccent)

	fillRect(tile, tile.Bounds(), bg)
	if card.Cover {
		fillRect(tile, image.Rect(0, 0, 22, h), accent)
		fillRect(tile, image.Rect(w-170, 28, w-28, 128), soft)
		fillRect(tile, image.Rect(56, 78, 130, 84), accent)
		y := drawWrappedSafe(tile, faceTitle, 56, 132, w-120, 34, 3, ink, card.Title)
		if card.Subtitle != "" {
			drawWrappedSafe(tile, faceSub, 56, y+12, w-120, 22, 2, muted, card.Subtitle)
		}
		if card.Footer != "" {
			drawSafeText(tile, faceSmall, 56, h-28, w-110, muted, card.Footer)
		}
		strokeRect(tile, tile.Bounds(), border, 1)
		return tile
	}

	fillRect(tile, image.Rect(0, 0, w, 6), accent)
	drawSafeText(tile, faceSub, 28, 36, w-56, accent, card.Title)
	bodyTop := 54
	if card.Subtitle != "" {
		drawSafeText(tile, faceSmall, 28, 54, w-56, muted, card.Subtitle)
		bodyTop = 68
	}
	bodyBot := h - 30
	if len(card.Metrics) > 0 {
		n := min(4, len(card.Metrics))
		gap := 12
		cardW := (w - 56 - gap*(n-1)) / n
		for i := 0; i < n; i++ {
			m := card.Metrics[i]
			x0 := 28 + i*(cardW+gap)
			r := image.Rect(x0, bodyTop+6, x0+cardW, bodyTop+132)
			fillRect(tile, r, surf)
			strokeRect(tile, r, border, 1)
			fillRect(tile, image.Rect(x0, bodyTop+6, x0+cardW, bodyTop+11), accent)
			drawSafeText(tile, faceTitle, x0+12, bodyTop+56, cardW-24, accent, m.Value)
			drawSafeText(tile, faceBody, x0+12, bodyTop+84, cardW-24, ink, m.Label)
			if m.Note != "" {
				drawSafeText(tile, faceSmall, x0+12, bodyTop+108, cardW-24, muted, m.Note)
			}
		}
		if len(card.Bullets) > 0 {
			y := bodyTop + 158
			for _, b := range card.Bullets {
				if y > bodyBot-10 {
					break
				}
				y = drawWrappedSafe(tile, faceSmall, 32, y, w-64, 16, 2, muted, "• "+b)
			}
		}
	} else if card.Table != nil && len(card.Table.Headers) > 0 {
		cols := min(6, len(card.Table.Headers))
		colW := (w - 56) / cols
		rowH := 28
		rHead := image.Rect(28, bodyTop+4, 28+colW*cols, bodyTop+4+rowH)
		fillRect(tile, rHead, accent)
		for c := 0; c < cols; c++ {
			drawSafeText(tile, faceSmall, 34+c*colW, bodyTop+22, colW-10, onAccent, card.Table.Headers[c])
		}
		maxRows := min(len(card.Table.Rows), (bodyBot-(bodyTop+4+rowH))/rowH)
		for rIdx := 0; rIdx < maxRows; rIdx++ {
			y0 := bodyTop + 4 + rowH*(rIdx+1)
			rowBg := surf
			if rIdx%2 == 1 {
				rowBg = soft
			}
			fillRect(tile, image.Rect(28, y0, 28+colW*cols, y0+rowH), rowBg)
			strokeRect(tile, image.Rect(28, y0, 28+colW*cols, y0+rowH), border, 1)
			for c := 0; c < cols && c < len(card.Table.Rows[rIdx]); c++ {
				drawSafeText(tile, faceSmall, 34+c*colW, y0+19, colW-10, ink, card.Table.Rows[rIdx][c])
			}
		}
	} else if len(card.Columns) > 0 {
		n := min(3, len(card.Columns))
		gap := 12
		colW := (w - 56 - gap*(n-1)) / n
		for i := 0; i < n; i++ {
			col := card.Columns[i]
			x0 := 28 + i*(colW+gap)
			r := image.Rect(x0, bodyTop+4, x0+colW, bodyBot-4)
			fillRect(tile, r, surf)
			strokeRect(tile, r, border, 1)
			y := bodyTop + 26
			if col.Heading != "" {
				drawSafeText(tile, faceBody, x0+10, y, colW-20, accent, col.Heading)
				y += 20
			}
			for _, b := range append(append([]string{}, col.Paragraphs...), col.Bullets...) {
				if y > bodyBot-14 {
					break
				}
				y = drawWrappedSafe(tile, faceSmall, x0+10, y, colW-20, 16, 3, ink, "• "+b)
			}
		}
	} else {
		textMaxW := w - 76
		box := image.Rect(28, bodyTop+4, w-28, bodyBot-4)
		if card.Image != nil {
			textMaxW = (w-68)/2 - 20
			box = image.Rect(28, bodyTop+4, 28+(w-68)/2, bodyBot-4)
			imgBox := image.Rect(box.Max.X+12, bodyTop+4, w-28, bodyBot-4)
			fillRect(tile, imgBox, surf)
			strokeRect(tile, imgBox, border, 1)
			if decoded, _, err := image.Decode(bytes.NewReader(card.Image.Bytes)); err == nil {
				bw, bh := imgBox.Dx()-8, imgBox.Dy()-8
				sw, sh := decoded.Bounds().Dx(), decoded.Bounds().Dy()
				if sw > 0 && sh > 0 && bw > 0 && bh > 0 {
					tw, th := bw, sh*bw/sw
					if th > bh {
						th = bh
						tw = sw * bh / sh
					}
					ox := imgBox.Min.X + (imgBox.Dx()-tw)/2
					oy := imgBox.Min.Y + (imgBox.Dy()-th)/2
					xdraw.ApproxBiLinear.Scale(tile, image.Rect(ox, oy, ox+tw, oy+th), decoded, decoded.Bounds(), draw.Over, nil)
				}
			}
		}
		fillRect(tile, box, surf)
		strokeRect(tile, box, border, 1)
		y := bodyTop + 28
		for _, item := range card.Bullets {
			if y > bodyBot-16 {
				break
			}
			y = drawWrappedSafe(tile, faceBody, 42, y, textMaxW, 20, 3, ink, item)
			y += 4
		}
	}
	drawSafeText(tile, faceSmall, 28, h-10, w-110, muted, card.Footer)
	drawSafeText(tile, faceSmall, w-70, h-10, 50, muted, card.PageNum)
	strokeRect(tile, tile.Bounds(), border, 1)
	return tile
}

func renderDeckThumbnail(ctx context.Context, style profile, cards []slidePreviewCard) ([]byte, error) {
	if len(cards) == 0 {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	needsUnicode := false
	for _, c := range cards {
		if specNeedsUnicode(Spec{Title: c.Title, Subtitle: c.Subtitle, Sections: []Section{{Bullets: c.Bullets, Metrics: c.Metrics, Columns: c.Columns, Table: c.Table}}}) {
			needsUnicode = true
			break
		}
	}
	f, err := loadFontForUnicode(needsUnicode)
	if err != nil {
		return nil, err
	}
	makeFace := func(sz float64) (font.Face, error) {
		return opentype.NewFace(f, &opentype.FaceOptions{Size: sz, DPI: 72, Hinting: font.HintingFull})
	}
	faceTitle, err := makeFace(26)
	if err != nil {
		return nil, err
	}
	defer faceTitle.Close()
	faceSub, err := makeFace(17)
	if err != nil {
		return nil, err
	}
	defer faceSub.Close()
	faceBody, err := makeFace(13)
	if err != nil {
		return nil, err
	}
	defer faceBody.Close()
	faceSmall, err := makeFace(11)
	if err != nil {
		return nil, err
	}
	defer faceSmall.Close()

	sample := cards
	if len(sample) > 4 {
		sample = sample[:4]
	}
	tiles := make([]*image.RGBA, 0, len(sample))
	for _, c := range sample {
		tiles = append(tiles, renderSlideTile(faceTitle, faceSub, faceBody, faceSmall, style, c))
	}
	var out *image.RGBA
	pad := 18
	bg := color.RGBA{R: 232, G: 238, B: 242, A: 255}
	if len(tiles) == 1 {
		out = tiles[0]
	} else {
		cols := 2
		rows := (len(tiles) + 1) / 2
		tw, th := tiles[0].Bounds().Dx(), tiles[0].Bounds().Dy()
		out = image.NewRGBA(image.Rect(0, 0, pad*(cols+1)+tw*cols, pad*(rows+1)+th*rows))
		fillRect(out, out.Bounds(), bg)
		for i, t := range tiles {
			r, c := i/cols, i%cols
			x0 := pad + c*(tw+pad)
			y0 := pad + r*(th+pad)
			draw.Draw(out, image.Rect(x0, y0, x0+tw, y0+th), t, image.Point{}, draw.Src)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func renderDocThumbnail(ctx context.Context, style profile, spec Spec) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := loadFont(spec)
	if err != nil {
		return nil, err
	}
	faceH1, err := opentype.NewFace(f, &opentype.FaceOptions{Size: 28, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, err
	}
	defer faceH1.Close()
	faceH2, err := opentype.NewFace(f, &opentype.FaceOptions{Size: 18, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, err
	}
	defer faceH2.Close()
	faceBody, err := opentype.NewFace(f, &opentype.FaceOptions{Size: 14, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, err
	}
	defer faceBody.Close()
	const w, h = 680, 960
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	fillRect(canvas, canvas.Bounds(), readColor(style.Surface))
	fillRect(canvas, image.Rect(48, 36, 132, 42), readColor(style.Accent))
	y := drawWrappedSafe(canvas, faceH1, 48, 86, w-96, 36, 3, readColor(style.Ink), spec.Title)
	if spec.Subtitle != "" {
		y = drawWrappedSafe(canvas, faceBody, 48, y+6, w-96, 22, 2, readColor(style.Muted), spec.Subtitle)
	}
	y += 14
	for _, s := range spec.Sections {
		if y > h-60 {
			break
		}
		if s.Heading != "" {
			y = drawWrappedSafe(canvas, faceH2, 48, y, w-96, 26, 2, readColor(style.Accent), s.Heading)
			y += 4
		}
		for _, p := range s.Paragraphs {
			if y > h-50 {
				break
			}
			y = drawWrappedSafe(canvas, faceBody, 48, y, w-96, 21, 4, readColor(style.Ink), p)
			y += 6
		}
		for _, b := range s.Bullets {
			if y > h-50 {
				break
			}
			y = drawWrappedSafe(canvas, faceBody, 56, y, w-104, 21, 3, readColor(style.Ink), "• "+b)
			y += 4
		}
		if s.Table != nil && y < h-80 {
			y = drawWrappedSafe(canvas, faceBody, 48, y, w-96, 20, 2, readColor(style.Muted), strings.Join(s.Table.Headers, " | "))
			for _, row := range s.Table.Rows {
				if y > h-50 {
					break
				}
				y = drawWrappedSafe(canvas, faceBody, 48, y, w-96, 20, 2, readColor(style.Ink), strings.Join(row, " | "))
			}
		}
		y += 10
	}
	strokeRect(canvas, canvas.Bounds(), readColor(style.Border), 1)
	var buf bytes.Buffer
	if err := png.Encode(&buf, canvas); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
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
