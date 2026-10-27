package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/noosphere/backend/internal/gateway/providers"
)

// Use a fixture encryption key so these persistence tests never access the
// user's Credential Manager. All configured endpoints are data, not contacted.
func modelIdentityGateway(t *testing.T) *Gateway {
	t.Helper()
	g := &Gateway{
		configPath: filepath.Join(t.TempDir(), "models.yaml"),
		encKey:     bytes.Repeat([]byte{42}, 32),
		config: &GatewayConfig{DefaultModel: "other", Models: []ModelConfig{
			{
				ID: "configured", Name: "Configured model", Provider: "ollama",
				BaseURL: "http://127.0.0.1:11434/v1", APIKey: "fixture-identity-secret", ModelName: "acme/bonsai:8b",
				Vendor: "user-vendor", ModelFamily: "user-family", Deployment: "local", ContextLength: 32768,
				Metadata: &ModelMetadata{Vendor: "old-vendor", Family: "old-family", Architecture: "old-architecture", ParameterSize: "7B", Quantization: "Q4_0", ContextLength: 4096, Source: "old-source"},
				Enabled:  true, Temperature: 0.7, MaxTokens: 8192,
			},
			{ID: "other", Name: "Other model", Provider: "ollama", ModelName: "other:latest", Enabled: true, IsDefault: true, Metadata: &ModelMetadata{Family: "other-family", Source: "fixture"}},
		}},
	}
	if err := g.saveConfig(); err != nil {
		t.Fatal(err)
	}
	return g
}

func copyIdentityConfig(config *GatewayConfig) *GatewayConfig {
	copy := *config
	copy.Models = append([]ModelConfig(nil), config.Models...)
	for i := range copy.Models {
		if copy.Models[i].Metadata != nil {
			metadata := *copy.Models[i].Metadata
			copy.Models[i].Metadata = &metadata
		}
	}
	return &copy
}

func identityConfigBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestDiscoveredMetadataOnlyUsesConfiguredModelIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, provider, configured, discovered string
		wantMetadata                           bool
	}{
		{"exact configured model", "ollama", "acme/bonsai:8b", "acme/bonsai:8b", true},
		{"different namespace", "ollama", "acme/bonsai:8b", "other/bonsai:8b", false},
		{"missing namespace", "ollama", "acme/bonsai:8b", "bonsai:8b", false},
		{"different explicit tag", "ollama", "acme/bonsai:8b", "acme/bonsai:latest", false},
		{"different model", "ollama", "acme/bonsai:8b", "acme/other:8b", false},
		{"omitted configured default tag", "ollama", "acme/bonsai", "acme/bonsai:latest", true},
		{"omitted discovered default tag", "ollama", "acme/bonsai:latest", "acme/bonsai", true},
		{"registry port is not a model tag", "ollama", "localhost:5000/acme/bonsai", "localhost:5000/acme/bonsai:latest", true},
		{"explicit tag is not latest", "ollama", "acme/bonsai:8b", "acme/bonsai", false},
		{"case remains significant", "ollama", "acme/Bonsai:8b", "acme/bonsai:8b", false},
		{"other provider needs exact name", "openai_compatible", "bonsai", "bonsai:latest", false},
		{"empty configured model", "ollama", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := modelIdentityGateway(t)
			g.config.Models[0].Provider = tc.provider
			g.config.Models[0].ModelName = tc.configured
			if err := g.saveConfig(); err != nil {
				t.Fatal(err)
			}
			want := copyIdentityConfig(g.config)
			before := identityConfigBytes(t, g.configPath)
			snapshot := g.config.Models[0]
			err := g.syncDiscoveredModelMetadata(&snapshot, providers.ModelDiscovery{Models: []providers.ModelIdentity{
				{Name: "unrelated:latest", Family: "wrong-family", ParameterSize: "99B", Quantization: "wrong-quantization"},
				{Name: tc.discovered, Family: "bonsai", ParameterSize: "8.2B", Quantization: "Q5_K_M"},
			}})
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantMetadata {
				want.Models[0].Metadata = &ModelMetadata{Family: "bonsai", ParameterSize: "8.2B", Quantization: "Q5_K_M", Source: tc.provider}
			} else if !bytes.Equal(before, identityConfigBytes(t, g.configPath)) {
				t.Fatal("unrelated discovery rewrote the persisted configuration")
			}
			if !reflect.DeepEqual(g.config, want) {
				t.Fatalf("discovery changed the wrong configuration fields: got %+v, want %+v", g.config.Models[0], want.Models[0])
			}
		})
	}
}

