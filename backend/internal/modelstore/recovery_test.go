package modelstore

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Removing a manifest's credential check must fail even when callers construct
// typed records directly instead of going through strict JSON decoding.
func TestCredentialsRejectedInsideOptionsAndConfig(t *testing.T) {
	m := sampleManifest()
	m.Stable.ModelName = "stable"
	b, _, err := CanonicalManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{strings.Replace(string(b), `"requested":{`, `"requested":{"api_key":"secret",`, 1), strings.Replace(string(b), `"environment":{`, `"environment":{"token":"secret",`, 1)} {
		if _, err = DecodeManifest([]byte(raw)); err == nil {
			t.Fatal("credential config accepted")
		}
	}
	m.Profile.Requested.KeepAlive = "Bearer SECRET"
	if _, _, err = CanonicalManifest(m); err == nil {
		t.Fatal("credential-shaped keep_alive accepted")
	}
}
func TestOpenValidatesChecksumAndRollsBackCollidingOldSchema(t *testing.T) {
	s, p := openTest(t)
	s.Close()
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE schema_migrations SET checksum='corrupt' WHERE version=1"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if bad, err := Open(p); err == nil {
		bad.Close()
		t.Fatal("checksum mismatch accepted")
	}
	old := filepath.Join(t.TempDir(), "old.sqlite")
	db, err = sql.Open("sqlite", old)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TABLE model_revisions(original TEXT); INSERT INTO model_revisions VALUES('preserve')"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if bad, err := Open(old); err == nil {
		bad.Close()
		t.Fatal("colliding schema migrated")
	}
	db, err = sql.Open("sqlite", old)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var original string
	if err = db.QueryRow("SELECT original FROM model_revisions").Scan(&original); err != nil || original != "preserve" {
		t.Fatalf("old db damaged: %q %v", original, err)
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='schema_migrations'").Scan(&count); err != nil || count != 0 {
		t.Fatal("partial schema remained")
	}
	backups, _ := filepath.Glob(old + ".pre-migration-*.sqlite")
	if len(backups) != 1 {
		t.Fatal("missing old schema backup")
	}
}
func TestReopenRetainsManifestResultsAndFailure(t *testing.T) {
	s, p := openTest(t)
	ctx := context.Background()
	_, report := completeTest(t, s, "eval")
	s.Close()
	var err error
	s, err = Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e, err := s.GetEvaluation(ctx, "eval")
	if err != nil || e.Status != "completed" || e.Manifest.Profile.Requested.Temperature == nil || *e.Manifest.Profile.Requested.Temperature != 0 || e.Manifest.Dataset.Cases[0].Expected != "42" {
		t.Fatalf("manifest %+v %v", e, err)
	}
	r, err := s.GetReport(ctx, report.ID)
	if err != nil || r.Counts.Fail != 1 || !r.PromotionEligible {
		t.Fatalf("report %+v %v", r, err)
	}
	a, err := s.ListAttempts(ctx, "eval")
	if err != nil || len(a) != 2 || a[0].Verdict != "compile_fail" || a[0].Result.Actual != "42" {
		t.Fatalf("attempts %+v %v", a, err)
	}
}
func TestArtifactDatabaseFailureNeverCreatesDanglingReference(t *testing.T) {
	s, _ := openTest(t)
	ctx := context.Background()
	if _, err := s.db.Exec("CREATE TRIGGER deny_artifact BEFORE INSERT ON artifacts BEGIN SELECT RAISE(ABORT,'fixture failure'); END"); err != nil {
		t.Fatal(err)
	}
	a, err := s.PublishArtifact(ctx, []byte("orphan-safe"), ArtifactMetadata{MediaType: "text/plain", Provenance: "offline_fixture"})
	if err == nil {
		t.Fatal("insert failure ignored")
	}
	var n int
	if err = s.db.QueryRow("SELECT count(*) FROM artifacts").Scan(&n); err != nil || n != 0 {
		t.Fatal("artifact row committed")
	}
	if _, err = os.Stat(filepath.Join(s.artifactDir, a.SHA256)); err != nil {
		t.Fatal("published orphan expected")
	}
	if _, err = s.ReadArtifact(ctx, a.ID); !errors.Is(err, ErrArtifactMissing) {
		t.Fatalf("orphan exposed %v", err)
	}
}
func TestReleaseCASDenialAndSafeRollback(t *testing.T) {
	s, _ := openTest(t)
	ctx := context.Background()
	_, r := completeTest(t, s, "eval")
	p, err := s.SavePolicy(ctx, samplePolicy())
	if err != nil {
		t.Fatal(err)
	}
	rel, err := s.CreateRelease(ctx, CreateReleaseRequest{ID: "release", WorkspaceID: "ws", WorkflowID: "codelens.codetask.go.v1", FilterHash: "all", ReportID: r.ID, PolicyID: p.ID, Idempotency: Idempotency{Key: "create", RequestHash: "create"}})
	if err != nil {
		t.Fatal(err)
	}
	apply := func(cmd, key string, rev int64, gate bool) (Release, error) {
		return s.ApplyReleaseTransition(ctx, ReleaseTransition{ReleaseID: rel.ID, ExpectedRevision: rev, Command: cmd, Actor: "user", ReasonCode: "CONFIRMED", EvidenceHash: r.Hash, Decision: func() GateDecision { d := decisionFixture(t, p, r); d.Allowed = gate; return d }(), Idempotency: Idempotency{Key: key, RequestHash: key}})
	}
	rel, err = apply("start_offline", "start", rel.Revision, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = apply("advance", "no_gate", rel.Revision, false); !errors.Is(err, ErrInsufficientEvidence) {
		t.Fatalf("uncomputed gates accepted %v", err)
	}
	rel, err = apply("advance", "shadow", rel.Revision, true)
	if err != nil || rel.State != "SHADOW" {
		t.Fatalf("shadow %+v %v", rel, err)
	}
	old := rel.Revision
	rel, err = apply("advance", "canary", rel.Revision, true)
	if err != nil || rel.TargetBPS != 500 || !rel.AdmissionEnabled {
		t.Fatalf("canary %+v %v", rel, err)
	}
	if _, err = apply("advance", "stale", old, true); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale revision %v", err)
	}
	if _, err = apply("advance", "stale", old, true); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale receipt %v", err)
	}
	if err = os.Remove(filepath.Join(s.artifactDir, r.Selected[0].OutputSHA256)); err != nil {
		t.Fatal(err)
	}
	rel, err = apply("rollback", "safe", rel.Revision, false)
	if err != nil || rel.State != "ROLLED_BACK" || rel.AdmissionEnabled || rel.TargetBPS != 0 {
		t.Fatalf("rollback requires no promotion evidence %+v %v", rel, err)
	}
	events, err := s.ListReleaseEvents(ctx, rel.ID)
	if err != nil || len(events) != 5 {
		t.Fatalf("audit events=%d %v", len(events), err)
	}
}
func TestCancellationRevokesLeaseAndPreservesCompleted(t *testing.T) {
	s, _ := openTest(t)
	ctx := context.Background()
	e := createTestEvaluation(t, s, "eval")
	e, err := s.ClaimEvaluation(ctx, e.ID, "run", e.Revision)
	if err != nil {
		t.Fatal(err)
	}
	appendResult(t, s, e.ID, e.LeaseGeneration, "stable", 1, "pass")
	a, err := s.AppendAttempt(ctx, Attempt{ID: "active", EvaluationID: e.ID, CaseID: "case1", Arm: "candidate", AttemptNo: 1, LeaseGeneration: e.LeaseGeneration, State: "running", Verdict: "not_scored"})
	if err != nil {
		t.Fatal(err)
	}
	e, _ = s.GetEvaluation(ctx, e.ID)
	if err = s.CancelEvaluation(ctx, e.ID, "old-run", e.LeaseGeneration, e.Revision); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("old run cancellation %v", err)
	}
	if err = s.CancelEvaluation(ctx, e.ID, "run", e.LeaseGeneration, e.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = s.FinalizeAttempt(ctx, a.ID, a.LeaseGeneration, AttemptOutcome{State: "completed", Verdict: "pass"}); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("late completion %v", err)
	}
	all, err := s.ListAttempts(ctx, e.ID)
	if err != nil || all[0].State != "cancelled" || all[1].State != "completed" {
		t.Fatalf("cancellation history %+v %v", all, err)
	}
}
func TestMissingArtifactCannotEnterTransactionOrAdvance(t *testing.T) {
	s, _ := openTest(t)
	ctx := context.Background()
	e := createTestEvaluation(t, s, "eval")
	e, err := s.ClaimEvaluation(ctx, e.ID, "run", e.Revision)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.PublishArtifact(ctx, []byte("lost before reference"), ArtifactMetadata{MediaType: "text/plain", Provenance: "offline_fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(s.artifactDir, a.SHA256)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AppendAttempt(ctx, Attempt{ID: "dangling", EvaluationID: e.ID, CaseID: "case1", Arm: "stable", AttemptNo: 1, LeaseGeneration: e.LeaseGeneration, State: "completed", Verdict: "pass", OutputArtifact: a.ID}); !errors.Is(err, ErrArtifactMissing) {
		t.Fatalf("dangling attempt committed: %v", err)
	}
	all, _ := s.ListAttempts(ctx, e.ID)
	if len(all) != 0 {
		t.Fatal("dangling attempt row exists")
	}
}
func TestAttemptBoundsAndCategoryMissingDenominators(t *testing.T) {
	s, _ := openTest(t)
	ctx := context.Background()
	e := createTestEvaluation(t, s, "eval")
	e, err := s.ClaimEvaluation(ctx, e.ID, "run", e.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AppendAttempt(ctx, Attempt{ID: "negative", EvaluationID: e.ID, CaseID: "case1", Arm: "stable", AttemptNo: 1, LeaseGeneration: e.LeaseGeneration, State: "running", Verdict: "not_scored", Metrics: Metrics{WallNS: Metric{Value: Float(-1), Unit: "ns"}}}); err == nil {
		t.Fatal("negative duration accepted")
	}
	e, _ = s.GetEvaluation(ctx, e.ID)
	r, err := s.CompleteEvaluation(ctx, e.ID, e.LeaseGeneration, e.Revision)
	if err != nil {
		t.Fatal(err)
	}
	cat := r.Categories["logic"]
	if r.Counts.Planned != 2 || r.Counts.Unknown != 2 || cat.Stable.Unknown != 1 || cat.Candidate.Unknown != 1 || r.PromotionEligible {
		t.Fatalf("missing cases lost denominators: %+v %+v", r.Counts, cat)
	}
}
func TestArtifactDuplicatePreservesOriginalProvenance(t *testing.T) {
	s, _ := openTest(t)
	ctx := context.Background()
	a, err := s.PublishArtifact(ctx, []byte("same content"), ArtifactMetadata{MediaType: "text/plain", Provenance: "offline_fixture"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.PublishArtifact(ctx, []byte("same content"), ArtifactMetadata{MediaType: "text/plain", Provenance: "offline_real"})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID || b.Metadata.Provenance != "offline_fixture" {
		t.Fatalf("dedup fabricated provenance: %+v", b)
	}
}

func TestCanonicalManifestNormalizesNegativeZero(t *testing.T) {
	m := sampleManifest()
	_, positive, err := CanonicalManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	m.Profile.Requested.Temperature = Float(math.Copysign(0, -1))
	_, negative, err := CanonicalManifest(m)
	if err != nil || positive != negative {
		t.Fatalf("equivalent zeros differ: %s %s %v", positive, negative, err)
	}
}
func TestReleaseCreationDenialReceiptSurvivesEvidenceRepair(t *testing.T) {
	s, _ := openTest(t)
	ctx := context.Background()
	_, report := completeTest(t, s, "eval")
	p, err := s.SavePolicy(ctx, samplePolicy())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.artifactDir, report.Selected[0].OutputSHA256)
	evidence, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	req := CreateReleaseRequest{ID: "release", WorkspaceID: "ws", WorkflowID: "codelens.codetask.go.v1", FilterHash: "all", ReportID: report.ID, PolicyID: p.ID, Idempotency: Idempotency{Key: "create", RequestHash: "create"}}
	if _, err = s.CreateRelease(ctx, req); !errors.Is(err, ErrInsufficientEvidence) {
		t.Fatalf("creation accepted missing artifact: %v", err)
	}
	if err = os.WriteFile(path, evidence, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateRelease(ctx, req); !errors.Is(err, ErrInsufficientEvidence) {
		t.Fatalf("deterministic creation denial not replayed: %v", err)
	}
	req.Idempotency = Idempotency{Key: "after-repair", RequestHash: "after-repair"}
	if _, err = s.CreateRelease(ctx, req); err != nil {
		t.Fatal(err)
	}
}
func TestMigrationChecksumIndependentOfCheckoutLineEndings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "line-endings.sqlite")
	db, err := connect(path, "rwc")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m := builtInMigrations()[0]
	m.sql = strings.ReplaceAll(strings.ReplaceAll(m.sql, "\r\n", "\n"), "\n", "\r\n")
	if err = applyMigrations(context.Background(), db, []migration{m}); err != nil {
		t.Fatal(err)
	}
	version, err := migrationState(context.Background(), db)
	if err != nil || version != 1 {
		t.Fatalf("checkout line endings invalidated migration: version=%d err=%v", version, err)
	}
}
