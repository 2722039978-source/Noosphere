package modelstore

import "context"

// ReportCaseAttempts reads the immutable selection, including superseded attempts.
// It never substitutes the latest retry or an attempt changed after the snapshot.
func (s *Store) ReportCaseAttempts(ctx context.Context, reportID, evaluationID, caseID string) ([]Attempt, error) {
	report, err := s.GetReport(ctx, reportID)
	if err != nil {
		return nil, err
	}
	if report.EvaluationID != evaluationID {
		return nil, ErrNotFound
	}
	out := []Attempt{}
	seen := map[string]bool{}
	for _, ref := range report.Selected {
		if ref.CaseID != caseID {
			continue
		}
		if seen[ref.Arm] {
			return nil, ErrInsufficientEvidence
		}
		seen[ref.Arm] = true
		a, err := s.GetAttempt(ctx, ref.ID)
		if err != nil {
			return nil, err
		}
		if a.EvaluationID != evaluationID {
			return nil, ErrInsufficientEvidence
		}
		if err = verifyReportAttempt(ref, a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func verifyReportAttempt(ref SelectedAttempt, a Attempt) error {
	if ref.ID != a.ID || ref.CaseID != a.CaseID || ref.Arm != a.Arm || ref.AttemptNo != a.AttemptNo || ref.State != a.State || ref.Verdict != a.Verdict || ref.AttemptHash == "" || ref.ResultHash == "" {
		return ErrInsufficientEvidence
	}
	_, attemptHash, err := immutableBody(a)
	if err != nil {
		return err
	}
	_, resultHash, err := immutableBody(a.Result)
	if err != nil {
		return err
	}
	if ref.AttemptHash != attemptHash || ref.ResultHash != resultHash {
		return ErrInsufficientEvidence
	}
	return nil
}
