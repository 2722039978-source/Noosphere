package modellab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/noosphere/backend/internal/modelstore"
)

func TestHTTPEnvironmentExposesConfiguredBudgetWithoutResourceAttestation(t *testing.T) {
	budget := Budget{MaxContext: 2048, MaxOutput: 768, InferenceTimeout: 90 * time.Second, CleanupTimeout: 10 * time.Second}
	lab := &Service{runtime: &evaluationRuntime{config: RuntimeConfig{
		Budget: budget,
		CollectEnvironment: func(context.Context) (modelstore.EnvironmentSnapshot, error) {
			return modelstore.EnvironmentSnapshot{OS: "windows", Arch: "amd64", CollectorVersion: "http-budget-test", VRAM: unknownMetric("bytes", "not collected")}, nil
		},
	}}}
	mux := http.NewServeMux()
	lab.RegisterRoutes(mux, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/model-lab/environment", nil))
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		Environment modelstore.EnvironmentSnapshot `json:"environment"`
		Preflight   struct {
			Available bool   `json:"available"`
			Code      string `json:"code"`
			Budget    Budget `json:"budget"`
		} `json:"preflight"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Preflight.Budget != budget {
		t.Fatalf("configured budget lost: got %+v, want %+v", response.Preflight.Budget, budget)
	}
	if response.Preflight.Available || response.Preflight.Code != "RUNNER_UNAVAILABLE" || response.Environment.VRAM.Value != nil {
		t.Fatalf("configuration must not attest runtime or GPU availability: %+v", response)
	}
}
