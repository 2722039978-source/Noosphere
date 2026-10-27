package modelstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"
)

func validateAttempt(a Attempt, m Manifest) error {
	for _, metric := range []Metric{a.Metrics.WallNS, a.Metrics.LoadNS, a.Metrics.PromptNS, a.Metrics.DecodeNS, a.Metrics.Tokens, a.Metrics.CPU, a.Metrics.RAM, a.Metrics.AvailableVRAM, a.Metrics.WarmupCost} {
		if metric.Value != nil && *metric.Value < 0 {
			return fmt.Errorf("%w: negative metric", ErrInvalid)
		}
	}
	for _, stamp := range []string{a.StartedAt, a.FinishedAt} {
		if stamp != "" {
			if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
				return fmt.Errorf("%w: timestamp", ErrInvalid)
			}
		}
	}
	if !validID(a.ID) || a.AttemptNo < 1 || a.LeaseGeneration < 1 || (a.Arm != "stable" && a.Arm != "candidate") {
		return ErrInvalid
	}
	found := false
	for _, c := range m.Dataset.Cases {
		if c.ID == a.CaseID {
			found = true
			if a.InputArtifact != c.InputArtifact {
				return fmt.Errorf("%w: attempt input differs from frozen case", ErrInvalid)
			}
		}
	}
	if !found {
		return ErrInvalid
	}
	switch a.State {
	case "completed":
		switch a.Verdict {
		case "pass", "functional_fail", "invalid_output", "compile_fail", "resource_fail":
		default:
			return ErrInvalid
		}
		if a.OutputArtifact == "" {
			return fmt.Errorf("%w: scored output required", ErrInvalid)
		}
	case "failed":
		if a.Verdict != "infra_error" {
			return ErrInvalid
		}
	case "queued", "running", "cancelled", "interrupted":
		if a.Verdict != "not_scored" {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}
func artifactRefs(a Attempt) []string {
	refs := []string{a.InputArtifact, a.OutputArtifact, a.Metrics.WarmupArtifact}
	return append(refs, a.Result.EvidenceArtifacts...)
}
func (s *Store) validateArtifacts(ctx context.Context, q queryer, a Attempt) error {
	for _, id := range artifactRefs(a) {
		if id != "" {
			if _, e := s.verifyArtifact(ctx, q, id); e != nil {
				return e
			}
		}
	}
	return nil
}
func saveAttempt(ctx context.Context, tx *sql.Tx, a Attempt, update bool) error {
	result, _, e := immutableBody(a.Result)
	if e != nil {
		return e
	}
	metrics, _, e := immutableBody(a.Metrics)
	if e != nil {
		return e
	}
	body, _, e := immutableBody(a)
	if e != nil {
		return e
	}
	if update {
		res, e := tx.ExecContext(ctx, "UPDATE evaluation_attempts SET state=?,verdict=?,error_code=?,output_artifact=?,result_json=?,metrics_json=?,body_json=?,finished_at=? WHERE id=? AND state IN ('queued','running') AND lease_generation=?", a.State, a.Verdict, a.ErrorCode, null(a.OutputArtifact), string(result), string(metrics), string(body), null(a.FinishedAt), a.ID, a.LeaseGeneration)
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return ErrStaleLease
		}
		return nil
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO evaluation_attempts(id,evaluation_id,case_id,arm,attempt_no,lease_generation,state,verdict,error_code,input_artifact,output_artifact,result_json,metrics_json,body_json,started_at,finished_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", a.ID, a.EvaluationID, a.CaseID, a.Arm, a.AttemptNo, a.LeaseGeneration, a.State, a.Verdict, a.ErrorCode, null(a.InputArtifact), null(a.OutputArtifact), string(result), string(metrics), string(body), a.StartedAt, null(a.FinishedAt))
	return e
}
func decodeAttempt(body string) (Attempt, error) {
	var wrap struct {
		SchemaVersion  int     `json:"schema_version"`
		EncoderVersion string  `json:"encoder_version"`
		Payload        Attempt `json:"payload"`
	}
	e := json.Unmarshal([]byte(body), &wrap)
	if e == nil && (wrap.SchemaVersion != 1 || wrap.EncoderVersion != EncoderVersion) {
		e = ErrInvalid
	}
	return wrap.Payload, e
}
func (s *Store) AppendAttempt(ctx context.Context, a Attempt) (Attempt, error) {
	e := s.write(ctx, func(tx *sql.Tx) error {
		ev, e := getEvaluation(ctx, tx, a.EvaluationID)
		if e != nil {
			return e
		}
		if e = lease(ev, a.LeaseGeneration); e != nil {
			return e
		}
		if e = frozen(ctx, tx, a.EvaluationID); e != nil {
			return e
		}
		if e = validateAttempt(a, ev.Manifest); e != nil {
			return e
		}
		if e = s.validateArtifacts(ctx, tx, a); e != nil {
			return e
		}
		var max int
		if e = tx.QueryRowContext(ctx, "SELECT coalesce(max(attempt_no),0) FROM evaluation_attempts WHERE evaluation_id=? AND case_id=? AND arm=?", a.EvaluationID, a.CaseID, a.Arm).Scan(&max); e != nil {
			return e
		}
		if a.AttemptNo != max+1 {
			return fmt.Errorf("%w: attempt number must be latest+1", ErrInvalid)
		}
		if max > 0 && a.SupersedesReason == "" {
			a.SupersedesReason = "explicit_retry"
		}
		if a.StartedAt == "" {
			a.StartedAt = now()
		}
		if a.State != "queued" && a.State != "running" && a.FinishedAt == "" {
			a.FinishedAt = now()
		}
		if e = saveAttempt(ctx, tx, a, false); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, "UPDATE evaluations SET revision=revision+1 WHERE id=?", a.EvaluationID)
		return e
	})
	return a, e
}
func (s *Store) FinalizeAttempt(ctx context.Context, id string, generation int64, o AttemptOutcome) (Attempt, error) {
	var a Attempt
	e := s.write(ctx, func(tx *sql.Tx) error {
		var b string
		e := tx.QueryRowContext(ctx, "SELECT body_json FROM evaluation_attempts WHERE id=?", id).Scan(&b)
		if e != nil {
			return e
		}
		a, e = decodeAttempt(b)
		if e != nil {
			return e
		}
		ev, e := getEvaluation(ctx, tx, a.EvaluationID)
		if e != nil {
			return e
		}
		if e = lease(ev, generation); e != nil {
			return e
		}
		if a.LeaseGeneration != generation {
			return ErrStaleLease
		}
		if e = frozen(ctx, tx, a.EvaluationID); e != nil {
			return e
		}
		a.State = o.State
		a.Verdict = o.Verdict
		a.ErrorCode = o.ErrorCode
		a.OutputArtifact = o.OutputArtifact
		a.Result = o.Result
		a.Metrics = o.Metrics
		a.FinishedAt = now()
		if a.State == "running" || a.State == "queued" {
			return ErrInvalid
		}
		if e = validateAttempt(a, ev.Manifest); e != nil {
			return e
		}
		if e = s.validateArtifacts(ctx, tx, a); e != nil {
			return e
		}
		if e = saveAttempt(ctx, tx, a, true); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, "UPDATE evaluations SET revision=revision+1 WHERE id=?", a.EvaluationID)
		return e
	})
	return a, e
}
func listAttempts(ctx context.Context, q queryer, id string) ([]Attempt, error) {
	rows, e := q.QueryContext(ctx, "SELECT body_json FROM evaluation_attempts WHERE evaluation_id=? ORDER BY case_id,arm,attempt_no", id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Attempt{}
	for rows.Next() {
		var b string
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		a, e := decodeAttempt(b)
		if e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) ListAttempts(ctx context.Context, id string) ([]Attempt, error) {
	return listAttempts(ctx, s.db, id)
}
func countOne(c *Counts, a Attempt) {
	c.Attempted++
	switch a.State {
	case "completed":
		c.Scored++
		if a.Verdict == "pass" {
			c.Pass++
		} else {
			c.Fail++
		}
	case "failed":
		c.Infra++
	case "cancelled":
		c.Cancelled++
	case "interrupted":
		c.Interrupted++
	default:
		c.Unknown++
	}
}
func (s *Store) buildReport(ctx context.Context, q queryer, e Evaluation) (Report, error) {
	all, err := listAttempts(ctx, q, e.ID)
	if err != nil {
		return Report{}, err
	}
	latest := map[string]Attempt{}
	for _, a := range all {
		latest[a.CaseID+"/"+a.Arm] = a
	}
	r := Report{EvaluationID: e.ID, SchemaVersion: 1, EncoderVersion: EncoderVersion, ManifestHash: e.ManifestHash, EvaluationRevision: e.Revision, Categories: map[string]CategoryCounts{}, Selected: []SelectedAttempt{}, Superseded: []Attempt{}}
	r.Counts.Planned = len(e.Manifest.Dataset.Cases) * 2
	for _, c := range e.Manifest.Dataset.Cases {
		for _, id := range []string{c.InputArtifact, c.FixtureArtifact} {
			if id != "" {
				if _, err := s.verifyArtifact(ctx, q, id); err != nil {
					r.IneligibilityReasons = append(r.IneligibilityReasons, "ARTIFACT_MISSING:"+id)
				}
			}
		}
	}
	for _, a := range all {
		if latest[a.CaseID+"/"+a.Arm].ID != a.ID {
			r.Superseded = append(r.Superseded, a)
		}
	}
	for _, c := range e.Manifest.Dataset.Cases {
		cat := r.Categories[c.Category]
		cat.Stable.Planned++
		cat.Candidate.Planned++
		pair := []Attempt{}
		for _, arm := range []string{"stable", "candidate"} {
			a, found := latest[c.ID+"/"+arm]
			if !found {
				if arm == "stable" {
					cat.Stable.Unknown++
				} else {
					cat.Candidate.Unknown++
				}
				r.Counts.Unknown++
				continue
			}
			countOne(&r.Counts, a)
			if arm == "stable" {
				countOne(&cat.Stable, a)
			} else {
				countOne(&cat.Candidate, a)
			}
			selected := SelectedAttempt{ID: a.ID, CaseID: a.CaseID, Arm: a.Arm, AttemptNo: a.AttemptNo, State: a.State, Verdict: a.Verdict}
			_, selected.ResultHash, err = immutableBody(a.Result)
			if err != nil {
				return r, err
			}
			_, selected.AttemptHash, err = immutableBody(a)
			if err != nil {
				return r, err
			}
			for _, id := range artifactRefs(a) {
				if id == "" {
					continue
				}
				art, err := s.verifyArtifact(ctx, q, id)
				if err != nil {
					r.IneligibilityReasons = append(r.IneligibilityReasons, "ARTIFACT_MISSING:"+id)
					continue
				}
				if id == a.InputArtifact {
					selected.InputSHA256 = art.SHA256
				}
				if id == a.OutputArtifact {
					selected.OutputSHA256 = art.SHA256
				}
			}
			r.Selected = append(r.Selected, selected)
			pair = append(pair, a)
		}
		if len(pair) == 2 && pair[0].State == "completed" && pair[1].State == "completed" {
			r.Counts.PairedComplete++
			index := 0
			if pair[0].Verdict != "pass" {
				index += 2
			}
			if pair[1].Verdict != "pass" {
				index++
			}
			r.PairMatrix[index]++
		}
		r.Categories[c.Category] = cat
	}
	_, r.AttemptSetHash, err = canonical(r.Selected)
	if err != nil {
		return r, err
	}
	if r.Counts.Scored != r.Counts.Planned {
		r.IneligibilityReasons = append(r.IneligibilityReasons, "INCOMPLETE_ATTEMPTS")
	}
	if validateModelRevision(e.Manifest.Stable) != nil || validateModelRevision(e.Manifest.Candidate) != nil {
		r.IneligibilityReasons = append(r.IneligibilityReasons, "INVALID_IDENTITY")
	}
	if e.Manifest.Stable.IdentityLevel == "unknown" || e.Manifest.Candidate.IdentityLevel == "unknown" {
		r.IneligibilityReasons = append(r.IneligibilityReasons, "UNKNOWN_IDENTITY")
	}
	r.PromotionEligible = len(r.IneligibilityReasons) == 0
	return r, nil
}
func (s *Store) CompleteEvaluation(ctx context.Context, id string, generation, revision int64) (Report, error) {
	return s.CompleteEvaluationWithAnalysis(ctx, id, generation, revision, nil)
}

// ReportAnalyzer runs only bounded local calculation against the same snapshot
// that will be committed. It must not call store methods or external services.
// readArtifact verifies bytes through the transaction; returning an error aborts
// both report insertion and evaluation completion.
type ReportAnalyzer func(Evaluation, Report, []Attempt, func(string) ([]byte, error)) (json.RawMessage, error)

func (s *Store) CompleteEvaluationWithAnalysis(ctx context.Context, id string, generation, revision int64, analyze ReportAnalyzer) (Report, error) {
	var out Report
	err := s.write(ctx, func(tx *sql.Tx) error {
		e, err := getEvaluation(ctx, tx, id)
		if err != nil {
			return err
		}
		if err = lease(e, generation); err != nil {
			return err
		}
		if e.Revision != revision {
			return ErrRevisionConflict
		}
		out, err = s.buildReport(ctx, tx, e)
		if err != nil {
			return err
		}
		if analyze != nil {
			attempts, err := listAttempts(ctx, tx, id)
			if err != nil {
				return err
			}
			out.Analysis, err = analyze(e, out, attempts, func(id string) ([]byte, error) {
				a, err := s.verifyArtifact(ctx, tx, id)
				if err != nil {
					return nil, err
				}
				return readVerified(filepath.Join(s.artifactDir, a.SHA256), a.SHA256, a.SizeBytes)
			})
			if err != nil {
				return err
			}
			if len(out.Analysis) == 0 || !json.Valid(out.Analysis) {
				return ErrInvalid
			}
			out.Analysis, _, err = canonical(out.Analysis)
			if err != nil {
				return err
			}
		}
		status := "incomplete"
		if out.Counts.Scored == out.Counts.Planned && len(out.IneligibilityReasons) == 0 {
			status = "completed"
		}
		body, h, err := canonical(out)
		if err != nil {
			return err
		}
		out.Hash = h
		out.ID = "report_" + h
		body, _, err = canonical(out)
		if err != nil {
			return err
		}
		out.Hash = digest(body)
		out.ID = "report_" + h
		_, err = tx.ExecContext(ctx, "INSERT INTO reports(id,evaluation_id,report_hash,schema_version,body_json,created_at) VALUES(?,?,?,1,?,?)", out.ID, id, out.Hash, string(body), now())
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE evaluations SET status=?,revision=revision+1,finished_at=? WHERE id=? AND revision=?", status, now(), id, revision)
		return err
	})
	return out, err
}
func (s *Store) getReport(ctx context.Context, q queryer, id string) (Report, error) {
	var r Report
	var b string
	err := q.QueryRowContext(ctx, "SELECT body_json,report_hash FROM reports WHERE id=?", id).Scan(&b, &r.Hash)
	if err == sql.ErrNoRows {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	h := r.Hash
	if digest([]byte(b)) != h {
		return r, ErrInsufficientEvidence
	}
	if err = json.Unmarshal([]byte(b), &r); err != nil {
		return r, err
	}
	r.Hash = h
	e, err := getEvaluation(ctx, q, r.EvaluationID)
	if err != nil {
		return r, err
	}
	current, err := s.buildReport(ctx, q, e)
	if err != nil {
		return r, err
	}
	r.IneligibilityReasons = current.IneligibilityReasons
	if e.Status != "completed" {
		r.IneligibilityReasons = append(r.IneligibilityReasons, "EVALUATION_NOT_COMPLETED")
	}
	if r.AttemptSetHash != current.AttemptSetHash || r.ManifestHash != e.ManifestHash {
		r.IneligibilityReasons = append(r.IneligibilityReasons, "SUPERSEDED_ATTEMPT_SET")
	}
	r.PromotionEligible = len(r.IneligibilityReasons) == 0
	return r, nil
}
func (s *Store) GetReport(ctx context.Context, id string) (Report, error) {
	var out Report
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	out, err = s.getReport(ctx, tx, id)
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}
