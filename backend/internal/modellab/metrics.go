package modellab

import (
	"encoding/json"
	"math"

	"github.com/noosphere/backend/internal/modelstore"
)

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func knownMetric(m modelstore.Metric, unit string, positive bool) bool {
	return m.Value != nil && finite(*m.Value) && m.Unit == unit && m.Source != "" && m.Source != "unavailable" && (m.Quality == "observed" || m.Quality == "reported" || m.Quality == "host_snapshot" || m.Quality == "user_declared") && *m.Value >= 0 && (!positive || *m.Value > 0)
}
func normalizedMetric(m modelstore.Metric, unit string) modelstore.Metric {
	if knownMetric(m, unit, false) {
		return m
	}
	why := m.MissingReason
	if why == "" {
		why = "metric unavailable or invalid provenance/unit"
	}
	return unknownMetric(unit, why)
}

type AttemptDiagnostics struct {
	CaseID              string            `json:"case_id"`
	Arm                 string            `json:"arm"`
	AttemptID           string            `json:"attempt_id"`
	LoadingProtocol     string            `json:"loading_protocol"`
	WarmVerified        bool              `json:"warm_verified"`
	WallNS              modelstore.Metric `json:"formal_wall_ns"`
	EngineTotalNS       modelstore.Metric `json:"formal_engine_total_ns"`
	LoadNS              modelstore.Metric `json:"formal_load_ns"`
	PromptNS            modelstore.Metric `json:"prompt_ns"`
	DecodeNS            modelstore.Metric `json:"decode_ns"`
	Tokens              modelstore.Metric `json:"tokens"`
	TokensPerSecond     modelstore.Metric `json:"tokens_per_second"`
	WarmupCost          modelstore.Metric `json:"warmup_cost_ns"`
	WarmupEngineTotalNS modelstore.Metric `json:"warmup_engine_total_ns"`
	WarmupLoadNS        modelstore.Metric `json:"warmup_load_ns"`
	WarmupArtifact      string            `json:"warmup_artifact,omitempty"`
	CompilerNS          modelstore.Metric `json:"compiler_ns"`
	PreparationNS       modelstore.Metric `json:"wasi_preparation_ns"`
	ExecutionNS         modelstore.Metric `json:"wasi_execution_ns"`
	ScorerNS            modelstore.Metric `json:"scorer_total_ns"`
	ScoreArtifact       string            `json:"score_artifact,omitempty"`
	CPU                 modelstore.Metric `json:"cpu_percent"`
	RAM                 modelstore.Metric `json:"ram_bytes"`
	AvailableVRAM       modelstore.Metric `json:"available_vram_bytes"`
}

