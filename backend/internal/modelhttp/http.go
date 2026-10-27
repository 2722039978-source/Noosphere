// Package modelhttp contains the bounded JSON transport shared by model services.
package modelhttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	"github.com/noosphere/backend/internal/gateway/providers"
	"github.com/noosphere/backend/internal/modelstore"
	"github.com/noosphere/backend/internal/workspaces"
)

const MaxBody = 64 << 10
const MaxResponse = 8 << 20

var errResponseLimit = errors.New("response limit exceeded")

type boundedResponse struct {
	bytes.Buffer
	limit int
}

func (b *boundedResponse) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, errResponseLimit
	}
	return b.Buffer.Write(p)
}

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

func ID(v string) bool { return safeID.MatchString(v) }
func JSON(w http.ResponseWriter, status int, v any) {
	writeJSON(w, status, v, MaxResponse)
}
func writeJSON(w http.ResponseWriter, status int, v any, limit int) {
	buffer := boundedResponse{limit: limit}
	if err := json.NewEncoder(&buffer).Encode(v); err != nil {
		code := "SERIALIZATION_FAILED"
		if errors.Is(err, errResponseLimit) {
			code = "RESPONSE_TOO_LARGE"
		}
		Error(w, &providers.EvaluationError{Code: code}, nil)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buffer.Bytes())
}
func Decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, MaxBody)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return workspaces.ErrInvalid
	}
	if d.Decode(new(any)) != io.EOF {
		return workspaces.ErrInvalid
	}
	return nil
}
func Query(r *http.Request, names ...string) error {
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		return workspaces.ErrInvalid
	}
	allowed := map[string]bool{}
	for _, n := range names {
		allowed[n] = true
	}
	for k, v := range q {
		if !allowed[k] || len(v) != 1 || v[0] == "" {
			return workspaces.ErrInvalid
		}
	}
	return nil
}
func Page(r *http.Request) (string, int, error) {
	if e := Query(r, "cursor", "limit"); e != nil {
		return "", 0, e
	}
	cursor := r.URL.Query().Get("cursor")
	if cursor != "" && !ID(cursor) {
		return "", 0, workspaces.ErrInvalid
	}
	n, e := Limit(r, 20)
	return cursor, n, e
}
func Limit(r *http.Request, def int) (int, error) {
	v := r.URL.Query().Get("limit")
	if v == "" {
		return def, nil
	}
	n, e := strconv.Atoi(v)
	if e != nil || n < 1 || n > 100 {
		return 0, workspaces.ErrInvalid
	}
	return n, nil
}
func Key(r *http.Request) (string, error) {
	v := r.Header.Values("Idempotency-Key")
	if len(v) != 1 || !ID(v[0]) {
		return "", workspaces.ErrInvalid
	}
	return v[0], nil
}
func Error(w http.ResponseWriter, e error, details any) {
	status, code := 503, "STORAGE_UNAVAILABLE"
	switch {
	case errors.Is(e, modelstore.ErrNotFound), errors.Is(e, workspaces.ErrNotFound):
		status, code = 404, "NOT_FOUND"
	case errors.Is(e, workspaces.ErrInvalid):
		status, code = 400, "INVALID_REQUEST"
	case errors.Is(e, modelstore.ErrRevisionConflict):
		status, code = 409, "REVISION_CONFLICT"
	case errors.Is(e, modelstore.ErrIdempotencyConflict):
		status, code = 409, "IDEMPOTENCY_CONFLICT"
	case errors.Is(e, modelstore.ErrEvaluationFrozen):
		status, code = 409, "EVALUATION_FROZEN"
	case errors.Is(e, modelstore.ErrModelDrift):
		status, code = 409, "MODEL_DRIFT"
	case errors.Is(e, modelstore.ErrInsufficientEvidence), errors.Is(e, modelstore.ErrArtifactMissing):
		status, code = 409, "INSUFFICIENT_EVIDENCE"
	case errors.Is(e, modelstore.ErrStaleLease):
		status, code = 409, "STALE_LEASE"
	case errors.Is(e, modelstore.ErrInvalid), errors.Is(e, workspaces.ErrConflict):
		status, code = 409, "INVALID_RECORD"
	case errors.Is(e, workspaces.ErrBusy):
		status, code = 429, "QUEUE_FULL"
	}
	var p *providers.EvaluationError
	if errors.As(e, &p) {
		code = p.Code
		switch code {
		case "INVALID_REQUEST":
			status = 400
		case "UNSUPPORTED_MODE", "UNSUPPORTED_CAPABILITY":
			status = 422
		case "BUDGET_EXCEEDED":
			status = 429
		case "MODEL_DRIFT", "INSUFFICIENT_EVIDENCE":
			status = 409
		default:
			status = 503
		}
	}
	// Never expose raw storage/provider errors, which can contain URLs or host paths.
	body := map[string]any{"code": code, "message": "Model engineering request failed: " + code}
	if details != nil {
		body["details"] = details
	}
	JSON(w, status, map[string]any{"error": body})
}
