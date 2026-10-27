package modelops

import (
	"fmt"
	"github.com/noosphere/backend/internal/modellab"
	"github.com/noosphere/backend/internal/modelstore"
	"time"
)

func DemoPolicy() modelstore.ReleasePolicy {
	rules := []modelstore.PolicyRule{}
	for _, v := range []struct{ id, domain string }{{"identity", "identity"}, {"complete_pairs", "statistics"}, {"quality_categories", "quality"}, {"paired_latency", "resources"}, {"isolation", "isolation"}} {
		rules = append(rules, modelstore.PolicyRule{ID: v.id, Domain: v.domain, Stage: "all", Operator: "eq", Required: modelstore.BoolValue(true), Unit: "boolean", MinimumSamples: 12})
	}
	for _, stage := range []string{"SHADOW", "CANARY_5", "CANARY_25", "CANARY_50"} {
		rules = append(rules, modelstore.PolicyRule{ID: "stage_" + stage, Domain: "simulation", Stage: stage, Operator: "eq", Required: modelstore.BoolValue(true), Unit: "boolean", MinimumSamples: 20, ObservationWindowNS: int64(60 * time.Second)})
	}
	return modelstore.ReleasePolicy{ID: PolicyID, Mode: "simulation", Version: PolicyID, RollbackRetentionNS: int64(7 * 24 * time.Hour), ReleaseRules: rules, RollbackRules: []modelstore.PolicyRule{{ID: "safety_alarm", Domain: "simulation", Stage: "all", Operator: "eq", Required: modelstore.BoolValue(true), Unit: "boolean"}}}
}

func identity(s modelstore.SimulationSnapshot) bool {
	m := s.Evaluation.Manifest
	_, _, err := modelstore.CanonicalManifest(m)
	if m.Stable.Digest != "" && m.Stable.Provider == m.Candidate.Provider && m.Stable.Digest == m.Candidate.Digest {
		return false
	}
	return err == nil && s.Release.Mode == "simulation" && m.WorkflowID == Workflow && s.Release.WorkflowID == Workflow && s.Release.FilterHash == TaskType && m.MemoryMode != "snapshot" && m.Stable.ID == s.Release.StableRevisionID && m.Candidate.ID == s.Release.CandidateRevisionID && m.Stable.ID != m.Candidate.ID && s.Policy.ID == PolicyID && s.Policy.Version == PolicyID && s.Report.ManifestHash == s.Evaluation.ManifestHash
}
func decision(s modelstore.SimulationSnapshot, stage string) modelstore.GateDecision {
	d := modelstore.GateDecision{Allowed: true, PolicyHash: s.Policy.Hash, EvidenceHash: s.Report.Hash, Rules: []modelstore.RuleResult{}, Reasons: []string{}}
	c, err := modellab.ReadComparison(s.Report)
	complete := err == nil && s.EvidenceError == nil && c.Status == "complete" && c.ManifestHash == s.Report.ManifestHash && c.AttemptSetHash == s.Report.AttemptSetHash && c.Source == s.Evaluation.Manifest.Source && s.Report.Counts.Planned == 24 && s.Report.Counts.Scored == 24 && s.Report.Counts.PairedComplete == 12 && len(s.Report.Selected) == 24 && len(s.Evaluation.Manifest.Dataset.Cases) == 12 && c.Arms["candidate"].N == 12 && c.Arms["stable"].N == 12 && len(c.Categories) == 3
	quality := complete && c.Arms["candidate"].Successes >= 9 && s.Report.PairMatrix[1] == 0
	for _, cat := range c.Categories {
		if cat.PairedComplete != 4 || cat.Candidate.N != 4 || cat.Stable.N != 4 {
			complete = false
		}
		if cat.Candidate.Successes < 3 {
			quality = false
		}
	}
	quality = quality && complete
	isolated := true
	for _, f := range c.Failures {
		if f.ErrorCode == "SANDBOX_VIOLATION" {
			isolated = false
		}
	}
	perf := err == nil && c.Performance.Status == "sufficient" && c.Performance.SampleCount == 12 && c.Performance.RequiredPairs == 12 && c.Performance.MedianRatio.Value != nil && *c.Performance.MedianRatio.Value <= 1.5 && !c.Performance.CandidateOOM
	values := map[string]bool{"identity": identity(s), "complete_pairs": complete, "quality_categories": quality, "paired_latency": perf, "isolation": isolated && err == nil}
	sim, simErr := ReadSimulation(s.Release)
	for _, rule := range s.Policy.ReleaseRules {
		if rule.Stage != "all" && rule.Stage != stage {
			continue
		}
		value, known := values[rule.ID]
		samples := int64(12)
		window := int64(0)
		reason := fmt.Sprintf("%s: exact simulation-demo-v1 requirement", rule.ID)
		if rule.Domain == "simulation" {
			known = true
			value = false
			samples = 0
			if simErr == nil && len(sim.Stages) > 0 {
				v := sim.Stages[len(sim.Stages)-1]
				current := []modelstore.SimulationAssignment{}
				for _, a := range s.Assignments {
					if a.CohortID == v.CohortID && a.RevokedAt == "" {
						current = append(current, a)
					}
				}
				samples = int64(len(current))
				window = (sim.ClockSeconds - v.WindowStartSeconds) * int64(time.Second)
				value = v.State == stage && v.Replayed && v.UniqueCases == 12 && len(current) == 20 && v.AdmissionEpoch > 0 && window >= int64(60*time.Second) && len(sim.Alerts) == 0 && v.CandidateUserOutputs == 0 && v.SideEffects == 0 && (stage == "SHADOW" || s.Release.AdmissionEnabled)
				n := 0
				seen := map[string]bool{}
				for _, a := range current {
					if a.AdmittedEpoch != v.AdmissionEpoch || a.Source != "simulation_replay" || a.OfflineSource != c.Source {
						value = false
					}
					if seen[a.SessionKey] {
						value = false
					}
					seen[a.SessionKey] = true
					if a.Arm == "candidate" {
						n++
					}
				}
				value = value && n == candidateCount(stage)
			}
		}
		status := "pass"
		observed := modelstore.BoolValue(value)
		if !known || err != nil || (rule.ID == "complete_pairs" && !complete) || (rule.ID == "paired_latency" && c.Performance.Status != "sufficient") || samples < rule.MinimumSamples || window < rule.ObservationWindowNS {
			status = "insufficient"
			observed = modelstore.UnknownValue()
		} else if !value {
			status = "fail"
		}
		if status != "pass" {
			d.Allowed = false
			d.Reasons = append(d.Reasons, reason)
		}
		d.Rules = append(d.Rules, modelstore.RuleResult{ID: rule.ID, Observed: observed, Required: rule.Required, Unit: rule.Unit, EvidenceIDs: []string{s.Report.ID}, Status: status, Reason: reason, SampleCount: samples, ObservationWindowNS: window})
	}
	if len(d.Rules) == 0 {
		d.Allowed = false
		d.Reasons = append(d.Reasons, "frozen policy unavailable")
	}
	return d
}
