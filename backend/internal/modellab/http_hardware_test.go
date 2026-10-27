package modellab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/noosphere/backend/internal/hostinfo"
	"github.com/noosphere/backend/internal/modelstore"
)

func TestHTTPHardwareInspectionDoesNotRequireToolchain(t *testing.T) {
	for _, tc := range []struct {
		name      string
		hardware  bool
		cancelled bool
		status    int
	}{{"hardware-only", true, false, 200}, {"legacy-error", false, false, 503}, {"cancelled", true, true, 503}} {
		t.Run(tc.name, func(t *testing.T) {
			env := modelstore.EnvironmentSnapshot{OS: "windows", Arch: "amd64", CollectorVersion: "test", VRAM: unknownMetric("bytes", "not collected")}
			if tc.hardware {
				env.Hardware = &hostinfo.Inventory{SchemaVersion: 1, Platform: "windows", System: hostinfo.Device{Model: "PC model"}}
			}
			lab := &Service{runtime: &evaluationRuntime{config: RuntimeConfig{Budget: DefaultBudget(), CollectEnvironment: func(context.Context) (modelstore.EnvironmentSnapshot, error) {
				return env, fmt.Errorf("trusted Go unavailable")
			}}}}
			mux := http.NewServeMux()
			lab.RegisterRoutes(mux, nil)
			r := httptest.NewRequest("GET", "/api/model-lab/environment", nil)
			if tc.cancelled {
				ctx, cancel := context.WithCancel(r.Context())
				cancel()
				r = r.WithContext(ctx)
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatal(w.Code, w.Body.String())
			}
			if tc.status == 200 {
				var response struct {
					Environment modelstore.EnvironmentSnapshot `json:"environment"`
					Preflight   struct {
						Available bool   `json:"available"`
						Code      string `json:"code"`
					} `json:"preflight"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if response.Environment.Hardware == nil || response.Environment.Hardware.System.Model != "PC model" || response.Preflight.Available || response.Preflight.Code != "ENVIRONMENT_UNAVAILABLE" {
					t.Fatal(response)
				}
			}
		})
	}
}
