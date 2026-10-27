package modelstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
)

// ClaimDispatchedEvaluation claims only the immutable generation captured in a
// Workspace Run. A duplicate dispatch can never borrow the latest generation.
func (s *Store) ClaimDispatchedEvaluation(ctx context.Context, id, run string, dispatch int64) (Evaluation, error) {
	var out Evaluation
	err := s.write(ctx, func(tx *sql.Tx) error {
		e, err := getEvaluation(ctx, tx, id)
		if err != nil {
			return err
		}
		if e.Status != "queued" || e.CancelRequested || e.LeaseGeneration != dispatch || !validID(run) || dispatch == math.MaxInt64 || (e.RunID != "" && e.RunID != run) {
			return ErrStaleLease
		}
		if _, _, err = CanonicalManifest(e.Manifest); err != nil {
			return ErrInsufficientEvidence
		}
		if err = frozen(ctx, tx, id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE evaluations SET status='running',run_id=?,lease_generation=lease_generation+1,revision=revision+1 WHERE id=?", run, id)
		if err != nil {
			return err
		}
		out, err = getEvaluation(ctx, tx, id)
		return err
	})
	return out, err
}

// BindResumedRun binds the lease explicitly granted by ResumeEvaluation once.
func (s *Store) BindResumedRun(ctx context.Context, id, run string, dispatch int64) (Evaluation, error) {
	var out Evaluation
	err := s.write(ctx, func(tx *sql.Tx) error {
		e, err := getEvaluation(ctx, tx, id)
		if err != nil {
			return err
		}
		if e.Status != "running" || e.CancelRequested || e.LeaseGeneration != dispatch || e.RunID != "" || !validID(run) {
			return ErrStaleLease
		}
		if err = frozen(ctx, tx, id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE evaluations SET run_id=?,revision=revision+1 WHERE id=?", run, id)
		if err != nil {
			return err
		}
		out, err = getEvaluation(ctx, tx, id)
		return err
	})
	return out, err
}

func ownsDispatch(e Evaluation, run string, d int64) bool {
	return d >= 0 && ((e.LeaseGeneration == d && (e.RunID == "" || e.RunID == run)) || (d < math.MaxInt64 && e.LeaseGeneration == d+1 && e.RunID == run))
}

// CancelDispatchedEvaluation atomically checks dispatch ownership and persists
// intent. Proven stale task references are successful no-ops, never new leases.
func (s *Store) CancelDispatchedEvaluation(ctx context.Context, id, workspace, run string, dispatch int64) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		e, err := getEvaluation(ctx, tx, id)
		if err != nil {
			return err
		}
		if e.Manifest.WorkspaceID != workspace {
			return ErrNotFound
		}
		if !ownsDispatch(e, run, dispatch) {
			return nil
		}
		if e.Status == "completed" || e.Status == "incomplete" || e.Status == "cancelled" {
			return nil
		}
		if e.LeaseGeneration == math.MaxInt64 {
			return ErrInvalid
		}
		if err = frozen(ctx, tx, id); err != nil {
			return err
		}
		all, err := listAttempts(ctx, tx, id)
		if err != nil {
			return err
		}
		latest := map[string]Attempt{}
		for _, a := range all {
			latest[a.CaseID+"/"+a.Arm] = a
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
				if _, ok := latest[c.ID+"/"+arm]; ok {
					continue
				}
				// Cancellation before worker claim still has a real dispatch but no active
				// lease. Its terminal unscored records use the revoked next generation.
				g := e.LeaseGeneration
				if g < 1 {
					g = 1
				}
				a := Attempt{ID: "cancel_" + digest([]byte(fmt.Sprintf("%s/%s/%s/%d", id, c.ID, arm, g)))[:40], EvaluationID: id, CaseID: c.ID, Arm: arm, AttemptNo: 1, LeaseGeneration: g, State: "cancelled", Verdict: "not_scored", ErrorCode: "USER_CANCELLED", InputArtifact: c.InputArtifact, StartedAt: now(), FinishedAt: now()}
				if err = validateAttempt(a, e.Manifest); err != nil {
					return err
				}
				if err = saveAttempt(ctx, tx, a, false); err != nil {
					return err
				}
			}
		}
		_, err = tx.ExecContext(ctx, "UPDATE evaluations SET status='cancelled',cancel_requested=1,run_id=?,lease_generation=lease_generation+1,revision=revision+1,finished_at=? WHERE id=?", run, now(), id)
		return err
	})
}

