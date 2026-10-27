package modellab

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/noosphere/backend/internal/modelstore"
)

type CategoryComparison struct {
	Stable         Proportion `json:"stable"`
	Candidate      Proportion `json:"candidate"`
	PairMatrix     [4]int     `json:"pair_matrix"`
	PairedComplete int        `json:"paired_complete"`
	Loss           Proportion `json:"loss_rate"`
}

// CompletionProgress is descriptive: two model arms of one case are correlated.
type CompletionProgress struct {
	Scored  int      `json:"scored"`
	Planned int      `json:"planned"`
	Rate    *float64 `json:"rate"`
}
type Failure struct {
	CaseID    string            `json:"case_id"`
	Category  string            `json:"category"`
	Arm       string            `json:"arm"`
	AttemptID string            `json:"attempt_id"`
	State     string            `json:"state"`
	Verdict   string            `json:"verdict"`
	ErrorCode string            `json:"error_code,omitempty"`
	Result    modelstore.Result `json:"result"`
}

// Comparison keeps ability, performance, resources, failures and uncertainty
// alongside each other; it does not produce a universal capability score.
type Comparison struct {
	Version        string                               `json:"version"`
	Source         string                               `json:"source"`
	Status         string                               `json:"status"`
	ManifestHash   string                               `json:"manifest_hash"`
	AttemptSetHash string                               `json:"attempt_set_hash"`
	PairLabels     [4]string                            `json:"pair_matrix_labels"`
	Arms           map[string]Proportion                `json:"arms"`
	Completion     CompletionProgress                   `json:"completion"`
	Categories     map[string]CategoryComparison        `json:"categories"`
	Loss           Proportion                           `json:"loss_rate"`
	Failures       []Failure                            `json:"failures"`
	Performance    LatencyComparison                    `json:"performance"`
	Diagnostics    []AttemptDiagnostics                 `json:"diagnostics"`
	MetricCoverage map[string]map[string]MetricCoverage `json:"metric_coverage"`
	Environment    modelstore.EnvironmentSnapshot       `json:"environment"`
	Limitations    []string                             `json:"limitations"`
}

func (s *Service) BuildReport(ctx context.Context, evaluationID string) (modelstore.Report, error) {
	e, err := s.store.GetEvaluation(ctx, evaluationID)
	if err != nil {
		return modelstore.Report{}, err
	}
	return s.buildReportAt(ctx, e.ID, e.LeaseGeneration, e.Revision)
}

// buildReportAt keeps the executing worker's lease fence, even if another worker
// has explicitly resumed the evaluation before this transaction begins.
func (s *Service) buildReportAt(ctx context.Context, id string, generation, revision int64) (modelstore.Report, error) {
	return s.store.CompleteEvaluationWithAnalysis(ctx, id, generation, revision, func(e modelstore.Evaluation, r modelstore.Report, all []modelstore.Attempt, read func(string) ([]byte, error)) (json.RawMessage, error) {
		c, err := Compare(e, r, all, read)
		if err != nil {
			return nil, err
		}
		return json.Marshal(c)
	})
}

// ReadComparison never recalculates historical bodies lacking v1 analysis.
func ReadComparison(r modelstore.Report) (Comparison, error) {
	var c Comparison
	if len(r.Analysis) == 0 {
		return c, fmt.Errorf("%w: historical analysis unavailable", modelstore.ErrInsufficientEvidence)
	}
	if err := json.Unmarshal(r.Analysis, &c); err != nil {
		return c, err
	}
	if c.Version != "regression-intelligence-v1" {
		return c, fmt.Errorf("%w: unsupported report analysis version", modelstore.ErrInsufficientEvidence)
	}
	return c, nil
}

