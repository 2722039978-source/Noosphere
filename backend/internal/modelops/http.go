package modelops

import (
	"net/http"
	"strconv"

	"github.com/noosphere/backend/internal/gateway/providers"
	"github.com/noosphere/backend/internal/modelhttp"
	"github.com/noosphere/backend/internal/modelstore"
	"github.com/noosphere/backend/internal/workspaces"
)

func validCommand(c Command) bool {
	switch c {
	case StartOffline, Advance, Pause, Resume, Reject, Rollback:
		return true
	}
	return false
}
func (s *Service) RegisterRoutes(mux *http.ServeMux, manager *workspaces.Manager) {
	route := func(pattern string, fn func(http.ResponseWriter, *http.Request) error) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			s.admissionMu.Lock()
			if s.admissionStopped {
				s.admissionMu.Unlock()
				modelhttp.Error(w, workspaces.ErrClosed, nil)
				return
			}
			s.httpWork.Add(1)
			s.admissionMu.Unlock()
			defer s.httpWork.Done()
			for _, name := range []string{"ws", "id"} {
				if v := r.PathValue(name); v != "" && !modelhttp.ID(v) {
					modelhttp.Error(w, workspaces.ErrInvalid, nil)
					return
				}
			}
			ws := r.PathValue("ws")
			if ws != "" {
				if _, err := manager.Workspace(ws); err != nil {
					modelhttp.Error(w, err, nil)
					return
				}
			}
			if id := r.PathValue("id"); id != "" {
				owner, ownerErr := s.store.ReleaseWorkspace(r.Context(), id)
				if ownerErr != nil {
					modelhttp.Error(w, ownerErr, nil)
					return
				}
				if owner != ws {
					modelhttp.Error(w, modelstore.ErrNotFound, nil)
					return
				}
				v, e := s.store.GetRelease(r.Context(), id)
				if e != nil {
					modelhttp.Error(w, e, nil)
					return
				}
				if v.WorkspaceID != ws {
					modelhttp.Error(w, &providers.EvaluationError{Code: "STORAGE_UNAVAILABLE"}, nil)
					return
				}
			}
			if err := fn(w, r); err != nil {
				details := map[string]any{}
				id := r.PathValue("id")
				if id != "" {
					if v, e := s.store.GetRelease(r.Context(), id); e == nil && v.WorkspaceID == ws {
						details["release_id"] = id
						details["current_revision"] = v.Revision
					}
					if d, e := s.store.SimulationReceiptDecision(r.Context(), ws, id, r.Header.Get("Idempotency-Key")); e == nil && d != nil {
						details["gates"] = d.Rules
					}
				}
				modelhttp.Error(w, err, details)
			}
		})
	}
	route("GET /api/model-ops/policies", func(w http.ResponseWriter, r *http.Request) error {
		if e := modelhttp.Query(r, "mode"); e != nil {
			return e
		}
		if r.URL.Query().Get("mode") != "simulation" {
			return &providers.EvaluationError{Code: "UNSUPPORTED_MODE"}
		}
		p, e := s.store.GetPolicy(r.Context(), PolicyID)
		if e != nil {
			return e
		}
		modelhttp.JSON(w, 200, map[string]any{"policies": []any{struct {
			modelstore.ReleasePolicy
			PolicyHash string `json:"policy_hash"`
		}{p, p.Hash}}})
		return nil
	})
	base := "/api/workspaces/{ws}/releases"
	route("POST "+base, func(w http.ResponseWriter, r *http.Request) error {
		if e := modelhttp.Query(r); e != nil {
			return e
		}
		key, e := modelhttp.Key(r)
		if e != nil {
			return e
		}
		var b struct {
			Mode     string `json:"mode"`
			Workflow string `json:"workflow_id"`
			Report   string `json:"report_id"`
			Policy   string `json:"policy_id,omitempty"`
			Filter   string `json:"task_type_filter,omitempty"`
		}
		if e = modelhttp.Decode(w, r, &b); e != nil {
			return e
		}
		if b.Mode != "simulation" {
			return &providers.EvaluationError{Code: "UNSUPPORTED_MODE"}
		}
		if !modelhttp.ID(b.Report) || !modelhttp.ID(b.Workflow) {
			return workspaces.ErrInvalid
		}
		if b.Workflow != Workflow || (b.Policy != "" && b.Policy != PolicyID) || (b.Filter != "" && b.Filter != TaskType) {
			return &providers.EvaluationError{Code: "UNSUPPORTED_CAPABILITY"}
		}
		// Verify report ownership before invoking creation, including failed gates.
		owner, e := s.store.ReportWorkspace(r.Context(), b.Report)
		if e != nil {
			return e
		}
		if owner != r.PathValue("ws") {
			return modelstore.ErrNotFound
		}
		v, e := s.Create(r.Context(), r.PathValue("ws"), CreateSpec{Mode: b.Mode, WorkflowID: b.Workflow, TaskTypeFilter: TaskType, ReportID: b.Report}, key)
		if e != nil {
			return e
		}
		modelhttp.JSON(w, 201, v)
		return nil
	})
	route("GET "+base, func(w http.ResponseWriter, r *http.Request) error {
		c, n, e := modelhttp.Page(r)
		if e != nil {
			return e
		}
		ids, next, e := s.store.PageIDs(r.Context(), "releases", r.PathValue("ws"), "", c, n)
		if e != nil {
			return e
		}
		items := []modelstore.Release{}
		for _, id := range ids {
			v, e := s.store.GetRelease(r.Context(), id)
			if e != nil {
				return e
			}
			items = append(items, v)
		}
		modelhttp.JSON(w, 200, map[string]any{"releases": items, "next_cursor": next})
		return nil
	})
	route("GET "+base+"/{id}", func(w http.ResponseWriter, r *http.Request) error {
		if e := modelhttp.Query(r); e != nil {
			return e
		}
		v, e := s.store.GetRelease(r.Context(), r.PathValue("id"))
		if e != nil {
			return e
		}
		modelhttp.JSON(w, 200, v)
		return nil
	})
	route("POST "+base+"/{id}/preview", func(w http.ResponseWriter, r *http.Request) error {
		if e := modelhttp.Query(r); e != nil {
			return e
		}
		var b struct {
			Command Command `json:"command"`
		}
		if e := modelhttp.Decode(w, r, &b); e != nil {
			return e
		}
		if !validCommand(b.Command) {
			return workspaces.ErrInvalid
		}
		v, e := s.Preview(r.Context(), r.PathValue("ws"), r.PathValue("id"), b.Command)
		if e != nil {
			return e
		}
		modelhttp.JSON(w, 200, v)
		return nil
	})
	route("POST "+base+"/{id}/commands", func(w http.ResponseWriter, r *http.Request) error {
		if e := modelhttp.Query(r); e != nil {
			return e
		}
		key, e := modelhttp.Key(r)
		if e != nil {
			return e
		}
		var b struct {
			Command  Command `json:"command"`
			Revision *int64  `json:"expected_revision"`
		}
		if e = modelhttp.Decode(w, r, &b); e != nil {
			return e
		}
		if !validCommand(b.Command) || b.Revision == nil || *b.Revision < 1 {
			return workspaces.ErrInvalid
		}
		v, e := s.Apply(r.Context(), r.PathValue("ws"), r.PathValue("id"), b.Command, *b.Revision, key)
		if e != nil {
			return e
		}
		modelhttp.JSON(w, 200, v)
		return nil
	})
	route("POST "+base+"/{id}/simulation", func(w http.ResponseWriter, r *http.Request) error {
		if e := modelhttp.Query(r); e != nil {
			return e
		}
		key, e := modelhttp.Key(r)
		if e != nil {
			return e
		}
		var b struct {
			Scenario string `json:"scenario"`
			Revision *int64 `json:"expected_revision"`
			Seed     int64  `json:"seed,omitempty"`
			Seconds  int64  `json:"virtual_seconds,omitempty"`
		}
		if e = modelhttp.Decode(w, r, &b); e != nil {
			return e
		}
		if b.Revision == nil || *b.Revision < 1 || b.Seed < 0 || b.Seconds < 0 || b.Seconds > 3600 {
			return workspaces.ErrInvalid
		}
		switch b.Scenario {
		case "replay_stage", "inject_oom", "inject_failure":
		default:
			return workspaces.ErrInvalid
		}
		v, e := s.Schedule(r.Context(), manager, r.PathValue("ws"), r.PathValue("id"), SimulationRequest{Scenario: b.Scenario, Seed: b.Seed, VirtualSeconds: b.Seconds}, *b.Revision, key)
		if e != nil {
			return e
		}
		modelhttp.JSON(w, 202, map[string]any{"release_id": r.PathValue("id"), "run_id": v.ID, "status": v.Status})
		return nil
	})
	route("GET "+base+"/{id}/events", func(w http.ResponseWriter, r *http.Request) error {
		if e := modelhttp.Query(r, "after", "limit"); e != nil {
			return e
		}
		after := int64(0)
		if q := r.URL.Query().Get("after"); q != "" {
			v, e := strconv.ParseInt(q, 10, 64)
			if e != nil || v < 0 {
				return workspaces.ErrInvalid
			}
			after = v
		}
		n, e := modelhttp.Limit(r, 100)
		if e != nil {
			return e
		}
		items, e := s.store.ReleaseEventsPage(r.Context(), r.PathValue("id"), after, n)
		if e != nil {
			return e
		}
		events := []any{}
		for _, v := range items {
			events = append(events, struct {
				modelstore.ReleaseEvent
				EventHash string `json:"event_hash"`
			}{v, v.EventHash})
			after = v.Sequence
		}
		modelhttp.JSON(w, 200, map[string]any{"events": events, "next_after": after})
		return nil
	})
}

// StopAdmission rejects new HTTP controls; checkpoints still persist through
// the worker until the Manager has fully drained.
func (s *Service) StopAdmission() {
	s.admissionMu.Lock()
	s.admissionStopped = true
	s.admissionMu.Unlock()
}

func (s *Service) DrainHTTP() { s.httpWork.Wait() }
