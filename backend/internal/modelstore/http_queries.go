package modelstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// PageIDs only admits fixed tables, and scopes rows before the cursor/limit.
// The cursor is a last-seen immutable server ID; ordering survives new inserts.
func (s *Store) PageIDs(ctx context.Context, kind, owner, caseID, cursor string, limit int) ([]string, string, error) {
	if limit < 1 || limit > 100 || (cursor != "" && !validID(cursor)) {
		return nil, "", ErrInvalid
	}
	table, where, args := "", "", []any{}
	switch kind {
	case "models":
		table = "model_revisions"
	case "profiles":
		table = "inference_profiles"
	case "evaluations":
		table = "evaluations"
		where = "workspace_id=? AND "
		args = append(args, owner)
	case "releases":
		table = "releases"
		where = "workspace_id=? AND "
		args = append(args, owner)
	case "attempts":
		table = "evaluation_attempts"
		where = "evaluation_id=? AND "
		args = append(args, owner)
		if caseID != "" {
			where += "case_id=? AND "
			args = append(args, caseID)
		}
	default:
		return nil, "", ErrInvalid
	}
	args = append(args, cursor, limit+1)
	rows, e := s.db.QueryContext(ctx, "SELECT id FROM "+table+" WHERE "+where+"id>? ORDER BY id LIMIT ?", args...)
	if e != nil {
		return nil, "", e
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			return nil, "", e
		}
		ids = append(ids, id)
	}
	if e = rows.Err(); e != nil {
		return nil, "", e
	}
	next := ""
	if len(ids) > limit {
		ids = ids[:limit]
		next = ids[len(ids)-1]
	}
	return ids, next, nil
}
func (s *Store) GetAttempt(ctx context.Context, id string) (Attempt, error) {
	var body string
	if !validID(id) {
		return Attempt{}, ErrInvalid
	}
	if e := s.db.QueryRowContext(ctx, "SELECT body_json FROM evaluation_attempts WHERE id=?", id).Scan(&body); e != nil {
		return Attempt{}, e
	}
	return decodeAttempt(body)
}

func evaluationWorkspace(ctx context.Context, q queryer, id string) (string, error) {
	var ws string
	err := q.QueryRowContext(ctx, "SELECT workspace_id FROM evaluations WHERE id=?", id).Scan(&ws)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	return ws, err
}

// Scalar owner reads precede any JSON decode/evidence read at HTTP boundaries.
func (s *Store) EvaluationWorkspace(ctx context.Context, id string) (string, error) {
	return evaluationWorkspace(ctx, s.db, id)
}
func (s *Store) ReleaseWorkspace(ctx context.Context, id string) (string, error) {
	var ws string
	err := s.db.QueryRowContext(ctx, "SELECT workspace_id FROM releases WHERE id=?", id).Scan(&ws)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	return ws, err
}

// ReportWorkspace reads only the owning FK. HTTP checks it before decoding or
// verifying any report/evidence, so damaged foreign records remain opaque.
func (s *Store) ReportWorkspace(ctx context.Context, id string) (string, error) {
	var ws string
	err := s.db.QueryRowContext(ctx, "SELECT e.workspace_id FROM reports r JOIN evaluations e ON e.id=r.evaluation_id WHERE r.id=?", id).Scan(&ws)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	return ws, err
}
func (s *Store) AttemptProgress(ctx context.Context, id string) (Counts, error) {
	e, err := s.GetEvaluation(ctx, id)
	if err != nil {
		return Counts{}, err
	}
	counts := Counts{Planned: len(e.Manifest.Dataset.Cases) * 2}
	rows, err := s.db.QueryContext(ctx, `SELECT body_json FROM evaluation_attempts a WHERE evaluation_id=? AND attempt_no=(SELECT max(attempt_no) FROM evaluation_attempts b WHERE b.evaluation_id=a.evaluation_id AND b.case_id=a.case_id AND b.arm=a.arm)`, id)
	if err != nil {
		return counts, err
	}
	defer rows.Close()
	pairs := map[string]int{}
	for rows.Next() {
		var body string
		if err = rows.Scan(&body); err != nil {
			return counts, err
		}
		a, err := decodeAttempt(body)
		if err != nil {
			return counts, err
		}
		countOne(&counts, a)
		if a.State == "completed" {
			pairs[a.CaseID]++
		}
	}
	for _, n := range pairs {
		if n == 2 {
			counts.PairedComplete++
		}
	}
	counts.Unknown = counts.Planned - counts.Scored - counts.Infra - counts.Cancelled - counts.Interrupted
	return counts, rows.Err()
}
func (s *Store) ReleaseEventsPage(ctx context.Context, id string, after int64, limit int) ([]ReleaseEvent, error) {
	if !validID(id) || after < 0 || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	previous := ""
	var sequence int64
	err := s.db.QueryRowContext(ctx, "SELECT sequence,event_hash FROM release_events WHERE release_id=? AND sequence<=? ORDER BY sequence DESC LIMIT 1", id, after).Scan(&sequence, &previous)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id,body_json,event_hash FROM release_events WHERE release_id=? AND sequence>? ORDER BY sequence LIMIT ?", id, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ReleaseEvent{}
	for rows.Next() {
		var body, hash string
		var eventID int64
		if err = rows.Scan(&eventID, &body, &hash); err != nil {
			return nil, err
		}
		var wrap struct {
			SchemaVersion  int          `json:"schema_version"`
			EncoderVersion string       `json:"encoder_version"`
			Payload        ReleaseEvent `json:"payload"`
		}
		if err = json.Unmarshal([]byte(body), &wrap); err != nil {
			return nil, err
		}
		v := wrap.Payload
		if wrap.SchemaVersion != 1 || wrap.EncoderVersion != EncoderVersion || digest([]byte(body)) != hash || v.PreviousHash != previous || v.Sequence != sequence+1 {
			return nil, fmt.Errorf("audit page hash chain invalid")
		}
		v.ID = eventID
		v.EventHash = hash
		previous = hash
		sequence = v.Sequence
		items = append(items, v)
	}
	return items, rows.Err()
}

// SimulationReceiptDecision returns the frozen decision of this owned intent,
// so a replayed denial never displays gates recomputed from newer evidence.
func (s *Store) SimulationReceiptDecision(ctx context.Context, ws, id, key string) (*GateDecision, error) {
	if !validID(ws) || !validID(id) || !validID(key) {
		return nil, ErrInvalid
	}
	var body string
	err := s.db.QueryRowContext(ctx, "SELECT response_json FROM idempotency_keys WHERE workspace_id=? AND operation='simulation_control' AND key=?", ws, key).Scan(&body)
	if err != nil {
		return nil, err
	}
	var rec receipt
	if err = json.Unmarshal([]byte(body), &rec); err != nil {
		return nil, err
	}
	if rec.Release == nil || rec.Release.ID != id {
		return nil, ErrNotFound
	}
	return rec.Decision, nil
}
