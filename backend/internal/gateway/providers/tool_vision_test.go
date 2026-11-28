package providers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLargePreviewUsesBothToolTransports(t *testing.T) {
	url := "data:image/jpeg;base64," + strings.Repeat("AQID", 80000)
	msg := ToolMessage{Role: "user", Content: "Inspect page", ImageURL: url}
	stored, err := json.Marshal(CheckpointToolMessage(msg))
	if err != nil || bytes.Contains(stored, []byte("base64")) {
		t.Fatal("checkpoint contained transport pixels")
	}
	for _, transport := range []string{"chat", "responses"} {
		t.Run(transport, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				imageType := "image_url"
				if transport == "responses" {
					imageType = "input_image"
				}
				if !bytes.Contains(raw, []byte(url)) || !bytes.Contains(raw, []byte(`"type":"`+imageType+`"`)) {
					t.Error("upstream did not receive preview")
				}
				w.Header().Set("Content-Type", "application/json")
				if transport == "responses" {
					io.WriteString(w, `{"output":[{"type":"message","content":[{"type":"output_text","text":"observed"}]}]}`)
				} else {
					io.WriteString(w, `{"choices":[{"message":{"content":"observed"},"finish_reason":"stop"}]}`)
				}
			}))
			defer srv.Close()
			p := &OpenAIProvider{baseURL: srv.URL, client: srv.Client()}
			got, err := p.ChatTools(t.Context(), "local", []ToolMessage{msg}, nil, ChatOptions{ToolTransport: transport})
			if err != nil || got.Message.Content != "observed" {
				t.Fatal(got, err)
			}
		})
	}
}
