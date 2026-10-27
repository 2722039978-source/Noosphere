package modelstore

import (
	"context"
	"errors"
	"testing"
)

func TestDispatchFencesQueuedCancelResumeAndLateCheckpoint(t *testing.T) {
	ctx := context.Background()
	s, _ := openTest(t)
	e := createTestEvaluation(t, s, "dispatch")
	first, err := s.ClaimDispatchedEvaluation(ctx, e.ID, "run1", e.LeaseGeneration)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ClaimDispatchedEvaluation(ctx, e.ID, "duplicate", e.LeaseGeneration); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("duplicate claim: %v", err)
	}
	a, err := s.AppendAttempt(ctx, Attempt{ID: "attempt", EvaluationID: e.ID, CaseID: "case1", Arm: "stable", AttemptNo: 1, LeaseGeneration: first.LeaseGeneration, State: "running", Verdict: "not_scored"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.InterruptEvaluation(ctx, e.ID, "run1", first.LeaseGeneration, "APP_RESTARTED"); err != nil {
		t.Fatal(err)
	}
	e, err = s.GetEvaluation(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := s.ResumeEvaluation(ctx, e.ID, e.Revision, e.ManifestHash)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.CancelDispatchedEvaluation(ctx, e.ID, "ws", "run1", first.LeaseGeneration-1); err != nil {
		t.Fatal(err)
	}
	current, _ := s.GetEvaluation(ctx, e.ID)
	if current.CancelRequested || current.LeaseGeneration != resumed.LeaseGeneration {
		t.Fatal("old run cancelled new unbound lease")
	}
	bound, err := s.BindResumedRun(ctx, e.ID, "run2", resumed.LeaseGeneration)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CheckpointAttempt(ctx, a.ID, first.LeaseGeneration, AttemptOutcome{}); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("late checkpoint: %v", err)
	}
	if err = s.CancelDispatchedEvaluation(ctx, e.ID, "ws", "run2", bound.LeaseGeneration); err != nil {
		t.Fatal(err)
	}
	current, _ = s.GetEvaluation(ctx, e.ID)
	if current.Status != "cancelled" || !current.CancelRequested {
		t.Fatal("cancel intent missing")
	}
	queued := createTestEvaluation(t, s, "queued")
	if err = s.CancelDispatchedEvaluation(ctx, queued.ID, "ws", "queuedrun", queued.LeaseGeneration); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ClaimDispatchedEvaluation(ctx, queued.ID, "queuedrun", queued.LeaseGeneration); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("cancelled queued claimed: %v", err)
	}
}
