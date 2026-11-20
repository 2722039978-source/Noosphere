package attachmentfile

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noosphere/backend/internal/ownedprocess"
)

func TestMain(m *testing.M) {
	ownedprocess.RunSupervisor()
	if len(os.Args) >= 2 && os.Args[1] == PDFWorkerFlag {
		var err error
		if len(os.Args) == 3 {
			err = RunPDFFileWorker(os.Args[2], os.Stdout)
		} else {
			err = RunPDFWorker(os.Stdin, os.Stdout)
		}
		if err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestTextExtractionPreservesChineseAndNormalizesLines(t *testing.T) {
	for _, test := range []struct {
		name string
		data []byte
	}{
		{"utf8", []byte("测试\r\ncase")},
		{"utf16le", []byte{0xff, 0xfe, 0x4b, 0x6d, 0xd5, 0x8b, '\r', 0, '\n', 0, 'c', 0, 'a', 0, 's', 0, 'e', 0}},
		{"gb18030", []byte{0xb2, 0xe2, 0xca, 0xd4, '\r', '\n', 'c', 'a', 's', 'e'}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := Extract(context.Background(), "中文.txt", test.data)
			if err != nil || len(got) != 1 || got[0].Text != "测试\ncase" || got[0].StartLine != 1 || got[0].EndLine != 2 {
				t.Fatalf("sections=%+v err=%v", got, err)
			}
			path := filepath.Join(t.TempDir(), "中文.txt")
			if err := os.WriteFile(path, test.data, 0600); err != nil {
				t.Fatal(err)
			}
			encoding, err := DetectTextEncoding(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			var streamed strings.Builder
			err = ScanTextFile(t.Context(), path, encoding, func(r rune) { streamed.WriteRune(r) })
			if err != nil || streamed.String() != got[0].Text {
				t.Fatalf("streamed text=%q err=%v", streamed.String(), err)
			}
		})
	}
}

func TestTextAdapterExplainsUnusableFiles(t *testing.T) {
	for _, test := range []struct {
		name    string
		content []byte
		message string
	}{
		{"payload.zip", []byte{0x50, 0x4b}, "不支持"},
		{"image.png", []byte{0x89, 0x50}, "仅传输文本"},
		{"fake.txt", []byte{0x00, 0x01}, "文本编码"},
		{"oversized.txt", make([]byte, MaxBytes+1), "200 MB"},
		{"../unsafe.txt", []byte("test"), "名称无效"},
	} {
		_, err := Extract(context.Background(), test.name, test.content)
		if err == nil || !strings.Contains(err.Error(), test.message) {
			t.Fatalf("%s: %v", test.name, err)
		}
	}
}

func textPDFFixture(first, second string) []byte {
	pdf := "%PDF-1.4\n"
	xref := []int{0}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 6 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 7 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	for _, value := range []string{first, second} {
		stream := fmt.Sprintf("BT /F1 12 Tf 40 740 Td (%s) Tj ET", value)
		objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	}
	for i, obj := range objects {
		xref = append(xref, len(pdf))
		pdf += fmt.Sprintf("%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	offset := len(pdf)
	pdf += fmt.Sprintf("xref\n0 %d\n0000000000 65535 f \n", len(xref))
	for _, pos := range xref[1:] {
		pdf += fmt.Sprintf("%010d 00000 n \n", pos)
	}
	pdf += fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(xref), offset)
	return []byte(pdf)
}

func TestPDFPagesAndNoTextBoundary(t *testing.T) {
	result := readPDF(bytes.NewReader(textPDFFixture("first datum", "second datum")))
	if result.Error != "" || len(result.Sections) != 2 || result.Sections[0].Page != 1 || result.Sections[1].Page != 2 ||
		!strings.Contains(result.Sections[1].Text, "second datum") {
		t.Fatalf("unexpected extraction %+v", result)
	}
	result = readPDF(bytes.NewReader(textPDFFixture("", "")))
	if !strings.Contains(result.Error, "OCR") {
		t.Fatalf("image-only PDF must get a text alternative: %+v", result)
	}
}

func TestManagedPDFWorkerParsesOriginalAndStdin(t *testing.T) {
	raw := textPDFFixture("first datum", "second datum")
	file := filepath.Join(t.TempDir(), "附件.pdf")
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		read func(context.Context) ([]Section, error)
	}{
		{"stdin", func(ctx context.Context) ([]Section, error) { return Extract(ctx, "附件.pdf", raw) }},
		{"original", func(ctx context.Context) ([]Section, error) { return ExtractPDFFile(ctx, file) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			sections, err := test.read(t.Context())
			if err != nil || len(sections) != 2 || sections[1].Page != 2 || !strings.Contains(sections[1].Text, "second datum") {
				t.Fatalf("managed worker extraction: sections=%+v, err=%v", sections, err)
			}
		})
	}
}
