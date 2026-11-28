package render

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/noosphere/backend/internal/agent/sandbox"
)

func hostPreview(ctx context.Context, source io.Reader, ext, dataDir string) (Preview, error) {
	switch ext {
	case ".docx", ".pptx":
		return Preview{}, fmt.Errorf("Windows 当前无法隔离重排此 Office 文件，且文件没有可用的内置缩略图")
	case ".html", ".htm":
	default:
		return Preview{}, fmt.Errorf("不支持的排版文件格式")
	}
	dir, e := os.MkdirTemp("", "noosphere-document-")
	if e != nil {
		return Preview{}, e
	}
	defer os.RemoveAll(dir)
	f, e := os.OpenFile(filepath.Join(dir, "input"+ext), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return Preview{}, e
	}
	_, e = io.Copy(f, io.LimitReader(source, maxDocument+1))
	closed := f.Close()
	if e != nil {
		return Preview{}, e
	}
	if closed != nil {
		return Preview{}, closed
	}
	if e = sandbox.RenderStaticHTML(ctx, dir, ext, dataDir); e != nil {
		return Preview{}, e
	}
	name := "page.png"
	root, e := os.OpenRoot(dir)
	if e != nil {
		return Preview{}, e
	}
	defer root.Close()
	st, e := root.Lstat(name)
	if e != nil || !st.Mode().IsRegular() || st.Size() > maxDocument {
		return Preview{}, fmt.Errorf("隔离渲染未生成有效文件")
	}
	result, e := root.Open(name)
	if e != nil {
		return Preview{}, e
	}
	defer result.Close()
	raw, e := io.ReadAll(io.LimitReader(result, (8<<20)+1))
	if e != nil || len(raw) > 8<<20 {
		return Preview{}, fmt.Errorf("HTML 截图超出限制")
	}
	png, e := normalizeImage(raw)
	return rasterPreview(png, "gowkhtmltoimage", "静态 HTML/CSS 排版器已在无网络 AppContainer 中渲染 1280 × 900 首屏；JavaScript 未执行", e)
}
