package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/noosphere/backend/internal/core"
	"github.com/noosphere/backend/internal/gateway"
)

func newTestMux(t *testing.T) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":{"code":"NOT_FOUND","category":"system","message":"route not found"}}`))
	})
	registerRoutes(mux, core.New(), gateway.New(t.TempDir()+"/models.yaml"))
	return mux
}

func TestHealthHandler(t *testing.T) {
	t.Setenv("NOOSPHERE_BACKEND_INSTANCE", "fixture-instance")
	lc := core.New()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	healthHandler(lc)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response body: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", resp["status"])
	}
	if resp["version"] != Version {
		t.Errorf("expected version=%s, got %v", Version, resp["version"])
	}
	if resp["instance_id"] != "fixture-instance" {
		t.Errorf("missing backend ownership identity: %v", resp["instance_id"])
	}
	if resp["modules"] == nil {
		t.Error("expected modules field in response")
	}
}

func TestUnknownPathReturnsStructured404(t *testing.T) {
	mux := newTestMux(t)
	req := httptest.NewRequest(http.MethodGet, "/api/gateway/nonexistent", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	var resp map[string]map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] == nil || resp["error"]["code"] != "NOT_FOUND" {
		t.Errorf("expected structured NOT_FOUND error, got %s", w.Body.String())
	}
}

func TestKnownGETEndpointsDoNot404(t *testing.T) {
	mux := newTestMux(t)
	paths := []string{
		"/api/gateway/health",
		"/api/gateway/models",
		"/api/gateway/stats",
		"/api/gateway/logs",
	}
	for _, path := range paths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Errorf("GET %s should not return 404, got %d", path, w.Code)
		}
	}
}

func TestWrongMethodIs404NotHandled(t *testing.T) {
	// GET /api/gateway/chat 只注册了 POST — 错误方法不应进入业务 handler
	mux := newTestMux(t)
	req := httptest.NewRequest(http.MethodGet, "/api/gateway/chat", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for GET /chat (POST-only endpoint), got %d", w.Code)
	}
}

func TestChatEndpointDispatched(t *testing.T) {
	// POST /api/gateway/chat 应被分发到 HandleChat (进入业务逻辑, 返回业务错误而非路由 404)
	mux := newTestMux(t)
	req := httptest.NewRequest(http.MethodPost, "/api/gateway/chat", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code == http.StatusNotFound {
		t.Error("POST /api/gateway/chat should be dispatched to HandleChat, not 404 at router level")
	}
}
