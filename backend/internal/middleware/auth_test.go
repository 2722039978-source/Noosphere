package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocalBearerAuthentication(t *testing.T) {
	for _, tc := range []struct {
		name, token, header string
		want                int
	}{{"missing", "secret", "", 401}, {"wrong", "secret", "Bearer wrong", 401}, {"valid", "secret", "Bearer secret", 204}, {"unconfigured", "", "", 503}} {
		t.Run(tc.name, func(t *testing.T) {
			h := BearerAuth(tc.token)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
			r := httptest.NewRequest("POST", "/api/private", nil)
			r.Header.Set("Authorization", tc.header)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d, want %d", w.Code, tc.want)
			}
		})
	}
}
