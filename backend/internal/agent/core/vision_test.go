package core

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	deliveryartifact "github.com/noosphere/backend/internal/agent/artifact"
	"github.com/noosphere/backend/internal/gateway/providers"
)

func TestPresentedPixelsSurviveBudgetPauseWithoutStoringBase64(t *testing.T) {
	_, d := fixture(t)
	img := image.NewNRGBA(image.Rect(0, 0, 2560, 100))
	for x := range 2560 {
		for y := range 100 {
			img.SetNRGBA(x, y, color.NRGBA{R: 220, G: 20, B: 12, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := d.Write(t.Context(), "slide.png", buf.Bytes()); err != nil {
		t.Fatal(err)
	}
	turns := 0
	s := &memorySession{}
	m := func(ctx context.Context, msg []providers.ToolMessage, defs []providers.ToolDefinition) (*providers.ToolResponse, error) {
		turns++
		if turns == 1 {
			return reply("", call("draw", "present", `{"path":"slide.png"}`)), nil
		}
		view := msg[len(msg)-1]
		if view.Role != "user" || msg[len(msg)-2].ToolCallID != "draw" || !strings.HasPrefix(view.ImageURL, "data:image/jpeg;base64,") {
			t.Fatal("missing pixel observation after the complete tool batch")
		}
		raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(view.ImageURL, "data:image/jpeg;base64,"))
		if err != nil {
			t.Fatal(err)
		}
		got, err := jpeg.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		r, g, _, _ := got.At(30, 12).RGBA()
		if got.Bounds().Dx() != 1280 || r < 50000 || g > 8000 {
			t.Fatal("unexpected thumbnail pixels")
		}
		return reply("checked red slide"), nil
	}
	o := Options{MaxModelCalls: 1, Present: func(ctx context.Context, p string) (any, error) {
		return deliveryartifact.Present(ctx, d, p, t.TempDir())
	},
		PreviewImage: func(ctx context.Context, m json.RawMessage) (string, error) {
			return deliveryartifact.ModelPreview(ctx, d, m)
		}}
	_, err := Execute(t.Context(), "Review the image", m, d, s, o)
	waitFor(t, err, "budget_exhausted")
	if bytes.Contains(s.raw, []byte("data:image")) {
		t.Fatal("checkpoint stored image bytes")
	}
	s.input = append(s.input, Control{Seq: 1, Action: "extend"})
	if _, err = Execute(t.Context(), "Review the image", m, d, s, o); err != nil {
		t.Fatal(err)
	}
	if turns != 2 {
		t.Fatal(turns)
	}
}
