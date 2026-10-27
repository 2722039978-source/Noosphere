package modelstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// SimulationSnapshot is loaded under the same transaction that commits the
// decision. Callbacks must be bounded pure functions: no Store/provider calls.
type SimulationSnapshot struct {
	Release       Release
	Report        Report
	Evaluation    Evaluation
	Policy        ReleasePolicy
	EvidenceError error
	Assignments   []SimulationAssignment
}
type SimulationAssignment struct {
	SessionKey    string `json:"session_key"`
	CohortID      string `json:"cohort_id"`
	Arm           string `json:"arm"`
	AdmittedEpoch int64  `json:"admitted_epoch"`
	RevokedAt     string `json:"revoked_at,omitempty"`
	Source        string `json:"source"`
	OfflineSource string `json:"offline_source"`
}
type SimulationIntent struct {
	WorkspaceID, ReleaseID, Operation, Command, Actor, ReasonCode string
	ExpectedRevision                                              int64
	Payload                                                       json.RawMessage
	Idempotency                                                   Idempotency
}
type SimulationMutation struct {
	Command                   string
	Simulation                json.RawMessage
	Assignments               []SimulationAssignment
	Decision                  GateDecision
	NeedsEvidence             bool
	PromotedAt, RollbackUntil string
	Close                     bool
	Noop                      bool
}
type SimulationEvaluator func(SimulationSnapshot) (SimulationMutation, error)

func (s *Store) simulationSnapshot(ctx context.Context, tx *sql.Tx, r Release) (SimulationSnapshot, error) {
	snap := SimulationSnapshot{Release: r}
	var err error
	snap.Report, err = s.getReport(ctx, tx, r.ReportID)
	if err != nil {
		snap.Report.ID = r.ReportID
		if e := tx.QueryRowContext(ctx, "SELECT evaluation_id FROM reports WHERE id=?", r.ReportID).Scan(&snap.Report.EvaluationID); e != nil {
			return snap, e
		}
	}
	if err != nil || !snap.Report.PromotionEligible {
		snap.EvidenceError = ErrInsufficientEvidence
	}
	snap.Evaluation, err = getEvaluation(ctx, tx, snap.Report.EvaluationID)
	if err != nil {
		return snap, err
	}
	snap.Policy, err = getPolicy(ctx, tx, r.PolicyID)
	if err != nil {
		snap.EvidenceError = ErrInsufficientEvidence
	}
	snap.Assignments, err = listSimulationAssignments(ctx, tx, r.ID)
	return snap, err
}

func intentID(prefix, workspace, key string) string {
	sum := sha256.Sum256([]byte(workspace + "/" + key))
	return fmt.Sprintf("%s%x", prefix, sum[:16])
}

// CreateSimulationRelease assigns a stable server ID before the existing atomic
// create receipt/evidence/lineage checks. Caller IDs cannot forge another record.
func (s *Store) CreateSimulationRelease(ctx context.Context, r CreateReleaseRequest, validate func(SimulationSnapshot) error) (Release, error) {
	r.ID = intentID("rel-", r.WorkspaceID, r.Idempotency.Key)
	if validate == nil {
		return Release{}, ErrInvalid
	}
	return s.createRelease(ctx, r, validate)
}
func (s *Store) CreateClonedEvaluation(ctx context.Context, parent string, r CreateEvaluationRequest) (Evaluation, error) {
	p, e := s.GetEvaluation(ctx, parent)
	if e != nil {
		return Evaluation{}, e
	}
	if p.Manifest.WorkspaceID != r.Manifest.WorkspaceID || r.Manifest.ParentEvaluationID != parent {
		return Evaluation{}, ErrNotFound
	}
	r.ID = intentID("eval-", r.Manifest.WorkspaceID, r.Idempotency.Key)
	return s.CreateEvaluation(ctx, r)
}

