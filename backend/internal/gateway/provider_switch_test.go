package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProviderSwitchUsesOwnCredential(t *testing.T) {
	var received []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = append(received, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}]}`))
	}))
	defer upstream.Close()
	g := newTestGateway(t)
	for _, credential := range []string{"fixture-a", "fixture-b", "fixture-rotated"} {
		m := &ModelConfig{ID: "one", Provider: "openai_compatible", BaseURL: upstream.URL, APIKey: credential, ModelName: "test"}
		if _, _, _, err := g.callLLMOnce(context.Background(), m, nil, "", 0, 10); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(received, ",") != "Bearer fixture-a,Bearer fixture-b,Bearer fixture-rotated" {
		t.Fatalf("provider reused another configuration's credential: %v", received)
	}
}

func TestModelEditPreservesCredentialWhenBlank(t *testing.T) {
	g := newTestGateway(t)
	for _, body := range []string{
		`{"id":"one","name":"One","provider":"openai_compatible","base_url":"https://example.com","model_name":"test","api_key":"fixture-original"}`,
		`{"id":"one","name":"Renamed","provider":"openai_compatible","base_url":"https://example.com","model_name":"test","api_key":"","is_default":true}`,
	} {
		w := httptest.NewRecorder()
		g.HandleAddModel(w, httptest.NewRequest("POST", "/api/gateway/models", strings.NewReader(body)))
		if w.Code != 201 {
			t.Fatal(w.Body.String())
		}
	}
	if m := g.findModel("one"); m.APIKey != "fixture-original" || !m.IsDefault {
		t.Fatal("editing/default selection erased the saved credential")
	}
}

func TestDefaultSwitchKeepsGenerationOptions(t *testing.T) {
	g := newTestGateway(t)
	g.config.Models = []ModelConfig{{ID: "custom", ModelName: "custom", Enabled: true, MaxTokens: 8192, Temperature: 0.7}}
	w := httptest.NewRecorder()
	g.HandleAddModel(w, httptest.NewRequest("POST", "/api/gateway/models", strings.NewReader(`{"id":"custom","name":"custom","provider":"openai_compatible","base_url":"https://example.com","model_name":"custom","is_default":true}`)))
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	m := g.findModel("")
	if m.MaxTokens != 8192 || m.Temperature != 0.7 {
		t.Fatalf("default switch reset options: %+v", m)
	}
}
