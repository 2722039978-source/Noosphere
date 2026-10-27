package errors_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperr "github.com/noosphere/backend/internal/errors"
)

func TestErrorWritersPreserveEscapedMessage(t *testing.T) {
	message := "模型 \"candidate\" 失败\n检查 E:\\models\\local\t后重试"
	for name, write := range map[string]func(http.ResponseWriter, *apperr.AppError){
		"json": apperr.WriteJSON,
		"http": func(w http.ResponseWriter, err *apperr.AppError) {
			apperr.WriteHTTP(w, httptest.NewRequest(http.MethodGet, "/test", nil), err)
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			write(w, apperr.InvalidInput(apperr.CatGateway, message))
			if w.Code != http.StatusBadRequest || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("unexpected error status or content type: %d %q", w.Code, w.Header().Get("Content-Type"))
			}
			var body struct {
				Error apperr.AppError `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("invalid error JSON: %v; body=%q", err, w.Body.String())
			}
			if body.Error.Message != message || body.Error.Code != apperr.ErrInvalidInput || body.Error.Category != apperr.CatGateway {
				t.Fatalf("error payload changed: %+v", body.Error)
			}
		})
	}
}
