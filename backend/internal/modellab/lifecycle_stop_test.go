package modellab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/noosphere/backend/internal/evalrunner"
	"github.com/noosphere/backend/internal/gateway"
	"github.com/noosphere/backend/internal/gateway/providers"
)

func TestCleanupFailureStopsAfterNonfatalInferenceFailure(t *testing.T) {
	lab, store, _, fixture, spec, _ := orchestrationLab(t)
	fixture.failCode = "PROVIDER_UNAVAILABLE"
	var releases atomic.Int32
	lab.runtime.release = func(context.Context, string, providers.EvaluationRequest) (providers.ModelResidencySnapshot, error) {
		releases.Add(1)
		fixture.mu.Lock()
		fixture.resident = false // Actual unload may succeed while its confirmation fails.
		fixture.mu.Unlock()
		return providers.ModelResidencySnapshot{}, identityError("RELEASE_UNCONFIRMED", "fixture unload confirmation failed")
	}
	e, err := lab.CreateEvaluation(context.Background(), "local", spec)
	if err != nil {
		t.Fatal(err)
	}
	e = awaitEvaluation(t, store, e.ID)
	attempts, err := store.ListAttempts(context.Background(), e.ID)
	if err != nil {
		t.Fatal(err)
	}
	fixture.mu.Lock()
	calls, formal := len(fixture.calls), fixture.formal
	fixture.mu.Unlock()
	if e.Status != "interrupted" || len(attempts) != 1 || calls != 1 || formal != 0 || releases.Load() != 1 {
		t.Fatalf("cleanup uncertainty admitted another arm: status=%s attempts=%d calls=%d formal=%d releases=%d", e.Status, len(attempts), calls, formal, releases.Load())
	}
	a := attempts[0]
	if a.State != "failed" || a.Verdict != "infra_error" || a.ErrorCode != "RELEASE_UNCONFIRMED" || a.Metrics.WarmVerified {
		t.Fatalf("release uncertainty not retained: %+v", a)
	}
	codes := map[string]bool{}
	for _, id := range a.Result.EvidenceArtifacts {
		raw, err := store.ReadArtifact(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		var p phaseEvidence
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatal(err)
		}
		codes[p.ErrorCode] = true
	}
	if !codes["PROVIDER_UNAVAILABLE"] || !codes["RELEASE_UNCONFIRMED"] {
		t.Fatalf("joined failure evidence lost: %v", codes)
	}
	t.Log("nonfatal inference plus unconfirmed release stopped after one warmup; both phase reasons persisted")
}

func TestLateForegroundPreemptionRequiresExplicitResume(t *testing.T) {
	for _, timing := range []string{"cleanup", "session_close"} {
		t.Run(timing, func(t *testing.T) {
			lab, store, _, fixture, spec, _ := orchestrationLab(t)
			var preempt context.CancelCauseFunc
			var warmups, formals atomic.Int32
			lab.runtime.begin = func(ctx context.Context) (context.Context, func(), error) {
				session, cancel := context.WithCancelCause(ctx)
				preempt = cancel
				return session, func() {
					if timing == "session_close" {
						cancel(gateway.ErrForegroundPreempted)
					}
					cancel(context.Canceled)
				}, nil
			}
			original := lab.runtime.infer
			lab.runtime.infer = func(ctx context.Context, id string, req providers.EvaluationRequest) (*providers.EvaluationResponse, error) {
				if req.Kind == "warmup" {
					warmups.Add(1)
					return original(ctx, id, req)
				}
				formals.Add(1)
				return &providers.EvaluationResponse{Output: nativeInvalidSource, RawResponse: json.RawMessage(`{"done":true}`)}, nil
			}
			lab.runtime.release = func(ctx context.Context, id string, req providers.EvaluationRequest) (providers.ModelResidencySnapshot, error) {
				attempts, err := store.ListAttempts(ctx, newEvaluationID("local", spec.IdempotencyKey))
				committed := false
				for _, a := range attempts {
					if a.ID == req.AttemptID {
						committed = a.Result.InferenceCompleted
					}
				}
				if err != nil || !committed {
					t.Error("formal completion was not checkpointed before late preemption", err)
				}
				fixture.mu.Lock()
				fixture.resident = false
				fixture.mu.Unlock()
				if timing == "cleanup" {
					preempt(gateway.ErrForegroundPreempted)
				}
				return providers.ModelResidencySnapshot{}, nil
			}
			e, err := lab.CreateEvaluation(context.Background(), "local", spec)
			if err != nil {
				t.Fatal(err)
			}
			e = awaitEvaluation(t, store, e.ID)
			attempts, err := store.ListAttempts(context.Background(), e.ID)
			if err != nil {
				t.Fatal(err)
			}
			if e.Status != "interrupted" || e.CancelRequested || len(attempts) != 1 || warmups.Load() != 1 || formals.Load() != 1 {
				t.Fatalf("late foreground admitted next arm: status=%s cancel=%t attempts=%d warmups=%d formals=%d", e.Status, e.CancelRequested, len(attempts), warmups.Load(), formals.Load())
			}
			a := attempts[0]
			if a.State != "interrupted" || a.Verdict != "not_scored" || a.ErrorCode != "FOREGROUND_PREEMPTED" || !a.Result.InferenceCompleted || a.OutputArtifact == "" || a.Result.ScorerVersion != "" {
				t.Fatalf("late foreground discarded committed output or reached scoring: %+v", a)
			}
			if raw, err := store.ReadArtifact(context.Background(), a.OutputArtifact); err != nil || string(raw) != nativeInvalidSource {
				t.Fatal("committed source missing", err)
			}
			t.Log("late foreground stopped before scoring/next arm, retained complete formal output for explicit resume")
		})
	}
}

func TestFatalInferenceTraversesJoinedCleanupCauses(t *testing.T) {
	for _, code := range []string{"RELEASE_UNCONFIRMED", "STORAGE_UNAVAILABLE"} {
		err := errors.Join(identityError("INFERENCE_TIMEOUT", "original failure"), identityError(code, "cleanup failed"))
		if !fatalInference(fmt.Errorf("arm: %w", err)) {
			t.Errorf("joined cleanup %s masked by nonfatal inference", code)
		}
	}
	for _, err := range []error{
		providers.EvaluationContextError(context.DeadlineExceeded),
		&providers.EvaluationError{Code: "PROVIDER_UNAVAILABLE", Cause: errors.New("transport error")},
		&evalrunner.Error{Code: "RUNNER_TIMEOUT", Err: context.DeadlineExceeded},
	} {
		if fatalInference(err) {
			t.Errorf("semantic error classification replaced by transport cause: %v", err)
		}
	}
}
