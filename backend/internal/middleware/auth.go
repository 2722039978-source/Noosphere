package middleware

import (
	"crypto/subtle"
	"net/http"
)

// BearerAuth is independent of CORS: a local caller still needs the secret
// delivered directly by the parent process. Never log the supplied token.
func BearerAuth(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token == "" {
				http.Error(w, "local authentication is not configured", http.StatusServiceUnavailable)
				return
			}
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 4*1024*1024)
			next.ServeHTTP(w, r)
		})
	}
}
