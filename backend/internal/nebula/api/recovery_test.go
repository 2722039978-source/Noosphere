package api

import (
	"encoding/json"
	"github.com/noosphere/backend/internal/nebula/engine"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchContractAndSessionRecovery(t *testing.T) {
	dir := t.TempDir()
	eng, err := engine.Open(engine.DefaultOptions(dir))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { eng.Close() })
	mux := http.NewServeMux()
	NewServer(eng).RegisterRoutes(mux)
	post := func(path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(body)))
		return w
	}
	if w := post("/api/v1/sessions/demo/memories", `{"content":"database retry"}`); w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	w := post("/api/v1/sessions/demo/search", `{"query":"retry"}`)
	var response struct {
		Results []struct {
			Key    string `json:"key"`
			Record *struct {
				Content string `json:"content"`
			} `json:"record"`
		} `json:"results"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || response.Results[0].Key == "" || response.Results[0].Record == nil {
		t.Fatalf("IPC search contract broken: %s", w.Body.String())
	}
	eng.Close()
	eng, err = engine.Open(engine.DefaultOptions(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	mux = http.NewServeMux()
	NewServer(eng).RegisterRoutes(mux)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/sessions", nil))
	if !strings.Contains(w.Body.String(), `"id":"demo"`) {
		t.Fatalf("session disappeared after restart: %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("DELETE", "/api/v1/sessions/demo", nil))
	if len(eng.Records("demo:")) != 0 {
		t.Fatal("session delete retained memories")
	}
}