func TestDiscoveredMetadataPersistsFactsWithoutChangingOverridesOrSelection(t *testing.T) {
	for _, isDefault := range []bool{false, true} {
		t.Run(map[bool]string{false: "other default", true: "configured default"}[isDefault], func(t *testing.T) {
			g := modelIdentityGateway(t)
			if isDefault {
				g.config.DefaultModel = "configured"
				g.config.Models[0].IsDefault = true
				g.config.Models[1].IsDefault = false
			}
			want := copyIdentityConfig(g.config)
			want.Models[0].Metadata = &ModelMetadata{Family: "bonsai", ParameterSize: "8.2B", Quantization: "Q5_K_M", Source: "ollama"}
			snapshot := g.config.Models[0]
			err := g.syncDiscoveredModelMetadata(&snapshot, providers.ModelDiscovery{Models: []providers.ModelIdentity{{
				Name: "acme/bonsai:8b", Family: " bonsai ", ParameterSize: " 8.2B ", Quantization: " Q5_K_M ", LocalVerified: false,
			}}})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(g.config, want) {
				t.Fatalf("metadata sync changed explicit overrides, credentials, selection or generation settings: got %+v, want %+v", g.config, want)
			}
			if bytes.Contains(identityConfigBytes(t, g.configPath), []byte("fixture-identity-secret")) {
				t.Fatal("discovery persistence wrote the credential in plaintext")
			}
			// Exercise the actual loading/decryption path with the same fixture key.
			restored := &Gateway{configPath: g.configPath, encKey: append([]byte(nil), g.encKey...)}
			restored.loadConfig()
			if !reflect.DeepEqual(restored.config, want) {
				t.Fatalf("restart lost metadata or changed the configured identity: got %+v, want %+v", restored.config, want)
			}
			w := httptest.NewRecorder()
			restored.HandleListModels(w, httptest.NewRequest(http.MethodGet, "/api/gateway/models", nil))
			var listed struct {
				DefaultModel string        `json:"default_model"`
				Models       []ModelConfig `json:"models"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusOK || listed.DefaultModel != want.DefaultModel || len(listed.Models) != 2 || !reflect.DeepEqual(listed.Models[0].Metadata, want.Models[0].Metadata) || listed.Models[0].Vendor != "user-vendor" || listed.Models[0].ModelFamily != "user-family" {
				t.Fatalf("persisted identity not exposed correctly to consumers: HTTP %d %s", w.Code, w.Body)
			}
			if strings.Contains(w.Body.String(), "fixture-identity-secret") || strings.Contains(w.Body.String(), "api_key") {
				t.Fatal("metadata listing exposed a configured credential")
			}
		})
	}
}

func TestDiscoveredMetadataRejectsConfigurationDriftWithoutWriting(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*GatewayConfig)
	}{
		{"disabled", func(c *GatewayConfig) { c.Models[0].Enabled = false }},
		{"provider", func(c *GatewayConfig) { c.Models[0].Provider = "openai_compatible" }},
		{"endpoint", func(c *GatewayConfig) { c.Models[0].BaseURL = "http://127.0.0.1:22434/v1" }},
		{"credential", func(c *GatewayConfig) { c.Models[0].APIKey = "rotated-fixture-secret" }},
		{"model name", func(c *GatewayConfig) { c.Models[0].ModelName = "acme/bonsai:14b" }},
		{"removed model", func(c *GatewayConfig) { c.Models = c.Models[1:] }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := modelIdentityGateway(t)
			snapshot := g.config.Models[0]
			tc.mutate(g.config)
			if err := g.saveConfig(); err != nil {
				t.Fatal(err)
			}
			want := copyIdentityConfig(g.config)
			before := identityConfigBytes(t, g.configPath)
			err := g.syncDiscoveredModelMetadata(&snapshot, providers.ModelDiscovery{Models: []providers.ModelIdentity{{Name: "acme/bonsai:8b", Family: "bonsai"}}})
			var evaluationError *providers.EvaluationError
			if !errors.As(err, &evaluationError) || evaluationError.Code != "MODEL_DRIFT" {
				t.Fatalf("configuration drift accepted: %v", err)
			}
			if !reflect.DeepEqual(g.config, want) || !bytes.Equal(before, identityConfigBytes(t, g.configPath)) {
				t.Fatal("stale discovery changed the new configuration in memory or on disk")
			}
		})
	}
}

func TestDiscoveredMetadataRejectsAmbiguousIdentity(t *testing.T) {
	for _, names := range [][]string{{"acme/bonsai:8b", "acme/bonsai:8b"}, {"acme/bonsai", "acme/bonsai:latest"}} {
		t.Run(strings.Join(names, "+"), func(t *testing.T) {
			g := modelIdentityGateway(t)
			g.config.Models[0].ModelName = names[0]
			want := copyIdentityConfig(g.config)
			before := identityConfigBytes(t, g.configPath)
			snapshot := g.config.Models[0]
			err := g.syncDiscoveredModelMetadata(&snapshot, providers.ModelDiscovery{Models: []providers.ModelIdentity{
				{Name: names[0], Family: "one"}, {Name: names[1], Family: "two"},
			}})
			var evaluationError *providers.EvaluationError
			if !errors.As(err, &evaluationError) || evaluationError.Code != "INVALID_OUTPUT" {
				t.Fatalf("ambiguous identity accepted: %v", err)
			}
			if !reflect.DeepEqual(g.config, want) || !bytes.Equal(before, identityConfigBytes(t, g.configPath)) {
				t.Fatal("ambiguous discovery changed configuration")
			}
		})
	}
}

func TestDiscoveredMetadataNoFactsOrUnchangedFactsDoNotRewriteConfig(t *testing.T) {
	for _, tc := range []struct {
		name     string
		identity providers.ModelIdentity
	}{
		{"no facts", providers.ModelIdentity{Name: "acme/bonsai:8b", Family: " \t", ParameterSize: " ", Quantization: "\n"}},
		{"unchanged facts", providers.ModelIdentity{Name: "acme/bonsai:8b", Family: "bonsai", ParameterSize: "8.2B", Quantization: "Q5_K_M"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := modelIdentityGateway(t)
			g.config.Models[0].Metadata = &ModelMetadata{Family: "bonsai", ParameterSize: "8.2B", Quantization: "Q5_K_M", Source: "ollama"}
			if err := g.saveConfig(); err != nil {
				t.Fatal(err)
			}
			want := copyIdentityConfig(g.config)
			before := identityConfigBytes(t, g.configPath)
			snapshot := g.config.Models[0]
			if err := g.syncDiscoveredModelMetadata(&snapshot, providers.ModelDiscovery{Models: []providers.ModelIdentity{tc.identity}}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(g.config, want) || !bytes.Equal(before, identityConfigBytes(t, g.configPath)) {
				t.Fatal("no new identity facts should not erase metadata or rewrite encrypted configuration")
			}
		})
	}
}

func TestDiscoveredMetadataRollsBackOnPersistenceFailure(t *testing.T) {
	g := modelIdentityGateway(t)
	want := copyIdentityConfig(g.config)
	path := g.configPath
	before := identityConfigBytes(t, path)
	g.configPath = filepath.Join(t.TempDir(), "missing", "models.yaml")
	snapshot := g.config.Models[0]
	err := g.syncDiscoveredModelMetadata(&snapshot, providers.ModelDiscovery{Models: []providers.ModelIdentity{{Name: "acme/bonsai:8b", Family: "bonsai"}}})
	var evaluationError *providers.EvaluationError
	if !errors.As(err, &evaluationError) || evaluationError.Code != "STORAGE_UNAVAILABLE" {
		t.Fatalf("failed persistence reported success: %v", err)
	}
	if !reflect.DeepEqual(g.config, want) || !bytes.Equal(before, identityConfigBytes(t, path)) {
		t.Fatal("failed persistence changed the last usable metadata")
	}
}

func TestModelTargetEditClearsStaleMetadataAndRestoresPersistedResult(t *testing.T) {
	for _, tc := range []struct {
		name, field, value string
		sendOldMetadata    bool
		wantCleared        bool
	}{
		{"model change with omitted metadata", "model_name", "acme/bonsai:14b", false, true},
		{"model change with echoed metadata", "model_name", "acme/bonsai:14b", true, true},
		{"provider change", "provider", "openai_compatible", true, true},
		{"endpoint change", "base_url", "http://127.0.0.1:22434/v1", true, true},
		{"cosmetic rename", "name", "Renamed model", false, false},
		{"equivalent endpoint slash", "base_url", "http://127.0.0.1:11434/v1/", false, false},
		{"credential rotation", "api_key", "rotated-fixture-secret", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := modelIdentityGateway(t)
			model := g.config.Models[0]
			body := map[string]any{
				"id": model.ID, "name": model.Name, "provider": model.Provider, "base_url": model.BaseURL,
				"model_name": model.ModelName, "vendor": model.Vendor, "model_family": model.ModelFamily,
				"deployment": model.Deployment, "context_length": model.ContextLength,
			}
			body[tc.field] = tc.value
			if tc.sendOldMetadata {
				body["metadata"] = model.Metadata
			}
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			g.HandleAddModel(w, httptest.NewRequest(http.MethodPost, "/api/gateway/models", bytes.NewReader(raw)))
			if w.Code != http.StatusCreated {
				t.Fatalf("edit failed: HTTP %d %s", w.Code, w.Body)
			}
			for _, actual := range []*Gateway{g, {configPath: g.configPath, encKey: append([]byte(nil), g.encKey...)}} {
				if actual != g {
					actual.loadConfig()
				}
				got := actual.config.Models[0]
				if tc.wantCleared {
					if got.Metadata != nil || got.ContextLength != 0 {
						t.Fatalf("new target retained old identity facts: %+v", got)
					}
				} else if !reflect.DeepEqual(got.Metadata, model.Metadata) || got.ContextLength != 32768 {
					t.Fatalf("same target lost its identity facts: %+v", got)
				}
				wantKey := "fixture-identity-secret"
				if tc.field == "api_key" {
					wantKey = "rotated-fixture-secret"
				}
				if got.APIKey != wantKey || got.Vendor != "user-vendor" || got.ModelFamily != "user-family" || !got.Enabled || got.Temperature != 0.7 || got.MaxTokens != 8192 || actual.config.DefaultModel != "other" || got.IsDefault || !actual.config.Models[1].IsDefault {
					t.Fatalf("target edit changed unrelated user configuration: %+v", actual.config)
				}
			}
			if strings.Contains(w.Body.String(), "fixture-identity-secret") || strings.Contains(w.Body.String(), "rotated-fixture-secret") {
				t.Fatal("edit response exposed a saved credential")
			}
		})
	}
}
