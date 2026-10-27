package modellab

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/noosphere/backend/internal/modelhttp"
	"github.com/noosphere/backend/internal/modelstore"
	"github.com/noosphere/backend/internal/workspaces"
)

type evaluationReference struct {
	EvaluationID string `json:"evaluation_id"`
	RunID        string `json:"run_id"`
	Status       string `json:"status"`
	Revision     int64  `json:"revision"`
}
type attemptSummary struct {
	ID               string `json:"id"`
	CaseID           string `json:"case_id"`
	Arm              string `json:"arm"`
	AttemptNo        int    `json:"attempt_no"`
	State            string `json:"state"`
	Verdict          string `json:"verdict"`
	ErrorCode        string `json:"error_code,omitempty"`
	SupersedesReason string `json:"supersedes_reason,omitempty"`
}
type artifactContent struct {
	ID        string `json:"id"`
	Content   string `json:"content,omitempty"`
	Missing   bool   `json:"missing"`
	Truncated bool   `json:"truncated"`
	Redacted  bool   `json:"redacted,omitempty"`
}

func (s *Service) RegisterRoutes(mux *http.ServeMux, manager *workspaces.Manager) {
	route := func(pattern string, fn func(http.ResponseWriter, *http.Request) error) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			s.httpMu.Lock()
			if s.httpStopped {
				s.httpMu.Unlock()
				modelhttp.Error(w, workspaces.ErrClosed, nil)
				return
			}
			s.httpWork.Add(1)
			s.httpMu.Unlock()
			defer s.httpWork.Done()
			for _, name := range []string{"ws", "id", "case"} {
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
			if err := fn(w, r); err != nil {
				details := map[string]any{}
				if id := r.PathValue("id"); id != "" {
					if owner, ownerErr := s.store.EvaluationWorkspace(r.Context(), id); ownerErr == nil && owner == ws {
						if e, readErr := s.store.GetEvaluation(r.Context(), id); readErr == nil {
							details["current_revision"] = e.Revision
						}
					}
				}
				modelhttp.Error(w, err, details)
			}
		})
	}
	route("POST /api/model-lab/discovery", func(w http.ResponseWriter, r *http.Request) error {
		if e := modelhttp.Query(r); e != nil {
			return e
		}
		var body struct {
			GatewayModelID string `json:"gateway_model_id"`
		}
		if e := modelhttp.Decode(w, r, &body); e != nil {
			return e
		}
		if !modelhttp.ID(body.GatewayModelID) {
			return workspaces.ErrInvalid
		}
		if s.gateway == nil {
			return identityError("PROVIDER_UNAVAILABLE", "gateway unavailable")
		}
		d, e := s.Discover(r.Context(), body.GatewayModelID)
		if e != nil {
			return e
		}
		modelhttp.JSON(w, 200, d)
		return nil
	})
	route("POST /api/model-lab/models", func(w http.ResponseWriter, r *http.Request) error {
		if e := modelhttp.Query(r); e != nil {
			return e
		}
		var body struct {
			GatewayModelID string `json:"gateway_model_id"`
			Hash           string `json:"discovered_identity_hash"`
		}
		if e := modelhttp.Decode(w, r, &body); e != nil {
			return e
		}
		if !modelhttp.ID(body.GatewayModelID) || len(body.Hash) != 64 {
			return workspaces.ErrInvalid
		}
		if s.gateway == nil {
			return identityError("PROVIDER_UNAVAILABLE", "gateway unavailable")
		}
		d, e := s.Discover(r.Context(), body.GatewayModelID)
		if e != nil {
			return e
		}
		name := ""
		for _, m := range d.Models {
			if m.MetadataHash == body.Hash {
				if name != "" {
					return identityError("MODEL_DRIFT", "ambiguous discovery")
				}
				name = m.Name
			}
		}
		if name == "" {
			return identityError("MODEL_DRIFT", "fresh discovery confirmation required")
		}
		m, e := s.Register(r.Context(), body.GatewayModelID, name, body.Hash)
		if e != nil {
			return e
		}
		modelhttp.JSON(w, 201, m)
		return nil
	})
	route("GET /api/model-lab/models", func(w http.ResponseWriter, r *http.Request) error {
		c, n, e := modelhttp.Page(r)
		if e != nil {
			return e
		}
		ids, next, e := s.store.PageIDs(r.Context(), "models", "", "", c, n)
		if e != nil {
			return e
		}
		items := []modelstore.ModelRevision{}
		for _, id := range ids {
			m, e := s.GetModel(r.Context(), id)
			if e != nil {
				return e
			}
			items = append(items, m)
		}
		modelhttp.JSON(w, 200, map[string]any{"models": items, "next_cursor": next})
		return nil
	})
	route("GET /api/model-lab/datasets", func(w http.ResponseWriter, r *http.Request) error {
		if e := modelhttp.Query(r); e != nil {
			return e
		}
		d, e := LoadDataset(DatasetID)
		if e != nil {
			return e
		}
		modelhttp.JSON(w, 200, map[string]any{"datasets": []modelstore.DatasetRevision{d.Revision()}})
		return nil
	})
	route("POST /api/model-lab/profiles", func(w http.ResponseWriter, r *http.Request) error {
		if e := modelhttp.Query(r); e != nil {
			return e
		}
		var body struct {
			Requested modelstore.Options `json:"requested"`
		}
		if e := modelhttp.Decode(w, r, &body); e != nil {
			return e
		}
		p, e := s.SaveProfile(r.Context(), modelstore.InferenceProfile{Requested: body.Requested})
		if e != nil {
			return e
		}
		modelhttp.JSON(w, 201, p)
		return nil
	})
	route("GET /api/model-lab/profiles", func(w http.ResponseWriter, r *http.Request) error {
		c, n, e := modelhttp.Page(r)
		if e != nil {
			return e
		}
		ids, next, e := s.store.PageIDs(r.Context(), "profiles", "", "", c, n)
		if e != nil {
			return e
		}
		items := []modelstore.InferenceProfile{}
		for _, id := range ids {
			p, e := s.GetProfile(r.Context(), id)
			if e != nil {
				return e
			}
			items = append(items, p)
		}
		modelhttp.JSON(w, 200, map[string]any{"profiles": items, "next_cursor": next})
		return nil
	})
	route("GET /api/model-lab/environment", func(w http.ResponseWriter, r *http.Request) error {
		if e := modelhttp.Query(r); e != nil {
			return e
		}
		if s.runtime == nil {
			return identityError("PROVIDER_UNAVAILABLE", "runtime unavailable")
		}
		env, e := s.runtime.config.CollectEnvironment(r.Context())
		environmentAvailable := e == nil
		if e != nil && (env.Hardware == nil || r.Context().Err() != nil) {
			return identityError("PROVIDER_UNAVAILABLE", "environment unavailable")
		}
		env, e = freezeEnvironment(env)
		if e != nil {
			return e
		}
		_, ready := s.runtimeReady()
		// These are configured ceilings, not measured RAM/VRAM availability.
		// Returning them keeps clients aligned with the same budget used by
		// CreateEvaluation, including deployments with lower local limits.
		// Read-only hardware inspection remains useful on machines without Go.
		// This does not relax the collector error used by evaluation admission.
		pre := map[string]any{"available": ready == nil && environmentAvailable, "budget": s.runtime.config.Budget}
		if ready != nil {
			pre["code"] = "RUNNER_UNAVAILABLE"
		}
		if !environmentAvailable {
			pre["code"] = "ENVIRONMENT_UNAVAILABLE"
		}
		modelhttp.JSON(w, 200, map[string]any{"environment": env, "preflight": pre})
		return nil
	})
	base := "/api/workspaces/{ws}"
	route("POST "+base+"/evaluations", func(w http.ResponseWriter, r *http.Request) error {
		if e := modelhttp.Query(r); e != nil {
			return e
		}
		key, e := modelhttp.Key(r)
		if e != nil {
			return e
		}
		var body struct {
			Stable    string `json:"stable_revision_id"`
			Candidate string `json:"candidate_revision_id"`
			Profile   string `json:"profile_id"`
			Dataset   string `json:"dataset_id,omitempty"`
			Workflow  string `json:"workflow_id,omitempty"`
		}
		if e = modelhttp.Decode(w, r, &body); e != nil {
			return e
		}
		for _, id := range []string{body.Stable, body.Candidate, body.Profile} {
			if !modelhttp.ID(id) {
				return workspaces.ErrInvalid
			}
		}
		for _, id := range []string{body.Dataset, body.Workflow} {
			if id != "" && !modelhttp.ID(id) {
				return workspaces.ErrInvalid
			}
		}
		v, e := s.CreateEvaluation(r.Context(), r.PathValue("ws"), EvaluationSpec{StableRevisionID: body.Stable, CandidateRevisionID: body.Candidate, ProfileID: body.Profile, DatasetID: body.Dataset, WorkflowID: body.Workflow, IdempotencyKey: key})
		if e != nil {
			return e
		}
		modelhttp.JSON(w, 202, s.reference(r, v, manager))
		return nil
	})
	route("GET "+base+"/evaluations", func(w http.ResponseWriter, r *http.Request) error {
		c, n, e := modelhttp.Page(r)
		if e != nil {
			return e
		}
		ids, next, e := s.store.PageIDs(r.Context(), "evaluations", r.PathValue("ws"), "", c, n)
		if e != nil {
			return e
		}
		items := []modelstore.Evaluation{}
		for _, id := range ids {
			v, e := s.store.GetEvaluation(r.Context(), id)
			if e != nil {
				return e
			}
			items = append(items, v)
		}
		modelhttp.JSON(w, 200, map[string]any{"evaluations": items, "next_cursor": next})
		return nil
	})
	route("GET "+base+"/evaluations/{id}", func(w http.ResponseWriter, r *http.Request) error {
		v, e := s.ownedEvaluation(r)
		if e != nil {
			return e
		}
		c, n, e := modelhttp.Page(r)
		if e != nil {
			return e
		}
		ids, next, e := s.store.PageIDs(r.Context(), "attempts", v.ID, "", c, n)
		if e != nil {
			return e
		}
		items := []attemptSummary{}
		for _, id := range ids {
			a, e := s.store.GetAttempt(r.Context(), id)
			if e != nil {
				return e
			}
			items = append(items, attemptSummary{a.ID, a.CaseID, a.Arm, a.AttemptNo, a.State, a.Verdict, a.ErrorCode, a.SupersedesReason})
		}
		p, e := s.store.AttemptProgress(r.Context(), v.ID)
		if e != nil {
			return e
		}
		reportID := ""
		if report, err := s.store.LatestReport(r.Context(), v.ID); err == nil {
			reportID = report.ID
		} else if !errors.Is(err, modelstore.ErrNotFound) {
			return err
		}
		modelhttp.JSON(w, 200, map[string]any{"evaluation": v, "attempts": items, "progress": p, "latest_report_id": reportID, "next_cursor": next})
		return nil
	})
	route("POST "+base+"/evaluations/{id}/commands", func(w http.ResponseWriter, r *http.Request) error {
		if _, e := s.ownedEvaluation(r); e != nil {
			return e
		}
		if e := modelhttp.Query(r); e != nil {
			return e
		}
		key, e := modelhttp.Key(r)
		if e != nil {
			return e
		}
		var b struct {
			Command  string `json:"command"`
			Revision *int64 `json:"expected_revision"`
			CaseID   string `json:"case_id,omitempty"`
			Arm      string `json:"arm,omitempty"`
		}
		if e = modelhttp.Decode(w, r, &b); e != nil {
			return e
		}
		if b.Revision == nil || *b.Revision < 1 {
			return workspaces.ErrInvalid
		}
		switch b.Command {
		case "cancel", "resume":
			if b.CaseID != "" || b.Arm != "" {
				return workspaces.ErrInvalid
			}
		case "retry_case":
			if !modelhttp.ID(b.CaseID) || (b.Arm != "stable" && b.Arm != "candidate") {
				return workspaces.ErrInvalid
			}
		default:
			return workspaces.ErrInvalid
		}
		v, e := s.Control(r.Context(), modelstore.EvaluationControl{WorkspaceID: r.PathValue("ws"), EvaluationID: r.PathValue("id"), Command: b.Command, ExpectedRevision: *b.Revision, CaseID: b.CaseID, Arm: b.Arm, Key: key})
		if e != nil {
			return e
		}
		modelhttp.JSON(w, 202, s.reference(r, v, manager))
		return nil
	})
	route("GET "+base+"/reports/{id}", func(w http.ResponseWriter, r *http.Request) error {
		if e := modelhttp.Query(r); e != nil {
			return e
		}
		owner, e := s.store.ReportWorkspace(r.Context(), r.PathValue("id"))
		if e != nil {
			return e
		}
		if owner != r.PathValue("ws") {
			return modelstore.ErrNotFound
		}
		report, e := s.store.GetReport(r.Context(), r.PathValue("id"))
		if e != nil {
			return e
		}
		reasons := report.IneligibilityReasons
		if reasons == nil {
			reasons = []string{}
		}
		modelhttp.JSON(w, 200, map[string]any{"report": report, "report_hash": report.Hash, "promotion_eligibility": map[string]any{"eligible": report.PromotionEligible, "reasons": reasons}})
		return nil
	})
	route("GET "+base+"/evaluations/{id}/cases/{case}", s.handleCase)
}
func (s *Service) ownedEvaluation(r *http.Request) (modelstore.Evaluation, error) {
	owner, err := s.store.EvaluationWorkspace(r.Context(), r.PathValue("id"))
	if err != nil {
		return modelstore.Evaluation{}, err
	}
	if owner != r.PathValue("ws") {
		return modelstore.Evaluation{}, modelstore.ErrNotFound
	}
	v, e := s.store.GetEvaluation(r.Context(), r.PathValue("id"))
	if errors.Is(e, modelstore.ErrInvalid) {
		return modelstore.Evaluation{}, identityError("STORAGE_UNAVAILABLE", "stored evaluation invalid")
	}
	if e == nil && v.Manifest.WorkspaceID != r.PathValue("ws") {
		return modelstore.Evaluation{}, identityError("STORAGE_UNAVAILABLE", "stored evaluation ownership invalid")
	}
	return v, e
}
func (s *Service) reference(r *http.Request, e modelstore.Evaluation, m *workspaces.Manager) evaluationReference {
	runID := e.RunID
	if runID == "" {
		if run, err := m.FindReferencedRun(e.Manifest.WorkspaceID, "evaluation", workspaces.TaskReference{ID: e.ID, Generation: e.LeaseGeneration}); err == nil {
			runID = run.ID
		}
	}
	return evaluationReference{e.ID, runID, e.Status, e.Revision}
}
func (s *Service) handleCase(w http.ResponseWriter, r *http.Request) error {
	ev, e := s.ownedEvaluation(r)
	if e != nil {
		return e
	}
	var selected *modelstore.Case
	for i := range ev.Manifest.Dataset.Cases {
		if ev.Manifest.Dataset.Cases[i].ID == r.PathValue("case") {
			selected = &ev.Manifest.Dataset.Cases[i]
		}
	}
	if selected == nil {
		return modelstore.ErrNotFound
	}
	c, n, e := modelhttp.Page(r)
	if e != nil {
		return e
	}
	ids, next, e := s.store.PageIDs(r.Context(), "attempts", ev.ID, selected.ID, c, n)
	if e != nil {
		return e
	}
	items := []modelstore.Attempt{}
	refs := []string{selected.InputArtifact, selected.FixtureArtifact}
	for _, id := range ids {
		a, e := s.store.GetAttempt(r.Context(), id)
		if e != nil {
			return e
		}
		items = append(items, a)
		refs = append(refs, a.InputArtifact, a.OutputArtifact, a.Metrics.WarmupArtifact)
		refs = append(refs, a.Result.EvidenceArtifacts...)
	}
	artifacts := []artifactContent{}
	seen := map[string]bool{}
	remaining := 256 << 10
	for _, id := range refs {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		v := artifactContent{ID: id}
		if remaining == 0 {
			v.Truncated = true
		} else {
			raw, err := s.store.ReadArtifact(r.Context(), id)
			if errors.Is(err, modelstore.ErrArtifactMissing) {
				v.Missing = true
			} else if err != nil {
				return err
			} else {
				if id == selected.InputArtifact {
					raw, v.Redacted = presentFrozenInput(raw)
				}
				if len(raw) > remaining {
					raw = raw[:remaining]
					v.Truncated = true
				}
				v.Content = string(raw)
				remaining -= len(raw)
			}
		}
		artifacts = append(artifacts, v)
	}
	modelhttp.JSON(w, 200, map[string]any{"case": selected, "attempts": items, "artifacts": artifacts, "next_cursor": next})
	return nil
}

// Frozen input alone contains private raw CodeLens protocol responses. The
// presentation deliberately withholds those fields; public context, prompts,
// and original evidence hashes remain. Stored bytes and model outputs do not
// change, and redacted:true distinguishes this view from byte-exact evidence.
func presentFrozenInput(raw []byte) ([]byte, bool) {
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil || string(root["schema_version"]) != "1" {
		return raw, false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(root["context"], &fields) != nil {
		return raw, false
	}
	redacted := false
	for _, name := range []string{"analysis_response", "health_response"} {
		if _, ok := fields[name]; ok {
			delete(fields, name)
			redacted = true
		}
	}
	if !redacted {
		return raw, false
	}
	root["context"], _ = json.Marshal(fields)
	view, err := json.Marshal(root)
	if err != nil {
		return raw, false
	}
	return view, true
}
