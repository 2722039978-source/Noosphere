package modellab

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/noosphere/backend/internal/gateway"
	"github.com/noosphere/backend/internal/gateway/providers"
	"github.com/noosphere/backend/internal/modelstore"
)

func TestPartialFailureRestartExplicitResumeAndDrift(t *testing.T) {
	lab, store, _, f, spec, _ := orchestrationLab(t)
	ctx := context.Background()
	f.failCode = "OUT_OF_MEMORY"
	e, err := lab.CreateEvaluation(ctx, "local", spec)
	if err != nil {
		t.Fatal(err)
	}
	e = awaitEvaluation(t, store, e.ID)
	if e.Status != "interrupted" {
		t.Fatalf("OOM must stop new admissions: %s", e.Status)
	}
	attempts, _ := store.ListAttempts(ctx, e.ID)
	if len(attempts) != 1 || attempts[0].State != "failed" || attempts[0].ErrorCode != "OUT_OF_MEMORY" || attempts[0].Metrics.WarmupArtifact == "" {
		t.Fatalf("missing independent failure/warmup evidence %+v", attempts)
	}
	f.mu.Lock()
	calls := len(f.calls)
	f.mu.Unlock()
	if err = lab.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	if len(f.calls) != calls {
		t.Fatal("restart automatically inferred")
	}
	f.mu.Unlock()
	oldCollector := lab.runtime.config.CollectEnvironment
	lab.runtime.config.CollectEnvironment = func(ctx context.Context) (modelstore.EnvironmentSnapshot, error) {
		env, err := oldCollector(ctx)
		env.GPU = "changed declared environment"
		return env, err
	}
	if _, err = lab.Resume(ctx, "local", e.ID, e.Revision); !errors.Is(err, modelstore.ErrInsufficientEvidence) && errorCode(err) != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("environment drift accepted: %v", err)
	}
	lab.runtime.config.CollectEnvironment = oldCollector
	resumed, err := lab.Resume(ctx, "local", e.ID, e.Revision)
	if err != nil {
		t.Fatal(err)
	}
	resumed = awaitEvaluation(t, store, resumed.ID)
	attempts, _ = store.ListAttempts(ctx, e.ID)
	if len(attempts) != 2 || attempts[1].AttemptNo != 2 {
		t.Fatalf("explicit resume lineage %+v", attempts)
	}
	t.Logf("restart trace evaluation=%s old_generation=%d resumed_generation=%d attempts=%d status=%s", e.ID, e.LeaseGeneration, resumed.LeaseGeneration, len(attempts), resumed.Status)
	// Durable creation without any Workspace dispatch is interrupted on startup.
	orphan, err := store.CreateEvaluation(ctx, modelstore.CreateEvaluationRequest{ID: "dispatch-crash-window", Manifest: e.Manifest, Idempotency: modelstore.Idempotency{Key: "dispatch-crash-window"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = lab.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	orphan, _ = store.GetEvaluation(ctx, orphan.ID)
	if orphan.Status != "interrupted" {
		t.Fatal("undispatched evaluation did not reconcile")
	}
}

func TestCancelKeepsCompletedCasesAndRejectsLateOutput(t *testing.T) {
	lab, store, manager, _, spec, _ := orchestrationLab(t)
	ctx := context.Background()
	entered, late := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	lab.runtime.infer = func(ctx context.Context, id string, req providers.EvaluationRequest) (*providers.EvaluationResponse, error) {
		n := calls.Add(1)
		if n == 3 {
			close(entered)
			<-late
		}
		if req.Kind == "warmup" {
			return &providers.EvaluationResponse{RawResponse: json.RawMessage(`{"done":true}`)}, nil
		}
		return &providers.EvaluationResponse{Output: "not Go source", RawResponse: json.RawMessage(`{"done":true}`)}, nil
	}
	// This fixture models residency independently of the inference completion.
	lab.runtime.inspect = func(context.Context, string, providers.EvaluationRequest) (providers.ModelResidencySnapshot, error) {
		return providers.ModelResidencySnapshot{TargetResident: calls.Load()%2 == 1}, nil
	}
	e, err := lab.CreateEvaluation(ctx, "local", spec)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("second arm not reached")
	}
	runs, err := manager.Runs("local")
	if err != nil {
		t.Fatal(err)
	}
	var runID string
	for _, run := range runs {
		if run.ReferenceID == e.ID {
			runID = run.ID
		}
	}
	if _, err = manager.CancelContext(ctx, runID); err != nil {
		t.Fatal(err)
	}
	close(late)
	e = awaitEvaluation(t, store, e.ID)
	if e.Status != "cancelled" {
		t.Fatal(e.Status)
	}
	attempts, _ := store.ListAttempts(ctx, e.ID)
	completed, cancelled := 0, 0
	for _, a := range attempts {
		if a.State == "completed" {
			completed++
			if a.Verdict != "invalid_output" {
				t.Fatal(a.Verdict)
			}
		}
		if a.State == "cancelled" {
			cancelled++
		}
	}
	if completed != 1 || cancelled != 23 {
		t.Fatalf("completed=%d cancelled=%d total=%d", completed, cancelled, len(attempts))
	}
	lab.Stop()
	manager.Close()
	lab.Close()
	after, _ := store.ListAttempts(ctx, e.ID)
	if len(after) != 24 {
		t.Fatal("late output altered cancelled denominator")
	}
}

func TestResumeUsesCommittedOutputWithoutDuplicateInference(t *testing.T) {
	lab, store, _, f, spec, _ := orchestrationLab(t)
	ctx := context.Background()
	f.failCode = "OUT_OF_MEMORY"
	e, err := lab.CreateEvaluation(ctx, "local", spec)
	if err != nil {
		t.Fatal(err)
	}
	e = awaitEvaluation(t, store, e.ID)
	lease, err := store.ResumeEvaluation(ctx, e.ID, e.Revision, e.ManifestHash)
	if err != nil {
		t.Fatal(err)
	}
	lease, err = store.BindResumedRun(ctx, e.ID, "crashed-score-run", lease.LeaseGeneration)
	if err != nil {
		t.Fatal(err)
	}
	c := e.Manifest.Dataset.Cases[0]
	source, err := lab.publishSource(ctx, "invalid Go source but durably received")
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.AppendAttempt(ctx, modelstore.Attempt{ID: "committed-before-crash", EvaluationID: e.ID, CaseID: c.ID, Arm: "stable", AttemptNo: 2, LeaseGeneration: lease.LeaseGeneration, State: "running", Verdict: "not_scored", InputArtifact: c.InputArtifact, Metrics: emptyMetrics()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CheckpointAttempt(ctx, attempt.ID, lease.LeaseGeneration, modelstore.AttemptOutcome{OutputArtifact: source, Result: modelstore.Result{InferenceCompleted: true}, Metrics: emptyMetrics()}); err != nil {
		t.Fatal(err)
	}
	if err = store.InterruptEvaluation(ctx, e.ID, lease.RunID, lease.LeaseGeneration, "APP_RESTARTED"); err != nil {
		t.Fatal(err)
	}
	e, _ = store.GetEvaluation(ctx, e.ID)
	f.mu.Lock()
	before := len(f.calls)
	f.mu.Unlock()
	if _, err = lab.Resume(ctx, "local", e.ID, e.Revision); err != nil {
		t.Fatal(err)
	}
	e = awaitEvaluation(t, store, e.ID)
	attempts, _ := store.ListAttempts(ctx, e.ID)
	var recovered modelstore.Attempt
	for _, a := range attempts {
		if a.CaseID == c.ID && a.Arm == "stable" && a.AttemptNo == 3 {
			recovered = a
		}
	}
	if recovered.State != "completed" || recovered.Verdict != "invalid_output" || recovered.OutputArtifact != source || recovered.Metrics.WarmVerified {
		t.Fatalf("committed output was not safely recovered: %+v", recovered)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) != before+1 || f.formal != 0 {
		t.Fatalf("duplicate provider call on committed source: calls=%d before=%d formal=%d", len(f.calls), before, f.formal)
	}
}

func TestWarmupCostsAreRecordedAndResidencyFailureBlocksGate(t *testing.T) {
	lab, store, _, f, spec, _ := orchestrationLab(t)
	ctx := context.Background()
	lab.runtime.inspect = func(context.Context, string, providers.EvaluationRequest) (providers.ModelResidencySnapshot, error) {
		return providers.ModelResidencySnapshot{}, nil
	}
	e, err := lab.CreateEvaluation(ctx, "local", spec)
	if err != nil {
		t.Fatal(err)
	}
	e = awaitEvaluation(t, store, e.ID)
	if e.Status != "incomplete" {
		t.Fatal(e.Status)
	}
	report, err := store.LatestReport(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts.Infra != 24 || report.Counts.Scored != 0 || report.PromotionEligible {
		t.Fatalf("missing residency became a score: %+v", report)
	}
	attempts, _ := store.ListAttempts(ctx, e.ID)
	for _, a := range attempts {
		if a.Metrics.WarmVerified || a.Metrics.WarmupArtifact == "" || a.Metrics.WarmupCost.Value == nil || *a.Metrics.WarmupCost.Value < 0 || a.Metrics.AvailableVRAM.Value != nil {
			t.Fatalf("invented warm/resource metric: %+v", a.Metrics)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.formal != 0 || len(f.calls) != 24 {
		t.Fatalf("formal inference ran without residency: formal=%d warmup=%d", f.formal, len(f.calls))
	}
}

func TestDriftRequiresNewEvaluationEvenIfTagIsRestored(t *testing.T) {
	lab, store, _, f, spec, _ := orchestrationLab(t)
	ctx := context.Background()
	original := lab.runtime.verify
	var checks atomic.Int32
	lab.runtime.verify = func(ctx context.Context, m modelstore.ModelRevision) error {
		if checks.Add(1) >= 3 {
			return identityError("MODEL_DRIFT", "changed after creation")
		}
		return original(ctx, m)
	}
	e, err := lab.CreateEvaluation(ctx, "local", spec)
	if err != nil {
		t.Fatal(err)
	}
	e = awaitEvaluation(t, store, e.ID)
	lab.runtime.verify = original
	all, _ := store.ListAttempts(ctx, e.ID)
	if len(all) != 1 || all[0].ErrorCode != "MODEL_DRIFT" || all[0].Result.ErrorStage != "identity_preflight" {
		t.Fatalf("pre-inference drift was not durable %+v", all)
	}
	if _, err = lab.Resume(ctx, "local", e.ID, e.Revision); errorCode(err) != "MODEL_DRIFT" {
		t.Fatalf("restored tag was allowed to reuse drifted experiment: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) != 0 {
		t.Fatal("drift caused inference")
	}
}

func TestForegroundPreemptionPersistsInterruptionWithoutUserCancel(t *testing.T) {
	lab, store, manager, _, spec, _ := orchestrationLab(t)
	ctx := context.Background()
	var preempt context.CancelCauseFunc
	lab.runtime.begin = func(ctx context.Context) (context.Context, func(), error) {
		session, cancel := context.WithCancelCause(ctx)
		preempt = cancel
		return session, func() { cancel(context.Canceled) }, nil
	}
	lab.runtime.infer = func(context.Context, string, providers.EvaluationRequest) (*providers.EvaluationResponse, error) {
		preempt(gateway.ErrForegroundPreempted)
		return nil, providers.EvaluationContextError(context.Canceled)
	}
	e, err := lab.CreateEvaluation(ctx, "local", spec)
	if err != nil {
		t.Fatal(err)
	}
	e = awaitEvaluation(t, store, e.ID)
	if e.Status != "interrupted" || e.CancelRequested {
		t.Fatalf("foreground recorded user cancellation %+v", e)
	}
	attempts, _ := store.ListAttempts(ctx, e.ID)
	if len(attempts) != 1 || attempts[0].State != "interrupted" || attempts[0].Verdict != "not_scored" || attempts[0].ErrorCode != "FOREGROUND_PREEMPTED" {
		t.Fatalf("preemption classification %+v", attempts)
	}
	runs, _ := manager.Runs("local")
	for _, run := range runs {
		if run.ReferenceID == e.ID && run.Status == "cancelled" {
			t.Fatal("task center misclassified operational preemption as user cancellation")
		}
	}
}
