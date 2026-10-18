package workspaces

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// HTTP must expose validation/not-found/capacity failures instead of success-shaped empty results.
func TestHTTPWorkspaceAndRunContract(t *testing.T) {
	m := testManager(t)
	mux := http.NewServeMux()
	m.RegisterRoutes(mux)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		return w
	}
	if w := request("GET", "/api/workspaces", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"id":"local"`) {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	for _, tc := range []struct {
		method, path, body string
		code               int
	}{
		{"POST", "/api/workspaces", `{"name":""}`, 400},
		{"POST", "/api/workspaces", `{"name":"x"} {}`, 400},
		{"GET", "/api/workspaces/missing/runs", "", 404},
		{"GET", "/api/runs/missing", "", 404},
		{"GET", "/api/runs/missing/events?after=no", "", 400},
		{"POST", "/api/workspaces/local/runs", `{"kind":"unknown"}`, 400},
	} {
		w := request(tc.method, tc.path, tc.body)
		if w.Code != tc.code {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
	}
	m.Register("diagnosis", "interactive", func(context.Context, Workspace, json.RawMessage, func(string, string, any)) (any, error) {
		return map[string]string{"answer": "ok"}, nil
	})
	w := request("POST", "/api/workspaces/local/runs", `{"kind":"diagnosis","title":"check","input":{"issue":"a"}}`)
	if w.Code != 202 {
		t.Fatalf("submit: %d %s", w.Code, w.Body.String())
	}
	var r Run
	if e := json.Unmarshal(w.Body.Bytes(), &r); e != nil {
		t.Fatal(e)
	}
	waitRun(t, m, r.ID, "completed")
	if w = request("GET", "/api/runs/"+r.ID+"/events?after=0", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"completed"`) {
		t.Fatalf("events: %d %s", w.Code, w.Body.String())
	}
	if w = request("POST", "/api/runs/"+r.ID+"/cancel", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"completed"`) {
		t.Fatalf("terminal cancel: %d %s", w.Code, w.Body.String())
	}
}
