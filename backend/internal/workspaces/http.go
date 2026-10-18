package workspaces

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
)

func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func respondError(w http.ResponseWriter, e error) {
	status := http.StatusInternalServerError
	message := "Workspace storage unavailable"
	switch {
	case errors.Is(e, ErrInvalid):
		status = http.StatusBadRequest
		message = e.Error()
	case errors.Is(e, ErrNotFound):
		status = http.StatusNotFound
		message = e.Error()
	case errors.Is(e, ErrConflict):
		status = http.StatusConflict
		message = e.Error()
	case errors.Is(e, ErrBusy):
		status = http.StatusTooManyRequests
		message = e.Error()
	case errors.Is(e, ErrClosed):
		status = http.StatusServiceUnavailable
		message = e.Error()
	}
	respond(w, status, map[string]string{"error": message})
}
func decode(w http.ResponseWriter, r *http.Request, value any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxResultBytes)
	d := json.NewDecoder(r.Body)
	if e := d.Decode(value); e != nil {
		return ErrInvalid
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return ErrInvalid
	}
	return nil
}

func (m *Manager) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/workspaces", func(w http.ResponseWriter, r *http.Request) {
		items, e := m.Workspaces()
		if e != nil {
			respondError(w, e)
			return
		}
		respond(w, 200, map[string]any{"workspaces": items})
	})
	mux.HandleFunc("POST /api/workspaces", func(w http.ResponseWriter, r *http.Request) {
		var input Workspace
		if e := decode(w, r, &input); e != nil {
			respondError(w, e)
			return
		}
		item, e := m.SaveWorkspace(input)
		if e != nil {
			respondError(w, e)
			return
		}
		respond(w, 200, item)
	})
	mux.HandleFunc("GET /api/workspaces/{id}/runs", func(w http.ResponseWriter, r *http.Request) {
		items, e := m.Runs(r.PathValue("id"))
		if e != nil {
			respondError(w, e)
			return
		}
		respond(w, 200, map[string]any{"runs": items})
	})
	mux.HandleFunc("POST /api/workspaces/{id}/runs", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Kind  string          `json:"kind"`
			Title string          `json:"title"`
			Input json.RawMessage `json:"input"`
		}
		if e := decode(w, r, &input); e != nil {
			respondError(w, e)
			return
		}
		item, e := m.Submit(r.PathValue("id"), input.Kind, input.Title, input.Input)
		if e != nil {
			respondError(w, e)
			return
		}
		respond(w, http.StatusAccepted, item)
	})
	mux.HandleFunc("GET /api/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		item, e := m.Run(r.PathValue("id"))
		if e != nil {
			respondError(w, e)
			return
		}
		respond(w, 200, item)
	})
	mux.HandleFunc("GET /api/runs/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		var after int64
		var e error
		if value := r.URL.Query().Get("after"); value != "" {
			after, e = strconv.ParseInt(value, 10, 64)
			if e != nil || after < 0 {
				respondError(w, ErrInvalid)
				return
			}
		}
		items, e := m.Events(r.PathValue("id"), after)
		if e != nil {
			respondError(w, e)
			return
		}
		respond(w, 200, map[string]any{"events": items})
	})
	mux.HandleFunc("POST /api/runs/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		item, e := m.Cancel(r.PathValue("id"))
		if e != nil {
			respondError(w, e)
			return
		}
		respond(w, 200, item)
	})
}