// Compare is bound to one frozen manifest and the store's exact selected set.
// A caller cannot silently replace a selected attempt with an older success.
func Compare(e modelstore.Evaluation, r modelstore.Report, all []modelstore.Attempt, read func(string) ([]byte, error)) (Comparison, error) {
	c := Comparison{Version: "regression-intelligence-v1", Source: e.Manifest.Source, Status: "complete", ManifestHash: r.ManifestHash, AttemptSetHash: r.AttemptSetHash, PairLabels: [4]string{"stable_pass_candidate_pass", "stable_pass_candidate_fail", "stable_fail_candidate_pass", "stable_fail_candidate_fail"}, Arms: map[string]Proportion{}, Categories: map[string]CategoryComparison{}, Failures: []Failure{}, Diagnostics: []AttemptDiagnostics{}, Environment: e.Manifest.Environment, Limitations: []string{"Wilson intervals describe selected independent benchmark cases only; replay and retries do not increase n.", "Small samples and correlated tasks do not establish general workflow ability.", "warm_verified observes internal loading protocol; it does not prove absence of external process interference.", "Host snapshots and Ollama residency estimates are not per-model peak GPU measurements; missing resources cannot establish resource superiority."}}
	if e.Manifest.Source == "offline_fixture" {
		c.Limitations = append(c.Limitations, "Synthetic offline_fixture evidence is not live production proof.")
	}
	if r.ManifestHash != e.ManifestHash || r.EvaluationID != e.ID {
		return c, fmt.Errorf("%w: comparison manifest mismatch", modelstore.ErrInvalid)
	}
	byID := map[string]modelstore.Attempt{}
	latest := map[string]modelstore.Attempt{}
	for _, a := range all {
		if a.EvaluationID != e.ID {
			return c, modelstore.ErrInvalid
		}
		byID[a.ID] = a
		k := a.CaseID + "/" + a.Arm
		if a.AttemptNo > latest[k].AttemptNo {
			latest[k] = a
		}
	}
	chosen := map[string]modelstore.Attempt{}
	for _, sel := range r.Selected {
		a, ok := byID[sel.ID]
		k := sel.CaseID + "/" + sel.Arm
		if !ok || latest[k].ID != sel.ID || a.CaseID != sel.CaseID || a.Arm != sel.Arm || a.AttemptNo != sel.AttemptNo || a.State != sel.State || a.Verdict != sel.Verdict {
			return c, fmt.Errorf("%w: selected attempt mismatch", modelstore.ErrInvalid)
		}
		chosen[k] = a
	}
	stable, candidate := modelstore.Counts{}, modelstore.Counts{}
	for _, cat := range r.Categories {
		stable.Scored += cat.Stable.Scored
		stable.Pass += cat.Stable.Pass
		candidate.Scored += cat.Candidate.Scored
		candidate.Pass += cat.Candidate.Pass
	}
	c.Arms["stable"] = Wilson95(stable.Pass, stable.Scored)
	c.Arms["candidate"] = Wilson95(candidate.Pass, candidate.Scored)
	c.Completion = CompletionProgress{Scored: r.Counts.Scored, Planned: r.Counts.Planned}
	if r.Counts.Planned > 0 {
		c.Completion.Rate = modelstore.Float(float64(r.Counts.Scored) / float64(r.Counts.Planned))
	}
	c.Loss = Wilson95(r.PairMatrix[1], r.Counts.PairedComplete)
	if r.Counts.Scored != r.Counts.Planned || len(r.IneligibilityReasons) > 0 {
		c.Status = "incomplete"
	}
	for category, counts := range r.Categories {
		c.Categories[category] = CategoryComparison{Stable: Wilson95(counts.Stable.Pass, counts.Stable.Scored), Candidate: Wilson95(counts.Candidate.Pass, counts.Candidate.Scored)}
	}
	for _, cs := range e.Manifest.Dataset.Cases {
		for _, arm := range []string{"stable", "candidate"} {
			a, ok := chosen[cs.ID+"/"+arm]
			if !ok {
				c.Failures = append(c.Failures, Failure{CaseID: cs.ID, Category: cs.Category, Arm: arm, State: "missing", Verdict: "not_scored"})
				continue
			}
			if a.State != "completed" || a.Verdict != "pass" {
				c.Failures = append(c.Failures, Failure{cs.ID, cs.Category, arm, a.ID, a.State, a.Verdict, a.ErrorCode, a.Result})
			}
			c.Diagnostics = append(c.Diagnostics, diagnose(a, read))
		}
		stable, sok := chosen[cs.ID+"/stable"]
		candidate, cok := chosen[cs.ID+"/candidate"]
		if sok && cok && stable.State == "completed" && candidate.State == "completed" {
			cat := c.Categories[cs.Category]
			index := 0
			if stable.Verdict != "pass" {
				index += 2
			}
			if candidate.Verdict != "pass" {
				index++
			}
			cat.PairMatrix[index]++
			cat.PairedComplete++
			c.Categories[cs.Category] = cat
		}
	}
	for category, cat := range c.Categories {
		cat.Loss = Wilson95(cat.PairMatrix[1], cat.PairedComplete)
		c.Categories[category] = cat
	}
	c.Performance = pairedLatency(e, chosen, read)
	// Superseded attempts still incurred warmup cost. Reused durable outputs
	// reference the same artifact and must not invent another provider call.
	warmups := map[string]bool{}
	for _, a := range all {
		if a.Metrics.WarmupArtifact != "" {
			warmups[a.Metrics.WarmupArtifact] = true
		}
	}
	c.Performance.WarmupCalls = len(warmups)
	c.MetricCoverage = metricCoverage(c.Diagnostics, len(e.Manifest.Dataset.Cases))
	return c, nil
}
