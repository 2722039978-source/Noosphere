package modelstore

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestReportAnalysisAtomicAndLegacyHistory(t *testing.T) {
	s, p := openTest(t)
	ctx := context.Background()
	e := createTestEvaluation(t, s, "eval")
	e, err := s.ClaimEvaluation(ctx, e.ID, "run", e.Revision)
	if err != nil {
		t.Fatal(err)
	}
	appendResult(t, s, e.ID, e.LeaseGeneration, "stable", 1, "pass")
	appendResult(t, s, e.ID, e.LeaseGeneration, "candidate", 1, "compile_fail")
	e, _ = s.GetEvaluation(ctx, e.ID)
	marker := errors.New("analyzer failure")
	_, err = s.CompleteEvaluationWithAnalysis(ctx, e.ID, e.LeaseGeneration, e.Revision, func(Evaluation, Report, []Attempt, func(string) ([]byte, error)) (json.RawMessage, error) {
		return nil, marker
	})
	if !errors.Is(err, marker) {
		t.Fatalf("analyzer error: %v", err)
	}
	var n int
	if err = s.db.QueryRow("SELECT count(*) FROM reports").Scan(&n); err != nil || n != 0 {
		t.Fatalf("partial report: %d %v", n, err)
	}
	after, _ := s.GetEvaluation(ctx, e.ID)
	if after.Revision != e.Revision || after.Status != e.Status {
		t.Fatal("partial completion on analyzer error")
	}
	legacy, err := s.CompleteEvaluation(ctx, e.ID, e.LeaseGeneration, e.Revision)
	if err != nil {
		t.Fatal(err)
	}
	var before string
	if err = s.db.QueryRow("SELECT body_json FROM reports WHERE id=?", legacy.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if len(legacy.Analysis) != 0 {
		t.Fatal("legacy analyzer unexpectedly ran")
	}
	s.Close()
	s, err = Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.GetReport(ctx, legacy.ID)
	if err != nil || got.Hash != legacy.Hash || len(got.Analysis) != 0 {
		t.Fatalf("legacy read changed: %+v %v", got, err)
	}
	var again string
	if err = s.db.QueryRow("SELECT body_json FROM reports WHERE id=?", legacy.ID).Scan(&again); err != nil || again != before {
		t.Fatal("historic bytes rewritten")
	}
}
