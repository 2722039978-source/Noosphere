package modellab

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Only exact host rules are supported. The stable strict JSON parser dependency
// is included in this scorer's combined source hash without changing old bytes.
func scoreReviewer(ctx context.Context, ref CaseRef, output string) (result TextScoreResult, err error) {
	started := time.Now()
	defer func() { result.TotalDuration = time.Since(started) }()
	d, e, err := authoritative(ref)
	if err != nil {
		return result, err
	}
	if !d.isReviewer() || e.Reviewer == nil {
		return result, fmt.Errorf("trusted reviewer case required")
	}
	result = TextScoreResult{TaskType: "local_reviewer", CaseID: e.ID, CaseVersion: e.Version, CaseHash: e.CaseHash, FixtureHash: e.FixtureHash, DatasetID: d.revision.BenchmarkID, DatasetVersion: d.revision.Version, DatasetHash: d.revision.ContentHash, ScorerVersion: d.revision.ScorerVersion, ScorerHash: d.scorerHash, RunnerVersion: d.revision.RunnerVersion, RawOutputHash: digest([]byte(output)), TotalAssertions: 2, Status: "failed", Rules: []TextRuleResult{}}
	if err = ctx.Err(); err != nil {
		result.Status, result.ErrorStage, result.ErrorCode, result.Reason = "cancelled", "scoring", "USER_CANCELLED", "reviewer scoring cancelled"
		return result, err
	}
	formatErr := validateTextOutput(output)
	correct := false
	if formatErr == nil && e.Reviewer.Method == "exact_json" {
		var actual, expected any
		actual, formatErr = parseTextJSON([]byte(output))
		if formatErr == nil {
			expected, err = parseTextJSON(e.ExpectedOutput)
			if err != nil {
				return result, err
			}
			correct = equalTextJSON(actual, expected)
		}
	} else if formatErr == nil {
		var expected string
		if err = json.Unmarshal(e.ExpectedOutput, &expected); err != nil {
			return result, err
		}
		correct = normalizeTextAnswer(output) == normalizeTextAnswer(expected)
	}
	for i, rule := range e.Rules {
		v := TextRuleResult{TextRule: rule, Passed: formatErr == nil && (i == 0 || correct)}
		if v.Passed {
			result.PassedAssertions++
		} else if formatErr != nil {
			v.Reason = formatErr.Error()
		} else {
			v.Reason = "答案不符合固定资料、对话状态或明确输出约束"
		}
		result.Rules = append(result.Rules, v)
	}
	result.Passed = result.PassedAssertions == 2
	if result.Passed {
		result.Status = "passed"
	} else if formatErr != nil {
		result.ErrorStage, result.ErrorCode, result.Reason = "output", "INVALID_OUTPUT", formatErr.Error()
	} else {
		result.ErrorStage, result.ErrorCode, result.Reason = "assertion", "WRONG_ANSWER", "答案不符合固定资料、对话状态或明确输出约束"
	}
	return result, nil
}
