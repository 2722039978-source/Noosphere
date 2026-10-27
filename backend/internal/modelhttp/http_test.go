package modelhttp

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResponseLimitCommitsOnlyCompleteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, 201, map[string]string{"value": "bounded"}, 64)
	if w.Code != 201 || !strings.Contains(w.Body.String(), `"value":"bounded"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	writeJSON(w, 201, map[string]string{"value": strings.Repeat("x", 64)}, 32)
	if w.Code != 503 || !strings.Contains(w.Body.String(), `"code":"RESPONSE_TOO_LARGE"`) || strings.Contains(w.Body.String(), strings.Repeat("x", 64)) {
		t.Fatal(w.Code, w.Body.String())
	}
}
