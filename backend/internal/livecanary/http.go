package livecanary

import (
	"net/http"
	"strconv"

	"github.com/noosphere/backend/internal/modelhttp"
	"github.com/noosphere/backend/internal/modelstore"
	"github.com/noosphere/backend/internal/workspaces"
)

func (s *Service) RegisterRoutes(mux *http.ServeMux, manager *workspaces.Manager) {
	route := func(pattern string, fn func(http.ResponseWriter, *http.Request) error) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			s.mu.Lock()
			if s.closed {
				s.mu.Unlock()
				modelhttp.Error(w, workspaces.ErrClosed, nil)
				return
			}
			s.httpWork.Add(1)
			s.mu.Unlock()
			defer s.httpWork.Done()
			for _, name := range []string{"ws", "id", "request"} {
				v := r.PathValue(name)
				if v != "" && !modelhttp.ID(v) {
					modelhttp.Error(w, workspaces.ErrInvalid, nil)
					return
				}
			}
			ws := r.PathValue("ws")
			if _, err := manager.Workspace(ws); err != nil {
				modelhttp.Error(w, err, nil)
				return
			}
			if err := fn(w, r); err != nil {
				var details any
				if id := r.PathValue("id"); id != "" {
					if current, e := s.Get(r.Context(), ws, id); e == nil {
						details = map[string]any{"current_revision": current.Release.Revision, "decision": current.Decision}
					}
				}
				modelhttp.Error(w, err, details)
			}
		})
	}
	root := "/api/workspaces/{ws}/live-canaries"
	route("GET "+root, func(w http.ResponseWriter, r *http.Request) error {
		cursor, limit, err := modelhttp.Page(r)
		if err != nil {
			return err
		}
		items, next, err := s.store.ListLive(r.Context(), r.PathValue("ws"), cursor, limit)
		if err != nil {
			return err
		}
		modelhttp.JSON(w, 200, map[string]any{"releases": items, "next_cursor": next, "default_policy": DefaultPolicy(), "source": "live_provider", "request_limit": modelstore.LiveRequestLimit})
		return nil
	})
	route("POST "+root, func(w http.ResponseWriter, r *http.Request) error {
		if err := modelhttp.Query(r); err != nil {
			return err
		}
		var spec CreateSpec
		if err := modelhttp.Decode(w, r, &spec); err != nil {
			return err
		}
		key, err := modelhttp.Key(r)
		if err != nil {
			return err
		}
		value, err := s.Create(r.Context(), r.PathValue("ws"), key, spec)
		if err != nil {
			return err
		}
		modelhttp.JSON(w, 201, value)
		return nil
	})
	route("GET "+root+"/{id}", func(w http.ResponseWriter, r *http.Request) error {
		if err := modelhttp.Query(r); err != nil {
			return err
		}
		value, err := s.Get(r.Context(), r.PathValue("ws"), r.PathValue("id"))
		if err != nil {
			return err
		}
		modelhttp.JSON(w, 200, value)
		return nil
	})
	route("POST "+root+"/{id}/commands", func(w http.ResponseWriter, r *http.Request) error {
		if err := modelhttp.Query(r); err != nil {
			return err
		}
		var body struct {
			Command  string `json:"command"`
			Revision int64  `json:"expected_revision"`
		}
		if err := modelhttp.Decode(w, r, &body); err != nil {
			return err
		}
		key, err := modelhttp.Key(r)
		if err != nil {
			return err
		}
		value, err := s.Command(r.Context(), r.PathValue("ws"), r.PathValue("id"), body.Command, key, body.Revision)
		if err != nil {
			return err
		}
		modelhttp.JSON(w, 200, value)
		return nil
	})
	route("POST "+root+"/{id}/requests", func(w http.ResponseWriter, r *http.Request) error {
		if err := modelhttp.Query(r); err != nil {
			return err
		}
		var body struct {
			Prompt string `json:"prompt"`
		}
		if err := modelhttp.Decode(w, r, &body); err != nil {
			return err
		}
		key, err := modelhttp.Key(r)
		if err != nil {
			return err
		}
		value, err := s.Request(r.Context(), r.PathValue("ws"), r.PathValue("id"), key, body.Prompt)
		if err != nil {
			return err
		}
		modelhttp.JSON(w, 200, value)
		return nil
	})
	route("GET "+root+"/{id}/requests", func(w http.ResponseWriter, r *http.Request) error {
		cursor, limit, err := modelhttp.Page(r)
		if err != nil {
			return err
		}
		if limit > 20 {
			limit = 20
		}
		items, next, err := s.store.ListLiveRequests(r.Context(), r.PathValue("ws"), r.PathValue("id"), cursor, limit)
		if err != nil {
			return err
		}
		modelhttp.JSON(w, 200, map[string]any{"requests": items, "next_cursor": next})
		return nil
	})
	route("GET "+root+"/{id}/requests/{request}", func(w http.ResponseWriter, r *http.Request) error {
		if err := modelhttp.Query(r); err != nil {
			return err
		}
		value, err := s.store.GetLiveRequest(r.Context(), r.PathValue("ws"), r.PathValue("id"), r.PathValue("request"))
		if err != nil {
			return err
		}
		modelhttp.JSON(w, 200, value)
		return nil
	})
	route("POST "+root+"/{id}/feedback", func(w http.ResponseWriter, r *http.Request) error {
		if err := modelhttp.Query(r); err != nil {
			return err
		}
		var body struct {
			RequestID string `json:"request_id"`
			Rating    string `json:"rating"`
			Note      string `json:"note"`
		}
		if err := modelhttp.Decode(w, r, &body); err != nil {
			return err
		}
		if !modelhttp.ID(body.RequestID) {
			return workspaces.ErrInvalid
		}
		key, err := modelhttp.Key(r)
		if err != nil {
			return err
		}
		value, err := s.Feedback(r.Context(), r.PathValue("ws"), r.PathValue("id"), body.RequestID, body.Rating, body.Note, key)
		if err != nil {
			return err
		}
		modelhttp.JSON(w, 200, value)
		return nil
	})
	route("GET "+root+"/{id}/events", func(w http.ResponseWriter, r *http.Request) error {
		if err := modelhttp.Query(r, "after", "limit"); err != nil {
			return err
		}
		limit, err := modelhttp.Limit(r, 20)
		if err != nil {
			return err
		}
		// Each event contains a frozen, bounded gate observation set. Keep its
		// transport page below the desktop's eight MiB response ceiling.
		if limit > 5 {
			limit = 5
		}
		after := int64(0)
		if value := r.URL.Query().Get("after"); value != "" {
			after, err = strconv.ParseInt(value, 10, 64)
			if err != nil || after < 0 {
				return workspaces.ErrInvalid
			}
		}
		events, err := s.store.LiveEvents(r.Context(), r.PathValue("ws"), r.PathValue("id"), after, limit)
		if err != nil {
			return err
		}
		next := after
		if len(events) > 0 {
			next = events[len(events)-1].Sequence
		}
		modelhttp.JSON(w, 200, map[string]any{"events": events, "next_after": next})
		return nil
	})
}
