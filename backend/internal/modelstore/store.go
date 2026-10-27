package modelstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Store struct {
	db          *sql.DB
	path        string
	artifactDir string
	readOnly    bool
	mu          sync.Mutex
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func connect(path, mode string) (*sql.DB, error) {
	uriPath := filepath.ToSlash(path)
	if filepath.VolumeName(path) != "" {
		uriPath = "/" + uriPath
	}
	u := url.URL{Scheme: "file", Path: uriPath}
	q := url.Values{}
	q.Set("mode", mode)
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	u.RawQuery = q.Encode()
	db, e := sql.Open("sqlite", u.String())
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	if e = db.Ping(); e != nil {
		db.Close()
		return nil, e
	}
	return db, nil
}

// Open never performs restart recovery or schedules execution. For future schema
// versions it opens SQLite mode=ro and all mutation methods return ErrReadOnly.
func Open(path string) (*Store, error) {
	path, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	ctx := context.Background()
	version := 0
	if _, err := os.Stat(path); err == nil {
		probe, err := connect(path, "ro")
		if err != nil {
			return nil, err
		}
		version, err = migrationState(ctx, probe)
		if err != nil {
			probe.Close()
			return nil, err
		}
		if version > SchemaVersion {
			return &Store{db: probe, path: path, artifactDir: filepath.Join(filepath.Dir(path), "model-engineering", "artifacts"), readOnly: true}, nil
		}
		probe.Close()
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return nil, e
	}
	db, e := connect(path, "rwc")
	if e != nil {
		return nil, e
	}
	fail := func(err error) (*Store, error) { db.Close(); return nil, err }
	if _, e = db.Exec("PRAGMA journal_mode=WAL"); e != nil {
		return fail(e)
	}
	if _, e = db.Exec("PRAGMA synchronous=FULL"); e != nil {
		return fail(e)
	}
	// VACUUM INTO includes committed WAL pages. Existing old databases are backed
	// up before any schema mutation, never copied as a live .sqlite file alone.
	if version < SchemaVersion {
		var objects int
		if e = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name NOT LIKE 'sqlite_%'").Scan(&objects); e != nil {
			return fail(e)
		}
		if objects > 0 {
			backup := path + ".pre-migration-" + time.Now().UTC().Format("20060102T150405.000000000") + ".sqlite"
			if _, e = db.Exec("VACUUM INTO ?", backup); e != nil {
				return fail(fmt.Errorf("migration backup: %w", e))
			}
		}
		if e = applyMigrations(ctx, db, builtInMigrations()[version:]); e != nil {
			return fail(e)
		}
	}
	s := &Store{db: db, path: path, artifactDir: filepath.Join(filepath.Dir(path), "model-engineering", "artifacts")}
	if e = os.MkdirAll(s.artifactDir, 0700); e != nil {
		return fail(e)
	}
	return s, nil
}
func (s *Store) Close() error   { return s.db.Close() }
func (s *Store) ReadOnly() bool { return s.readOnly }
func (s *Store) write(ctx context.Context, fn func(*sql.Tx) error) error {
	if s.readOnly {
		return ErrReadOnly
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = fn(tx); e != nil {
		return e
	}
	return tx.Commit()
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func null(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func getEvaluation(ctx context.Context, q queryer, id string) (Evaluation, error) {
	var e Evaluation
	var b string
	err := q.QueryRowContext(ctx, "SELECT id,manifest_json,manifest_hash,status,revision,lease_generation,coalesce(run_id,''),cancel_requested,started_at,coalesce(finished_at,'') FROM evaluations WHERE id=?", id).Scan(&e.ID, &b, &e.ManifestHash, &e.Status, &e.Revision, &e.LeaseGeneration, &e.RunID, &e.CancelRequested, &e.StartedAt, &e.FinishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	if err != nil {
		return e, err
	}
	e.Manifest, err = decodeManifestRecord([]byte(b))
	if err != nil {
		return e, err
	}
	if digest([]byte(b)) != e.ManifestHash {
		err = fmt.Errorf("manifest hash mismatch")
	}
	return e, err
}
func (s *Store) GetEvaluation(ctx context.Context, id string) (Evaluation, error) {
	return getEvaluation(ctx, s.db, id)
}

type receipt struct {
	Decision   *GateDecision `json:"decision,omitempty"`
	Evaluation *Evaluation   `json:"evaluation,omitempty"`
	Release    *Release      `json:"release,omitempty"`
	Error      string        `json:"error,omitempty"`
}

func getReceipt(ctx context.Context, tx *sql.Tx, ws, op string, k Idempotency) (receipt, bool, error) {
	var r receipt
	var h, b string
	if !validID(k.Key) || k.RequestHash == "" {
		return r, false, fmt.Errorf("%w: idempotency key/request hash required", ErrInvalid)
	}
	err := tx.QueryRowContext(ctx, "SELECT request_hash,response_json FROM idempotency_keys WHERE workspace_id=? AND operation=? AND key=?", ws, op, k.Key).Scan(&h, &b)
	if err == sql.ErrNoRows {
		return r, false, nil
	}
	if err != nil {
		return r, false, err
	}
	if h != k.RequestHash {
		return r, true, ErrIdempotencyConflict
	}
	err = json.Unmarshal([]byte(b), &r)
	return r, true, err
}
func putReceipt(ctx context.Context, tx *sql.Tx, ws, op string, k Idempotency, r receipt) error {
	b, _, e := canonical(r)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO idempotency_keys VALUES(?,?,?,?,?,?)", ws, op, k.Key, k.RequestHash, string(b), time.Now().UTC().Add(7*24*time.Hour).Format(time.RFC3339Nano))
	return e
}
func insertImmutable(ctx context.Context, tx *sql.Tx, table, id, hashcol string, v any, extraColumns string, extra []any) error {
	b, h, e := immutableBody(v)
	if e != nil {
		return e
	}
	var old string
	e = tx.QueryRowContext(ctx, "SELECT "+hashcol+" FROM "+table+" WHERE id=?", id).Scan(&old)
	if e == nil {
		if old != h {
			return fmt.Errorf("%w: immutable %s id reused", ErrInvalid, table)
		}
		return nil
	}
	if e != sql.ErrNoRows {
		return e
	}
	cols := "id," + hashcol + ",body_json,created_at"
	args := []any{id, h, string(b), now()}
	if table == "environment_snapshots" {
		cols = "id," + hashcol + ",body_json,captured_at"
	}
	marks := "?,?,?,?"
	if extraColumns != "" {
		cols += "," + extraColumns
		args = append(args, extra...)
		for range extra {
			marks += ",?"
		}
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO "+table+" ("+cols+") VALUES("+marks+")", args...)
	return e
}
func (s *Store) CreateEvaluation(ctx context.Context, r CreateEvaluationRequest) (Evaluation, error) {
	var out Evaluation
	if s.readOnly {
		return out, ErrReadOnly
	}
	r.Idempotency.RequestHash = requestDigest(r)
	b, h, e := CanonicalManifest(r.Manifest)
	if e != nil {
		return out, e
	}
	if !validID(r.ID) {
		return out, ErrInvalid
	}
	e = s.write(ctx, func(tx *sql.Tx) error {
		receipt, found, e := getReceipt(ctx, tx, r.Manifest.WorkspaceID, "create_evaluation", r.Idempotency)
		if e != nil {
			return e
		}
		if found {
			if receipt.Evaluation == nil {
				return ErrInvalid
			}
			out = *receipt.Evaluation
			return nil
		}
		m := r.Manifest
		for _, model := range []ModelRevision{m.Stable, m.Candidate} {
			if e = insertImmutable(ctx, tx, "model_revisions", model.ID, "identity_hash", model, "gateway_model_id,provider,model_name,identity_level", []any{model.GatewayModelID, model.Provider, model.ModelName, model.IdentityLevel}); e != nil {
				return e
			}
		}
		if e = insertImmutable(ctx, tx, "inference_profiles", m.Profile.ID, "profile_hash", m.Profile, "", nil); e != nil {
			return e
		}
		if e = insertImmutable(ctx, tx, "dataset_revisions", m.Dataset.ID, "content_hash", m.Dataset, "benchmark_id,version", []any{m.Dataset.BenchmarkID, m.Dataset.Version}); e != nil {
			return e
		}
		if e = insertImmutable(ctx, tx, "environment_snapshots", m.Environment.ID, "snapshot_hash", m.Environment, "", nil); e != nil {
			return e
		}
		for _, c := range m.Dataset.Cases {
			for _, a := range []string{c.InputArtifact, c.FixtureArtifact} {
				if a != "" {
					if _, e = s.verifyArtifact(ctx, tx, a); e != nil {
						return e
					}
				}
			}
			_, ch, e := immutableBody(c)
			if e != nil {
				return e
			}
			_, e = tx.ExecContext(ctx, "INSERT INTO dataset_cases(dataset_id,case_id,category,artifact_id,fixture_artifact,case_hash) VALUES(?,?,?,?,?,?) ON CONFLICT(dataset_id,case_id) DO NOTHING", m.Dataset.ID, c.ID, c.Category, null(c.InputArtifact), null(c.FixtureArtifact), ch)
			if e != nil {
				return e
			}
		}
		out = Evaluation{ID: r.ID, Manifest: m, ManifestHash: h, Status: "queued", Revision: 1, StartedAt: now()}
		_, e = tx.ExecContext(ctx, "INSERT INTO evaluations(id,workspace_id,workflow_id,stable_revision_id,candidate_revision_id,profile_id,dataset_id,environment_id,source,status,revision,manifest_json,manifest_hash,lease_generation,cancel_requested,started_at) VALUES(?,?,?,?,?,?,?,?,?,'queued',1,?,?,0,0,?)", r.ID, m.WorkspaceID, m.WorkflowID, m.Stable.ID, m.Candidate.ID, m.Profile.ID, m.Dataset.ID, m.Environment.ID, m.Source, string(b), h, out.StartedAt)
		if e != nil {
			return e
		}
		return putReceipt(ctx, tx, m.WorkspaceID, "create_evaluation", r.Idempotency, receiptTypeEvaluation(out))
	})
	return out, e
}
func receiptTypeEvaluation(e Evaluation) receipt { return receipt{Evaluation: &e} }
func frozen(ctx context.Context, q queryer, eval string) error {
	var n int
	e := q.QueryRowContext(ctx, "SELECT count(*) FROM releases r JOIN reports p ON p.id=r.report_id WHERE p.evaluation_id=? AND r.closed_at IS NULL AND r.state NOT IN ('REJECTED','ROLLED_BACK')", eval).Scan(&n)
	if e != nil {
		return e
	}
	if n > 0 {
		return ErrEvaluationFrozen
	}
	return nil
}
func (s *Store) ClaimEvaluation(ctx context.Context, id, run string, revision int64) (Evaluation, error) {
	var out Evaluation
	e := s.write(ctx, func(tx *sql.Tx) error {
		var e error
		out, e = getEvaluation(ctx, tx, id)
		if e != nil {
			return e
		}
		if _, _, err := CanonicalManifest(out.Manifest); err != nil {
			return ErrInsufficientEvidence
		}
		if out.Revision != revision {
			return ErrRevisionConflict
		}
		if out.Status != "queued" || out.CancelRequested || !validID(run) {
			return ErrStaleLease
		}
		if e = frozen(ctx, tx, id); e != nil {
			return e
		}
		res, e := tx.ExecContext(ctx, "UPDATE evaluations SET status='running',run_id=?,lease_generation=lease_generation+1,revision=revision+1 WHERE id=? AND revision=? AND status='queued' AND cancel_requested=0", run, id, revision)
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return ErrRevisionConflict
		}
		out, e = getEvaluation(ctx, tx, id)
		if e != nil {
			return e
		}
		b, _, e := canonical(receiptTypeEvaluation(out))
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, "UPDATE idempotency_keys SET response_json=? WHERE workspace_id=? AND operation='create_evaluation' AND json_extract(response_json,'$.evaluation.id')=?", string(b), out.Manifest.WorkspaceID, id)
		return e
	})
	return out, e
}

// ResumeEvaluation is explicit. verifiedManifestHash must come from the caller's
// fresh provider/environment/dataset verification; this layer verifies equality.
// It grants a new lease but never creates attempts or runs inference.
func (s *Store) ResumeEvaluation(ctx context.Context, id string, revision int64, verifiedManifestHash string) (Evaluation, error) {
	var out Evaluation
	e := s.write(ctx, func(tx *sql.Tx) error {
		var e error
		out, e = getEvaluation(ctx, tx, id)
		if e != nil {
			return e
		}
		if e = frozen(ctx, tx, id); e != nil {
			return e
		}
		if _, _, err := CanonicalManifest(out.Manifest); err != nil {
			return ErrInsufficientEvidence
		}
		if out.Revision != revision {
			return ErrRevisionConflict
		}
		if out.ManifestHash != verifiedManifestHash {
			return ErrInsufficientEvidence
		}
		if out.Status == "queued" || out.Status == "running" {
			return ErrInvalid
		}
		_, e = tx.ExecContext(ctx, "UPDATE evaluations SET status='running',lease_generation=lease_generation+1,revision=revision+1,cancel_requested=0,run_id=NULL,finished_at=NULL WHERE id=? AND revision=?", id, revision)
		if e != nil {
			return e
		}
		out, e = getEvaluation(ctx, tx, id)
		return e
	})
	return out, e
}
func lease(e Evaluation, g int64) error {
	if e.LeaseGeneration != g || e.Status != "running" || e.CancelRequested {
		return ErrStaleLease
	}
	return nil
}
