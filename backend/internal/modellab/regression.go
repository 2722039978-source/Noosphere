package modellab

import (
	"fmt"

	"github.com/noosphere/backend/internal/modelstore"
)

type CaseRatio struct {
	CaseID           string            `json:"case_id"`
	StableAttempt    string            `json:"stable_attempt"`
	CandidateAttempt string            `json:"candidate_attempt"`
	Ratio            modelstore.Metric `json:"ratio"`
}
type LatencyComparison struct {
	Status        string            `json:"status"`
	RequiredPairs int               `json:"required_pairs"`
	SampleCount   int               `json:"sample_count"`
	PlannedPairs  int               `json:"planned_pairs"`
	MedianRatio   modelstore.Metric `json:"paired_median_ratio"`
	Cases         []CaseRatio       `json:"cases"`
	Reasons       []string          `json:"reasons"`
	WarmupCalls   int               `json:"warmup_calls"`
	CandidateOOM  bool              `json:"candidate_oom"`
}

func pairedLatency(e modelstore.Evaluation, selected map[string]modelstore.Attempt, read func(string) ([]byte, error)) LatencyComparison {
	p := LatencyComparison{Status: "insufficient", RequiredPairs: 12, PlannedPairs: len(e.Manifest.Dataset.Cases), MedianRatio: unknownMetric("ratio", "all 12 comparable pairs required"), Cases: []CaseRatio{}, Reasons: []string{}}
	if p.PlannedPairs != 12 {
		p.Reasons = append(p.Reasons, "simulation-demo-v1 requires exactly 12 independent cases")
	}
	if e.Manifest.Environment.ID == "" {
		p.Reasons = append(p.Reasons, "same-machine environment identity unavailable")
	}
	ratios := []float64{}
	for _, c := range e.Manifest.Dataset.Cases {
		s, sok := selected[c.ID+"/stable"]
		a, aok := selected[c.ID+"/candidate"]
		row := CaseRatio{CaseID: c.ID, StableAttempt: s.ID, CandidateAttempt: a.ID, Ratio: unknownMetric("ratio", "missing or incomparable formal inference")}
		if a.ErrorCode == "OUT_OF_MEMORY" {
			p.CandidateOOM = true
			p.Reasons = append(p.Reasons, c.ID+": candidate OOM")
		}
		why := ""
		for _, v := range []modelstore.Attempt{s, a} {
			switch {
			case !sok || !aok:
				why = "missing selected attempt"
			case !v.Result.InferenceCompleted || v.Result.Truncated || v.ErrorCode == "INFERENCE_TIMEOUT":
				why = "formal inference incomplete, truncated or timeout-censored"
			case !v.Metrics.WarmVerified || v.Metrics.LoadingProtocol != LoadingProtocol || v.Metrics.WarmupArtifact == "":
				why = "warm residency or loading protocol not verified"
			case !knownMetric(v.Metrics.WallNS, "ns", true) || v.Metrics.WallNS.Source != "host_monotonic" || v.Metrics.WallNS.Quality != "observed":
				why = "wall must be a positive known ns measurement"
			}
			// WarmVerified records the observed protocol; a new comparison also
			// needs its required evidence readable at this build-time snapshot.
			// Historical analysis is never passed here again by GetReport.
			if why == "" {
				if read == nil {
					why = "warmup evidence unavailable: verified artifact reader missing"
				} else if _, err := read(v.Metrics.WarmupArtifact); err != nil {
					why = fmt.Sprintf("warmup evidence unavailable for %s: %s", v.Arm, v.Metrics.WarmupArtifact)
				}
			}
			if why != "" {
				break
			}
		}
		if why == "" {
			ratio := *a.Metrics.WallNS.Value / *s.Metrics.WallNS.Value
			if finite(ratio) {
				row.Ratio = observedMetric(ratio, "ratio", "paired_formal_inference_wall")
				row.Ratio.Quality = "derived"
				ratios = append(ratios, ratio)
			} else {
				why = "non-finite ratio"
			}
		}
		if why != "" {
			row.Ratio.MissingReason = why
			p.Reasons = append(p.Reasons, fmt.Sprintf("%s: %s", c.ID, why))
		}
		p.Cases = append(p.Cases, row)
	}
	p.SampleCount = len(ratios)
	if len(p.Reasons) == 0 && len(ratios) == 12 {
		p.Status = "sufficient"
		p.MedianRatio = observedMetric(median(ratios), "ratio", "median_of_12_paired_case_ratios")
		p.MedianRatio.Quality = "derived"
	}
	return p
}
