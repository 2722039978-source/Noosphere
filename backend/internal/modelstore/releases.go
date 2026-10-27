package modelstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"fmt"
)

func (s *Store) SavePolicy(ctx context.Context, p ReleasePolicy) (ReleasePolicy, error) {
	p.SchemaVersion = 1
	p.EncoderVersion = EncoderVersion
	if err := validatePolicy(p); err != nil {
		return p, err
	}
	_, hash, encodeErr := immutableBody(p)
	if encodeErr != nil {
		return p, encodeErr
	}
	p.Hash = hash
	err := s.write(ctx, func(tx *sql.Tx) error {
		return insertImmutable(ctx, tx, "release_policies", p.ID, "policy_hash", p, "mode", []any{p.Mode})
	})
	return p, err
}
func decodeRelease(b string) (Release, error) {
	var r Release
	err := json.Unmarshal([]byte(b), &r)
	return r, err
}
func getRelease(ctx context.Context, q queryer, id string) (Release, error) {
	var b string
	err := q.QueryRowContext(ctx, "SELECT body_json FROM releases WHERE id=?", id).Scan(&b)
	if err == sql.ErrNoRows {
		return Release{}, ErrNotFound
	}
	if err != nil {
		return Release{}, err
	}
	return decodeRelease(b)
}
func (s *Store) GetRelease(ctx context.Context, id string) (Release, error) {
	return getRelease(ctx, s.db, id)
}
func saveRelease(ctx context.Context, tx *sql.Tx, r Release, create bool) error {
	b, _, err := canonical(r)
	if err != nil {
		return err
	}
	if !create {
		res, err := tx.ExecContext(ctx, "UPDATE releases SET state=?,paused_from=?,stable_revision_id=?,candidate_revision_id=?,revision=?,route_epoch=?,target_bps=?,admission_enabled=?,body_json=?,updated_at=?,closed_at=? WHERE id=? AND revision=?", r.State, r.PausedFrom, r.StableRevisionID, r.CandidateRevisionID, r.Revision, r.RouteEpoch, r.TargetBPS, r.AdmissionEnabled, string(b), now(), null(r.ClosedAt), r.ID, r.Revision-1)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return ErrRevisionConflict
		}
		return nil
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO releases(id,workspace_id,workflow_id,filter_hash,mode,state,paused_from,stable_revision_id,candidate_revision_id,policy_id,report_id,revision,route_epoch,target_bps,admission_enabled,body_json,created_at,updated_at,closed_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", r.ID, r.WorkspaceID, r.WorkflowID, r.FilterHash, r.Mode, r.State, r.PausedFrom, r.StableRevisionID, r.CandidateRevisionID, r.PolicyID, r.ReportID, r.Revision, r.RouteEpoch, r.TargetBPS, r.AdmissionEnabled, string(b), now(), now(), null(r.ClosedAt))
	return err
}
func requestDigest(v any) string { _, h, _ := canonical(v); return h }
func (s *Store) CreateRelease(ctx context.Context, req CreateReleaseRequest) (Release, error) {
	return s.createRelease(ctx, req, nil)
}
func (s *Store) createRelease(ctx context.Context, req CreateReleaseRequest, validate func(SimulationSnapshot) error) (Release, error) {
	var r Release
	var outcome error
	req.Idempotency.RequestHash = requestDigest(req)
	err := s.write(ctx, func(tx *sql.Tx) error {
		rec, found, err := getReceipt(ctx, tx, req.WorkspaceID, "create_release", req.Idempotency)
		if err != nil {
			return err
		}
		if found {
			if rec.Error == "INSUFFICIENT_EVIDENCE" {
				outcome = ErrInsufficientEvidence
				return nil
			}
			if rec.Release == nil {
				return ErrInvalid
			}
			r = *rec.Release
			return nil
		}
		report, err := s.getReport(ctx, tx, req.ReportID)
		if err != nil && !errors.Is(err, ErrInsufficientEvidence) {
			return err
		}
		if err != nil || !report.PromotionEligible {
			outcome = ErrInsufficientEvidence
			return putReceipt(ctx, tx, req.WorkspaceID, "create_release", req.Idempotency, receipt{Error: "INSUFFICIENT_EVIDENCE"})
		}
		e, err := getEvaluation(ctx, tx, report.EvaluationID)
		if err != nil {
			return err
		}
		if e.Manifest.WorkspaceID != req.WorkspaceID || e.Manifest.WorkflowID != req.WorkflowID {
			return ErrNotFound
		}
		if !validID(req.ID) || req.FilterHash == "" {
			return ErrInvalid
		}
		policy, err := getPolicy(ctx, tx, req.PolicyID)
		if err != nil {
			return err
		}
		mode := policy.Mode
		r = Release{ID: req.ID, WorkspaceID: req.WorkspaceID, WorkflowID: req.WorkflowID, FilterHash: req.FilterHash, Mode: mode, State: "DRAFT", StableRevisionID: e.Manifest.Stable.ID, CandidateRevisionID: e.Manifest.Candidate.ID, PreviousStable: e.Manifest.Stable.ID, ReportID: req.ReportID, PolicyID: req.PolicyID, Revision: 1}
		if validate != nil {
			// Once a promoted release closes, its candidate is the workflow's
			// simulation baseline. Historical reports cannot silently reset it.
			var previousBody string
			lineageErr := tx.QueryRowContext(ctx, "SELECT body_json FROM releases WHERE workspace_id=? AND workflow_id=? AND filter_hash=? AND mode=? AND closed_at IS NOT NULL AND state IN ('PROMOTED','ROLLED_BACK') ORDER BY rowid DESC LIMIT 1", r.WorkspaceID, r.WorkflowID, r.FilterHash, r.Mode).Scan(&previousBody)
			if lineageErr != nil && lineageErr != sql.ErrNoRows {
				return lineageErr
			}
			if lineageErr == nil {
				previous, err := decodeRelease(previousBody)
				if err != nil {
					return err
				}
				baseline := previous.PreviousStable
				if previous.State == "PROMOTED" {
					baseline = previous.CandidateRevisionID
				}
				if baseline != r.StableRevisionID {
					outcome = ErrInsufficientEvidence
					return putReceipt(ctx, tx, r.WorkspaceID, "create_release", req.Idempotency, receipt{Error: "INSUFFICIENT_EVIDENCE"})
				}
			}
			if err = validate(SimulationSnapshot{Release: r, Report: report, Evaluation: e, Policy: policy}); err != nil {
				if !errors.Is(err, ErrInsufficientEvidence) {
					return err
				}
				outcome = err
				return putReceipt(ctx, tx, r.WorkspaceID, "create_release", req.Idempotency, receipt{Error: "INSUFFICIENT_EVIDENCE"})
			}
			r.ServingRevisionID = r.StableRevisionID
		}
		if err = saveRelease(ctx, tx, r, true); err != nil {
			return err
		}
		return putReceipt(ctx, tx, r.WorkspaceID, "create_release", req.Idempotency, receipt{Release: &r})
	})
	if err != nil {
		return r, err
	}
	return r, outcome
}
func appendReleaseEvent(ctx context.Context, tx *sql.Tx, r Release, from, actor, reason, evidence string, d GateDecision) error {
	var seq int64
	var previous string
	err := tx.QueryRowContext(ctx, "SELECT sequence,event_hash FROM release_events WHERE release_id=? ORDER BY sequence DESC LIMIT 1", r.ID).Scan(&seq, &previous)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	var policy string
	if err = tx.QueryRowContext(ctx, "SELECT policy_hash FROM release_policies WHERE id=?", r.PolicyID).Scan(&policy); err != nil {
		return err
	}
	event := ReleaseEvent{Simulation: r.Simulation, ReleaseID: r.ID, Sequence: seq + 1, FromState: from, ToState: r.State, Actor: actor, ReasonCode: reason, PolicyHash: policy, EvidenceHash: evidence, Decision: d, PreviousHash: previous, CreatedAt: now()}
	b, h, err := immutableBody(event)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO release_events(release_id,sequence,from_state,to_state,actor,reason_code,policy_hash,evidence_hash,body_json,previous_hash,event_hash,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", r.ID, event.Sequence, from, r.State, actor, reason, policy, evidence, string(b), previous, h, event.CreatedAt)
	return err
}
func transition(r Release, command string) (Release, bool, error) {
	advance := false
	switch command {
	case "start_offline":
		if r.State != "DRAFT" {
			return r, false, ErrInvalid
		}
		r.State = "OFFLINE_EVAL"
	case "advance":
		advance = true
		switch r.State {
		case "OFFLINE_EVAL":
			r.State = "SHADOW"
		case "SHADOW":
			r.State = "CANARY_5"
			r.TargetBPS = 500
		case "CANARY_5":
			r.State = "CANARY_25"
			r.TargetBPS = 2500
		case "CANARY_25":
			r.State = "CANARY_50"
			r.TargetBPS = 5000
		case "CANARY_50":
			r.State = "PROMOTED"
			r.TargetBPS = 10000
		default:
			return r, false, ErrInvalid
		}
	case "pause":
		switch r.State {
		case "OFFLINE_EVAL", "SHADOW", "CANARY_5", "CANARY_25", "CANARY_50":
			r.PausedFrom = r.State
			r.State = "PAUSED"
		case "PROMOTED":
			r.State = "ROLLED_BACK"
		default:
			return r, false, ErrInvalid
		}
	case "resume":
		if r.State != "PAUSED" {
			return r, false, ErrInvalid
		}
		switch r.PausedFrom {
		case "OFFLINE_EVAL", "SHADOW", "CANARY_5", "CANARY_25", "CANARY_50":
			r.State = r.PausedFrom
			r.PausedFrom = ""
			advance = true
		default:
			return r, false, ErrInvalid
		}
	case "reject":
		if r.State != "DRAFT" && r.State != "OFFLINE_EVAL" && r.State != "SHADOW" && !(r.State == "PAUSED" && (r.PausedFrom == "OFFLINE_EVAL" || r.PausedFrom == "SHADOW")) {
			return r, false, ErrInvalid
		}
		r.State = "REJECTED"
	case "rollback":
		if r.PreviousStable == "" {
			return r, false, ErrInvalid
		}
		switch r.State {
		case "CANARY_5", "CANARY_25", "CANARY_50", "PROMOTED":
			r.State = "ROLLED_BACK"
		case "PAUSED":
			if r.PausedFrom != "CANARY_5" && r.PausedFrom != "CANARY_25" && r.PausedFrom != "CANARY_50" {
				return r, false, ErrInvalid
			}
			r.State = "ROLLED_BACK"
		default:
			return r, false, ErrInvalid
		}
	default:
		return r, false, ErrInvalid
	}
	r.AdmissionEnabled = r.State == "CANARY_5" || r.State == "CANARY_25" || r.State == "CANARY_50" || r.State == "PROMOTED"
	if r.State == "ROLLED_BACK" || r.State == "REJECTED" {
		r.TargetBPS = 0
		r.StableRevisionID = r.PreviousStable
		r.ClosedAt = now()
	}
	return r, advance, nil
}

// ApplyReleaseTransition is a concrete transactional state storage boundary.
// Advance/resume require a policy decision from the future modelops service;
// this package cannot calculate quality/latency/identity gates from external data.
func (s *Store) ApplyReleaseTransition(ctx context.Context, req ReleaseTransition) (Release, error) {
	var out Release
	var outcome error
	req.Idempotency.RequestHash = requestDigest(req)
	err := s.write(ctx, func(tx *sql.Tx) error {
		r, err := getRelease(ctx, tx, req.ReleaseID)
		if err != nil {
			return err
		}
		rec, found, err := getReceipt(ctx, tx, r.WorkspaceID, "release_command", req.Idempotency)
		if err != nil {
			return err
		}
		if found {
			if rec.Release != nil {
				out = *rec.Release
			}
			switch rec.Error {
			case "REVISION_CONFLICT":
				outcome = ErrRevisionConflict
			case "INSUFFICIENT_EVIDENCE":
				outcome = ErrInsufficientEvidence
			case "INVALID_RECORD":
				outcome = ErrInvalid
			}
			return nil
		}
		out = r
		reject := func(cause error, code string, audit bool) error {
			outcome = cause
			if audit {
				if err := appendReleaseEvent(ctx, tx, r, r.State, req.Actor, code, req.EvidenceHash, req.Decision); err != nil {
					return err
				}
			}
			return putReceipt(ctx, tx, r.WorkspaceID, "release_command", req.Idempotency, receipt{Release: &out, Error: code, Decision: &req.Decision})
		}
		if r.Revision != req.ExpectedRevision {
			return reject(ErrRevisionConflict, "REVISION_CONFLICT", false)
		}
		if r.ClosedAt != "" {
			return reject(ErrInvalid, "INVALID_RECORD", true)
		}
		next, needsGate, err := transition(r, req.Command)
		if err != nil {
			return reject(ErrInvalid, "INVALID_RECORD", true)
		}
		if req.Command == "start_offline" || needsGate {
			report, err := s.getReport(ctx, tx, r.ReportID)
			if err != nil || !report.PromotionEligible || report.Hash != req.EvidenceHash {
				return reject(ErrInsufficientEvidence, "INSUFFICIENT_EVIDENCE", true)
			}
			if needsGate {
				policy, err := getPolicy(ctx, tx, r.PolicyID)
				if err != nil {
					return reject(ErrInsufficientEvidence, "INSUFFICIENT_EVIDENCE", true)
				}
				stage := r.State
				if r.State == "PAUSED" {
					stage = next.State
				}
				if req.Decision.EvidenceHash != report.Hash || validateGateDecision(policy, req.Decision, stage) != nil {
					return reject(ErrInsufficientEvidence, "INSUFFICIENT_EVIDENCE", true)
				}
			}
		}
		if req.Actor == "" || req.ReasonCode == "" {
			return reject(ErrInvalid, "INVALID_RECORD", true)
		}
		next.Revision++
		next.RouteEpoch++
		if err = saveRelease(ctx, tx, next, false); err != nil {
			return err
		}
		if !next.AdmissionEnabled {
			if _, err = tx.ExecContext(ctx, "UPDATE release_assignments SET revoked_at=? WHERE release_id=? AND arm='candidate' AND revoked_at IS NULL", now(), r.ID); err != nil {
				return err
			}
		}
		if err = appendReleaseEvent(ctx, tx, next, r.State, req.Actor, req.ReasonCode, req.EvidenceHash, req.Decision); err != nil {
			return err
		}
		out = next
		return putReceipt(ctx, tx, r.WorkspaceID, "release_command", req.Idempotency, receipt{Release: &out, Decision: &req.Decision})
	})
	if err != nil {
		return out, err
	}
	return out, outcome
}
func (s *Store) ListReleaseEvents(ctx context.Context, id string) ([]ReleaseEvent, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,body_json,event_hash FROM release_events WHERE release_id=? ORDER BY sequence", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReleaseEvent{}
	previous := ""
	for rows.Next() {
		var id int64
		var b, h string
		if err = rows.Scan(&id, &b, &h); err != nil {
			return nil, err
		}
		var w struct {
			SchemaVersion  int          `json:"schema_version"`
			EncoderVersion string       `json:"encoder_version"`
			Payload        ReleaseEvent `json:"payload"`
		}
		if err = json.Unmarshal([]byte(b), &w); err != nil {
			return nil, err
		}
		if digest([]byte(b)) != h || w.Payload.PreviousHash != previous || w.Payload.Sequence != int64(len(out)+1) {
			return nil, fmt.Errorf("audit hash chain invalid")
		}
		w.Payload.ID = id
		w.Payload.EventHash = h
		previous = h
		out = append(out, w.Payload)
	}
	return out, rows.Err()
}
