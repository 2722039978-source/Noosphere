package modelstore

import (
	"context"
	"database/sql"
	"encoding/json"
)

// RecoverInterrupted is an explicit startup reconciliation. It never replays a
// task. Completed attempts/reports survive; outstanding generations are revoked.
// Simulation releases pause with their clock/checkpoint body unchanged.
func (s *Store) RecoverInterrupted(ctx context.Context) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT id FROM evaluations WHERE status IN ('queued','running')")
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, id := range ids {
			attempts, err := listAttempts(ctx, tx, id)
			if err != nil {
				return err
			}
			for _, a := range attempts {
				if a.State == "queued" || a.State == "running" {
					a.State = "interrupted"
					a.Verdict = "not_scored"
					a.ErrorCode = "APP_RESTARTED"
					a.FinishedAt = now()
					if err = saveAttempt(ctx, tx, a, true); err != nil {
						return err
					}
				}
			}
			if _, err = tx.ExecContext(ctx, "UPDATE evaluations SET status='interrupted',lease_generation=lease_generation+1,revision=revision+1,finished_at=? WHERE id=?", now(), id); err != nil {
				return err
			}
		}
		rows, err = tx.QueryContext(ctx, "SELECT body_json FROM releases WHERE closed_at IS NULL AND state IN ('OFFLINE_EVAL','SHADOW','CANARY_5','CANARY_25','CANARY_50')")
		if err != nil {
			return err
		}
		releases := []Release{}
		for rows.Next() {
			var b string
			if err = rows.Scan(&b); err != nil {
				rows.Close()
				return err
			}
			r, err := decodeRelease(b)
			if err != nil {
				rows.Close()
				return err
			}
			releases = append(releases, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, r := range releases {
			from := r.State
			r.PausedFrom = r.State
			r.State = "PAUSED"
			r.Revision++
			r.RouteEpoch++
			r.AdmissionEnabled = false
			if len(r.Simulation) > 0 {
				var fields map[string]json.RawMessage
				if json.Unmarshal(r.Simulation, &fields) == nil {
					var task struct {
						RunID      string          `json:"run_id"`
						Generation int64           `json:"generation"`
						Status     string          `json:"status"`
						Request    json.RawMessage `json:"request"`
					}
					if json.Unmarshal(fields["task"], &task) == nil && (task.Status == "queued" || task.Status == "running") {
						task.Status = "interrupted"
						task.Generation++
						fields["task"], _ = json.Marshal(task)
						r.Simulation, _ = json.Marshal(fields)
					}
				}
			}
			if err = saveRelease(ctx, tx, r, false); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, "UPDATE release_assignments SET revoked_at=? WHERE release_id=? AND arm='candidate' AND revoked_at IS NULL", now(), r.ID); err != nil {
				return err
			}
			if err = appendReleaseEvent(ctx, tx, r, from, "system", "APP_RESTARTED", "", GateDecision{}); err != nil {
				return err
			}
		}
		return nil
	})
}

// CancelEvaluation persists intent and terminal unscored attempts before a caller
// cancels its external Run. Completed reports are never overwritten by old runs.
func (s *Store) CancelEvaluation(ctx context.Context, id, runID string, generation, revision int64) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		e, err := getEvaluation(ctx, tx, id)
		if err != nil {
			return err
		}
		if e.Status == "completed" || e.Status == "incomplete" || e.Status == "cancelled" {
			return nil
		}
		if e.Revision != revision {
			return ErrRevisionConflict
		}
		if e.LeaseGeneration != generation || (e.RunID != "" && e.RunID != runID) {
			return ErrStaleLease
		}
		if err = frozen(ctx, tx, id); err != nil {
			return err
		}
		all, err := listAttempts(ctx, tx, id)
		if err != nil {
			return err
		}
		for _, a := range all {
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
		_, err = tx.ExecContext(ctx, "UPDATE evaluations SET status='cancelled',cancel_requested=1,lease_generation=lease_generation+1,revision=revision+1,finished_at=? WHERE id=?", now(), id)
		return err
	})
}
