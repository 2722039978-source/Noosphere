package toolpack

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"html/template"
	"sort"
	"strings"
	textTemplate "text/template"
)

const relType = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
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
func webPage(assets map[string]string, spec Spec) ([]byte, error) {
	t, err := template.New("page").Parse(assets["page.html"])
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	err = t.Execute(&b, spec)
	return b.Bytes(), err
}

type relationship struct{ id, kind, target string }

func relationships(links ...relationship) string {
	var b strings.Builder
	b.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for _, r := range links {
		fmt.Fprintf(&b, `<Relationship Id="%s" Type="%s%s" Target="%s"/>`, r.id, relType, r.kind, r.target)
	}
	b.WriteString("</Relationships>")
	return b.String()
}
func types(parts map[string]string) string {
	var b strings.Builder
	b.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>`)
	for _, name := range keys(parts) {
		fmt.Fprintf(&b, `<Override PartName="/%s" ContentType="%s%s"/>`, name, typesPrefix, parts[name])
	}
	b.WriteString("</Types>")
	return b.String()
}
func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func archive(parts map[string]string) ([]byte, error) {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for _, name := range keys(parts) {
		w, err := z.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err = w.Write([]byte(parts[name])); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
func word(assets map[string]string, style profile, spec Spec) ([]byte, error) {
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
	paragraph(spec.Title, style.Ink, 48, true, 320)
	for _, s := range spec.Sections {
		if s.Heading != "" {
			paragraph(s.Heading, style.Accent, 32, true, 140)
		}
		for _, p := range s.Paragraphs {
			paragraph(p, style.Muted, 22, false, 120)
		}
	}
	doc, err := tmpl(assets["document.xml"], map[string]string{"Body": body.String()})
	if err != nil {
		return nil, err
	}
	return archive(map[string]string{"[Content_Types].xml": types(map[string]string{"word/document.xml": "wordprocessingml.document.main+xml"}), "_rels/.rels": relationships(relationship{"rId1", "officeDocument", "word/document.xml"}), "word/document.xml": doc})
}
func textShape(id int, x, y, w, h int, size int, color, font string, bold bool, lines []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<p:sp><p:nvSpPr><p:cNvPr id="%d" name="Text %d"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/></p:spPr><p:txBody><a:bodyPr wrap="none" lIns="0" tIns="0" rIns="0" bIns="0"/><a:lstStyle/>`, id, id, x, y, w, h)
	weight := ""
	if bold {
		weight = ` b="1"`
	}
	for _, line := range lines {
		fmt.Fprintf(&b, `<a:p><a:r><a:rPr lang="zh-CN" sz="%d"%s><a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:latin typeface="Arial"/><a:ea typeface="%s"/></a:rPr><a:t>%s</a:t></a:r><a:endParaRPr lang="zh-CN" sz="%d"/></a:p>`, size, weight, color, escape(font), escape(line), size)
	}
	b.WriteString("</p:txBody></p:sp>")
	return b.String()
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
func slides(assets map[string]string, style profile, spec Spec) ([]byte, int, error) {
	parts := map[string]string{}
	overrides := map[string]string{"ppt/presentation.xml": "presentationml.presentation.main+xml", "ppt/slideMasters/slideMaster1.xml": "presentationml.slideMaster+xml", "ppt/slideLayouts/slideLayout1.xml": "presentationml.slideLayout+xml", "ppt/theme/theme1.xml": "theme+xml"}
	parts["_rels/.rels"] = relationships(relationship{"rId1", "officeDocument", "ppt/presentation.xml"})
	parts["ppt/theme/theme1.xml"] = assets["theme.xml"]
	parts["ppt/slideMasters/slideMaster1.xml"] = "<p:sldMaster" + slideNS + "><p:cSld><p:spTree>" + group + `</p:spTree></p:cSld><p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/><p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst><p:txStyles><p:titleStyle/><p:bodyStyle/><p:otherStyle/></p:txStyles></p:sldMaster>`
	parts["ppt/slideMasters/_rels/slideMaster1.xml.rels"] = relationships(relationship{"rId1", "slideLayout", "../slideLayouts/slideLayout1.xml"}, relationship{"rId2", "theme", "../theme/theme1.xml"})
	parts["ppt/slideLayouts/slideLayout1.xml"] = "<p:sldLayout" + slideNS + ` type="blank" preserve="1"><p:cSld name="Blank"><p:spTree>` + group + `</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>`
	parts["ppt/slideLayouts/_rels/slideLayout1.xml.rels"] = relationships(relationship{"rId1", "slideMaster", "../slideMasters/slideMaster1.xml"})
	links := []relationship{{"rId1", "slideMaster", "slideMasters/slideMaster1.xml"}}
	var ids strings.Builder
	count := 0
	add := func(shapes string) error {
		count++
		if count > 240 {
			return fmt.Errorf("幻灯片超过 240 页，请拆分文稿")
		}
		id := fmt.Sprintf("rId%d", count+1)
		name := fmt.Sprintf("slides/slide%d.xml", count)
		data, err := tmpl(assets["slide.xml"], map[string]string{"Group": group, "Shapes": shapes})
		if err != nil {
			return err
		}
		parts["ppt/"+name] = data
		overrides["ppt/"+name] = "presentationml.slide+xml"
		parts[fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", count)] = relationships(relationship{"rId1", "slideLayout", "../slideLayouts/slideLayout1.xml"})
		links = append(links, relationship{id, "slide", name})
		fmt.Fprintf(&ids, `<p:sldId id="%d" r:id="%s"/>`, 255+count, id)
		return nil
	}
	title := wrapped(spec.Title, 34)
	if len(title) > 5 {
		return nil, 0, fmt.Errorf("封面标题超过可用版面，请缩短标题")
	}
	if err := add(textShape(2, 750000, 1400000, 10800000, 4200000, 4200, style.Ink, style.Font, true, title)); err != nil {
		return nil, 0, err
	}
	for _, s := range spec.Sections {
		titles := wrapped(s.Heading, 46)
		if len(titles) > 2 {
			return nil, 0, fmt.Errorf("章节标题超过两行，请缩短")
		}
		lines := []string{}
		for _, p := range s.Paragraphs {
			lines = append(lines, wrapped(p, 64)...)
		}
		for first := true; first || len(lines) > 0; first = false {
			take := min(7, len(lines))
			shapes := textShape(2, 750000, 480000, 10800000, 1300000, 3000, style.Accent, style.Font, true, titles) + textShape(3, 750000, 2000000, 10800000, 4350000, 2100, style.Ink, style.Font, false, lines[:take])
			if err := add(shapes); err != nil {
				return nil, 0, err
			}
			lines = lines[take:]
		}
	}
	parts["ppt/presentation.xml"] = "<p:presentation" + slideNS + `><p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst><p:sldIdLst>` + ids.String() + `</p:sldIdLst><p:sldSz cx="12192000" cy="6858000" type="screen16x9"/><p:notesSz cx="6858000" cy="9144000"/></p:presentation>`
	parts["ppt/_rels/presentation.xml.rels"] = relationships(links...)
	parts["[Content_Types].xml"] = types(overrides)
	data, err := archive(parts)
	return data, count, err
}
