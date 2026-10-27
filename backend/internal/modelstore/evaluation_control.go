package modelstore

import (
	"context"
	"database/sql"
	"errors"
	"math"
)

type EvaluationControl struct {
	WorkspaceID      string `json:"workspace_id"`
	EvaluationID     string `json:"evaluation_id"`
	Command          string `json:"command"`
	ExpectedRevision int64  `json:"expected_revision"`
	CaseID           string `json:"case_id,omitempty"`
	Arm              string `json:"arm,omitempty"`
	Key              string `json:"-"`
}

func controlError(code string) error {
	switch code {
	case "REVISION_CONFLICT":
		return ErrRevisionConflict
	case "EVALUATION_FROZEN":
		return ErrEvaluationFrozen
	case "INSUFFICIENT_EVIDENCE":
		return ErrInsufficientEvidence
	case "MODEL_DRIFT":
		return ErrModelDrift
	case "INVALID_RECORD":
		return ErrInvalid
	}
	return nil
}

// RefuseEvaluationControl stores only deterministic frozen-preflight outcomes.
// Ownership, caller-intent hash, CAS/state/freeze and the preflight's manifest
// hash are checked in the same transaction as its original snapshot receipt.
// Transient provider/network/cancellation errors are not accepted here.
func (s *Store) RefuseEvaluationControl(ctx context.Context, r EvaluationControl, manifestHash, code string) (Evaluation, error) {
	var out Evaluation
	var outcome error
	if (code != "MODEL_DRIFT" && code != "INSUFFICIENT_EVIDENCE") || (r.Command != "resume" && r.Command != "retry_case") {
		return out, ErrInvalid
	}
	key := Idempotency{Key: r.Key, RequestHash: requestDigest(r)}
	err := s.write(ctx, func(tx *sql.Tx) error {
		owner, err := evaluationWorkspace(ctx, tx, r.EvaluationID)
		if err != nil {
			return err
		}
		if owner != r.WorkspaceID {
			return ErrNotFound
		}
		e, err := getEvaluation(ctx, tx, r.EvaluationID)
		if err != nil {
			return err
		}
		rec, found, err := getReceipt(ctx, tx, r.WorkspaceID, "evaluation_control", key)
		if err != nil {
			return err
		}
		if found {
			if rec.Evaluation == nil {
				return ErrInvalid
			}
			out = *rec.Evaluation
			outcome = controlError(rec.Error)
			return nil
		}
		out = e
		if guard := evaluationControlGuard(ctx, tx, e, r); guard != nil {
			if !errors.Is(guard, ErrRevisionConflict) && !errors.Is(guard, ErrEvaluationFrozen) && !errors.Is(guard, ErrInvalid) {
				return guard
			}
			outcome = guard
			return putReceipt(ctx, tx, r.WorkspaceID, "evaluation_control", key, receipt{Evaluation: &out, Error: guard.Error()})
		}
		if e.ManifestHash != manifestHash {
			return ErrInsufficientEvidence
		}
		outcome = controlError(code)
		return putReceipt(ctx, tx, r.WorkspaceID, "evaluation_control", key, receipt{Evaluation: &out, Error: code})
	})
	if err != nil {
		return out, err
	}
	return out, outcome
}

