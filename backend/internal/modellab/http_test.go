package modellab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noosphere/backend/internal/gateway/providers"
	"github.com/noosphere/backend/internal/middleware"
	"github.com/noosphere/backend/internal/modelstore"
	"github.com/noosphere/backend/internal/workspaces"
)

func TestHTTPSelectedRetryDispatchesExactlyOneArm(t *testing.T) {
	// Static-invalid outputs exercise the real worker/lineage/receipt path while
	// failing before the compiler or WASI executor can start.
	lab, store, manager, fixture, spec, _ := orchestrationLab(t)
	original := lab.runtime.infer
	lab.runtime.infer = func(ctx context.Context, id string, req providers.EvaluationRequest) (*providers.EvaluationResponse, error) {
		v, err := original(ctx, id, req)
		if v != nil && req.Kind != "warmup" {
			v.Output = "this is not Go source"
		}
		return v, err
	}
	ev, err := lab.CreateEvaluation(context.Background(), "local", spec)
	if err != nil {
		t.Fatal(err)
	}
	ev = awaitEvaluation(t, store, ev.ID)
	if ev.Status != "completed" {
		t.Fatal(ev.Status)
	}
	mux := http.NewServeMux()
	lab.RegisterRoutes(mux, manager)
	caseID := ev.Manifest.Dataset.Cases[0].ID
	body := `{"command":"retry_case","case_id":"` + caseID + `","arm":"candidate","expected_revision":` + fmt.Sprint(ev.Revision) + `}`
	post := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/workspaces/local/evaluations/"+ev.ID+"/commands", strings.NewReader(body))
		r.Header.Set("Idempotency-Key", "one-case-retry")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if w := post(); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	finished := awaitEvaluation(t, store, ev.ID)
	if finished.Status != "completed" {
		t.Fatal(finished.Status)
	}
	if w := post(); w.Code != 202 {
		t.Fatal("same-key response-loss retry", w.Code, w.Body.String())
	}
	attempts, err := store.ListAttempts(context.Background(), ev.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 25 {
		t.Fatal("retry changed unrelated attempts", len(attempts))
	}
	var retried []modelstore.Attempt
	for _, a := range attempts {
		if a.AttemptNo > 1 {
			retried = append(retried, a)
		}
	}
	if len(retried) != 1 || retried[0].CaseID != caseID || retried[0].Arm != "candidate" || retried[0].AttemptNo != 2 || retried[0].State != "completed" || retried[0].SupersedesReason != "explicit_retry_case" {
		t.Fatal(retried)
	}
	fixture.mu.Lock()
	formal := fixture.formal
	fixture.mu.Unlock()
	if formal != 25 {
		t.Fatal("retry inferred more than selected arm", formal)
	}
}

func TestHTTPPreflightRefusalReceiptAndTransientRetry(t *testing.T) {
	lab, store, manager, _, spec, _ := orchestrationLab(t)
	infer := lab.runtime.infer
	lab.runtime.infer = func(ctx context.Context, id string, req providers.EvaluationRequest) (*providers.EvaluationResponse, error) {
		v, err := infer(ctx, id, req)
		if v != nil && req.Kind != "warmup" {
			v.Output = "this is not Go source"
		}
		return v, err
	}
	ev, err := lab.CreateEvaluation(t.Context(), "local", spec)
	if err != nil {
		t.Fatal(err)
	}
	ev = awaitEvaluation(t, store, ev.ID)
	mux := http.NewServeMux()
	lab.RegisterRoutes(mux, manager)
	verify := lab.runtime.verify
	collect := lab.runtime.config.CollectEnvironment
	for _, tc := range []struct {
		name, code    string
		deterministic bool
	}{
		{"model-drift", "MODEL_DRIFT", true}, {"environment", "INSUFFICIENT_EVIDENCE", true}, {"provider", "PROVIDER_UNAVAILABLE", false}, {"timeout", "INFERENCE_TIMEOUT", false}, {"cancel", "USER_CANCELLED", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(func() { lab.runtime.verify = verify; lab.runtime.config.CollectEnvironment = collect })
			body := `{"command":"retry_case","case_id":"` + ev.Manifest.Dataset.Cases[0].ID + `","arm":"candidate","expected_revision":` + fmt.Sprint(ev.Revision) + `}`
			post := func(key string) *httptest.ResponseRecorder {
				r := httptest.NewRequest("POST", "/api/workspaces/local/evaluations/"+ev.ID+"/commands", strings.NewReader(body))
				r.Header.Set("Idempotency-Key", key)
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, r)
				return w
			}
			if tc.name == "environment" {
				lab.runtime.config.CollectEnvironment = func(ctx context.Context) (modelstore.EnvironmentSnapshot, error) {
					v, err := collect(ctx)
					v.GPU = "changed frozen environment"
					return v, err
				}
			} else {
				lab.runtime.verify = func(context.Context, modelstore.ModelRevision) error {
					return identityError(tc.code, "injected preflight condition")
				}
			}
			key := "preflight-" + tc.name
			first := post(key)
			status := 503
			if tc.deterministic {
				status = 409
			}
			if first.Code != status || !strings.Contains(first.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatal(first.Code, first.Body.String())
			}
			lab.runtime.verify = verify
			lab.runtime.config.CollectEnvironment = collect
			second := post(key)
			if tc.deterministic {
				if second.Code != 409 || !strings.Contains(second.Body.String(), `"code":"`+tc.code+`"`) {
					if second.Code == 202 {
						ev = awaitEvaluation(t, store, ev.ID)
					}
					t.Fatalf("same key changed refusal after restoration: %d %s", second.Code, second.Body.String())
				}
				if w := post(key + "-new"); w.Code != 202 {
					t.Fatal("new key blocked", w.Code, w.Body.String())
				}
			} else if second.Code != 202 {
				t.Fatal("transient refusal was cached", second.Code, second.Body.String())
			}
			ev = awaitEvaluation(t, store, ev.ID)
		})
	}
}

