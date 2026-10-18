package errors_test

import (
	"errors"
	"net/http"
	"testing"

	apperr "github.com/noosphere/backend/internal/errors"
)

func TestNew(t *testing.T) {
	e := apperr.New(apperr.ErrNotFound, apperr.CatGateway, "model not found")
	if e.Code != apperr.ErrNotFound {
		t.Errorf("code: got %s, want %s", e.Code, apperr.ErrNotFound)
	}
	if e.Category != apperr.CatGateway {
		t.Errorf("category: got %s, want %s", e.Category, apperr.CatGateway)
	}
	if e.HTTPStatus != http.StatusNotFound {
		t.Errorf("http status: got %d, want %d", e.HTTPStatus, http.StatusNotFound)
	}
}

func TestWrap(t *testing.T) {
	cause := errors.New("connection refused")
	e := apperr.Wrap(cause, apperr.ErrUnavailable, apperr.CatGateway, "LLM call failed")

	if !errors.Is(e, cause) {
		t.Error("Wrap should preserve the cause via Unwrap")
	}
	if e.Code != apperr.ErrUnavailable {
		t.Errorf("code: got %s, want %s", e.Code, apperr.ErrUnavailable)
	}
}

func TestConvenienceFunctions(t *testing.T) {
	nf := apperr.NotFound(apperr.CatNebula, "key not found")
	if nf.Code != apperr.ErrNotFound {
		t.Error("NotFound should set ErrNotFound")
	}

	ii := apperr.InvalidInput(apperr.CatGateway, "invalid body")
	if ii.Code != apperr.ErrInvalidInput {
		t.Error("InvalidInput should set ErrInvalidInput")
	}

	ie := apperr.Internal(apperr.CatSystem, "something broke")
	if ie.Code != apperr.ErrInternal {
		t.Error("Internal should set ErrInternal")
	}
	if ie.HTTPStatus != http.StatusInternalServerError {
		t.Errorf("Internal HTTP status: got %d, want 500", ie.HTTPStatus)
	}
}

func TestErrorString(t *testing.T) {
	e := apperr.New(apperr.ErrConflict, apperr.CatDevOps, "already exists")
	s := e.Error()
	if s == "" {
		t.Error("Error() should return non-empty string")
	}
	// Should contain code and category
	if !contains(s, "CONFLICT") || !contains(s, "devops") {
		t.Errorf("Error string missing code/category: %s", s)
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
