package attachmentfile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ledongthuc/pdf"
	"github.com/noosphere/backend/internal/ownedprocess"
)

const PDFWorkerFlag = "--attachment-pdf"
const maxPDFPages = 100

type pdfResult struct {
	Sections []Section
	Error    string
}

// PDF parsing takes place in a short-lived child without server credentials.
// Cancelling the request or exceeding the deadline kills the child.
func runPDF(parent context.Context, raw []byte) ([]Section, error) {
	return runPDFWorker(parent, 8*time.Second, bytes.NewReader(raw))
}

// ExtractPDFFile lets the isolated parser seek through the uploaded file
// directly, including PDFs larger than the worker's memory budget.
func ExtractPDFFile(parent context.Context, path string) ([]Section, error) {
	return runPDFWorker(parent, 45*time.Second, nil, path)
}

func runPDFWorker(parent context.Context, timeout time.Duration, input io.Reader, args ...string) ([]Section, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("无法打开 PDF 提取组件")
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := exec.Command(exe, append([]string{PDFWorkerFlag}, args...)...)
	cmd.Env = []string{"GOMEMLIMIT=96MiB", "GOMAXPROCS=2", "SystemRoot=" + os.Getenv("SystemRoot")}
	cmd.Stdin = input
	var output limitedWriter
	cmd.Stdout = &output
	if err := ownedprocess.Run(ctx, cmd, nil); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("PDF 提取已取消或超时，试试拆分 PDF 或导出为文本")
		}
		return nil, fmt.Errorf("PDF 提取失败，请确认文件可打开，或导出为文本")
	}
	var result pdfResult
	if json.Unmarshal(output.Bytes(), &result) != nil {
		return nil, fmt.Errorf("PDF 提取结果无效")
	}
	if result.Error != "" {
		return nil, fmt.Errorf("%s", result.Error)
	}
	return result.Sections, nil
}

// Reject before copying an unexpectedly large worker response into the host.
type limitedWriter struct{ bytes.Buffer }

func (w *limitedWriter) Write(data []byte) (int, error) {
	if w.Len()+len(data) > 7*maxTextBytes {
		return 0, fmt.Errorf("PDF 输出超过上限")
	}
	return w.Buffer.Write(data)
}

func RunPDFWorker(input io.Reader, output io.Writer) error {
	result := pdfResult{}
	if err := boundPDFProcess(); err != nil {
		result.Error = "无法为 PDF 提取设置资源限制"
	} else {
		result = readPDF(input)
	}
	return json.NewEncoder(output).Encode(result)
}

func RunPDFFileWorker(path string, output io.Writer) error {
	result := pdfResult{}
	if err := boundPDFProcess(); err != nil {
		result.Error = "无法为 PDF 提取设置资源限制"
	} else if file, err := os.Open(path); err != nil {
		result.Error = "无法打开附件原件，请重新选择文件"
	} else {
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			result.Error = "附件原件无效，请重新选择文件"
		} else {
			result = readPDFAt(file, info.Size())
		}
	}
	return json.NewEncoder(output).Encode(result)
}

func readPDF(input io.Reader) pdfResult {
	raw, err := io.ReadAll(io.LimitReader(input, (8<<20)+1))
	if err != nil || len(raw) > 8<<20 {
		return pdfResult{Error: "PDF 单次传输过大，请通过桌面分段上传"}
	}
	return readPDFAt(bytes.NewReader(raw), int64(len(raw)))
}

func readPDFAt(input io.ReaderAt, size int64) (result pdfResult) {
	defer func() {
		if recover() != nil {
			result = pdfResult{Error: "PDF 内容无法解析，请尝试导出为文本"}
		}
	}()
	if size < 1 || size > MaxBytes {
		return pdfResult{Error: fmt.Sprintf("PDF 为空或超过每份 %d MB 的上限", MaxMiB)}
	}
	header := make([]byte, min(size, 1024))
	if _, err := input.ReadAt(header, 0); err != nil || !bytes.Contains(header, []byte("%PDF-")) {
		return pdfResult{Error: "文件不是有效的 PDF"}
	}
	reader, err := pdf.NewReader(input, size)
	if err != nil {
		return pdfResult{Error: "无法打开 PDF，文件可能受密码保护或已损坏"}
	}
	pages := reader.NumPage()
	if pages < 1 || pages > maxPDFPages {
		return pdfResult{Error: "每份 PDF 支持 1–100 页，请拆分后添加"}
	}
	total := 0
	for i := 1; i <= pages; i++ {
		p := reader.Page(i)
		text, err := p.GetPlainText(nil)
		if err != nil {
			return pdfResult{Error: "无法提取 PDF 文字，请尝试导出为文本"}
		}
		text = strings.TrimSpace(strings.ToValidUTF8(text, ""))
		if text == "" {
			continue
		}
		total += len(text)
		if total > maxTextBytes {
			return pdfResult{Error: "PDF 提取的文本超过 1 MB，请拆分后添加"}
		}
		result.Sections = append(result.Sections, Section{Text: text, Page: i})
	}
	if len(result.Sections) == 0 {
		result.Error = "PDF 没有可提取的文字，扫描件可先运行 OCR 或导出为文本"
	}
	return result
}
