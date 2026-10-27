package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/noosphere/backend/internal/gateway/providers"
)

func TestUnsupportedProviderIsExplicit(t *testing.T) {
	g := &Gateway{config: &GatewayConfig{DefaultModel: "cloud", Models: []ModelConfig{{ID: "cloud", Provider: "openai", BaseURL: "https://example.com/v1", Enabled: true}, {ID: "unknown", Provider: "unknown", Enabled: true}, {ID: "disabled", Provider: "ollama", Enabled: false}}}, providers: map[string]providers.LLMProvider{}, sem: make(chan struct{}, 1)}
	for _, id := range []string{"cloud", "unknown", "disabled", "", "missing"} {
		_, err := g.DiscoverEvaluationModels(context.Background(), id)
		var e *providers.EvaluationError
		if !errors.As(err, &e) || e.Code != "UNSUPPORTED_CAPABILITY" {
			t.Fatalf("%q: %v", id, err)
		}
	}
}

func TestConfiguredEvaluationUsesCredentialsAndOptionalCapability(t *testing.T) {
	key := "fixture-one"
	var seen map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+key {
			t.Errorf("configured credential not used")
		}
		switch r.URL.Path {
		case "/api/version":
			fmt.Fprint(w, `{"version":"fixture-version"}`)
		case "/api/tags":
			fmt.Fprintf(w, `{"models":[{"name":"fixture:latest","size":123,"digest":"%s"}]}`, strings.Repeat("a", 64))
		case "/api/show":
			fmt.Fprint(w, `{"details":{"format":"gguf"},"model_info":{"general.architecture":"fixture"},"template":"fixture"}`)
		case "/api/chat":
			json.NewDecoder(r.Body).Decode(&seen)
			fmt.Fprint(w, `{"model":"fixture:latest","done":true,"message":{"content":"fixture answer"}}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer s.Close()
	g := &Gateway{config: &GatewayConfig{Models: []ModelConfig{{ID: "local", Provider: "ollama", BaseURL: s.URL + "/v1", APIKey: key, ModelName: "configured-chat-default", Temperature: 1, MaxTokens: 999, Enabled: true}}}, providers: map[string]providers.LLMProvider{}, sem: make(chan struct{}, 1)}
	d, err := g.DiscoverEvaluationModels(context.Background(), "local")
	if err != nil {
		t.Fatal(err)
	}
	id := d.Models[0]
	zero := 0.0
	seed := int64(0)
	r, err := g.Evaluate(context.Background(), "local", providers.EvaluationRequest{ModelName: id.Name, ExpectedDigest: id.Digest, ExpectedMetadataHash: id.MetadataHash, ExpectedEngineVersion: id.EngineVersion, Messages: []providers.Message{{Role: "user", Content: "fixture"}}, Options: providers.EvaluationOptions{Temperature: &zero, Seed: &seed}})
	if err != nil || r.Output != "fixture answer" || seen["options"].(map[string]any)["temperature"] != float64(0) || seen["options"].(map[string]any)["num_predict"] != nil {
		t.Fatalf("legacy defaults overwrote evaluation: %+v %v", seen, err)
	}
	p, err := g.getOrCreateProvider(&g.config.Models[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.(providers.ToolProvider); !ok {
		t.Fatal("Ollama lost legacy tool capability")
	}
	key = "fixture-two"
	g.mu.Lock()
	g.config.Models[0].APIKey = key
	g.mu.Unlock()
	d, err = g.DiscoverEvaluationModels(context.Background(), "local")
	if err != nil || d.Models[0].MetadataHash != id.MetadataHash || len(g.providers) != 1 {
		t.Fatalf("credential rotation altered identity or cache: %+v %v", d, err)
	}
}
func TestEvaluationFingerprintAndSemaphoreCancellation(t *testing.T) {
	g := &Gateway{config: &GatewayConfig{Models: []ModelConfig{{ID: "a", Provider: "ollama", BaseURL: "http://localhost:11434/v1", APIKey: "one", Enabled: true}, {ID: "b", Provider: "ollama", BaseURL: "http://127.0.0.1:11434/api/", APIKey: "two", Enabled: true}}}, providers: map[string]providers.LLMProvider{}, sem: make(chan struct{}, 1)}
	a, err := g.EvaluationSourceFingerprint("a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := g.EvaluationSourceFingerprint("b")
	if err != nil || a != b {
		t.Fatalf("normalized fingerprint mismatch %s %s %v", a, b, err)
	}
	g.config.Models[0].BaseURL = "http://127.0.0.1:11435/v1"
	_, err = g.Evaluate(context.Background(), "a", providers.EvaluationRequest{ExpectedSourceFingerprint: a})
	var sourceError *providers.EvaluationError
	if !errors.As(err, &sourceError) || sourceError.Code != "MODEL_DRIFT" || len(g.providers) != 0 {
		t.Fatalf("changed source was not rejected before provider creation: %v", err)
	}
	g.config.Models[0].BaseURL = "http://localhost:11434/v1"
	g.sem <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = g.Evaluate(ctx, "a", providers.EvaluationRequest{})
	var e *providers.EvaluationError
	if !errors.As(err, &e) || e.Code != "INFERENCE_TIMEOUT" {
		t.Fatalf("semaphore wait: %v", err)
	}
}
