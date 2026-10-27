package modellab

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/noosphere/backend/internal/evalrunner"
)

func TestScoreStageFailureClassification(t *testing.T) {
	live := context.Background()
	cancelled, cancel := context.WithCancel(live)
	cancel()
	privateTimeout := &evalrunner.Error{Code: "RUNNER_TIMEOUT", Err: context.DeadlineExceeded}
	for _, tc := range []struct {
		name, stage, status, errorStage string
		ctx                             context.Context
		err                             error
		propagate                       bool
	}{
		{"private-compiler-deadline", "compile", "infrastructure", "compile", live, privateTimeout, true},
		{"guest-execution-deadline", "execute", "failed", "execute", live, privateTimeout, false},
		{"guest-memory-budget", "execute", "failed", "execute", live, &evalrunner.Error{Code: "OUT_OF_MEMORY", Err: errors.New("guest memory exhausted")}, false},
		{"caller-cancel-during-compile", "compile", "cancelled", "compile", cancelled, privateTimeout, true},
		{"caller-cancel-during-execute", "execute", "cancelled", "execute", cancelled, privateTimeout, true},
		{"compiler-error", "compile", "failed", "compile", live, &evalrunner.Error{Code: "COMPILE_ERROR", Err: errors.New("undefined name")}, false},
		{"invalid-source", "compile", "failed", "source", live, &evalrunner.Error{Code: "INVALID_OUTPUT", Err: errors.New("wrong signature")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := ScoreResult{Status: "failed", TotalAssertions: 5}
			err := result.stageFailure(tc.ctx, tc.stage, tc.err)
			if result.Status != tc.status || result.ErrorStage != tc.errorStage || result.ErrorCode != runnerErrorCode(tc.err) || result.Reason != tc.err.Error() || result.Passed || result.PassedAssertions != 0 {
				t.Fatalf("unexpected score failure contract: %+v", result)
			}
			if tc.propagate && !errors.Is(err, tc.err) || !tc.propagate && err != nil {
				t.Fatalf("propagate=%v, returned=%v", tc.propagate, err)
			}
			if tc.name == "private-compiler-deadline" && tc.ctx.Err() != nil {
				t.Fatal("test must keep caller context live")
			}
		})
	}
}

func TestAllReferencesPassAndAllFaultsFailInWASI(t *testing.T) {
	binary := filepath.Join(runtime.GOROOT(), "bin", "go")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	runner, err := evalrunner.New(evalrunner.Config{GoBinary: binary, TempRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	scorer, err := NewScorer(runner)
	if err != nil {
		t.Fatal(err)
	}
	dataset, err := LoadDataset(DatasetID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range dataset.Cases() {
		t.Run(ref.ID(), func(t *testing.T) {
			e, err := dataset.Evidence(ref)
			if err != nil {
				t.Fatal(err)
			}
			good, err := scorer.Score(context.Background(), ref, e.Reference)
			if err != nil {
				t.Fatal(err)
			}
			if !good.Passed || good.Status != "passed" || !good.StaticValid || !good.CompileSucceeded || good.PassedAssertions != len(e.Assertions) || len(good.Assertions) != len(e.Assertions) {
				t.Fatalf("reference did not pass: %+v", good)
			}
			if good.ScorerHash == "" || good.DatasetHash == "" || good.RawSourceHash != digest([]byte(e.Reference)) || good.TotalDuration <= 0 {
				t.Fatal("missing provenance")
			}
			for _, v := range good.Assertions {
				if v.ExecutionDuration <= 0 || v.PreparationDuration <= 0 || len(v.Actual) == 0 {
					t.Fatalf("missing runner evidence: %+v", v)
				}
			}
			// Tampering with the public evidence copy must not alter host scoring.
			e.Assertions[0].Expected = []byte(`"FORGED_HOST_EXPECTED_7d22"`)
			e.Function = "main"
			e.OutputType = "string"
			bad, err := scorer.Score(context.Background(), ref, e.Faulty)
			if err != nil {
				t.Fatal(err)
			}
			if bad.Passed || bad.Status != "failed" || !bad.CompileSucceeded || bad.PassedAssertions >= bad.TotalAssertions {
				t.Fatalf("fault must compile but fail semantics: %+v", bad)
			}
			if bad.PassedAssertions == 0 {
				t.Fatal("fixtures must exercise partial diagnostics as well as failure")
			}
			if string(bad.Assertions[0].Expected) == string(e.Assertions[0].Expected) {
				t.Fatal("caller replaced host expected")
			}
			t.Logf("trusted-reference passed %d/%d; original-fault compiled and passed %d/%d (failed case)", good.PassedAssertions, good.TotalAssertions, bad.PassedAssertions, bad.TotalAssertions)
		})
	}
	ref := dataset.Cases()[0]
	e, _ := dataset.Evidence(ref)
	t.Run("adversarial-output", func(t *testing.T) {
		for _, tc := range []struct{ name, source, code string }{
			{"markdown", "```go\n" + e.Reference + "```", "INVALID_OUTPUT"},
			{"json-claim", `{"passed":true}`, "INVALID_OUTPUT"},
			{"invalid-json-text", `{ definitely broken`, "INVALID_OUTPUT"},
			{"forged-PASS", "PASS", "INVALID_OUTPUT"},
			{"patch", "--- solution.go\n+++ solution.go\n@@ -1 +1 @@\n+PASS", "INVALID_OUTPUT"},
			{"harness-overwrite", e.Reference + "\nfunc main() {}\n", "INVALID_OUTPUT"},
			{"signature", strings.Replace(e.Reference, "input string", "input int", 1), "INVALID_OUTPUT"},
			{"extra-stdout", `package main; import "fmt"; func TrimSpaces(input string) string {fmt.Println("PASS");return input}`, "INVALID_OUTPUT"},
			{"compile-error", `package main; func TrimSpaces(input string) string {return unknownIdentifier}`, "COMPILE_ERROR"},
			{"bounded-source", strings.Repeat("x", evalrunner.MaxSourceBytes+1), "INVALID_OUTPUT"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				r, err := scorer.Score(context.Background(), ref, tc.source)
				if err != nil || r.Passed || r.CompileSucceeded || r.ErrorCode != tc.code || r.Status != "failed" {
					t.Fatalf("%+v %v", r, err)
				}
			})
		}
		r, err := scorer.Score(context.Background(), ref, `package main; func TrimSpaces(input string) string {return "PASS"}`)
		if err != nil || r.Passed || !r.CompileSucceeded || r.PassedAssertions != 0 {
			t.Fatalf("model self-claim: %+v %v", r, err)
		}
	})
	t.Run("caller-cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r, err := scorer.Score(ctx, ref, e.Reference)
		if err == nil || r.Status != "cancelled" || r.Passed {
			t.Fatalf("%+v %v", r, err)
		}
	})
	t.Run("forged-case", func(t *testing.T) {
		if _, err := scorer.Score(context.Background(), CaseRef{}, e.Reference); err == nil {
			t.Fatal("forged case accepted")
		}
	})
	t.Run("closed-runner-infrastructure", func(t *testing.T) {
		if err := runner.Close(); err != nil {
			t.Fatal(err)
		}
		r, err := scorer.Score(context.Background(), ref, e.Reference)
		if err == nil || r.Status != "infrastructure" || r.Passed {
			t.Fatalf("%+v %v", r, err)
		}
	})
	if _, err = os.Stat(binary); err != nil {
		t.Fatal(err)
	}
}
