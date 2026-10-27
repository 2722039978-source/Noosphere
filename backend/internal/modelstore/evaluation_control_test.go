package modelstore

import (
	"context"
	"errors"
	"testing"
)

func TestEvaluationControlRetryIsAtomicAndSelected(t *testing.T) {
	s, _ := openTest(t)
	ctx := context.Background()
	_, report := completeTest(t, s, "eval")
	ev, _ := s.GetEvaluation(ctx, "eval")
	req := EvaluationControl{WorkspaceID: ev.Manifest.WorkspaceID, EvaluationID: ev.ID, Command: "retry_case", ExpectedRevision: ev.Revision, CaseID: ev.Manifest.Dataset.Cases[0].ID, Arm: "candidate", Key: "retry"}
	next, replayed, err := s.ControlEvaluation(ctx, req, ev.ManifestHash)
	if err != nil || replayed || next.Status != "running" {
		t.Fatal(next, replayed, err)
	}
	all, err := s.ListAttempts(ctx, ev.ID)
	if err != nil {
		t.Fatal(err)
	}
	var selected []Attempt
	for _, a := range all {
		if a.AttemptNo == 2 {
			selected = append(selected, a)
		}
	}
	if len(selected) != 1 || selected[0].CaseID != req.CaseID || selected[0].Arm != "candidate" || selected[0].State != "queued" || selected[0].SupersedesReason != "explicit_retry_case" {
		t.Fatal(selected)
	}
	old, err := s.GetReport(ctx, report.ID)
	if err != nil || old.PromotionEligible {
		t.Fatal("retry did not invalidate old report", err)
	}
	retry, found, err := s.ControlEvaluation(ctx, req, "ignored-on-replay")
	if err != nil || !found || retry.Revision != next.Revision {
		t.Fatal(retry, found, err)
	}
	req.Arm = "stable"
	if _, _, err = s.ControlEvaluation(ctx, req, ev.ManifestHash); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatal(err)
	}
	req.Key = "stale"
	req.ExpectedRevision = ev.Revision
	if _, _, err = s.ControlEvaluation(ctx, req, ev.ManifestHash); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal(err)
	}
}

func TestEvaluationControlQueuedCancelPersistsPlannedCounts(t *testing.T) {
	s, _ := openTest(t)
	ctx := context.Background()
	e := createTestEvaluation(t, s, "queued")
	r := EvaluationControl{WorkspaceID: e.Manifest.WorkspaceID, EvaluationID: e.ID, Command: "cancel", ExpectedRevision: e.Revision, Key: "cancel"}
	next, _, err := s.ControlEvaluation(ctx, r, "")
	if err != nil || !next.CancelRequested || next.Status != "cancelled" {
		t.Fatal(next, err)
	}
	counts, err := s.AttemptProgress(ctx, e.ID)
	if err != nil || counts.Cancelled != counts.Planned || counts.Unknown != 0 {
		t.Fatal(counts, err)
	}
	if _, err = s.ClaimDispatchedEvaluation(ctx, e.ID, "late", e.LeaseGeneration); !errors.Is(err, ErrStaleLease) {
		t.Fatal("cancelled dispatch claimed", err)
	}
	again, replayed, err := s.ControlEvaluation(ctx, r, "")
	if err != nil || !replayed || again.Revision != next.Revision {
		t.Fatal(again, replayed, err)
	}
}

func TestEvaluationPreflightRefusalReceiptChecksSnapshotAndIntent(t *testing.T) {
	s, _ := openTest(t)
	ctx := context.Background()
	completeTest(t, s, "eval")
	ev, err := s.GetEvaluation(ctx, "eval")
	if err != nil {
		t.Fatal(err)
	}
	r := EvaluationControl{WorkspaceID: ev.Manifest.WorkspaceID, EvaluationID: ev.ID, Command: "retry_case", ExpectedRevision: ev.Revision, CaseID: ev.Manifest.Dataset.Cases[0].ID, Arm: "candidate", Key: "drift"}
	if _, err = s.RefuseEvaluationControl(ctx, r, "wrong-manifest", "MODEL_DRIFT"); !errors.Is(err, ErrInsufficientEvidence) {
		t.Fatal("wrong preflight hash accepted", err)
	}
	if _, found, err := s.EvaluationControlReceipt(ctx, r); err != nil || found {
		t.Fatal("hash mismatch created receipt", found, err)
	}
	if _, err = s.RefuseEvaluationControl(ctx, r, ev.ManifestHash, "PROVIDER_UNAVAILABLE"); !errors.Is(err, ErrInvalid) {
		t.Fatal("transient denial may be persisted", err)
	}
	denied, err := s.RefuseEvaluationControl(ctx, r, ev.ManifestHash, "MODEL_DRIFT")
	if !errors.Is(err, ErrModelDrift) || denied.Revision != ev.Revision {
		t.Fatal(denied, err)
	}
	newIntent := r
	newIntent.Key = "new"
	changed, _, err := s.ControlEvaluation(ctx, newIntent, ev.ManifestHash)
	if err != nil {
		t.Fatal(err)
	}
	replayed, found, err := s.EvaluationControlReceipt(ctx, r)
	if !found || !errors.Is(err, ErrModelDrift) || replayed.Revision != ev.Revision || replayed.Revision == changed.Revision {
		t.Fatal("refusal snapshot changed", replayed, found, err)
	}
	r.Arm = "stable"
	if _, err = s.RefuseEvaluationControl(ctx, r, ev.ManifestHash, "MODEL_DRIFT"); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatal("changed intent reused denial", err)
	}
}