// PreviewSimulation reads a coherent snapshot and never writes events/receipts.
func (s *Store) PreviewSimulation(ctx context.Context, ws, id string, evaluate SimulationEvaluator) (SimulationSnapshot, SimulationMutation, error) {
	var snap SimulationSnapshot
	var mutation SimulationMutation
	err := s.write(ctx, func(tx *sql.Tx) error {
		r, e := getRelease(ctx, tx, id)
		if e != nil {
			return e
		}
		if r.WorkspaceID != ws {
			return ErrNotFound
		}
		snap, e = s.simulationSnapshot(ctx, tx, r)
		if e != nil {
			return e
		}
		mutation, e = evaluate(snap)
		return e
	})
	return snap, mutation, err
}

// ApplySimulation hashes caller intent only. Receipt replay precedes CAS and
// gate calculation, including deterministic denials and stale-revision results.
// Legacy ApplyReleaseTransition retains its historical request/hash behavior.
func (s *Store) ApplySimulation(ctx context.Context, req SimulationIntent, evaluate SimulationEvaluator) (Release, error) {
	var out Release
	var outcome error
	req.Idempotency.RequestHash = ""
	req.Idempotency.RequestHash = requestDigest(req)
	err := s.write(ctx, func(tx *sql.Tx) error {
		r, e := getRelease(ctx, tx, req.ReleaseID)
		if e != nil {
			return e
		}
		if r.WorkspaceID != req.WorkspaceID {
			return ErrNotFound
		}
		rec, found, e := getReceipt(ctx, tx, r.WorkspaceID, "simulation_control", req.Idempotency)
		if e != nil {
			return e
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
		deny := func(cause error, code string, d GateDecision, audit bool) error {
			outcome = cause
			if audit {
				if e := appendReleaseEvent(ctx, tx, r, r.State, req.Actor, code, d.EvidenceHash, d); e != nil {
					return e
				}
			}
			return putReceipt(ctx, tx, r.WorkspaceID, "simulation_control", req.Idempotency, receipt{Release: &out, Decision: &d, Error: code})
		}
		if r.Revision != req.ExpectedRevision {
			return deny(ErrRevisionConflict, "REVISION_CONFLICT", GateDecision{}, false)
		}
		if r.ClosedAt != "" {
			return deny(ErrInvalid, "INVALID_RECORD", GateDecision{}, true)
		}
		if r.Mode != "simulation" || req.Actor == "" || req.ReasonCode == "" {
			return deny(ErrInvalid, "INVALID_RECORD", GateDecision{}, true)
		}
		snap, e := s.simulationSnapshot(ctx, tx, r)
		if e != nil {
			return e
		}
		change, e := evaluate(snap)
		if e != nil {
			if errors.Is(e, ErrInvalid) {
				return deny(ErrInvalid, "INVALID_RECORD", change.Decision, true)
			}
			if errors.Is(e, ErrInsufficientEvidence) {
				return deny(ErrInsufficientEvidence, "INSUFFICIENT_EVIDENCE", change.Decision, true)
			}
			return e
		}
		if change.Noop {
			return putReceipt(ctx, tx, r.WorkspaceID, "simulation_control", req.Idempotency, receipt{Release: &out})
		}
		next := r
		if change.Command != "" {
			next, _, e = transition(r, change.Command)
			if e != nil {
				return deny(ErrInvalid, "INVALID_RECORD", change.Decision, true)
			}
		}
		progressive := change.Command == "advance" || change.Command == "start_offline" || change.Command == "resume" || change.NeedsEvidence
		if progressive {
			if snap.EvidenceError != nil || change.Decision.EvidenceHash != snap.Report.Hash || change.Decision.PolicyHash != snap.Policy.Hash || !change.Decision.Allowed {
				return deny(ErrInsufficientEvidence, "INSUFFICIENT_EVIDENCE", change.Decision, true)
			}
			if change.Command == "advance" && validateGateDecision(snap.Policy, change.Decision, r.State) != nil {
				return deny(ErrInsufficientEvidence, "INSUFFICIENT_EVIDENCE", change.Decision, true)
			}
		}
		if change.Close {
			if r.State != "PROMOTED" || r.RollbackUntil == "" {
				return deny(ErrInvalid, "INVALID_RECORD", change.Decision, true)
			}
			next.ClosedAt = now()
			next.AdmissionEnabled = false
		}
		if len(change.Simulation) > 0 {
			if !json.Valid(change.Simulation) || len(change.Simulation) > 256*1024 {
				return ErrInvalid
			}
			next.Simulation = change.Simulation
		}
		if next.State == "PROMOTED" && r.State != "PROMOTED" {
			if _, e = time.Parse(time.RFC3339Nano, change.PromotedAt); e != nil {
				return ErrInvalid
			}
			if _, e = time.Parse(time.RFC3339Nano, change.RollbackUntil); e != nil {
				return ErrInvalid
			}
			next.PromotedAt = change.PromotedAt
			next.RollbackUntil = change.RollbackUntil
		}
		next.ServingRevisionID = next.StableRevisionID
		if next.State == "PROMOTED" {
			next.ServingRevisionID = next.CandidateRevisionID
		}
		next.Revision++
		next.RouteEpoch++
		if e = saveRelease(ctx, tx, next, false); e != nil {
			return e
		}
		for _, a := range change.Assignments {
			if !validID(a.SessionKey) || !validID(a.CohortID) || (a.Arm != "stable" && a.Arm != "candidate") || a.Source != "simulation_replay" || a.AdmittedEpoch != next.RouteEpoch {
				return ErrInvalid
			}
			b, _, e := canonical(a)
			if e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, "INSERT INTO release_assignments(release_id,session_key,cohort_id,arm,admitted_epoch,body_json,created_at) VALUES(?,?,?,?,?,?,?)", r.ID, a.SessionKey, a.CohortID, a.Arm, a.AdmittedEpoch, string(b), now()); e != nil {
				return e
			}
		}
		if !next.AdmissionEnabled {
			if _, e = tx.ExecContext(ctx, "UPDATE release_assignments SET revoked_at=? WHERE release_id=? AND arm='candidate' AND revoked_at IS NULL", now(), r.ID); e != nil {
				return e
			}
		}
		if e = appendReleaseEvent(ctx, tx, next, r.State, req.Actor, req.ReasonCode, change.Decision.EvidenceHash, change.Decision); e != nil {
			return e
		}
		out = next
		return putReceipt(ctx, tx, r.WorkspaceID, "simulation_control", req.Idempotency, receipt{Release: &out, Decision: &change.Decision})
	})
	if err != nil {
		return out, err
	}
	return out, outcome
}

