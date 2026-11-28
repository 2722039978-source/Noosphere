package artifact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"strings"

	"github.com/noosphere/backend/internal/agent/host"
	"golang.org/x/image/draw"
)

// ModelPreview reads the immutable revision chosen by present. It scales a
// single page to a bounded transport size independently of the original file.
func ModelPreview(ctx context.Context, d *host.Draft, metadata json.RawMessage) (string, error) {
	var a Item
	if err := json.Unmarshal(metadata, &a); err != nil {
		return "", err
	}
	if !a.Preview || !strings.HasPrefix(a.PreviewMIME, "image/") {
		return "", nil
	}
	hash := a.PreviewHash
	if hash == "" {
		hash = a.SHA256
	}
	f, err := d.OpenBlob(ctx, hash)
	if err != nil {
		return "", err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (8<<20)+1))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	if len(raw) > 8<<20 || hex.EncodeToString(sum[:]) != hash {
		return "", errors.New("预览归档校验失败")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 48_000_000 {
		return "", errors.New("预览图像尺寸无效")
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	longest := max(cfg.Width, cfg.Height, 1280)
	dst := image.NewRGBA(image.Rect(0, 0, max(1, cfg.Width*1280/longest), max(1, cfg.Height*1280/longest)))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	var buf bytes.Buffer
	if err = jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 78}); err != nil {
		return "", err
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
