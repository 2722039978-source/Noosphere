package host

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
)

// StorePreview keeps a renderer's checked PNG beside the original immutable
// artifact. It never adds generated thumbnails to the project change set.
func (d *Draft) StorePreview(ctx context.Context, data []byte) (string, error) {
	if d.history {
		return "", fmt.Errorf("historical delivery is read-only")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(data) == 0 || len(data) > 8<<20 {
		return "", fmt.Errorf("preview exceeds size limit")
	}
	info, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || info.Width < 1 || info.Height < 1 || int64(info.Width)*int64(info.Height) > 32<<20 {
		return "", fmt.Errorf("invalid preview dimensions")
	}
	h := sha256.Sum256(data)
	hash := hex.EncodeToString(h[:])
	dest := filepath.Join(d.blobs(), hash)
	f, err := os.CreateTemp(d.blobs(), ".preview-")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if err = os.Link(f.Name(), dest); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", err
	}
	return hash, syncPath(d.blobs())
}
