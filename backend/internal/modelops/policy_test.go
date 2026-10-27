package modelops

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/noosphere/backend/internal/modellab"
	"github.com/noosphere/backend/internal/modelstore"
	"testing"
)

func TestDemoPolicyRequiresCompletePairsAndCategories(t *testing.T) {
	svc, st, report, _ := fixture(t, false)
	r := create(t, svc, report)
	ev, _ := st.GetEvaluation(context.Background(), report.EvaluationID)
	p, _ := st.GetPolicy(context.Background(), PolicyID)
	snap := modelstore.SimulationSnapshot{Release: r, Report: report, Evaluation: ev, Policy: p}
	if !decision(snap, "OFFLINE_EVAL").Allowed {
		t.Fatal("valid exact T6 comparison blocked")
	}
	for _, name := range []string{"missing_analysis", "missing_pair", "category_denominator", "category_quality", "loss", "latency", "oom", "identity"} {
		t.Run(name, func(t *testing.T) {
			v := snap
			c, _ := modellab.ReadComparison(report)
			switch name {
			case "missing_analysis":
				v.Report.Analysis = nil
			case "missing_pair":
				v.Report.Counts.PairedComplete = 11
			case "category_denominator":
				x := c.Categories["logic"]
				x.Candidate.N = 3
				c.Categories["logic"] = x
			case "category_quality":
				x := c.Categories["logic"]
				x.Candidate.Successes = 2
				c.Categories["logic"] = x
			case "loss":
				v.Report.PairMatrix[1] = 1
			case "latency":
				c.Performance.MedianRatio.Value = modelstore.Float(1.5000001)
			case "oom":
				c.Performance.CandidateOOM = true
			case "identity":
				v.Evaluation.Manifest.Stable.ID = "different"
			}
			if name != "missing_analysis" {
				v.Report.Analysis, _ = json.Marshal(c)
			}
			if decision(v, "OFFLINE_EVAL").Allowed {
				t.Fatal("mandatory gate bypass", name)
			}
		})
	}
}
func TestSupersededSuccessReportCannotPromoteAfterLatestFailedAttempt(t *testing.T) {
	s, st, report, _ := fixture(t, false)
	ctx := context.Background()
	ev, _ := st.GetEvaluation(ctx, report.EvaluationID)
	ev, e := st.ResumeEvaluation(ctx, ev.ID, ev.Revision, ev.ManifestHash)
	if e != nil {
		t.Fatal(e)
	}
	_, e = st.AppendAttempt(ctx, modelstore.Attempt{ID: "latest-failed", EvaluationID: ev.ID, CaseID: "case00", Arm: "candidate", AttemptNo: 2, LeaseGeneration: ev.LeaseGeneration, State: "failed", Verdict: "infra_error", ErrorCode: "PROVIDER_UNAVAILABLE", InputArtifact: ev.Manifest.Dataset.Cases[0].InputArtifact})
	if e != nil {
		t.Fatal(e)
	}
	old, e := st.GetReport(ctx, report.ID)
	if e != nil || old.PromotionEligible || old.Hash != report.Hash {
		t.Fatal("historical/currentness", e)
	}
	if _, e = s.Create(ctx, "ws", CreateSpec{Mode: "simulation", WorkflowID: Workflow, TaskTypeFilter: TaskType, ReportID: report.ID}, "superseded"); !errors.Is(e, modelstore.ErrInsufficientEvidence) {
		t.Fatal("old success report accepted", e)
	}
}
func TestActiveReleaseFreezesEvaluationAndCloneKeepsHistory(t *testing.T) {
	s, st, report, _ := fixture(t, false)
	ctx := context.Background()
	r := create(t, s, report)
	ev, _ := st.GetEvaluation(ctx, report.EvaluationID)
	if _, e := st.ResumeEvaluation(ctx, ev.ID, ev.Revision, ev.ManifestHash); !errors.Is(e, modelstore.ErrEvaluationFrozen) {
		t.Fatal("active release unfrozen", e)
	}
	clone, e := s.CloneEvaluation(ctx, "ws", ev.ID, "clone")
	if e != nil || clone.ID == ev.ID || clone.Manifest.ParentEvaluationID != ev.ID || clone.Status != "queued" {
		t.Fatalf("clone %+v %v", clone, e)
	}
	attempts, e := st.ListAttempts(ctx, clone.ID)
	if e != nil || len(attempts) != 0 {
		t.Fatal("cloned attempts", e)
	}
	original, _ := st.ListAttempts(ctx, ev.ID)
	if len(original) != 24 {
		t.Fatal("lost history")
	}
	retry, e := s.CloneEvaluation(ctx, "ws", ev.ID, "clone")
	if e != nil || retry.ID != clone.ID {
		t.Fatal("clone receipt", e)
	}
	_ = apply(t, s, r, Reject)
}
func TestQualityRefusalAndRejectChain(t *testing.T) {
	s, st, report, _ := fixture(t, true)
	r := create(t, s, report)
	r = apply(t, s, r, StartOffline)
	ctx := context.Background()
	if _, e := s.Apply(ctx, "ws", r.ID, Advance, r.Revision, "bad-quality"); !errors.Is(e, modelstore.ErrInsufficientEvidence) {
		t.Fatal(e)
	}
	r = apply(t, s, r, Reject)
	if r.State != "REJECTED" || r.AdmissionEnabled {
		t.Fatal("reject failed")
	}
	events, e := st.ListReleaseEvents(ctx, r.ID)
	if e != nil || len(events) != 3 || events[1].ReasonCode != "INSUFFICIENT_EVIDENCE" {
		t.Fatal("refusal audit", events, e)
	}
}
