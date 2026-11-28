package toolpack

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"html/template"
	"sort"
	"strings"
	textTemplate "text/template"
)

const relType = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
const pkgRelType = "http://schemas.openxmlformats.org/package/2006/relationships/"
const typesPrefix = "application/vnd.openxmlformats-officedocument."
const group = `<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>`
const slideNS = ` xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"`

func escape(s string) string { var b bytes.Buffer; xml.EscapeText(&b, []byte(s)); return b.String() }
func tmpl(source string, data any) (string, error) {
	t, err := textTemplate.New("part").Parse(source)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	err = t.Execute(&b, data)
	return b.String(), err
}

const richHTMLTemplate = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data:; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'"><title>{{.Spec.Title}}</title><style>*{box-sizing:border-box}body{margin:0 auto;max-width:66rem;padding:clamp(1.5rem,5vw,4.5rem);color:#{{.Style.Ink}};background:#{{.Style.Background}};font:1.05rem/1.7 system-ui,-apple-system,sans-serif}.hero{border-left:5px solid #{{.Style.Accent}};padding:.25rem 0 .25rem 1.25rem;margin-bottom:2.2rem}h1{font-size:clamp(2rem,4.5vw,3.4rem);line-height:1.15;margin:0 0 .5rem}.subtitle{color:#{{.Style.Muted}};font-size:1.15rem;margin:0}.meta{color:#{{.Style.Muted}};font-size:.9rem;margin-top:.5rem}section{background:#{{.Style.Surface}};border:1px solid #{{.Style.Border}};border-radius:14px;padding:1.5rem 1.75rem;margin-bottom:1.25rem}h2{font-size:1.4rem;color:#{{.Style.Accent}};margin:0 0 .35rem}.sec-sub{color:#{{.Style.Muted}};margin:0 0 1rem;font-size:.95rem}p{white-space:pre-wrap;overflow-wrap:anywhere;margin:.45rem 0}ul{margin:.5rem 0;padding-left:1.3rem}li{margin:.3rem 0}.metrics{display:grid;grid-template-columns:repeat(auto-fit,minmax(170px,1fr));gap:.85rem;margin:.75rem 0}.metric{background:#{{.Style.SoftAccent}};border:1px solid #{{.Style.Border}};border-radius:10px;padding:.9rem 1rem}.metric .val{font-size:1.65rem;font-weight:700;color:#{{.Style.Accent}};line-height:1.2}.metric .lbl{font-weight:600;margin-top:.25rem}.metric .note{font-size:.85rem;color:#{{.Style.Muted}};margin-top:.2rem}.cols{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:1rem;margin:.75rem 0}.col{background:#{{.Style.Background}};border:1px solid #{{.Style.Border}};border-radius:10px;padding:1rem}.col h3{margin:0 0 .45rem;font-size:1.05rem;color:#{{.Style.Accent}}}table{width:100%;border-collapse:collapse;margin:.75rem 0;font-size:.95rem}th{background:#{{.Style.Accent}};color:#{{.Style.OnAccent}};text-align:left;padding:.55rem .75rem}td{border-bottom:1px solid #{{.Style.Border}};padding:.5rem .75rem}tr:nth-child(even) td{background:#{{.Style.SoftAccent}}}.fig{margin:1rem 0 0;text-align:center}.fig img{max-width:100%;max-height:420px;border-radius:10px;border:1px solid #{{.Style.Border}}}.quote{border-left:4px solid #{{.Style.Accent}};background:#{{.Style.SoftAccent}};padding:1rem 1.25rem;border-radius:0 10px 10px 0;font-size:1.15rem}@media print{body{max-width:none;padding:12mm}section{break-inside:avoid}}</style><main><header class="hero"><h1>{{.Spec.Title}}</h1>{{if .Spec.Subtitle}}<p class="subtitle">{{.Spec.Subtitle}}</p>{{end}}{{if .Spec.Author}}<p class="meta">{{.Spec.Author}}</p>{{end}}</header>{{range $idx, $s := .Spec.Sections}}<section><h2>{{$s.Heading}}</h2>{{if $s.Subtitle}}<p class="sec-sub">{{$s.Subtitle}}</p>{{end}}{{if $s.Metrics}}<div class="metrics">{{range $s.Metrics}}<div class="metric"><div class="val">{{.Value}}</div><div class="lbl">{{.Label}}</div>{{if .Note}}<div class="note">{{.Note}}</div>{{end}}</div>{{end}}</div>{{end}}{{if $s.Table}}<table><thead><tr>{{range $s.Table.Headers}}<th>{{.}}</th>{{end}}</tr></thead><tbody>{{range $s.Table.Rows}}<tr>{{range .}}<td>{{.}}</td>{{end}}</tr>{{end}}</tbody></table>{{end}}{{if $s.Columns}}<div class="cols">{{range $s.Columns}}<div class="col">{{if .Heading}}<h3>{{.Heading}}</h3>{{end}}{{range .Paragraphs}}<p>{{.}}</p>{{end}}{{if .Bullets}}<ul>{{range .Bullets}}<li>{{.}}</li>{{end}}</ul>{{end}}</div>{{end}}</div>{{end}}{{if eq $s.Layout "quote"}}{{range $s.Paragraphs}}<blockquote class="quote">{{.}}</blockquote>{{end}}{{else}}{{range $s.Paragraphs}}<p>{{.}}</p>{{end}}{{end}}{{if $s.Bullets}}<ul>{{range $s.Bullets}}<li>{{.}}</li>{{end}}</ul>{{end}}{{with index $.Images $idx}}<figure class="fig"><img src="{{.}}" alt="{{$s.ImageAlt}}"></figure>{{end}}</section>{{end}}</main></html>`

