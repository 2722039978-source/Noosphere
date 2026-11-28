package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const maxLogBytes = 4 << 20

var sensitiveOutput = regexp.MustCompile(`(?i)(bearer\s+[a-z0-9_./+=-]{8,}|sk-[a-z0-9_-]{12,}|(?:api[_-]?key|access[_-]?token|password|secret)["']?\s*[:=]\s*["']?[^\s"',}]{4,})`)

func persistLog(work string, b *limitedOutput) (Result, error) {
	text := sensitiveOutput.ReplaceAllString(strings.ToValidUTF8(b.String(), "�"), "[redacted]")
	if b.clipped {
		text += "\n[日志达到本次命令的 4 MiB 保留上限]"
	}
	hash := sha256.Sum256([]byte(text))
	id := hex.EncodeToString(hash[:])
	snippet := text
	if len(snippet) > 32<<10 {
		snippet = strings.ToValidUTF8(snippet[:32<<10], "�") + "\n[已截断，可按范围读取命令日志]"
	}
	dir := filepath.Join(filepath.Dir(work), "command-logs")
	if e := os.MkdirAll(dir, 0700); e != nil {
		return Result{}, e
	}
	out := Result{Output: snippet, LogID: id, LogBytes: int64(len(text)), LogTruncated: b.clipped, Truncated: snippet != text}
	target := filepath.Join(dir, id)
	if _, e := os.Stat(target); os.IsNotExist(e) {
		f, e := os.CreateTemp(dir, ".pending-")
		if e != nil {
			return Result{}, e
		}
		defer os.Remove(f.Name())
		if _, e = io.WriteString(f, text); e == nil {
			e = f.Sync()
		}
		ce := f.Close()
		if e != nil {
			return Result{}, e
		}
		if ce != nil {
			return Result{}, ce
		}
		if e = os.Rename(f.Name(), target); e != nil {
			return Result{}, e
		}
	} else if e != nil {
		return Result{}, e
	}
	return out, nil
}
func ReadLog(ctx context.Context, work, id string, offset int64, limit int) (string, error) {
	raw, e := hex.DecodeString(id)
	if e != nil || len(raw) != 32 || offset < 0 || limit < 1 || limit > 32000 {
		return "", fmt.Errorf("命令日志引用或范围无效")
	}
	if e = ctx.Err(); e != nil {
		return "", e
	}
	root, e := os.OpenRoot(filepath.Join(filepath.Dir(work), "command-logs"))
	if e != nil {
		return "", e
	}
	defer root.Close()
	f, e := root.Open(id)
	if e != nil {
		return "", e
	}
	defer f.Close()
	b := make([]byte, limit+utf8.UTFMax)
	n, e := f.ReadAt(b, offset)
	if e != nil && e != io.EOF {
		return "", e
	}
	end := min(n, limit)
	for end > 0 && end < n && !utf8.RuneStart(b[end]) {
		end--
	}
	if end == 0 && n > 0 {
		_, end = utf8.DecodeRune(b[:n])
	}
	if !utf8.Valid(b[:end]) {
		return "", fmt.Errorf("请使用日志上一页返回的偏移量")
	}
	return string(b[:end]), nil
}
