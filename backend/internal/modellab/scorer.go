package modellab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/noosphere/backend/internal/evalrunner"
)

// Scorer accepts only a concrete T1 runner; no model-provided score or harness
// can substitute for host assertions. Every assertion receives a fresh instance.
type Scorer struct{ runner *evalrunner.Runner }

func NewScorer(runner *evalrunner.Runner) (*Scorer, error) {
	if runner == nil {
		return nil, fmt.Errorf("concrete WASI runner required")
	}
	return &Scorer{runner}, nil
}

type AssertionResult struct {
	Input               json.RawMessage `json:"input"`
	Expected            json.RawMessage `json:"expected"`
	Actual              json.RawMessage `json:"actual,omitempty"`
	Passed              bool            `json:"passed"`
	ErrorStage          string          `json:"error_stage,omitempty"`
	ErrorCode           string          `json:"error_code,omitempty"`
	Reason              string          `json:"reason,omitempty"`
	TotalDuration       time.Duration   `json:"total_duration_ns"`
	PreparationDuration time.Duration   `json:"preparation_duration_ns"`
	ExecutionDuration   time.Duration   `json:"execution_duration_ns"`
}
type ScoreResult struct {
	CaseID           string            `json:"case_id"`
	CaseVersion      string            `json:"case_version"`
	CaseHash         string            `json:"case_hash"`
	FixtureHash      string            `json:"fixture_hash"`
	DatasetID        string            `json:"dataset_id"`
	DatasetVersion   string            `json:"dataset_version"`
	DatasetHash      string            `json:"dataset_hash"`
	ScorerVersion    string            `json:"scorer_version"`
	ScorerHash       string            `json:"scorer_hash"`
	RunnerVersion    string            `json:"runner_version"`
	RawSourceHash    string            `json:"raw_source_hash"`
	Passed           bool              `json:"passed"`
	StaticValid      bool              `json:"static_valid"`
	CompileSucceeded bool              `json:"compile_succeeded"`
	PassedAssertions int               `json:"passed_assertions"`
	TotalAssertions  int               `json:"total_assertions"`
	Assertions       []AssertionResult `json:"assertions"`
	Status           string            `json:"status"` // passed/failed/cancelled/infrastructure
	ErrorStage       string            `json:"error_stage,omitempty"`
	ErrorCode        string            `json:"error_code,omitempty"`
	Reason           string            `json:"reason,omitempty"`
	CompileDuration  time.Duration     `json:"compile_duration_ns"`
	TotalDuration    time.Duration     `json:"total_duration_ns"`
}

func (s *Scorer) Score(ctx context.Context, ref CaseRef, generatedSource string) (result ScoreResult, err error) {
	start := time.Now()
	defer func() { result.TotalDuration = time.Since(start) }()
	d, e, err := authoritative(ref)
	if err != nil {
		return result, err
	}
	result = ScoreResult{CaseID: e.ID, CaseVersion: e.Version, CaseHash: e.CaseHash, FixtureHash: e.FixtureHash, DatasetID: d.revision.BenchmarkID, DatasetVersion: d.revision.Version, DatasetHash: d.revision.ContentHash, ScorerVersion: ScorerVersion, ScorerHash: d.scorerHash, RunnerVersion: d.revision.RunnerVersion, RawSourceHash: digest([]byte(generatedSource)), TotalAssertions: len(e.Assertions), Status: "failed"}
	compileStart := time.Now()
	module, compileErr := s.runner.Compile(ctx, evalrunner.CompileSpec{Source: generatedSource, FunctionName: e.Function, InputType: e.InputType, OutputType: e.OutputType})
	result.CompileDuration = time.Since(compileStart)
	if compileErr != nil {
		code := runnerErrorCode(compileErr)
		result.StaticValid = ctx.Err() == nil && (code == "COMPILE_ERROR" || code == "RUNNER_TIMEOUT")
		failureErr := result.stageFailure(ctx, "compile", compileErr)
		return result, failureErr
	}
	defer module.Close()
	result.StaticValid = true
	result.CompileSucceeded = true
	for _, a := range e.Assertions {
		verdict := AssertionResult{Input: append([]byte(nil), a.Input...), Expected: append([]byte(nil), a.Expected...)}
		execution, runErr := s.runner.RunCase(ctx, module, a.Input)
		verdict.TotalDuration = execution.TotalDuration
		verdict.PreparationDuration = execution.PreparationDuration
		verdict.ExecutionDuration = execution.ExecutionDuration
		verdict.Actual = append([]byte(nil), execution.Output...)
		if runErr != nil {
			verdict.ErrorStage = "execute"
			verdict.ErrorCode = runnerErrorCode(runErr)
			verdict.Reason = runErr.Error()
		} else {
			actual, parseErr := typedJSON(execution.Output, e.OutputType)
			expected, expectedErr := typedJSON(a.Expected, e.OutputType)
			if expectedErr != nil {
				return result, fmt.Errorf("trusted expected invalid: %w", expectedErr)
			}
			if parseErr != nil {
				verdict.ErrorStage = "output"
				verdict.ErrorCode = "INVALID_OUTPUT"
				verdict.Reason = parseErr.Error()
			} else {
				verdict.Passed = bytes.Equal(actual, expected)
				if !verdict.Passed {
					verdict.ErrorStage = "assertion"
					verdict.ErrorCode = "WRONG_ANSWER"
					verdict.Reason = "typed output differs from trusted expected"
				}
			}
		}
		result.Assertions = append(result.Assertions, verdict)
		if verdict.Passed {
			result.PassedAssertions++
		}
		if !verdict.Passed && result.ErrorCode == "" {
			result.ErrorStage = verdict.ErrorStage
			result.ErrorCode = verdict.ErrorCode
			result.Reason = verdict.Reason
		}
		if runErr != nil {
			if err := result.stageFailure(ctx, "execute", runErr); err != nil {
				return result, err
			}
		}
	}
	result.Passed = result.PassedAssertions == result.TotalAssertions
	if result.Passed {
		result.Status = "passed"
	}
	return result, nil
}
func runnerErrorCode(err error) string {
	var e *evalrunner.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return "RUNNER_INFRASTRUCTURE"
}
func operational(ctx context.Context, err error, code string) bool {
	return ctx.Err() != nil || errors.Is(err, context.Canceled) || code == "RUNNER_INFRASTRUCTURE" || code == "ARTIFACT_MISSING"
}

// stageFailure records the host result/error contract for a runner failure.
// Functional failures keep a nil error; operational failures are not scored.
func (r *ScoreResult) stageFailure(ctx context.Context, stage string, err error) error {
	code := runnerErrorCode(err)
	r.Status = "failed"
	// The compiler has its own private deadline. Unlike a guest execution
	// timeout, exhausting that host budget is infrastructure failure even when
	// the caller context is still live.
	terminal := operational(ctx, err, code) || stage == "compile" && code == "RUNNER_TIMEOUT"
	if r.ErrorCode == "" || terminal {
		r.ErrorStage, r.ErrorCode, r.Reason = stage, code, err.Error()
		if stage == "compile" && code == "INVALID_OUTPUT" {
			r.ErrorStage = "source"
		}
	}
	if terminal {
		r.Status = operationalStatus(ctx, err)
		return err
	}
	return nil
}
func operationalStatus(ctx context.Context, err error) string {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	return "infrastructure"
}