func webPage(style profile, spec Spec, media map[int]embeddedImage) ([]byte, error) {
	t, err := template.New("page").Parse(richHTMLTemplate)
	if err != nil {
		return nil, err
	}
	imgs := map[int]template.URL{}
	for idx, img := range media {
		imgs[idx] = template.URL("data:" + img.MIME + ";base64," + base64.StdEncoding.EncodeToString(img.Bytes))
	}
	var b bytes.Buffer
	err = t.Execute(&b, struct {
		Style  profile
		Spec   Spec
		Images map[int]template.URL
	}{Style: style, Spec: spec, Images: imgs})
	return b.Bytes(), err
}

type relationship struct {
	id, kind, target string
	pkg              bool
}

func relationships(links ...relationship) string {
	var b strings.Builder
	b.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for _, r := range links {
		prefix := relType
		if r.pkg {
			prefix = pkgRelType
		}
		fmt.Fprintf(&b, `<Relationship Id="%s" Type="%s%s" Target="%s"/>`, r.id, prefix, r.kind, r.target)
	}
	b.WriteString("</Relationships>")
	return b.String()
}

func types(parts map[string]string, hasPNG, hasJPG bool) string {
	var b strings.Builder
	b.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>`)
	if hasPNG {
		b.WriteString(`<Default Extension="png" ContentType="image/png"/>`)
	}
	if hasJPG {
		b.WriteString(`<Default Extension="jpg" ContentType="image/jpeg"/><Default Extension="jpeg" ContentType="image/jpeg"/>`)
	}
	for _, name := range keysString(parts) {
		fmt.Fprintf(&b, `<Override PartName="/%s" ContentType="%s%s"/>`, name, typesPrefix, parts[name])
	}
	b.WriteString("</Types>")
	return b.String()
}

func keysString(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func archive(parts map[string][]byte) ([]byte, error) {
	names := make([]string, 0, len(parts))
	for k := range parts {
		names = append(names, k)
	}
	sort.Strings(names)
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for _, name := range names {
		w, err := z.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err = w.Write(parts[name]); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func wordTable(style profile, headers []string, rows [][]string) string {
	var b strings.Builder
	b.WriteString(`<w:tbl><w:tblPr><w:tblW w:w="9140" w:type="dxa"/><w:tblBorders><w:top w:val="single" w:sz="4" w:space="0" w:color="` + style.Border + `"/><w:bottom w:val="single" w:sz="4" w:space="0" w:color="` + style.Border + `"/><w:left w:val="single" w:sz="4" w:space="0" w:color="` + style.Border + `"/><w:right w:val="single" w:sz="4" w:space="0" w:color="` + style.Border + `"/><w:insideH w:val="single" w:sz="4" w:space="0" w:color="` + style.Border + `"/><w:insideV w:val="single" w:sz="4" w:space="0" w:color="` + style.Border + `"/></w:tblBorders></w:tblPr>`)
	cell := func(text, fill, textColor string, bold bool) {
		weight := ""
		if bold {
			weight = "<w:b/>"
		}
		fmt.Fprintf(&b, `<w:tc><w:tcPr><w:shd w:val="clear" w:color="auto" w:fill="%s"/></w:tcPr><w:p><w:pPr><w:spacing w:before="60" w:after="60"/></w:pPr><w:r><w:rPr><w:rFonts w:ascii="Arial" w:eastAsia="%s" w:hAnsi="Arial"/>%s<w:color w:val="%s"/><w:sz w:val="20"/></w:rPr><w:t xml:space="preserve">%s</w:t></w:r></w:p></w:tc>`, fill, escape(style.Font), weight, textColor, escape(text))
	}
	if len(headers) > 0 {
		b.WriteString("<w:tr>")
		for _, h := range headers {
			cell(h, style.Accent, style.OnAccent, true)
		}
		b.WriteString("</w:tr>")
	}
	for i, row := range rows {
		fill := style.Surface
		if i%2 == 1 {
			fill = style.SoftAccent
		}
		b.WriteString("<w:tr>")
		for c := 0; c < max(len(headers), len(row)); c++ {
			val := ""
			if c < len(row) {
				val = row[c]
			}
			cell(val, fill, style.Ink, false)
		}
		b.WriteString("</w:tr>")
	}
	b.WriteString("</w:tbl>")
	return b.String()
}

func word(ctx context.Context, assets map[string]string, style profile, spec Spec, _ map[int]embeddedImage) ([]byte, error) {
	var body strings.Builder
	paragraph := func(text, color string, size int, bold bool, after int) {
		weight := ""
		if bold {
			weight = "<w:b/>"
		}
		for _, line := range strings.Split(text, "\n") {
			fmt.Fprintf(&body, `<w:p><w:pPr><w:spacing w:after="%d"/></w:pPr><w:r><w:rPr><w:rFonts w:ascii="Arial" w:eastAsia="%s" w:hAnsi="Arial"/>%s<w:color w:val="%s"/><w:sz w:val="%d"/></w:rPr><w:t xml:space="preserve">%s</w:t></w:r></w:p>`, after, escape(style.Font), weight, color, size, escape(line))
		}
	}
	paragraph(spec.Title, style.Ink, 48, true, 180)
	if strings.TrimSpace(spec.Subtitle) != "" {
		paragraph(spec.Subtitle, style.Muted, 24, false, 120)
	}
	if strings.TrimSpace(spec.Author) != "" {
		paragraph(spec.Author, style.Muted, 20, false, 240)
	}
	for _, s := range spec.Sections {
		if s.Heading != "" {
			paragraph(s.Heading, style.Accent, 32, true, 120)
		}
		if strings.TrimSpace(s.Subtitle) != "" {
			paragraph(s.Subtitle, style.Muted, 22, false, 100)
		}
		for _, p := range s.Paragraphs {
			paragraph(p, style.Ink, 22, false, 120)
		}
		for _, b := range s.Bullets {
			paragraph("• "+b, style.Ink, 22, false, 90)
		}
		if len(s.Metrics) > 0 {
			rows := make([][]string, 0, len(s.Metrics))
			for _, m := range s.Metrics {
				rows = append(rows, []string{m.Label, m.Value, m.Note})
			}
			body.WriteString(wordTable(style, []string{"指标", "数值", "说明"}, rows))
			paragraph("", style.Muted, 12, false, 80)
		}
		for _, col := range s.Columns {
			if col.Heading != "" {
				paragraph(col.Heading, style.Accent, 24, true, 80)
			}
			for _, p := range col.Paragraphs {
				paragraph(p, style.Ink, 22, false, 90)
			}
			for _, b := range col.Bullets {
				paragraph("• "+b, style.Ink, 22, false, 80)
			}
		}
		if s.Table != nil {
			body.WriteString(wordTable(style, s.Table.Headers, s.Table.Rows))
			paragraph("", style.Muted, 12, false, 100)
		}
	}
	doc, err := tmpl(assets["document.xml"], map[string]string{"Body": body.String()})
	if err != nil {
		return nil, err
	}
	parts := map[string][]byte{
		"word/document.xml": []byte(doc),
	}
	rels := []relationship{{id: "rId1", kind: "officeDocument", target: "word/document.xml"}}
	hasPNG := false
	if thumb, thumbErr := renderDocThumbnail(ctx, style, spec); thumbErr == nil && len(thumb) > 0 {
		parts["docProps/thumbnail.png"] = thumb
		rels = append(rels, relationship{id: "rId2", kind: "metadata/thumbnail", target: "docProps/thumbnail.png", pkg: true})
		hasPNG = true
	}
	parts["[Content_Types].xml"] = []byte(types(map[string]string{"word/document.xml": "wordprocessingml.document.main+xml"}, hasPNG, false))
	parts["_rels/.rels"] = []byte(relationships(rels...))
	return archive(parts)
}

func rectShape(id int, x, y, w, h int, fill, border string, rounded bool) string {
	geom := "rect"
	if rounded {
		geom = "roundRect"
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<p:sp><p:nvSpPr><p:cNvPr id="%d" name="Shape %d"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="%s"><a:avLst/></a:prstGeom>`, id, id, x, y, w, h, geom)
	if fill != "" {
		fmt.Fprintf(&b, `<a:solidFill><a:srgbClr val="%s"/></a:solidFill>`, fill)
	} else {
		b.WriteString(`<a:noFill/>`)
	}
	if border != "" {
		fmt.Fprintf(&b, `<a:ln w="12700"><a:solidFill><a:srgbClr val="%s"/></a:solidFill></a:ln>`, border)
	} else {
		b.WriteString(`<a:ln><a:noFill/></a:ln>`)
	}
	b.WriteString(`</p:spPr><p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:endParaRPr lang="zh-CN"/></a:p></p:txBody></p:sp>`)
	return b.String()
}