func TestHTTPStrictBoundariesAndOwnedReport(t *testing.T) {
	st, err := modelstore.Open(filepath.Join(t.TempDir(), "models.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	manager, err := workspaces.Open(filepath.Join(t.TempDir(), "ws.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	ws, err := manager.SaveWorkspace(workspaces.Workspace{Name: "one", ProjectPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	lab := New(st, nil)
	mux := http.NewServeMux()
	lab.RegisterRoutes(mux, manager)
	handler := middleware.BearerAuth("secret")(mux)
	request := func(method, path, body string, auth bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if auth {
			r.Header.Set("Authorization", "Bearer secret")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		method, path, body string
		auth               bool
		status             int
	}{
		{"GET", "/api/model-lab/models", "", false, 401},
		{"POST", "/api/model-lab/profiles", `{"requested":{},"effective":{}}`, true, 400},
		{"POST", "/api/model-lab/profiles", `{"requested":{}} {}`, true, 400},
		{"POST", "/api/model-lab/profiles", `{"requested":{},"padding":"` + strings.Repeat("x", 65536) + `"}`, true, 400},
		{"GET", "/api/model-lab/models?limit=101", "", true, 400},
		{"GET", "/api/model-lab/models?limit=1&limit=2", "", true, 400},
		{"GET", "/api/model-lab/models?cursor=..%2Fsecret", "", true, 400},
		{"GET", "/api/workspaces/other/reports/absent", "", true, 404},
		{"GET", "/api/workspaces/" + ws.ID + "/evaluations?unknown=1", "", true, 400},
	} {
		if w := request(tc.method, tc.path, tc.body, tc.auth); w.Code != tc.status {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
	p := `{"requested":{"temperature":0,"seed":7,"num_ctx":256,"max_output":32}}`
	w := request("POST", "/api/model-lab/profiles", p, true)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var profile modelstore.InferenceProfile
	if err = json.Unmarshal(w.Body.Bytes(), &profile); err != nil || profile.Requested.Temperature == nil || *profile.Requested.Temperature != 0 {
		t.Fatal(profile, err)
	}
	saved, err := st.GetInferenceProfile(context.Background(), profile.ID)
	if err != nil || saved.Confirmation != "requested_not_attested_by_engine" {
		t.Fatal(saved, err)
	}
}