func evaluationControlGuard(ctx context.Context, tx *sql.Tx, e Evaluation, r EvaluationControl) error {
	if e.Revision != r.ExpectedRevision {
		return ErrRevisionConflict
	}
	terminalCancel := r.Command == "cancel" && (e.Status == "completed" || e.Status == "incomplete" || e.Status == "cancelled")
	if terminalCancel {
		return nil
	}
	if err := frozen(ctx, tx, e.ID); err != nil {
		return err
	}
	if e.LeaseGeneration == math.MaxInt64 {
		return ErrInvalid
	}
	switch r.Command {
	case "cancel":
		return nil
	case "resume":
		if e.Status != "interrupted" && e.Status != "incomplete" && e.Status != "cancelled" {
			return ErrInvalid
		}
	case "retry_case":
		if e.Status == "queued" || e.Status == "running" {
			return ErrInvalid
		}
		if r.Arm != "stable" && r.Arm != "candidate" {
			return ErrInvalid
		}
		found := false
		for _, c := range e.Manifest.Dataset.Cases {
			if c.ID == r.CaseID {
				found = true
			}
		}
		if !found {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

// EvaluationControlReceipt permits replays before a fresh external preflight.
func (s *Store) EvaluationControlReceipt(ctx context.Context, r EvaluationControl) (Evaluation, bool, error) {
	var out Evaluation
	var found bool
	var outcome error
	err := s.write(ctx, func(tx *sql.Tx) error {
		e, err := getEvaluation(ctx, tx, r.EvaluationID)
		if err != nil {
			return err
		}
		if e.Manifest.WorkspaceID != r.WorkspaceID {
			return ErrNotFound
		}
		rec, ok, err := getReceipt(ctx, tx, r.WorkspaceID, "evaluation_control", Idempotency{Key: r.Key, RequestHash: requestDigest(r)})
		if err != nil {
			return err
		}
		found = ok
		if rec.Evaluation != nil {
			out = *rec.Evaluation
		}
		outcome = controlError(rec.Error)
		if !ok {
			if guard := evaluationControlGuard(ctx, tx, e, r); guard != nil {
				if !errors.Is(guard, ErrRevisionConflict) && !errors.Is(guard, ErrEvaluationFrozen) && !errors.Is(guard, ErrInvalid) {
					return guard
				}
				out = e
				found = true
				outcome = guard
				return putReceipt(ctx, tx, r.WorkspaceID, "evaluation_control", Idempotency{Key: r.Key, RequestHash: requestDigest(r)}, receipt{Evaluation: &out, Error: guard.Error()})
			}
		}
		return nil
	})
	if err != nil {
		return out, found, err
	}
	return out, found, outcome
}

// ControlEvaluation commits the control, explicit retry lineage, and receipt in
// one transaction. verifiedHash comes from service preflight, never HTTP input.
func (s *Store) ControlEvaluation(ctx context.Context, r EvaluationControl, verifiedHash string) (Evaluation, bool, error) {
	var out Evaluation
	var replayed bool
	var outcome error
	key := Idempotency{Key: r.Key, RequestHash: requestDigest(r)}
	err := s.write(ctx, func(tx *sql.Tx) error {
		e, err := getEvaluation(ctx, tx, r.EvaluationID)
		if err != nil {
			return err
		}
		if e.Manifest.WorkspaceID != r.WorkspaceID {
			return ErrNotFound
		}
		rec, found, err := getReceipt(ctx, tx, r.WorkspaceID, "evaluation_control", key)
		if err != nil {
			return err
		}
		if found {
			replayed = true
			if rec.Evaluation != nil {
				out = *rec.Evaluation
			}
			outcome = controlError(rec.Error)
			return nil
		}
		out = e
		deny := func(err error) error {
			outcome = err
			return putReceipt(ctx, tx, r.WorkspaceID, "evaluation_control", key, receipt{Evaluation: &out, Error: err.Error()})
		}
		if err = evaluationControlGuard(ctx, tx, e, r); err != nil {
			if errors.Is(err, ErrRevisionConflict) || errors.Is(err, ErrEvaluationFrozen) || errors.Is(err, ErrInvalid) {
				return deny(err)
			}
			return err
		}
		all, err := listAttempts(ctx, tx, e.ID)
		if err != nil {
			return err
		}
		switch r.Command {
		case "cancel":
			if e.Status != "completed" && e.Status != "incomplete" && e.Status != "cancelled" {
				latest := map[string]bool{}
				for _, a := range all {
					latest[a.CaseID+"/"+a.Arm] = true
					if a.State == "queued" || a.State == "running" {
						a.State = "cancelled"
						a.Verdict = "not_scored"
						a.ErrorCode = "USER_CANCELLED"
						a.FinishedAt = now()
						if err = saveAttempt(ctx, tx, a, true); err != nil {
							return err
						}
					}
				}
				for _, c := range e.Manifest.Dataset.Cases {
					for _, arm := range []string{"stable", "candidate"} {
						if latest[c.ID+"/"+arm] {
							continue
						}
						g := e.LeaseGeneration
						if g < 1 {
							g = 1
						}
						a := Attempt{ID: "cancel_" + requestDigest([]any{e.ID, c.ID, arm, g})[:40], EvaluationID: e.ID, CaseID: c.ID, Arm: arm, AttemptNo: 1, LeaseGeneration: g, State: "cancelled", Verdict: "not_scored", ErrorCode: "USER_CANCELLED", InputArtifact: c.InputArtifact, StartedAt: now(), FinishedAt: now()}
						if err = validateAttempt(a, e.Manifest); err != nil {
							return err
						}
						if err = saveAttempt(ctx, tx, a, false); err != nil {
							return err
						}
					}
				}
				_, err = tx.ExecContext(ctx, "UPDATE evaluations SET status='cancelled',cancel_requested=1,lease_generation=lease_generation+1,revision=revision+1,finished_at=? WHERE id=?", now(), e.ID)
			}
		case "resume", "retry_case":
			if (r.Command == "resume" && e.Status != "interrupted" && e.Status != "incomplete" && e.Status != "cancelled") || (r.Command == "retry_case" && (e.Status == "queued" || e.Status == "running")) {
				return deny(ErrInvalid)
			}
			if e.ManifestHash != verifiedHash {
				return deny(ErrInsufficientEvidence)
			}
			if _, _, err = CanonicalManifest(e.Manifest); err != nil {
				return deny(ErrInsufficientEvidence)
			}
			for _, a := range all {
				if a.ErrorCode == "MODEL_DRIFT" {
					return deny(ErrInsufficientEvidence)
				}
			}
			if r.Command == "retry_case" {
				if r.Arm != "stable" && r.Arm != "candidate" {
					return deny(ErrInvalid)
				}
				var c *Case
				for i := range e.Manifest.Dataset.Cases {
					if e.Manifest.Dataset.Cases[i].ID == r.CaseID {
						c = &e.Manifest.Dataset.Cases[i]
					}
				}
				if c == nil {
					return deny(ErrInvalid)
				}
				no := 0
				for _, a := range all {
					if a.CaseID == r.CaseID && a.Arm == r.Arm && a.AttemptNo > no {
						no = a.AttemptNo
					}
				}
				if no >= 100 {
					return deny(ErrInvalid)
				}
				a := Attempt{ID: "attempt_" + requestDigest([]any{e.ID, r.CaseID, r.Arm, no + 1})[:32], EvaluationID: e.ID, CaseID: r.CaseID, Arm: r.Arm, AttemptNo: no + 1, LeaseGeneration: e.LeaseGeneration + 1, State: "queued", Verdict: "not_scored", InputArtifact: c.InputArtifact, StartedAt: now(), SupersedesReason: "explicit_retry_case"}
				if err = validateAttempt(a, e.Manifest); err != nil {
					return err
				}
				if err = s.validateArtifacts(ctx, tx, a); err != nil {
					return err
				}
				if err = saveAttempt(ctx, tx, a, false); err != nil {
					return err
				}
			}
			_, err = tx.ExecContext(ctx, "UPDATE evaluations SET status='running',lease_generation=lease_generation+1,revision=revision+1,cancel_requested=0,run_id=NULL,finished_at=NULL WHERE id=?", e.ID)
		default:
			return deny(ErrInvalid)
		}
		if err != nil {
			return err
		}
		out, err = getEvaluation(ctx, tx, e.ID)
		if err != nil {
			return err
		}
		return putReceipt(ctx, tx, r.WorkspaceID, "evaluation_control", key, receipt{Evaluation: &out})
	})
	if err != nil {
		return out, replayed, err
	}
	return out, replayed, outcome
}