func textShape(id int, x, y, w, h int, size int, color, font string, bold bool, lines []string) string {
	return textShapeAligned(id, x, y, w, h, size, color, font, bold, "l", lines)
}

func textShapeAligned(id int, x, y, w, h int, size int, color, font string, bold bool, algn string, lines []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<p:sp><p:nvSpPr><p:cNvPr id="%d" name="Text %d"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/></p:spPr><p:txBody><a:bodyPr wrap="square" lIns="45720" tIns="22860" rIns="45720" bIns="22860"/><a:lstStyle/>`, id, id, x, y, w, h)
	weight := ""
	if bold {
		weight = ` b="1"`
	}
	for _, line := range lines {
		fmt.Fprintf(&b, `<a:p><a:pPr algn="%s"><a:spcAft><a:spcPts val="600"/></a:spcAft></a:pPr><a:r><a:rPr lang="zh-CN" sz="%d"%s><a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:latin typeface="Arial"/><a:ea typeface="%s"/></a:rPr><a:t>%s</a:t></a:r><a:endParaRPr lang="zh-CN" sz="%d"/></a:p>`, algn, size, weight, color, escape(font), escape(line), size)
	}
	b.WriteString("</p:txBody></p:sp>")
	return b.String()
}

func slideTableShape(id int, x, y, w, h int, style profile, tbl Table) string {
	cols := len(tbl.Headers)
	if cols == 0 {
		return ""
	}
	colW := w / cols
	rowsCount := len(tbl.Rows) + 1
	rowH := min(620000, max(360000, h/max(1, rowsCount)))
	var b strings.Builder
	fmt.Fprintf(&b, `<p:graphicFrame><p:nvGraphicFramePr><p:cNvPr id="%d" name="Table %d"/><p:cNvGraphicFramePr><a:graphicFrameLocks noGrp="1"/></p:cNvGraphicFramePr><p:nvPr/></p:nvGraphicFramePr><p:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></p:xfrm><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/table"><a:tbl><a:tblPr firstRow="1" bandRow="1"/><a:tblGrid>`, id, id, x, y, w, rowH*rowsCount)
	for i := 0; i < cols; i++ {
		fmt.Fprintf(&b, `<a:gridCol w="%d"/>`, colW)
	}
	b.WriteString(`</a:tblGrid>`)
	writeCell := func(text, fill, fg string, bold bool, sz int) {
		weight := ""
		if bold {
			weight = ` b="1"`
		}
		fmt.Fprintf(&b, `<a:tc><a:txBody><a:bodyPr/><a:lstStyle/><a:p><a:r><a:rPr lang="zh-CN" sz="%d"%s><a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:latin typeface="Arial"/><a:ea typeface="%s"/></a:rPr><a:t>%s</a:t></a:r><a:endParaRPr lang="zh-CN" sz="%d"/></a:p></a:txBody><a:tcPr anchor="ctr"><a:lnL w="12700"><a:solidFill><a:srgbClr val="%s"/></a:solidFill></a:lnL><a:lnR w="12700"><a:solidFill><a:srgbClr val="%s"/></a:solidFill></a:lnR><a:lnT w="12700"><a:solidFill><a:srgbClr val="%s"/></a:solidFill></a:lnT><a:lnB w="12700"><a:solidFill><a:srgbClr val="%s"/></a:solidFill></a:lnB><a:solidFill><a:srgbClr val="%s"/></a:solidFill></a:tcPr></a:tc>`, sz, weight, fg, escape(style.Font), escape(text), sz, style.Border, style.Border, style.Border, style.Border, fill)
	}
	fmt.Fprintf(&b, `<a:tr h="%d">`, rowH)
	for _, hdr := range tbl.Headers {
		writeCell(hdr, style.Accent, style.OnAccent, true, 1600)
	}
	b.WriteString(`</a:tr>`)
	for rIdx, row := range tbl.Rows {
		fill := style.Surface
		if rIdx%2 == 1 {
			fill = style.SoftAccent
		}
		fmt.Fprintf(&b, `<a:tr h="%d">`, rowH)
		for c := 0; c < cols; c++ {
			val := ""
			if c < len(row) {
				val = row[c]
			}
			writeCell(val, fill, style.Ink, false, 1500)
		}
		b.WriteString(`</a:tr>`)
	}
	b.WriteString(`</a:tbl></a:graphicData></a:graphic></p:graphicFrame>`)
	return b.String()
}

