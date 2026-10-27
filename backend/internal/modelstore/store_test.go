package modelstore

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleManifest() Manifest {
	return Manifest{SchemaVersion: 1, EncoderVersion: "canonical-json-v1", WorkspaceID: "ws", WorkflowID: "codelens.codetask.go.v1", Source: "offline_fixture", Stable: ModelRevision{ID: "stable", GatewayModelID: "local", Provider: "ollama", ModelName: "stable:1", IdentityLevel: "digest", Digest: "sha256:" + strings.Repeat("a", 64)}, Candidate: ModelRevision{ID: "candidate", GatewayModelID: "local", Provider: "ollama", ModelName: "candidate:1", IdentityLevel: "digest", Digest: "sha256:" + strings.Repeat("b", 64)}, Profile: InferenceProfile{ID: "profile", Requested: Options{Temperature: Float(0), Seed: Int(42)}}, Dataset: DatasetRevision{ID: "dataset", BenchmarkID: "go", Version: "v1", ContentHash: "dataset-hash", ScorerVersion: "host-v1", Cases: []Case{{ID: "case1", Category: "logic", InputArtifact: "", Expected: "42"}}}, Environment: EnvironmentSnapshot{ID: "env", OS: "windows", Arch: "amd64", CollectorVersion: "v1"}, MemoryMode: "not_applicable"}
}
func openTest(t *testing.T) (*Store, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "model-engineering.sqlite")
	s, e := Open(p)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s, p
}
func createTestEvaluation(t *testing.T, s *Store, id string) Evaluation {
	t.Helper()
	e, err := s.CreateEvaluation(context.Background(), CreateEvaluationRequest{ID: id, Manifest: sampleManifest(), Idempotency: Idempotency{Key: id, RequestHash: "create-" + id}})
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func TestManifestHashStableAndSecretsExcluded(t *testing.T) {
	m := sampleManifest()
	a, h, e := CanonicalManifest(m)
	if e != nil {
		t.Fatal(e)
	}
	b, h2, e := CanonicalManifest(m)
	if e != nil || h != h2 || !bytes.Equal(a, b) {
		t.Fatal("unstable canonical manifest")
	}
	if !bytes.Contains(a, []byte(`"temperature":0`)) {
		t.Fatal("explicit zero lost")
	}
	for _, field := range []string{`"api_key":"SECRET"`, `"token":"SECRET"`, `"authorization":"SECRET"`, `"endpoint":"https://u:SECRET@host"`} {
		raw := bytes.Replace(a, []byte(`"provider":"ollama"`), []byte(`"provider":"ollama",`+field), 1)
		if _, err := DecodeManifest(raw); err == nil {
			t.Fatalf("accepted credential/unknown config %s", field)
		}
	}
	m.Stable.EndpointFingerprint = "https://user:password@localhost"
	if _, _, err := CanonicalManifest(m); err == nil {
		t.Fatal("accepted endpoint credentials")
	}
}
func TestMigrationAtomicAndFutureVersionReadOnly(t *testing.T) {
	s, p := openTest(t)
	createTestEvaluation(t, s, "eval")
	s.Close()
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	if err = applyMigrations(context.Background(), db, []migration{{version: 2, sql: "CREATE TABLE half_done(id TEXT); INVALID SQL;"}}); err == nil {
		t.Fatal("expected failing migration")
	}
	var n int
	if err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='half_done'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("partial migration: %d %v", n, err)
	}
	if _, err = db.Exec("INSERT INTO schema_migrations(version,applied_at,checksum) VALUES(2,'future','future')"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err = Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if !s.ReadOnly() {
		t.Fatal("future schema must be read only")
	}
	if _, err = s.GetEvaluation(context.Background(), "eval"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateEvaluation(context.Background(), CreateEvaluationRequest{ID: "write", Manifest: sampleManifest()}); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("future write: %v", err)
	}
}
func TestArtifactCommitCrashWindows(t *testing.T) {
	s, p := openTest(t)
	ctx := context.Background()
	a, e := s.PublishArtifact(ctx, []byte("evidence"), ArtifactMetadata{MediaType: "text/plain", Provenance: "offline_fixture"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.PublishArtifact(ctx, make([]byte, MaxArtifactBytes+1), ArtifactMetadata{MediaType: "text/plain", Provenance: "offline_fixture"}); e == nil {
		t.Fatal("unbounded artifact")
	}
	// A crash before database commit may leave an orphan file, never a dangling DB reference.
	orphan := filepath.Join(s.artifactDir, strings.Repeat("a", 64))
	if e = os.WriteFile(orphan, []byte("orphan"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = Open(p)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	b, e := s.ReadArtifact(ctx, a.ID)
	if e != nil || string(b) != "evidence" {
		t.Fatalf("reopen evidence %q %v", b, e)
	}
	if e = os.WriteFile(filepath.Join(s.artifactDir, a.SHA256), []byte("corrupt"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReadArtifact(ctx, a.ID); !errors.Is(e, ErrArtifactMissing) {
		t.Fatalf("corruption: %v", e)
	}
}
func appendResult(t *testing.T, s *Store, eval string, gen int64, arm string, no int, verdict string) Attempt {
	t.Helper()
	a, e := s.PublishArtifact(context.Background(), []byte(arm+" output"), ArtifactMetadata{MediaType: "text/plain", Provenance: "offline_fixture"})
	if e != nil {
		t.Fatal(e)
	}
	evaluation, err := s.GetEvaluation(context.Background(), eval)
	if err != nil {
		t.Fatal(err)
	}
	v, e := s.AppendAttempt(context.Background(), Attempt{InputArtifact: evaluation.Manifest.Dataset.Cases[0].InputArtifact, ID: eval + arm + string(rune('0'+no)), EvaluationID: eval, CaseID: "case1", Arm: arm, AttemptNo: no, LeaseGeneration: gen, State: "completed", Verdict: verdict, OutputArtifact: a.ID, Result: Result{Actual: "42", Expected: "42"}})
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestAttemptUniqueAndStaleLeaseRejected(t *testing.T) {
	s, _ := openTest(t)
	ctx := context.Background()
	e := createTestEvaluation(t, s, "eval")
	e, err := s.ClaimEvaluation(ctx, e.ID, "run1", e.Revision)
	if err != nil {
		t.Fatal(err)
	}
	a := appendResult(t, s, e.ID, e.LeaseGeneration, "stable", 1, "pass")
	if _, err = s.AppendAttempt(ctx, a); err == nil {
		t.Fatal("duplicate attempt accepted")
	}
	if err = s.RecoverInterrupted(ctx); err != nil {
		t.Fatal(err)
	}
	a.ID = "late"
	a.Arm = "candidate"
	if _, err = s.AppendAttempt(ctx, a); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("late output: %v", err)
	}
	got, err := s.GetEvaluation(ctx, e.ID)
	if err != nil || got.Status != "interrupted" {
		t.Fatalf("recovery: %+v %v", got, err)
	}
	if _, err = s.ClaimEvaluation(ctx, e.ID, "auto-replay", got.Revision); err == nil {
		t.Fatal("interrupted evaluation auto claimed")
	}
}
func completeTest(t *testing.T, s *Store, id string) (Evaluation, Report) {
	t.Helper()
	ctx := context.Background()
	e := createTestEvaluation(t, s, id)
	e, err := s.ClaimEvaluation(ctx, e.ID, "run", e.Revision)
	if err != nil {
		t.Fatal(err)
	}
	appendResult(t, s, id, e.LeaseGeneration, "stable", 1, "pass")
	appendResult(t, s, id, e.LeaseGeneration, "candidate", 1, "compile_fail")
	e, err = s.GetEvaluation(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.CompleteEvaluation(ctx, id, e.LeaseGeneration, e.Revision)
	if err != nil {
		t.Fatal(err)
	}
	return e, r
}
func TestMissingArtifactInvalidatesEvidence(t *testing.T) {
	s, _ := openTest(t)
	ctx := context.Background()
	_, r := completeTest(t, s, "eval")
	if !r.PromotionEligible || r.Counts.Scored != 2 || r.Counts.Fail != 1 {
		t.Fatalf("compile failure is scored, not infra: %+v", r)
	}
	if err := os.Remove(filepath.Join(s.artifactDir, r.Selected[0].OutputSHA256)); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetReport(ctx, r.ID)
	if err != nil || got.PromotionEligible || !strings.Contains(strings.Join(got.IneligibilityReasons, ","), "ARTIFACT_MISSING") {
		t.Fatalf("missing artifact eligible %+v %v", got, err)
	}
}
func TestCurrentAttemptAndExplicitRecovery(t *testing.T) {
	s, _ := openTest(t)
	ctx := context.Background()
	_, r := completeTest(t, s, "eval")
	e, _ := s.GetEvaluation(ctx, "eval")
	e, err := s.ResumeEvaluation(ctx, "eval", e.Revision, r.ManifestHash)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.AppendAttempt(ctx, Attempt{ID: "retry", EvaluationID: e.ID, CaseID: "case1", Arm: "candidate", AttemptNo: 2, LeaseGeneration: e.LeaseGeneration, State: "running", Verdict: "not_scored"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetReport(ctx, r.ID)
	if err != nil || got.PromotionEligible {
		t.Fatalf("old report eligible %+v %v", got, err)
	}
	if err = s.RecoverInterrupted(ctx); err != nil {
		t.Fatal(err)
	}
	e, _ = s.GetEvaluation(ctx, e.ID)
	e, err = s.ResumeEvaluation(ctx, e.ID, e.Revision, r.ManifestHash)
	if err != nil {
		t.Fatal(err)
	}
	attempts, err := s.ListAttempts(ctx, e.ID)
	if err != nil || len(attempts) != 3 || attempts[1].ID != "retry" || attempts[1].State != "interrupted" {
		t.Fatalf("history %+v %v", attempts, err)
	}
	if _, err = s.FinalizeAttempt(ctx, "retry", e.LeaseGeneration-1, AttemptOutcome{State: "completed", Verdict: "pass"}); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("stale completion: %v", err)
	}
}
func TestReleaseStateEventAndEpochAtomic(t *testing.T) {
	s, _ := openTest(t)
	ctx := context.Background()
	_, report := completeTest(t, s, "eval")
	p, err := s.SavePolicy(ctx, samplePolicy())
	if err != nil {
		t.Fatal(err)
	}
	rel, err := s.CreateRelease(ctx, CreateReleaseRequest{ID: "release", WorkspaceID: "ws", WorkflowID: "codelens.codetask.go.v1", FilterHash: "all", ReportID: report.ID, PolicyID: p.ID, Idempotency: Idempotency{Key: "create", RequestHash: "create"}})
	if err != nil {
		t.Fatal(err)
	}
	e, _ := s.GetEvaluation(ctx, "eval")
	if _, err = s.ResumeEvaluation(ctx, e.ID, e.Revision, report.ManifestHash); !errors.Is(err, ErrEvaluationFrozen) {
		t.Fatalf("not frozen: %v", err)
	}
	req := ReleaseTransition{ReleaseID: rel.ID, ExpectedRevision: rel.Revision, Command: "start_offline", Actor: "user", ReasonCode: "CONFIRMED", EvidenceHash: report.Hash, Idempotency: Idempotency{Key: "start", RequestHash: "start"}}
	rel, err = s.ApplyReleaseTransition(ctx, req)
	if err != nil || rel.State != "OFFLINE_EVAL" || rel.RouteEpoch != 1 {
		t.Fatalf("start: %+v %v", rel, err)
	}
	same, err := s.ApplyReleaseTransition(ctx, req)
	if err != nil || same.Revision != rel.Revision {
		t.Fatalf("idempotent: %+v %v", same, err)
	}
	// Real SQLite failure after UPDATE must roll the whole state/event/receipt transaction back.
	if _, err = s.db.Exec("CREATE TRIGGER reject_event BEFORE INSERT ON release_events BEGIN SELECT RAISE(ABORT,'disk failure fixture'); END"); err != nil {
		t.Fatal(err)
	}
	req.ExpectedRevision = rel.Revision
	req.Command = "pause"
	req.Idempotency = Idempotency{Key: "pause", RequestHash: "pause"}
	if _, err = s.ApplyReleaseTransition(ctx, req); err == nil {
		t.Fatal("injected event failure did not fail")
	}
	got, _ := s.GetRelease(ctx, rel.ID)
	events, _ := s.ListReleaseEvents(ctx, rel.ID)
	if got.State != "OFFLINE_EVAL" || got.RouteEpoch != 1 || got.Revision != rel.Revision || len(events) != 1 {
		t.Fatalf("half transition %+v events=%d", got, len(events))
	}
	if _, err = s.db.Exec("DROP TRIGGER reject_event"); err != nil {
		t.Fatal(err)
	}
	got, err = s.ApplyReleaseTransition(ctx, req)
	if err != nil || got.State != "PAUSED" || got.AdmissionEnabled {
		t.Fatalf("retry %+v %v", got, err)
	}
	req.Idempotency.RequestHash = "changed"
	if _, err = s.ApplyReleaseTransition(ctx, req); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("key conflict %v", err)
	}
	// Immutable manifest and audit are protected at SQL boundary too.
	if _, err = s.db.Exec("UPDATE evaluations SET manifest_json='{}' WHERE id='eval'"); err == nil {
		t.Fatal("mutable manifest")
	}
	var raw string
	if err = s.db.QueryRow("SELECT response_json FROM idempotency_keys WHERE operation='release_command' AND key='pause'").Scan(&raw); err != nil || !json.Valid([]byte(raw)) {
		t.Fatalf("receipt %q %v", raw, err)
	}
}
