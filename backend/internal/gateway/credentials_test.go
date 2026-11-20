package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noosphere/backend/internal/gateway/secure"
	"gopkg.in/yaml.v3"
)

type fixtureSecretStore struct {
	status  secure.Status
	key     []byte
	creates int
}

func (s *fixtureSecretStore) Provider() string { return "fixture_store" }

func (s *fixtureSecretStore) Load(context.Context) ([]byte, error) {
	if s.status != "" {
		return nil, &secure.StoreError{Provider: s.Provider(), Status: s.status}
	}
	return append([]byte(nil), s.key...), nil
}

func (s *fixtureSecretStore) Create(_ context.Context, key []byte) error {
	s.creates++
	s.key = append([]byte(nil), key...)
	s.status = ""
	return nil
}

func writeModelFixture(t *testing.T, path, rawCredential string) {
	t.Helper()
	data, err := yaml.Marshal(persistedGatewayConfig{Models: []persistedModelConfig{{
		ModelConfig: ModelConfig{
			ID: "legacy", Name: "Legacy", Provider: "openai", ModelName: "saved-model",
			BaseURL: "https://example.test/v1", Enabled: true,
		},
		LegacyAPIKey: rawCredential,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

// Older releases decode api_key into the same slot used for outbound
// authorization. Emulate their decoder and require that slot to stay empty.
func assertSavedCredential(t *testing.T, path, id, expected string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var legacy struct {
		Models []struct {
			ID     string `yaml:"id"`
			APIKey string `yaml:"api_key"`
		} `yaml:"providers"`
	}
	if err := yaml.Unmarshal(data, &legacy); err != nil {
		t.Fatal(err)
	}
	for _, model := range legacy.Models {
		if model.APIKey != "" {
			t.Fatalf("older release could consume persisted credential for %s", model.ID)
		}
	}
	var persisted persistedGatewayConfig
	if err := yaml.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	for _, model := range persisted.Models {
		if model.ID == id && model.EncryptedAPIKey == expected {
			return
		}
	}
	t.Fatalf("expected encrypted_api_key for %s to survive", id)
}

func TestUnreadableCredentialBlocksProviderAndSurvivesOtherWrites(t *testing.T) {
	for _, status := range []secure.Status{secure.Locked, secure.PermissionDenied, secure.Unavailable, secure.NotFound, secure.Corrupt} {
		t.Run(string(status), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "models.yaml")
			v := credentialPrefix + "old-ciphertext"
			writeModelFixture(t, path, v)
			store := &fixtureSecretStore{status: status}
			g := &Gateway{configPath: path, secretStore: store}
			g.loadConfig()

			model := g.findModelByID("legacy")
			if model.APIKey != "" || model.Credential == nil || model.Credential.Status != status {
				t.Fatal("unverified bytes were promoted to a usable credential")
			}
			if _, err := g.getOrCreateProvider(model); err == nil {
				t.Fatal("unreadable credential allowed inference")
			}
			if result := g.probeModel(context.Background(), model, false, true); result.Code != string(credentialCode(status)) {
				t.Fatalf("wrong probe classification: %s", result.Code)
			}
			output, _ := json.Marshal(model)
			if bytes.Contains(output, []byte(v)) {
				t.Fatal("persisted credential leaked into model metadata")
			}

			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"id":"local","name":"Local","provider":"ollama","base_url":"http://127.0.0.1:11434","model_name":"local"}`))
			response := httptest.NewRecorder()
			g.HandleAddModel(response, request)
			if response.Code != http.StatusCreated || store.creates != 0 {
				t.Fatal(response.Code, response.Body.String(), store.creates)
			}
			assertSavedCredential(t, path, "legacy", v)
			g.loadConfig()
			model = g.findModelByID("legacy")
			if model.APIKey != "" || model.Credential == nil || model.storedCredential != v {
				t.Fatal("unreadable credential did not survive secure storage reload")
			}
		})
	}
}

func TestLegacyCredentialMustAuthenticateBeforeMigration(t *testing.T) {
	key, err := secure.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := secure.Encrypt(key, "test-only-key")
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct{ name, credential string }{
		{"legacy-windows", encoded},
		{"interim-prefixed", credentialPrefix + encoded},
	} {
		t.Run(input.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "models.yaml")
			writeModelFixture(t, path, input.credential)
			store := &fixtureSecretStore{key: key}
			g := NewWithSecretStore(path, store)
			if m := g.findModelByID("legacy"); m.APIKey != "test-only-key" || m.Credential != nil {
				t.Fatal("supported legacy ciphertext was not decoded")
			}
			if err := g.saveConfig(); err != nil {
				t.Fatal(err)
			}
			assertSavedCredential(t, path, "legacy", credentialPrefix+encoded)
			g = NewWithSecretStore(path, store)
			if m := g.findModelByID("legacy"); m.APIKey != "test-only-key" || m.Credential != nil {
				t.Fatal("migrated credential did not decode after restart")
			}
		})
	}
}

func TestOpaqueUnprefixedCredentialMovesOutOfLegacyField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.yaml")
	const legacy = "opaque-legacy-token"
	writeModelFixture(t, path, legacy)
	g := NewWithSecretStore(path, &fixtureSecretStore{status: secure.Locked})
	if err := g.saveConfig(); err != nil {
		t.Fatal(err)
	}
	assertSavedCredential(t, path, "legacy", legacy)
	g.loadConfig()
	if m := g.findModelByID("legacy"); m.APIKey != "" || m.storedCredential != legacy {
		t.Fatal("opaque legacy data was lost or promoted to a usable key")
	}
}

func TestCredentialSaveNeverReplacesLockedOrInvalidStore(t *testing.T) {
	for _, status := range []secure.Status{secure.Locked, secure.PermissionDenied, secure.Unavailable, secure.Corrupt} {
		t.Run(string(status), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "models.yaml")
			store := &fixtureSecretStore{status: status}
			g := &Gateway{configPath: path, secretStore: store, config: &GatewayConfig{}}
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"id":"cloud","provider":"openai","api_key":"test-secret"}`))
			w := httptest.NewRecorder()
			g.HandleAddModel(w, r)
			if w.Code == http.StatusCreated || store.creates != 0 || len(g.config.Models) != 0 ||
				!strings.Contains(w.Body.String(), string(credentialCode(status))) || strings.Contains(w.Body.String(), "test-secret") {
				t.Fatal(w.Code, w.Body.String(), store.creates)
			}
		})
	}
}

