package gateway

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestModelSelectionDoesNotSilentlyChangeExplicitTarget(t *testing.T) {
	g := &Gateway{config: &GatewayConfig{DefaultModel: "disabled", Models: []ModelConfig{{ID: "disabled"}, {ID: "ready", Enabled: true}}}}
	if got := g.findModel("missing"); got != nil {
		t.Fatalf("unknown explicit model must be rejected, got %s", got.ID)
	}
	if got := g.findModel("disabled"); got != nil {
		t.Fatalf("disabled explicit model must be rejected, got %s", got.ID)
	}
	if got := g.findModel(""); got == nil || got.ID != "ready" {
		t.Fatalf("implicit selection must find first enabled model, got %+v", got)
	}
}

func TestModelMutationRollsBackWhenPersistenceFails(t *testing.T) {
	for _, action := range []string{"add", "delete"} {
		t.Run(action, func(t *testing.T) {
			g := &Gateway{configPath: filepath.Join(t.TempDir(), "missing", "models.yaml"), config: &GatewayConfig{DefaultModel: "original", Models: []ModelConfig{{ID: "original", Enabled: true, IsDefault: true}}}}
			w := httptest.NewRecorder()
			if action == "add" {
				req := httptest.NewRequest(http.MethodPost, "/api/gateway/models", strings.NewReader(`{"id":"new","name":"New","provider":"openai","base_url":"https://example.invalid","model_name":"test","is_default":true}`))
				g.HandleAddModel(w, req)
			} else {
				req := httptest.NewRequest(http.MethodDelete, "/api/gateway/models/original", nil)
				req.SetPathValue("id", "original")
				g.HandleDeleteModel(w, req)
			}
			if w.Code < 500 {
				t.Fatalf("persistence failure must return server error, got %d", w.Code)
			}
			if len(g.config.Models) != 1 || g.config.Models[0].ID != "original" || g.config.DefaultModel != "original" {
				t.Fatalf("failed mutation changed in-memory configuration: %+v", g.config)
			}
		})
	}
}

func TestModelAddDoesNotEchoCredentialAndHasOneDefault(t *testing.T) {
	g := newTestGateway(t)
	for _, id := range []string{"first", "second"} {
		req := httptest.NewRequest(http.MethodPost, "/api/gateway/models", strings.NewReader(`{"id":"`+id+`","name":"Test","provider":"openai","base_url":"https://example.invalid","model_name":"test","api_key":"fixture-secret","is_default":true}`))
		w := httptest.NewRecorder()
		g.HandleAddModel(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("add failed: %s", w.Body.String())
		}
		if strings.Contains(w.Body.String(), "fixture-secret") {
			t.Fatal("model response echoed credential")
		}
	}
	count := 0
	for _, model := range g.config.Models {
		if model.IsDefault {
			count++
		}
	}
	if count != 1 || g.config.DefaultModel != "second" {
		t.Fatalf("expected one current default, got %d", count)
	}
}
