package workspaces

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxImageBytes = 8 << 20

func imageType(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	}
	return ""
}
func validateImage(r io.Reader, ref AttachmentRef) error {
	if ref.Size <= 0 || ref.Size > maxImageBytes {
		return fmt.Errorf("%w: 图片最大为 8 MiB", ErrInvalid)
	}
	config, format, err := image.DecodeConfig(r)
	if err != nil || "image/"+format != ref.ImageType || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 16000000 {
		return fmt.Errorf("%w: 无法读取图片，请使用 1600 万像素以内的 PNG 或 JPEG", ErrInvalid)
	}
	return nil
}
func (m *Manager) imageURL(ctx context.Context, record savedAttachment, ref AttachmentRef) (string, error) {
	var data []byte
	var err error
	if ref.Size <= 0 || ref.Size > maxImageBytes {
		return "", ErrInvalid
	}
	if record.diskBacked {
		file, err := os.Open(m.attachmentPath(ref.ID))
		if err != nil {
			return "", fmt.Errorf("%w: 图片文件缺失，请重新选择", ErrNotFound)
		}
		defer file.Close()
		data, err = io.ReadAll(io.LimitReader(file, int64(ref.Size)+1))
		if err != nil {
			return "", err
		}
	} else {
		err = m.db.QueryRowContext(ctx, "SELECT original FROM chat_attachments WHERE id=?", ref.ID).Scan(&data)
		if err != nil {
			return "", err
		}
	}
	if len(data) != ref.Size || fmt.Sprintf("%x", sha256.Sum256(data)) != ref.SourceHash {
		return "", fmt.Errorf("%w: 图片已变化，请重新选择", ErrConflict)
	}
	return "data:" + ref.ImageType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}