func slidePicShape(id int, rID, alt string, boxX, boxY, boxW, boxH, imgW, imgH int) string {
	tw, th := boxW, boxH
	if imgW > 0 && imgH > 0 {
		scaledH := int(int64(boxW) * int64(imgH) / int64(imgW))
		if scaledH <= boxH {
			th = scaledH
		} else {
			tw = int(int64(boxH) * int64(imgW) / int64(imgH))
		}
	}
	ox := boxX + (boxW-tw)/2
	oy := boxY + (boxH-th)/2
	return fmt.Sprintf(`<p:pic><p:nvPicPr><p:cNvPr id="%d" name="Picture %d" descr="%s"/><p:cNvPicPr><a:picLocks noChangeAspect="1"/></p:cNvPicPr><p:nvPr/></p:nvPicPr><p:blipFill><a:blip r:embed="%s"/><a:stretch><a:fillRect/></a:stretch></p:blipFill><p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr></p:pic>`, id, id, escape(alt), rID, ox, oy, tw, th)
}

func notesSlideXML(notes string, fontName string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><p:notes%s><p:cSld><p:spTree>%s<p:sp><p:nvSpPr><p:cNvPr id="2" name="Notes Placeholder"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr><p:ph type="body" idx="1"/></p:nvPr></p:nvSpPr><p:spPr/><p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:r><a:rPr lang="zh-CN" sz="1200"><a:latin typeface="Arial"/><a:ea typeface="%s"/></a:rPr><a:t>%s</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:notes>`, slideNS, group, escape(fontName), escape(notes))
}

func slideXMLWithBG(bgHex, shapes string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><p:sld%s><p:cSld><p:bg><p:bgPr><a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:effectLst/></p:bgPr></p:bg><p:spTree>%s%s</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`, slideNS, bgHex, group, shapes)
}

