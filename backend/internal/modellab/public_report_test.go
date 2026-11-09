package modellab

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"github.com/noosphere/backend/internal/modelstore"
)

// 输入记录明确使用 offline_fixture；测试执行真实 SQLite、artifact 和报告计算。
func publicReportRecords(t *testing.T, incomplete bool, unavailableMetric string) (*Service, modelstore.Evaluation) {
	t.Helper()
	ctx := context.Background()
	store, err := modelstore.Open(filepath.Join(t.TempDir(), "reports.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	hash := func(value string) string {
		sum := sha256.Sum256([]byte(value))
		return hex.EncodeToString(sum[:])
	}
	publish := func(content string) modelstore.Artifact {
		artifact, err := store.PublishArtifact(ctx, []byte(content), modelstore.ArtifactMetadata{MediaType: "text/plain", Provenance: "offline_fixture"})
		if err != nil {
			t.Fatal(err)
		}
		return artifact
	}
	input, output := publish("报告聚合输入记录"), publish("报告聚合输出记录")
	m := modelstore.Manifest{
		SchemaVersion: 1, EncoderVersion: modelstore.EncoderVersion,
		WorkspaceID: "public-workspace", WorkflowID: "codelens.codetask.go.v1",
		Source: "offline_fixture", MemoryMode: "not_applicable",
		Stable:      modelstore.ModelRevision{ID: "stable", GatewayModelID: "local", Provider: "ollama", ModelName: "example:1", IdentityLevel: "digest", Digest: hash("version-one")},
		Candidate:   modelstore.ModelRevision{ID: "candidate", GatewayModelID: "local", Provider: "ollama", ModelName: "example:2", IdentityLevel: "digest", Digest: hash("version-two")},
		Profile:     modelstore.InferenceProfile{ID: "profile"},
		Dataset:     modelstore.DatasetRevision{ID: "dataset", BenchmarkID: "go", Version: "v1", ContentHash: hash("dataset"), ScorerVersion: "host-v1"},
		Environment: modelstore.EnvironmentSnapshot{ID: "environment", OS: "windows", Arch: "amd64", CollectorVersion: "v1"},
	}
	for i := 0; i < 12; i++ {
		m.Dataset.Cases = append(m.Dataset.Cases, modelstore.Case{ID: fmt.Sprintf("case%02d", i), Category: "logic", InputArtifact: input.ID, Expected: "42"})
	}
	evaluation, err := store.CreateEvaluation(ctx, modelstore.CreateEvaluationRequest{ID: "evaluation", Manifest: m, Idempotency: modelstore.Idempotency{Key: "create"}})
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err = store.ClaimEvaluation(ctx, evaluation.ID, "run", evaluation.Revision)
	if err != nil {
		t.Fatal(err)
	}
	for i, cs := range m.Dataset.Cases {
		for _, arm := range []string{"stable", "candidate"} {
			if incomplete && i == 11 && arm == "candidate" {
				continue
			}
			a := modelstore.Attempt{
				ID: cs.ID + "-" + arm, EvaluationID: evaluation.ID, CaseID: cs.ID, Arm: arm,
				AttemptNo: 1, LeaseGeneration: evaluation.LeaseGeneration,
				State: "completed", Verdict: "pass", InputArtifact: input.ID, OutputArtifact: output.ID,
				Result: modelstore.Result{InferenceCompleted: true}, Metrics: emptyMetrics(),
			}
			a.Metrics.WallNS = observedMetric(float64(i+1)*100, "ns", "host_monotonic")
			if arm == "candidate" {
				a.Metrics.WallNS.Value = modelstore.Float(float64(12-i) * 100)
			}
			a.Metrics.LoadingProtocol = LoadingProtocol
			a.Metrics.WarmVerified = true
			a.Metrics.WarmupArtifact = publish("预热记录 " + a.ID).ID
			if (i == 0 && arm == "candidate") || i == 2 {
				a.Verdict = "compile_fail"
			}
			if i == 1 && arm == "stable" {
				a.Verdict = "functional_fail"
			}
			if incomplete && arm == "candidate" {
				switch i {
				case 8:
					a.State, a.Verdict, a.ErrorCode = "failed", "infra_error", "INFERENCE_TIMEOUT"
					a.Result.InferenceCompleted = false
				case 9:
					a.State, a.Verdict = "cancelled", "not_scored"
					a.Result.InferenceCompleted = false
				case 10:
					a.State, a.Verdict = "interrupted", "not_scored"
					a.Result.InferenceCompleted = false
				}
			}
			if i == 0 && arm == "candidate" {
				switch unavailableMetric {
				case "missing":
					a.Metrics.WallNS = unknownMetric("ns", "未采集")
				case "zero":
					a.Metrics.WallNS.Value = modelstore.Float(0)
				case "wrong_unit":
					a.Metrics.WallNS.Unit = "ms"
				case "incomplete":
					a.Result.InferenceCompleted = false
				}
			}
			if _, err := store.AppendAttempt(ctx, a); err != nil {
				t.Fatal(err)
			}
		}
	}
	return New(store, nil), evaluation
}

func publicBuildReport(t *testing.T, service *Service, evaluation modelstore.Evaluation) (modelstore.Report, Comparison) {
	t.Helper()
	report, err := service.BuildReport(context.Background(), evaluation.ID)
	if err != nil {
		t.Fatal(err)
	}
	comparison, err := ReadComparison(report)
	if err != nil {
		t.Fatal(err)
	}
	return report, comparison
}

func TestPublicReportAggregatesScoredAndIncompleteAttempts(t *testing.T) {
	for _, incomplete := range []bool{false, true} {
		t.Run(fmt.Sprintf("incomplete_%t", incomplete), func(t *testing.T) {
			service, evaluation := publicReportRecords(t, incomplete, "")
			report, comparison := publicBuildReport(t, service, evaluation)
			if report.Counts.Planned != 24 || report.Counts.Fail != 4 || comparison.Source != "offline_fixture" {
				t.Fatalf("报告总数或来源不正确: %+v", report)
			}
			wantScored, wantPairs, wantFailures, wantStatus := 24, 12, 4, "complete"
			wantMatrix := [4]int{9, 1, 1, 1}
			if incomplete {
				wantScored, wantPairs, wantFailures, wantStatus = 20, 8, 8, "incomplete"
				wantMatrix = [4]int{5, 1, 1, 1}
				if report.Counts.Infra != 1 || report.Counts.Cancelled != 1 || report.Counts.Interrupted != 1 || report.Counts.Unknown != 1 || report.PromotionEligible {
					t.Fatalf("未完成记录被计入已评分结果: %+v", report.Counts)
				}
			}
			if report.Counts.Scored != wantScored || report.Counts.PairedComplete != wantPairs || report.PairMatrix != wantMatrix || len(comparison.Failures) != wantFailures || comparison.Status != wantStatus {
				t.Fatalf("聚合计数不正确: report=%+v comparison=%+v", report.Counts, comparison)
			}
			if comparison.Loss.N != wantPairs || comparison.Loss.Rate == nil || *comparison.Loss.Rate != 1/float64(wantPairs) || comparison.Categories["logic"].PairMatrix != wantMatrix {
				t.Fatalf("成对样本的分母不正确: %+v", comparison)
			}
			if comparison.Completion.Rate == nil || *comparison.Completion.Rate != float64(wantScored)/24 {
				t.Fatalf("完成比例不正确: %+v", comparison.Completion)
			}
			coverage := comparison.MetricCoverage["candidate"]["available_vram_bytes"]
			if coverage.Samples != 0 || coverage.Missing != 12 || len(coverage.MissingReasons) == 0 {
				t.Fatalf("未知显存被当作有效数值: %+v", coverage)
			}
			stored, err := service.store.GetReport(context.Background(), report.ID)
			if err != nil || stored.Hash != report.Hash || string(stored.Analysis) != string(report.Analysis) {
				t.Fatalf("报告保存后内容不一致: %v", err)
			}
		})
	}
}

func TestPublicReportLatencyRequiresCompleteComparablePairs(t *testing.T) {
	for _, unavailable := range []string{"", "missing", "zero", "wrong_unit", "incomplete"} {
		t.Run("metric_"+unavailable, func(t *testing.T) {
			service, evaluation := publicReportRecords(t, false, unavailable)
			_, comparison := publicBuildReport(t, service, evaluation)
			performance := comparison.Performance
			if unavailable == "" {
				want := (6.0/7 + 7.0/6) / 2
				if performance.Status != "sufficient" || performance.SampleCount != 12 || performance.MedianRatio.Value == nil || math.Abs(*performance.MedianRatio.Value-want) > 1e-12 {
					t.Fatalf("应计算十二组成对比值的中位数: %+v", performance)
				}
			} else if performance.Status != "insufficient" || performance.SampleCount != 11 || performance.MedianRatio.Value != nil || len(performance.Reasons) == 0 {
				t.Fatalf("不完整指标不能产生总体性能比值: %+v", performance)
			}
		})
	}
}

func TestPublicReportRetryUsesLatestAttemptAndPreservesHistory(t *testing.T) {
	ctx := context.Background()
	service, evaluation := publicReportRecords(t, false, "")
	original, _ := publicBuildReport(t, service, evaluation)
	current, err := service.store.GetEvaluation(ctx, evaluation.ID)
	if err != nil {
		t.Fatal(err)
	}
	current, err = service.store.ResumeEvaluation(ctx, current.ID, current.Revision, current.ManifestHash)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.store.AppendAttempt(ctx, modelstore.Attempt{
		ID: "retry", EvaluationID: current.ID, CaseID: "case03", Arm: "candidate",
		AttemptNo: 2, LeaseGeneration: current.LeaseGeneration,
		State: "interrupted", Verdict: "not_scored", InputArtifact: current.Manifest.Dataset.Cases[3].InputArtifact,
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, comparison := publicBuildReport(t, service, current)
	if updated.Counts.Scored != 23 || updated.Counts.PairedComplete != 11 || len(updated.Superseded) != 1 || comparison.Arms["candidate"].N != 11 || comparison.Status != "incomplete" {
		t.Fatalf("重试记录没有取代原样本: %+v", updated)
	}
	selectedRetry := false
	for _, attempt := range updated.Selected {
		if attempt.ID == "retry" {
			selectedRetry = true
		}
		if attempt.ID == "case03-candidate" {
			t.Fatal("旧的成功记录仍被选中")
		}
	}
	if !selectedRetry {
		t.Fatal("新报告缺少当前重试记录")
	}
	history, err := service.store.GetReport(ctx, original.ID)
	if err != nil || history.Hash != original.Hash || string(history.Analysis) != string(original.Analysis) || history.PromotionEligible {
		t.Fatalf("历史报告内容改变或仍可晋级: %+v %v", history, err)
	}
	all, err := service.store.ListAttempts(ctx, current.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compare(current, original, all, nil); !errors.Is(err, modelstore.ErrInvalid) {
		t.Fatalf("不能用旧选择集合重新计算当前报告: %v", err)
	}
}
