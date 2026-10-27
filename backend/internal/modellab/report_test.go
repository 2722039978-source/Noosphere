package modellab

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noosphere/backend/internal/modelstore"
)

// Synthetic records exercise the persisted report path without inference or a compiler.
func reportFixture(t *testing.T, edit func(int, string, *modelstore.Attempt)) (*Service, modelstore.Evaluation) {
	return reportFixtureAt(t, t.TempDir(), edit)
}

func reportFixtureAt(t *testing.T, root string, edit func(int, string, *modelstore.Attempt)) (*Service, modelstore.Evaluation) {
	t.Helper()
	s, err := modelstore.Open(filepath.Join(root, "reports.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	m := modelstore.Manifest{SchemaVersion: 1, EncoderVersion: modelstore.EncoderVersion, WorkspaceID: "ws", WorkflowID: "codelens.codetask.go.v1", Source: "offline_fixture", MemoryMode: "not_applicable", Stable: modelstore.ModelRevision{ID: "stable", GatewayModelID: "local", Provider: "ollama", ModelName: "stable:1", IdentityLevel: "digest", Digest: strings.Repeat("a", 64)}, Candidate: modelstore.ModelRevision{ID: "candidate", GatewayModelID: "local", Provider: "ollama", ModelName: "candidate:1", IdentityLevel: "digest", Digest: strings.Repeat("b", 64)}, Profile: modelstore.InferenceProfile{ID: "profile"}, Dataset: modelstore.DatasetRevision{ID: "dataset", BenchmarkID: "go", Version: "v1", ContentHash: "synthetic-dataset-hash", ScorerVersion: "host-v1"}, Environment: modelstore.EnvironmentSnapshot{ID: "env", OS: "windows", Arch: "amd64", CollectorVersion: "synthetic-v1", VRAM: unknownMetric("bytes", "GPU collector unavailable")}}
	input, err := s.PublishArtifact(context.Background(), []byte("synthetic frozen input"), modelstore.ArtifactMetadata{MediaType: "text/plain", Provenance: "offline_fixture"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		m.Dataset.Cases = append(m.Dataset.Cases, modelstore.Case{ID: fmt.Sprintf("case%02d", i), Category: []string{"logic", "boundary", "repair"}[i/4], InputArtifact: input.ID, Expected: "synthetic expected"})
	}
	ev, err := s.CreateEvaluation(context.Background(), modelstore.CreateEvaluationRequest{ID: "eval", Manifest: m, Idempotency: modelstore.Idempotency{Key: "create"}})
	if err != nil {
		t.Fatal(err)
	}
	ev, err = s.ClaimEvaluation(context.Background(), ev.ID, "run", ev.Revision)
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.PublishArtifact(context.Background(), []byte("synthetic output"), modelstore.ArtifactMetadata{MediaType: "text/plain", Provenance: "offline_fixture"})
	if err != nil {
		t.Fatal(err)
	}
	score, _ := json.Marshal(ScoreResult{CaseID: "case00", ScorerVersion: "host-v1", CompileDuration: 700, TotalDuration: 1100, Assertions: []AssertionResult{{PreparationDuration: 100, ExecutionDuration: 300}}})
	art, err := s.PublishArtifact(context.Background(), score, modelstore.ArtifactMetadata{MediaType: "application/json", Provenance: "offline_fixture"})
	if err != nil {
		t.Fatal(err)
	}
	formal, err := s.PublishArtifact(context.Background(), []byte(`{"kind":"formal","response":{"total_duration_ns":90}}`), modelstore.ArtifactMetadata{MediaType: "application/json", Provenance: "offline_fixture"})
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range m.Dataset.Cases {
		for _, arm := range []string{"stable", "candidate"} {
			a := modelstore.Attempt{ID: c.ID + arm, EvaluationID: ev.ID, CaseID: c.ID, Arm: arm, AttemptNo: 1, LeaseGeneration: ev.LeaseGeneration, State: "completed", Verdict: "pass", InputArtifact: input.ID, OutputArtifact: out.ID, Result: modelstore.Result{InferenceCompleted: true, ScorerVersion: "host-v1"}, Metrics: emptyMetrics()}
			warmBody, _ := json.Marshal(map[string]any{"kind": "warmup", "synthetic": true, "attempt_id": a.ID, "response": map[string]int{"total_duration_ns": 900, "load_duration_ns": 800}})
			warm, err := s.PublishArtifact(context.Background(), warmBody, modelstore.ArtifactMetadata{MediaType: "application/json", Provenance: "offline_fixture"})
			if err != nil {
				t.Fatal(err)
			}
			a.Metrics.WallNS = observedMetric(float64(i+1)*100, "ns", "host_monotonic")
			a.Metrics.LoadNS = observedMetric(5, "ns", "ollama_native")
			a.Metrics.DecodeNS = observedMetric(50, "ns", "ollama_native")
			a.Metrics.Tokens = observedMetric(5, "tokens", "ollama_native")
			a.Metrics.WarmVerified = true
			a.Metrics.WarmupCost = observedMetric(999, "ns", "host_monotonic")
			a.Metrics.WarmupArtifact = warm.ID
			if i == 0 {
				a.Result.EvidenceArtifacts = []string{art.ID, formal.ID}
			}
			if edit != nil {
				edit(i, arm, &a)
			}
			if _, err = s.AppendAttempt(context.Background(), a); err != nil {
				t.Fatal(err)
			}
		}
	}
	ev, err = s.GetEvaluation(context.Background(), ev.ID)
	if err != nil {
		t.Fatal(err)
	}
	return New(s, nil), ev
}
func buildFixture(t *testing.T, s *Service, e modelstore.Evaluation) (modelstore.Report, Comparison) {
	t.Helper()
	r, err := s.BuildReport(context.Background(), e.ID)
	if err != nil {
		t.Fatal(err)
	}
	var c Comparison
	if err = json.Unmarshal(r.Analysis, &c); err != nil {
		t.Fatal(err)
	}
	return r, c
}
func TestCompileFailureIsScoredAndReportComplete(t *testing.T) {
	s, e := reportFixture(t, func(i int, arm string, a *modelstore.Attempt) {
		if i == 0 && arm == "candidate" {
			a.Verdict = "compile_fail"
			a.ErrorCode = "COMPILE_ERROR"
		}
		if i == 1 && arm == "stable" {
			a.Verdict = "functional_fail"
		}
		if i == 2 {
			a.Verdict = "functional_fail"
		}
	})
	r, c := buildFixture(t, s, e)
	if r.Counts.Scored != 24 || r.Counts.Fail != 4 || r.Counts.PairedComplete != 12 || r.PairMatrix != [4]int{9, 1, 1, 1} || c.Status != "complete" || len(c.Failures) != 4 || c.Loss.N != 12 || *c.Loss.Rate != 1.0/12 {
		t.Fatalf("bad comparison: %+v %+v", r, c)
	}
	if c.Categories["logic"].Candidate.N != 4 || c.Arms["candidate"].N != 12 || c.Performance.Status != "sufficient" {
		t.Fatalf("denominators or scored-failure latency lost: %+v", c)
	}
	logic := c.Categories["logic"]
	if logic.PairMatrix != [4]int{1, 1, 1, 1} || logic.PairedComplete != 4 || logic.Loss.N != 4 || *logic.Loss.Rate != 0.25 || *logic.Stable.Rate != *logic.Candidate.Rate {
		t.Fatalf("category rates hid paired regression: %+v", logic)
	}
	stored, err := s.store.GetReport(context.Background(), r.ID)
	if err != nil || string(stored.Analysis) != string(r.Analysis) || stored.Hash != r.Hash {
		t.Fatalf("persisted analysis: %v", err)
	}
	oldHash := r.Hash
	oldAnalysis := string(r.Analysis)
	fresh, _ := s.store.GetEvaluation(context.Background(), e.ID)
	fresh, err = s.store.ResumeEvaluation(context.Background(), e.ID, fresh.Revision, e.ManifestHash)
	if err != nil {
		t.Fatal(err)
	}
	// Explicit later interrupted attempt must replace the old pass, without changing history.
	_, err = s.store.AppendAttempt(context.Background(), modelstore.Attempt{ID: "retry", EvaluationID: e.ID, CaseID: "case03", Arm: "candidate", AttemptNo: 2, LeaseGeneration: fresh.LeaseGeneration, State: "interrupted", Verdict: "not_scored", InputArtifact: e.Manifest.Dataset.Cases[3].InputArtifact})
	if err != nil {
		t.Fatal(err)
	}
	r2, c2 := buildFixture(t, s, e)
	if r2.Counts.Scored != 23 || c2.Status != "incomplete" || c2.Loss.N != 11 || len(r2.Superseded) != 1 || c2.Arms["candidate"].N != 11 || c2.Performance.WarmupCalls != 24 {
		t.Fatalf("retry inflated denominator: %+v %+v", r2, c2)
	}
	historical, err := s.store.GetReport(context.Background(), r.ID)
	if err != nil || historical.PromotionEligible || historical.Hash != oldHash || string(historical.Analysis) != oldAnalysis {
		t.Fatalf("history changed: %+v %v", historical, err)
	}
	fresh, _ = s.store.GetEvaluation(context.Background(), e.ID)
	fresh, err = s.store.ResumeEvaluation(context.Background(), e.ID, fresh.Revision, e.ManifestHash)
	if err != nil {
		t.Fatal(err)
	}
	all, err := s.store.ListAttempts(context.Background(), e.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range all {
		if a.ID == "case03candidate" {
			a.ID = "durable-replay"
			a.AttemptNo = 3
			a.LeaseGeneration = fresh.LeaseGeneration
			if _, err = s.store.AppendAttempt(context.Background(), a); err != nil {
				t.Fatal(err)
			}
		}
	}
	_, replayed := buildFixture(t, s, e)
	if replayed.Loss.N != 12 || replayed.Arms["candidate"].N != 12 || replayed.Performance.WarmupCalls != 24 {
		t.Fatalf("replay counted as independent case/call: %+v", replayed)
	}
}
func TestPairedLatencyRatioUsesMedianOfTwelveCaseRatios(t *testing.T) {
	s, e := reportFixture(t, func(i int, arm string, a *modelstore.Attempt) {
		if arm == "candidate" {
			a.Metrics.WallNS.Value = modelstore.Float(float64(12-i) * 100)
		}
	})
	_, c := buildFixture(t, s, e)
	want := (6.0/7 + 7.0/6) / 2
	if c.Performance.Status != "sufficient" || c.Performance.SampleCount != 12 || math.Abs(*c.Performance.MedianRatio.Value-want) > 1e-12 || *c.Performance.MedianRatio.Value == 1 {
		t.Fatalf("not median of ratios: %+v", c.Performance)
	}
}
func TestLatencyMismatchOrTimeoutIsInsufficient(t *testing.T) {
	for _, bad := range []string{"zero", "missing", "timeout", "truncated", "cold", "protocol", "unknown_quality", "wrong_unit", "untrusted_wall", "oom"} {
		t.Run(bad, func(t *testing.T) {
			s, e := reportFixture(t, func(i int, arm string, a *modelstore.Attempt) {
				if i != 0 || arm != "candidate" {
					return
				}
				switch bad {
				case "zero":
					a.Metrics.WallNS.Value = modelstore.Float(0)
				case "missing":
					a.Metrics.WallNS.Value = nil
				case "timeout":
					a.State = "failed"
					a.Verdict = "infra_error"
					a.ErrorCode = "INFERENCE_TIMEOUT"
				case "truncated":
					a.Result.Truncated = true
				case "cold":
					a.Metrics.WarmVerified = false
				case "protocol":
					a.Metrics.LoadingProtocol = "cold_v1"
				case "unknown_quality":
					a.Metrics.WallNS.Quality = "unknown"
				case "wrong_unit":
					a.Metrics.WallNS.Unit = "ms"
				case "untrusted_wall":
					a.Metrics.WallNS.Source = "user_declared"
				case "oom":
					a.State = "failed"
					a.Verdict = "infra_error"
					a.ErrorCode = "OUT_OF_MEMORY"
				}
			})
			_, c := buildFixture(t, s, e)
			if c.Performance.Status != "insufficient" || c.Performance.MedianRatio.Value != nil || len(c.Performance.Reasons) == 0 {
				t.Fatalf("dropped invalid case: %+v", c.Performance)
			}
		})
	}
}

func TestMissingOrCorruptWarmupEvidenceMakesNewLatencyInsufficient(t *testing.T) {
	for _, damage := range []string{"missing", "corrupt", "unrelated_scorer_missing"} {
		t.Run(damage, func(t *testing.T) {
			root := t.TempDir()
			s, e := reportFixtureAt(t, root, nil)
			historical, before := buildFixture(t, s, e)
			if before.Performance.Status != "sufficient" || before.Performance.SampleCount != 12 || historical.PromotionEligible != true {
				t.Fatal("fixture is not initially complete and comparable")
			}
			all, err := s.store.ListAttempts(context.Background(), e.ID)
			if err != nil {
				t.Fatal(err)
			}
			warmups := map[string]bool{}
			target := ""
			for _, a := range all {
				warmups[a.Metrics.WarmupArtifact] = true
				if a.CaseID == "case00" && a.Arm == "candidate" {
					target = a.Metrics.WarmupArtifact
					if damage == "unrelated_scorer_missing" {
						target = a.Result.EvidenceArtifacts[0]
					}
				}
			}
			if len(warmups) != 24 || target == "" {
				t.Fatal("test must damage one unique warmup rather than all pairs")
			}
			fresh, err := s.store.GetEvaluation(context.Background(), e.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.store.ResumeEvaluation(context.Background(), e.ID, fresh.Revision, e.ManifestHash); err != nil {
				t.Fatal(err)
			}
			artifactPath := filepath.Join(root, "model-engineering", "artifacts", strings.TrimPrefix(target, "art_"))
			if damage == "corrupt" {
				err = os.WriteFile(artifactPath, []byte("corrupted synthetic evidence"), 0600)
			} else {
				err = os.Remove(artifactPath)
			}
			if err != nil {
				t.Fatal(err)
			}
			r, c := buildFixture(t, s, e)
			if r.PromotionEligible || c.Status != "incomplete" || !strings.Contains(strings.Join(r.IneligibilityReasons, "|"), "ARTIFACT_MISSING:") {
				t.Fatalf("missing evidence was not diagnosed: %+v %+v", r, c)
			}
			if damage == "unrelated_scorer_missing" {
				if c.Performance.Status != "sufficient" || c.Performance.SampleCount != 12 || c.Performance.MedianRatio.Value == nil {
					t.Fatalf("scorer evidence loss invalidated complete formal inference: %+v", c.Performance)
				}
			} else {
				if c.Performance.Status != "insufficient" || c.Performance.SampleCount != 11 || c.Performance.MedianRatio.Value != nil || c.Performance.Cases[0].Ratio.Value != nil || !strings.Contains(c.Performance.Cases[0].Ratio.MissingReason, "warmup evidence unavailable") || !strings.Contains(strings.Join(c.Performance.Reasons, "|"), "case00") {
					t.Fatalf("unavailable warmup still counted as comparable: %+v", c.Performance)
				}
			}
			old, err := s.store.GetReport(context.Background(), historical.ID)
			if err != nil || old.Hash != historical.Hash || string(old.Analysis) != string(historical.Analysis) || old.PromotionEligible || !strings.Contains(strings.Join(old.IneligibilityReasons, "|"), "ARTIFACT_MISSING:") {
				t.Fatalf("historical analysis changed or dynamic eligibility ignored loss: %+v %v", old, err)
			}
		})
	}
}
func TestUnknownGPUAndZeroDecodeDurationStayUnknown(t *testing.T) {
	s, e := reportFixture(t, func(i int, arm string, a *modelstore.Attempt) { a.Metrics.DecodeNS.Value = modelstore.Float(0) })
	_, c := buildFixture(t, s, e)
	for _, d := range c.Diagnostics {
		if d.AvailableVRAM.Value != nil || d.TokensPerSecond.Value != nil || d.TokensPerSecond.MissingReason == "" {
			t.Fatalf("fabricated metrics: %+v", d)
		}
	}
}
func TestColdWarmAndCompilerTimeSeparated(t *testing.T) {
	s, e := reportFixture(t, nil)
	_, c := buildFixture(t, s, e)
	d := c.Diagnostics[0]
	if *d.WallNS.Value != 100 || *d.WarmupCost.Value != 999 || *d.CompilerNS.Value != 700 || *d.PreparationNS.Value != 100 || *d.ExecutionNS.Value != 300 || *d.TokensPerSecond.Value != 1e8 || c.Performance.WarmupCalls != 24 {
		t.Fatalf("mixed timing: %+v %+v", d, c.Performance)
	}
	if d.EngineTotalNS.Value == nil || *d.EngineTotalNS.Value != 90 || d.WarmupLoadNS.Value == nil || *d.WarmupLoadNS.Value != 800 || d.WarmupEngineTotalNS.Value == nil || *d.WarmupEngineTotalNS.Value != 900 {
		t.Fatalf("engine/cold load timing lost: %+v", d)
	}
	if c.MetricCoverage["stable"]["available_vram_bytes"].Samples != 0 || c.MetricCoverage["stable"]["available_vram_bytes"].Missing != 12 || c.MetricCoverage["stable"]["formal_wall_ns"].Samples != 12 {
		t.Fatalf("metric denominators: %+v", c.MetricCoverage)
	}
	if _, err := ReadComparison(modelstore.Report{}); err == nil {
		t.Fatal("legacy analysis unavailable was silently recomputed")
	}
	if os.Getenv("NOOSPHERE_WRITE_REPORT_EXAMPLES") != "" {
		writeReportExamples(t)
	}
}
func writeReportExamples(t *testing.T) {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "docs", "roadmap", "examples")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"correct", "regression", "insufficient"} {
		s, e := reportFixture(t, func(i int, arm string, a *modelstore.Attempt) {
			if arm != "candidate" {
				return
			}
			if name == "regression" && i == 0 {
				a.Verdict = "compile_fail"
				a.ErrorCode = "COMPILE_ERROR"
			}
			if name == "insufficient" && i == 0 {
				a.Metrics.WarmVerified = false
			}
		})
		r, c := buildFixture(t, s, e)
		summary := map[string]any{"export_kind": "synthetic_summary_not_full_archival_report", "source": c.Source, "notice": "All timings and outcomes are synthetic fixtures, generated through real SQLite/artifact report code; not measured model performance or production proof.", "report_id": r.ID, "report_hash": r.Hash, "manifest_hash": r.ManifestHash, "attempt_set_hash": r.AttemptSetHash, "counts": r.Counts, "categories": r.Categories, "pair_matrix": r.PairMatrix, "pair_matrix_labels": c.PairLabels, "arms": c.Arms, "category_intervals": c.Categories, "completion": c.Completion, "loss_rate": c.Loss, "failures": c.Failures, "performance": c.Performance, "resource_example": map[string]any{"available_vram_bytes": c.Diagnostics[0].AvailableVRAM, "gpu_samples": c.MetricCoverage["candidate"]["available_vram_bytes"]}, "separate_timing_example": c.Diagnostics[0], "limitations": c.Limitations}
		raw, err := json.MarshalIndent(summary, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, "t6-synthetic-"+name+".json"), append(raw, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	}
}
