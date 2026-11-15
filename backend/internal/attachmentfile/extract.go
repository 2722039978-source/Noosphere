// Package attachmentfile extracts text from explicitly selected file bytes.
package attachmentfile

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

const MaxBytes = 8 << 20
const maxTextBytes = 1 << 20

type Section struct {
	Text      string
	Page      int
	StartLine int
	EndLine   int
}

var textExtensions = strings.Fields(".txt .log .md .markdown .rst .adoc .json .jsonl .yaml .yml .toml .ini .cfg .conf .properties .xml .html .htm .css .scss .csv .tsv .go .mod .sum .py .pyw .rs .js .jsx .mjs .cjs .ts .tsx .vue .svelte .astro .java .kt .kts .cs .fs .vb .c .h .cpp .hpp .cc .swift .rb .php .sh .bash .zsh .ps1 .psm1 .bat .cmd .sql .lua .r .dart .gd .tf .gitignore .editorconfig .gitattributes")

func ValidateName(name string) error {
	if strings.TrimSpace(name) == "" || len(name) > 1040 || name == "." || name == ".." || strings.ContainsAny(name, "/\\:") ||
		strings.ContainsFunc(name, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) {
		return fmt.Errorf("附件名称无效，请重新选择本地文件")
	}
	return nil
}

func Extract(ctx context.Context, name string, raw []byte) ([]Section, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	if len(raw) == 0 || len(raw) > MaxBytes {
		return nil, fmt.Errorf("附件为空或大于每份 8 MB 的上限")
	}
	switch ext := strings.ToLower(filepath.Ext(name)); ext {
	case ".pdf":
		return runPDF(ctx, raw)
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".heic", ".bmp", ".tif", ".tiff", ".svg":
		return nil, fmt.Errorf("当前对话接口仅传输文本，无法读取图片；请粘贴图片文字，或先用 OCR 转成文本再添加")
	default:
		base := strings.ToLower(name)
		if !slices.Contains(textExtensions, ext) && !slices.Contains([]string{"readme", "license", "makefile", "dockerfile", "gemfile", "cmakelists"}, base) {
			return nil, fmt.Errorf("不支持此文件格式，请选择文本、代码或有文字层的 PDF")
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	text, err := decodeText(raw)
	if err != nil {
		return nil, err
	}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	if len(text) > maxTextBytes {
		return nil, fmt.Errorf("提取的文本超过 1 MB，请拆分后添加")
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("文件没有可用文字")
	}
	return []Section{{Text: text, StartLine: 1, EndLine: strings.Count(text, "\n") + 1}}, nil
}

func decodeText(raw []byte) (string, error) {
	const encodingProblem = "无法读取文本编码；请使用 UTF-8、带 BOM 的 UTF-16 或 GB18030"
	var text string
	switch {
	case bytes.HasPrefix(raw, []byte{0xff, 0xfe}), bytes.HasPrefix(raw, []byte{0xfe, 0xff}):
		if len(raw)%2 != 0 {
			return "", fmt.Errorf("%s", encodingProblem)
		}
		var order binary.ByteOrder = binary.LittleEndian
		if raw[0] == 0xfe {
			order = binary.BigEndian
		}
		words := make([]uint16, 0, (len(raw)-2)/2)
		for i := 2; i < len(raw); i += 2 {
			words = append(words, order.Uint16(raw[i:i+2]))
		}
		text = string(utf16.Decode(words))
	case utf8.Valid(raw):
		text = string(bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf}))
	default:
		converted, err := simplifiedchinese.GB18030.NewDecoder().Bytes(raw)
		if err != nil {
			return "", fmt.Errorf("%s", encodingProblem)
		}
		text = string(converted)
	}
	if strings.ContainsFunc(text, func(r rune) bool {
		return r == utf8.RuneError || unicode.IsControl(r) && r != '\r' && r != '\n' && r != '\t'
	}) {
		return "", fmt.Errorf("%s", encodingProblem)
	}
	return text, nil
}