func (s *Store) ListSimulationAssignments(ctx context.Context, ws, id string) ([]SimulationAssignment, error) {
	r, e := s.GetRelease(ctx, id)
	if e != nil {
		return nil, e
	}
	if r.WorkspaceID != ws {
		return nil, ErrNotFound
	}
	return listSimulationAssignments(ctx, s.db, id)
}
func listSimulationAssignments(ctx context.Context, q queryer, id string) ([]SimulationAssignment, error) {
	rows, e := q.QueryContext(ctx, "SELECT session_key,cohort_id,arm,admitted_epoch,coalesce(revoked_at,''),body_json FROM release_assignments WHERE release_id=? ORDER BY cohort_id,session_key", id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []SimulationAssignment{}
	for rows.Next() {
		var b string
		var a SimulationAssignment
		if e = rows.Scan(&a.SessionKey, &a.CohortID, &a.Arm, &a.AdmittedEpoch, &a.RevokedAt, &b); e != nil {
			return nil, e
		}
		var metadata SimulationAssignment
		if e = json.Unmarshal([]byte(b), &metadata); e != nil {
			return nil, e
		}
		a.Source = metadata.Source
		a.OfflineSource = metadata.OfflineSource
		out = append(out, a)
	}
	return out, rows.Err()
}

// OpenSimulationReleases is used only by trusted startup reconciliation.
func (s *Store) OpenSimulationReleases(ctx context.Context) ([]Release, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT body_json FROM releases WHERE mode='simulation' AND closed_at IS NULL")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Release{}
	for rows.Next() {
		var body string
		if err = rows.Scan(&body); err != nil {
			return nil, err
		}
		r, e := decodeRelease(body)
		if e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