// CheckpointAttempt durably links completed upstream phases before scoring.
// Only the active generation can update an unfinished attempt.
func (s *Store) CheckpointAttempt(ctx context.Context, id string, generation int64, o AttemptOutcome) (Attempt, error) {
	var a Attempt
	err := s.write(ctx, func(tx *sql.Tx) error {
		var raw string
		if err := tx.QueryRowContext(ctx, "SELECT body_json FROM evaluation_attempts WHERE id=?", id).Scan(&raw); err != nil {
			return err
		}
		var err error
		a, err = decodeAttempt(raw)
		if err != nil {
			return err
		}
		e, err := getEvaluation(ctx, tx, a.EvaluationID)
		if err != nil {
			return err
		}
		if err = lease(e, generation); err != nil {
			return err
		}
		if a.LeaseGeneration != generation || (a.State != "queued" && a.State != "running") {
			return ErrStaleLease
		}
		if err = frozen(ctx, tx, e.ID); err != nil {
			return err
		}
		a.State = "running"
		a.OutputArtifact = o.OutputArtifact
		a.Result = o.Result
		a.Metrics = o.Metrics
		if err = validateAttempt(a, e.Manifest); err != nil {
			return err
		}
		if err = s.validateArtifacts(ctx, tx, a); err != nil {
			return err
		}
		if err = saveAttempt(ctx, tx, a, true); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE evaluations SET revision=revision+1 WHERE id=?", e.ID)
		return err
	})
	return a, err
}

func (s *Store) InterruptEvaluation(ctx context.Context, id, run string, generation int64, code string) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		e, err := getEvaluation(ctx, tx, id)
		if err != nil {
			return err
		}
		if (e.Status != "running" && e.Status != "queued") || e.CancelRequested || e.LeaseGeneration != generation || (e.RunID != "" && e.RunID != run) {
			return ErrStaleLease
		}
		if generation == math.MaxInt64 {
			return ErrInvalid
		}
		all, err := listAttempts(ctx, tx, id)
		if err != nil {
			return err
		}
		for _, a := range all {
			if a.State == "queued" || a.State == "running" {
				a.State = "interrupted"
				a.Verdict = "not_scored"
				a.ErrorCode = code
				a.FinishedAt = now()
				if err = saveAttempt(ctx, tx, a, true); err != nil {
					return err
				}
			}
		}
		_, err = tx.ExecContext(ctx, "UPDATE evaluations SET status='interrupted',lease_generation=lease_generation+1,revision=revision+1,finished_at=? WHERE id=?", now(), id)
		return err
	})
}

func (s *Store) ListEvaluations(ctx context.Context) ([]Evaluation, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM evaluations ORDER BY started_at,id")
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := make([]Evaluation, 0, len(ids))
	for _, id := range ids {
		e, err := s.GetEvaluation(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// LatestReport returns a reference only. Reports remain immutable in their table.
func (s *Store) LatestReport(ctx context.Context, id string) (Report, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, "SELECT body_json FROM reports WHERE evaluation_id=? ORDER BY created_at DESC,id DESC LIMIT 1", id).Scan(&raw)
	if err == sql.ErrNoRows {
		return Report{}, ErrNotFound
	}
	if err != nil {
		return Report{}, err
	}
	var r Report
	if err = json.Unmarshal([]byte(raw), &r); err != nil {
		return r, err
	}
	return s.GetReport(ctx, r.ID)
}