func diagnose(a modelstore.Attempt, read func(string) ([]byte, error)) AttemptDiagnostics {
	m := a.Metrics
	d := AttemptDiagnostics{CaseID: a.CaseID, Arm: a.Arm, AttemptID: a.ID, LoadingProtocol: m.LoadingProtocol, WarmVerified: m.WarmVerified, WallNS: normalizedMetric(m.WallNS, "ns"), LoadNS: normalizedMetric(m.LoadNS, "ns"), PromptNS: normalizedMetric(m.PromptNS, "ns"), DecodeNS: normalizedMetric(m.DecodeNS, "ns"), Tokens: normalizedMetric(m.Tokens, "tokens"), WarmupCost: normalizedMetric(m.WarmupCost, "ns"), WarmupArtifact: m.WarmupArtifact, CPU: normalizedMetric(m.CPU, "percent"), RAM: normalizedMetric(m.RAM, "bytes"), AvailableVRAM: normalizedMetric(m.AvailableVRAM, "bytes"), TokensPerSecond: unknownMetric("tokens/s", "complete engine token count and positive decode duration required"), CompilerNS: unknownMetric("ns", "scorer evidence unavailable"), PreparationNS: unknownMetric("ns", "WASI preparation evidence unavailable"), ExecutionNS: unknownMetric("ns", "WASI execution evidence unavailable"), ScorerNS: unknownMetric("ns", "scorer evidence unavailable")}
	d.EngineTotalNS = unknownMetric("ns", "formal engine total duration evidence unavailable")
	d.WarmupEngineTotalNS = unknownMetric("ns", "warmup engine total duration evidence unavailable")
	d.WarmupLoadNS = unknownMetric("ns", "warmup engine load duration evidence unavailable")
	if a.Result.InferenceCompleted && !a.Result.Truncated && knownMetric(m.Tokens, "tokens", false) && knownMetric(m.DecodeNS, "ns", true) && m.Tokens.Source == "ollama_native" && m.DecodeNS.Source == "ollama_native" {
		v := *m.Tokens.Value / (*m.DecodeNS.Value / 1e9)
		if finite(v) {
			d.TokensPerSecond = modelstore.Metric{Value: modelstore.Float(v), Unit: "tokens/s", Source: "ollama_native_eval_count/eval_duration_ns", Quality: "derived"}
		}
	}
	if read == nil {
		return d
	}
	if m.WarmupArtifact != "" {
		if raw, err := read(m.WarmupArtifact); err == nil {
			var phase phaseEvidence
			if json.Unmarshal(raw, &phase) == nil && phase.Kind == "warmup" && phase.Response != nil && phase.ErrorCode == "" {
				d.WarmupEngineTotalNS = normalizedMetric(reportedMetric(phase.Response.TotalDurationNS, "ns"), "ns")
				d.WarmupLoadNS = normalizedMetric(reportedMetric(phase.Response.LoadDurationNS, "ns"), "ns")
			}
		}
	}
	for _, id := range a.Result.EvidenceArtifacts {
		raw, err := read(id)
		if err != nil {
			continue
		}
		var phase phaseEvidence
		if a.Result.InferenceCompleted && !a.Result.Truncated && json.Unmarshal(raw, &phase) == nil && phase.Kind == "formal" && phase.Response != nil && phase.ErrorCode == "" {
			d.EngineTotalNS = normalizedMetric(reportedMetric(phase.Response.TotalDurationNS, "ns"), "ns")
		}
		// Phase artifacts lack scorer_version. Old score bodies without duration
		// fields remain unknown rather than synthesizing zero elapsed time.
		var v struct {
			CaseID        string   `json:"case_id"`
			ScorerVersion string   `json:"scorer_version"`
			Compile       *float64 `json:"compile_duration_ns"`
			Total         *float64 `json:"total_duration_ns"`
			Assertions    []struct {
				Preparation *float64 `json:"preparation_duration_ns"`
				Execution   *float64 `json:"execution_duration_ns"`
			} `json:"assertions"`
		}
		if d.ScoreArtifact != "" || json.Unmarshal(raw, &v) != nil || v.ScorerVersion == "" || v.ScorerVersion != a.Result.ScorerVersion || v.CaseID != a.CaseID {
			continue
		}
		metric := func(x *float64) modelstore.Metric {
			if x == nil || !finite(*x) || *x <= 0 {
				return unknownMetric("ns", "duration absent or invalid in scorer artifact")
			}
			return observedMetric(*x, "ns", "trusted_host_scorer")
		}
		d.ScoreArtifact = id
		d.CompilerNS = metric(v.Compile)
		d.ScorerNS = metric(v.Total)
		if len(v.Assertions) > 0 {
			prep, exec := 0.0, 0.0
			pok, eok := true, true
			for _, a := range v.Assertions {
				if a.Preparation == nil || !finite(*a.Preparation) || *a.Preparation < 0 {
					pok = false
				} else {
					prep += *a.Preparation
				}
				if a.Execution == nil || !finite(*a.Execution) || *a.Execution < 0 {
					eok = false
				} else {
					exec += *a.Execution
				}
			}
			if pok {
				d.PreparationNS = metric(&prep)
			}
			if eok {
				d.ExecutionNS = metric(&exec)
			}
		}
	}
	return d
}

type MetricCoverage struct {
	Planned        int            `json:"planned"`
	Samples        int            `json:"samples"`
	Missing        int            `json:"missing"`
	Unit           string         `json:"unit"`
	MissingReasons map[string]int `json:"missing_reasons"`
}

func diagnosticMetrics(d AttemptDiagnostics) map[string]modelstore.Metric {
	return map[string]modelstore.Metric{"formal_wall_ns": d.WallNS, "formal_engine_total_ns": d.EngineTotalNS, "formal_load_ns": d.LoadNS, "prompt_ns": d.PromptNS, "decode_ns": d.DecodeNS, "tokens": d.Tokens, "tokens_per_second": d.TokensPerSecond, "warmup_cost_ns": d.WarmupCost, "warmup_engine_total_ns": d.WarmupEngineTotalNS, "warmup_load_ns": d.WarmupLoadNS, "compiler_ns": d.CompilerNS, "wasi_preparation_ns": d.PreparationNS, "wasi_execution_ns": d.ExecutionNS, "scorer_total_ns": d.ScorerNS, "cpu_percent": d.CPU, "ram_bytes": d.RAM, "available_vram_bytes": d.AvailableVRAM}
}

func metricCoverage(ds []AttemptDiagnostics, planned int) map[string]map[string]MetricCoverage {
	out := map[string]map[string]MetricCoverage{}
	for _, arm := range []string{"stable", "candidate"} {
		out[arm] = map[string]MetricCoverage{}
		for name, m := range diagnosticMetrics(diagnose(modelstore.Attempt{Metrics: emptyMetrics()}, nil)) {
			out[arm][name] = MetricCoverage{Planned: planned, Missing: planned, Unit: m.Unit, MissingReasons: map[string]int{}}
		}
	}
	seen := map[string]int{}
	for _, d := range ds {
		seen[d.Arm]++
		for name, m := range diagnosticMetrics(d) {
			v := out[d.Arm][name]
			if m.Value != nil && finite(*m.Value) && m.Quality != "unknown" {
				v.Samples++
				v.Missing--
			} else {
				v.MissingReasons[m.MissingReason]++
			}
			out[d.Arm][name] = v
		}
	}
	for _, arm := range []string{"stable", "candidate"} {
		if seen[arm] < planned {
			for name, v := range out[arm] {
				v.MissingReasons["selected attempt missing"] = planned - seen[arm]
				out[arm][name] = v
			}
		}
	}
	return out
}
