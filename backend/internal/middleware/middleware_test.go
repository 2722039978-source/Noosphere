package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCORSAllowsConfiguredOrigin(t *testing.T) {
	h := CORS([]string{"http://localhost:1420"})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "http://localhost:1420")
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:1420" {
		t.Fatalf("allow origin = %q", got)
	}
	if !strings.Contains(rr.Header().Get("Access-Control-Allow-Headers"), "Idempotency-Key") {
		t.Fatal("idempotent controls blocked by CORS")
	}
}

func TestCORSRejectsUnknownOrigin(t *testing.T) {
	h := CORS([]string{"http://localhost:1420"})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("next handler must not be called")
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/gateway/chat", nil)
	req.Header.Set("Origin", "https://example.com")
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusForbidden)
	}
}

func TestCORSAllowsOriginlessSidecarRequest(t *testing.T) {
	h := CORS(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}