func TestExplicitCredentialSaveCreatesKeyWithoutOverwritingForeignCiphertext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.yaml")
	const opaqueCredential = "enc:v1:credential-from-another-platform"
	writeModelFixture(t, path, opaqueCredential)
	store := &fixtureSecretStore{status: secure.NotFound}
	g := &Gateway{configPath: path, secretStore: store}
	g.loadConfig()
	if store.creates != 0 {
		t.Fatal("startup replaced a missing master key")
	}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"id":"cloud","provider":"openai","api_key":"test-secret"}`))
	w := httptest.NewRecorder()
	g.HandleAddModel(w, r)
	if w.Code != http.StatusCreated || store.creates != 1 || strings.Contains(w.Body.String(), "test-secret") {
		t.Fatal(w.Code, w.Body.String(), store.creates)
	}
	assertSavedCredential(t, path, "legacy", opaqueCredential)
	cloud := g.findModelByID("cloud")
	if !strings.HasPrefix(cloud.storedCredential, credentialPrefix) {
		t.Fatal("new credential lacks its version marker")
	}
	assertSavedCredential(t, path, "cloud", cloud.storedCredential)
	g.loadConfig()
	if cloud, legacy := g.findModelByID("cloud"), g.findModelByID("legacy"); cloud.APIKey != "test-secret" ||
		cloud.Credential != nil || legacy.APIKey != "" || legacy.Credential == nil || legacy.storedCredential != opaqueCredential {
		t.Fatal("saving a new credential used or modified a foreign ciphertext")
	}
}