// Slide geometry uses an explicit line budget. Long sections become additional
// editable slides instead of overflowing the slide boundary.
func wrapped(s string, columns int) []string {
	var lines []string
	for _, paragraph := range strings.Split(s, "\n") {
		var line strings.Builder
		used := 0
		for _, r := range paragraph {
			w := 1
			if r > 127 {
				w = 2
			}
			if used+w > columns {
				lines = append(lines, line.String())
				line.Reset()
				used = 0
			}
			line.WriteRune(r)
			used += w
		}
		lines = append(lines, line.String())
	}
	return lines
}

type builtSlide struct {
	shapes   string
	extraRel []relationship
	notes    string
	card     slidePreviewCard
}

func slides(ctx context.Context, assets map[string]string, style profile, spec Spec, media map[int]embeddedImage) ([]byte, int, error) {
	parts := map[string][]byte{}
	overrides := map[string]string{
		"ppt/presentation.xml":            "presentationml.presentation.main+xml",
		"ppt/slideMasters/slideMaster1.xml": "presentationml.slideMaster+xml",
		"ppt/slideLayouts/slideLayout1.xml": "presentationml.slideLayout+xml",
		"ppt/theme/theme1.xml":            "theme+xml",
	}
	parts["ppt/theme/theme1.xml"] = []byte(assets["theme.xml"])
	parts["ppt/slideMasters/slideMaster1.xml"] = []byte("<p:sldMaster" + slideNS + "><p:cSld><p:spTree>" + group + `</p:spTree></p:cSld><p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/><p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst><p:txStyles><p:titleStyle/><p:bodyStyle/><p:otherStyle/></p:txStyles></p:sldMaster>`)
	parts["ppt/slideMasters/_rels/slideMaster1.xml.rels"] = []byte(relationships(
		relationship{id: "rId1", kind: "slideLayout", target: "../slideLayouts/slideLayout1.xml"},
		relationship{id: "rId2", kind: "theme", target: "../theme/theme1.xml"},
	))
	parts["ppt/slideLayouts/slideLayout1.xml"] = []byte("<p:sldLayout" + slideNS + ` type="blank" preserve="1"><p:cSld name="Blank"><p:spTree>` + group + `</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>`)
	parts["ppt/slideLayouts/_rels/slideLayout1.xml.rels"] = []byte(relationships(
		relationship{id: "rId1", kind: "slideMaster", target: "../slideMasters/slideMaster1.xml"},
	))

	var built []builtSlide
	titleLines := wrapped(spec.Title, 32)
	if len(titleLines) > 5 {
		return nil, 0, fmt.Errorf("封面标题超过可用版面，请缩短标题")
	}
	// Cover slide
	{
		var sh strings.Builder
		sh.WriteString(rectShape(2, 0, 0, 420000, 6858000, style.Accent, "", false))
		sh.WriteString(rectShape(3, 8950000, 520000, 2650000, 1750000, style.SoftAccent, style.Border, true))
		sh.WriteString(rectShape(4, 1050000, 1650000, 1450000, 95000, style.Accent, "", false))
		sh.WriteString(textShape(5, 1050000, 1950000, 9800000, 2500000, 4200, style.Ink, style.Font, true, titleLines))
		nextID := 6
		if strings.TrimSpace(spec.Subtitle) != "" {
			subLines := wrapped(spec.Subtitle, 52)
			sh.WriteString(textShape(nextID, 1050000, 4250000, 9600000, 1100000, 2200, style.Muted, style.Font, false, subLines[:min(3, len(subLines))]))
			nextID++
		}
		footerMeta := strings.TrimSpace(spec.Author)
		if footerMeta != "" {
			sh.WriteString(textShape(nextID, 1050000, 5950000, 9600000, 450000, 1600, style.Muted, style.Font, false, []string{footerMeta}))
		}
		built = append(built, builtSlide{
			shapes: sh.String(),
			card:   slidePreviewCard{Cover: true, Title: spec.Title, Subtitle: spec.Subtitle, Footer: footerMeta},
		})
	}

	hasPNG, hasJPG := false, false
	mediaCounter := 0

	for secIdx, s := range spec.Sections {
		headLines := wrapped(s.Heading, 46)
		if len(headLines) > 2 {
			return nil, 0, fmt.Errorf("章节标题超过两行，请缩短")
		}
		layout := strings.ToLower(strings.TrimSpace(s.Layout))
		if layout == "" {
			switch {
			case len(s.Metrics) > 0:
				layout = "metrics"
			case s.Table != nil:
				layout = "table"
			case len(s.Columns) > 0:
				layout = "two_column"
			default:
				layout = "bullets"
			}
		}

		headerShapes := func(titleTxt []string, idStart int) (string, int, int) {
			var h strings.Builder
			h.WriteString(rectShape(idStart, 0, 0, 12192000, 110000, style.Accent, "", false))
			h.WriteString(textShape(idStart+1, 680000, 340000, 10830000, 820000, 2800, style.Accent, style.Font, true, titleTxt))
			id := idStart + 2
			bodyTop := 1250000
			if strings.TrimSpace(s.Subtitle) != "" {
				sub := wrapped(s.Subtitle, 66)
				h.WriteString(textShape(id, 680000, 1020000, 10830000, 420000, 1600, style.Muted, style.Font, false, sub[:min(1, len(sub))]))
				id++
				bodyTop = 1520000
			}
			return h.String(), id, bodyTop
		}

		img, hasImg := media[secIdx]
		var imgRel relationship
		if hasImg {
			mediaCounter++
			mediaFile := fmt.Sprintf("ppt/media/image%d.%s", mediaCounter, img.Ext)
			parts[mediaFile] = img.Bytes
			if img.Ext == "png" {
				hasPNG = true
			} else {
				hasJPG = true
			}
			imgRel = relationship{id: "rId2", kind: "image", target: fmt.Sprintf("../media/image%d.%s", mediaCounter, img.Ext)}
		}

		switch {
		case layout == "metrics" && len(s.Metrics) > 0:
			hdr, id, bodyTop := headerShapes(headLines, 2)
			var sh strings.Builder
			sh.WriteString(hdr)
			n := min(4, len(s.Metrics))
			gap := 240000
			totalW := 10830000
			cardW := (totalW - gap*(n-1)) / n
			cardH := 2650000
			for i := 0; i < n; i++ {
				m := s.Metrics[i]
				x0 := 680000 + i*(cardW+gap)
				sh.WriteString(rectShape(id, x0, bodyTop+120000, cardW, cardH, style.Surface, style.Border, true))
				sh.WriteString(rectShape(id+1, x0, bodyTop+120000, cardW, 95000, style.Accent, "", false))
				sh.WriteString(textShape(id+2, x0+180000, bodyTop+420000, cardW-360000, 820000, 3400, style.Accent, style.Font, true, []string{m.Value}))
				sh.WriteString(textShape(id+3, x0+180000, bodyTop+1320000, cardW-360000, 560000, 1900, style.Ink, style.Font, true, wrapped(m.Label, 18)[:min(2, len(wrapped(m.Label, 18)))]))
				id += 4
				if m.Note != "" {
					notes := wrapped(m.Note, 22)
					sh.WriteString(textShape(id, x0+180000, bodyTop+1950000, cardW-360000, 720000, 1400, style.Muted, style.Font, false, notes[:min(2, len(notes))]))
					id++
				}
			}
			extraLines := []string{}
			for _, p := range s.Paragraphs {
				extraLines = append(extraLines, wrapped(p, 64)...)
			}
			for _, b := range s.Bullets {
				extraLines = append(extraLines, wrapped("● "+b, 64)...)
			}
			if len(extraLines) > 0 {
				sh.WriteString(rectShape(id, 680000, bodyTop+3020000, 10830000, 1650000, style.SoftAccent, style.Border, true))
				sh.WriteString(textShape(id+1, 880000, bodyTop+3200000, 10430000, 1300000, 1700, style.Ink, style.Font, false, extraLines[:min(4, len(extraLines))]))
			}
			built = append(built, builtSlide{
				shapes: sh.String(),
				notes:  s.Notes,
				card:   slidePreviewCard{Title: s.Heading, Subtitle: s.Subtitle, Metrics: s.Metrics, Bullets: append(append([]string{}, s.Paragraphs...), s.Bullets...)},
			})

		case layout == "table" && s.Table != nil && len(s.Table.Headers) > 0:
			rowsPerSlide := 7
			totalRows := len(s.Table.Rows)
			for start := 0; start == 0 || start < totalRows; start += rowsPerSlide {
				end := min(totalRows, start+rowsPerSlide)
				titleTxt := headLines
				if start > 0 {
					titleTxt = wrapped(s.Heading+"（续）", 46)
				}
				hdr, id, bodyTop := headerShapes(titleTxt, 2)
				var sh strings.Builder
				sh.WriteString(hdr)
				subTbl := Table{Headers: s.Table.Headers, Rows: s.Table.Rows[start:end]}
				sh.WriteString(slideTableShape(id, 680000, bodyTop+120000, 10830000, 4500000, style, subTbl))
				built = append(built, builtSlide{
					shapes: sh.String(),
					notes:  s.Notes,
					card:   slidePreviewCard{Title: s.Heading, Subtitle: s.Subtitle, Table: &subTbl},
				})
				if end >= totalRows {
					break
				}
			}

		case (layout == "two_column" || layout == "cards") && len(s.Columns) > 0:
			hdr, id, bodyTop := headerShapes(headLines, 2)
			var sh strings.Builder
			sh.WriteString(hdr)
			n := min(3, len(s.Columns))
			gap := 240000
			totalW := 10830000
			colW := (totalW - gap*(n-1)) / n
			colH := 6150000 - bodyTop
			maxChars := max(20, 56/n)
			for i := 0; i < n; i++ {
				col := s.Columns[i]
				x0 := 680000 + i*(colW+gap)
				sh.WriteString(rectShape(id, x0, bodyTop+80000, colW, colH, style.Surface, style.Border, true))
				id++
				contentY := bodyTop + 260000
				if col.Heading != "" {
					sh.WriteString(textShape(id, x0+180000, contentY, colW-360000, 520000, 2100, style.Accent, style.Font, true, wrapped(col.Heading, maxChars)[:min(2, len(wrapped(col.Heading, maxChars)))]))
					id++
					contentY += 620000
				}
				lines := []string{}
				for _, p := range col.Paragraphs {
					lines = append(lines, wrapped(p, maxChars)...)
				}
				for _, b := range col.Bullets {
					lines = append(lines, wrapped("● "+b, maxChars)...)
				}
				if len(lines) > 0 {
					sh.WriteString(textShape(id, x0+180000, contentY, colW-360000, colH-(contentY-bodyTop)-220000, 1700, style.Ink, style.Font, false, lines[:min(9, len(lines))]))
					id++
				}
			}
			built = append(built, builtSlide{
				shapes: sh.String(),
				notes:  s.Notes,
				card:   slidePreviewCard{Title: s.Heading, Subtitle: s.Subtitle, Columns: s.Columns},
			})

		case layout == "quote":
			hdr, id, bodyTop := headerShapes(headLines, 2)
			var sh strings.Builder
			sh.WriteString(hdr)
			sh.WriteString(rectShape(id, 980000, bodyTop+450000, 10230000, 3200000, style.SoftAccent, style.Border, true))
			sh.WriteString(rectShape(id+1, 980000, bodyTop+450000, 160000, 3200000, style.Accent, "", false))
			quoteLines := []string{}
			for _, p := range append(append([]string{}, s.Paragraphs...), s.Bullets...) {
				quoteLines = append(quoteLines, wrapped(p, 44)...)
			}
			sh.WriteString(textShape(id+2, 1460000, bodyTop+880000, 9300000, 2400000, 2600, style.Ink, style.Font, true, quoteLines[:min(5, len(quoteLines))]))
			built = append(built, builtSlide{
				shapes: sh.String(),
				notes:  s.Notes,
				card:   slidePreviewCard{Title: s.Heading, Subtitle: s.Subtitle, Bullets: quoteLines},
			})

		default:
			wrapCols := 60
			if hasImg {
				wrapCols = 32
			}
			lines := []string{}
			for _, p := range s.Paragraphs {
				lines = append(lines, wrapped(p, wrapCols)...)
			}
			for _, b := range s.Bullets {
				lines = append(lines, wrapped("● "+b, wrapCols)...)
			}
			perPage := 8
			for first := true; first || len(lines) > 0; first = false {
				take := min(perPage, len(lines))
				pageLines := lines[:take]
				titleTxt := headLines
				if !first {
					titleTxt = wrapped(s.Heading+"（续）", 46)
				}
				hdr, id, bodyTop := headerShapes(titleTxt, 2)
				var sh strings.Builder
				sh.WriteString(hdr)
				boxH := 6150000 - bodyTop
				var extraRels []relationship
				var cardImg *embeddedImage
				if hasImg && first {
					leftW := 5250000
					sh.WriteString(rectShape(id, 680000, bodyTop+80000, leftW, boxH, style.Surface, style.Border, true))
					sh.WriteString(textShape(id+1, 900000, bodyTop+280000, leftW-440000, boxH-360000, 1900, style.Ink, style.Font, false, pageLines))
					sh.WriteString(rectShape(id+2, 6170000, bodyTop+80000, 5340000, boxH, style.Surface, style.Border, true))
					sh.WriteString(slidePicShape(id+3, imgRel.id, s.ImageAlt, 6290000, bodyTop+200000, 5100000, boxH-240000, img.Width, img.Height))
					extraRels = append(extraRels, imgRel)
					imgCopy := img
					cardImg = &imgCopy
				} else {
					sh.WriteString(rectShape(id, 680000, bodyTop+80000, 10830000, boxH, style.Surface, style.Border, true))
					sh.WriteString(textShape(id+1, 920000, bodyTop+280000, 10350000, boxH-360000, 2000, style.Ink, style.Font, false, pageLines))
				}
				built = append(built, builtSlide{
					shapes:   sh.String(),
					extraRel: extraRels,
					notes:    s.Notes,
					card:     slidePreviewCard{Title: s.Heading, Subtitle: s.Subtitle, Bullets: pageLines, Image: cardImg},
				})
				lines = lines[take:]
			}
		}
	}

	if len(built) > 240 {
		return nil, 0, fmt.Errorf("幻灯片超过 240 页，请拆分文稿")
	}

	totalSlides := len(built)
	presLinks := []relationship{{id: "rId1", kind: "slideMaster", target: "slideMasters/slideMaster1.xml"}}
	var slideIDs strings.Builder
	previewCards := make([]slidePreviewCard, 0, totalSlides)

	for i := range built {
		num := i + 1
		sl := &built[i]
		pageLabel := fmt.Sprintf("%02d / %02d", num, totalSlides)
		sl.card.PageNum = pageLabel
		if !sl.card.Cover {
			sl.card.Footer = spec.Title
			footer := textShape(900, 680000, 6360000, 8200000, 340000, 1200, style.Muted, style.Font, false, []string{spec.Title}) +
				textShapeAligned(901, 9600000, 6360000, 1910000, 340000, 1200, style.Muted, style.Font, false, "r", []string{pageLabel})
			sl.shapes += footer
		}
		previewCards = append(previewCards, sl.card)

		slideName := fmt.Sprintf("slides/slide%d.xml", num)
		slidePath := "ppt/" + slideName
		parts[slidePath] = []byte(slideXMLWithBG(style.Background, sl.shapes))
		overrides[slidePath] = "presentationml.slide+xml"

		slideRels := []relationship{{id: "rId1", kind: "slideLayout", target: "../slideLayouts/slideLayout1.xml"}}
		slideRels = append(slideRels, sl.extraRel...)
		if strings.TrimSpace(sl.notes) != "" {
			notesName := fmt.Sprintf("notesSlides/notesSlide%d.xml", num)
			notesPath := "ppt/" + notesName
			notesRelID := fmt.Sprintf("rId%d", len(slideRels)+1)
			slideRels = append(slideRels, relationship{id: notesRelID, kind: "notesSlide", target: "../" + notesName})
			parts[notesPath] = []byte(notesSlideXML(sl.notes, style.Font))
			overrides[notesPath] = "presentationml.notesSlide+xml"
			parts[fmt.Sprintf("ppt/notesSlides/_rels/notesSlide%d.xml.rels", num)] = []byte(relationships(
				relationship{id: "rId1", kind: "slide", target: "../" + slideName},
			))
		}
		parts[fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", num)] = []byte(relationships(slideRels...))

		presRelID := fmt.Sprintf("rId%d", num+1)
		presLinks = append(presLinks, relationship{id: presRelID, kind: "slide", target: slideName})
		fmt.Fprintf(&slideIDs, `<p:sldId id="%d" r:id="%s"/>`, 255+num, presRelID)
	}

	parts["ppt/presentation.xml"] = []byte("<p:presentation" + slideNS + `><p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst><p:sldIdLst>` + slideIDs.String() + `</p:sldIdLst><p:sldSz cx="12192000" cy="6858000" type="screen16x9"/><p:notesSz cx="6858000" cy="9144000"/></p:presentation>`)
	parts["ppt/_rels/presentation.xml.rels"] = []byte(relationships(presLinks...))

	rootRels := []relationship{{id: "rId1", kind: "officeDocument", target: "ppt/presentation.xml"}}
	if thumb, thumbErr := renderDeckThumbnail(ctx, style, previewCards); thumbErr == nil && len(thumb) > 0 {
		parts["docProps/thumbnail.png"] = thumb
		rootRels = append(rootRels, relationship{id: "rId2", kind: "metadata/thumbnail", target: "docProps/thumbnail.png", pkg: true})
		hasPNG = true
	}
	parts["_rels/.rels"] = []byte(relationships(rootRels...))
	parts["[Content_Types].xml"] = []byte(types(overrides, hasPNG, hasJPG))

	data, err := archive(parts)
	return data, totalSlides, err
}
