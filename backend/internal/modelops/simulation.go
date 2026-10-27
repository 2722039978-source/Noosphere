package modelops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/noosphere/backend/internal/modelstore"
	"sort"
	"time"
)

type SimulationRequest struct {
	Scenario       string `json:"scenario"`
	Seed           int64  `json:"seed"`
	VirtualSeconds int64  `json:"virtual_seconds"`
}
type Simulation struct {
	Source        string          `json:"source"`
	OfflineSource string          `json:"offline_source"`
	Version       string          `json:"version"`
	Authorization string          `json:"authorization"`
	Seed          int64           `json:"seed"`
	ClockSeconds  int64           `json:"clock_seconds"`
	Stages        []Stage         `json:"stages"`
	Alerts        []string        `json:"alerts"`
	Events        []ScenarioEvent `json:"events"`
	Task          *SimulationTask `json:"task,omitempty"`
}
type SimulationTask struct {
	RunID      string            `json:"run_id"`
	Generation int64             `json:"generation"`
	Status     string            `json:"status"`
	Request    SimulationRequest `json:"request"`
}
type Stage struct {
	AdmissionEpoch       int64                             `json:"admission_epoch"`
	State                string                            `json:"state"`
	CohortID             string                            `json:"cohort_id"`
	WindowStartSeconds   int64                             `json:"window_start_seconds"`
	Replayed             bool                              `json:"replayed"`
	Assignments          []modelstore.SimulationAssignment `json:"assignments"`
	Source               string                            `json:"source"`
	OfflineSource        string                            `json:"offline_source"`
	ReportID             string                            `json:"report_id"`
	ReportHash           string                            `json:"report_hash"`
	CaseIDs              []string                          `json:"case_ids"`
	ReplayCount          int                               `json:"replay_count"`
	UniqueCases          int                               `json:"unique_cases"`
	RealInferenceCount   int                               `json:"real_inference_count"`
	CandidateUserOutputs int                               `json:"candidate_user_outputs"`
	SideEffects          int                               `json:"side_effects"`
}
type ScenarioEvent struct {
	Scenario      string `json:"scenario"`
	Source        string `json:"source"`
	OfflineSource string `json:"offline_source"`
	ReportID      string `json:"report_id"`
	ClockSeconds  int64  `json:"clock_seconds"`
}

func ReadSimulation(r modelstore.Release) (Simulation, error) {
	var v Simulation
	err := json.Unmarshal(r.Simulation, &v)
	if err != nil || v.Version != "simulation-v1" || v.Authorization != "synthetic" || v.ClockSeconds < 0 {
		return v, modelstore.ErrInsufficientEvidence
	}
	return v, nil
}
func encodeSimulation(v Simulation) json.RawMessage { b, _ := json.Marshal(v); return b }
func freshSimulation() Simulation {
	return Simulation{Version: "simulation-v1", Authorization: "synthetic", Seed: 7, Stages: []Stage{}, Alerts: []string{}, Events: []ScenarioEvent{}}
}
func candidateCount(stage string) int {
	return map[string]int{"SHADOW": 0, "CANARY_5": 1, "CANARY_25": 5, "CANARY_50": 10, "PROMOTED": 20}[stage]
}
func enterStage(sim *Simulation, s modelstore.SimulationSnapshot, state string) []modelstore.SimulationAssignment {
	if state != "SHADOW" && state != "CANARY_5" && state != "CANARY_25" && state != "CANARY_50" && state != "PROMOTED" {
		return nil
	}
	cohort := fmt.Sprintf("cohort-%d-%s", len(sim.Stages)+1, state)
	ids := make([]string, 20)
	for i := range ids {
		ids[i] = fmt.Sprintf("%s-%02d", cohort, i)
	}
	rank := func(id string) string {
		h := sha256.Sum256([]byte(fmt.Sprintf("%d/%s/%s", sim.Seed, s.Release.ID, id)))
		return hex.EncodeToString(h[:])
	}
	sort.Slice(ids, func(i, j int) bool { return rank(ids[i]) < rank(ids[j]) })
	a := []modelstore.SimulationAssignment{}
	for i, id := range ids {
		arm := "stable"
		if i < candidateCount(state) {
			arm = "candidate"
		}
		a = append(a, modelstore.SimulationAssignment{SessionKey: id, CohortID: cohort, Arm: arm, AdmittedEpoch: s.Release.RouteEpoch + 1, Source: "simulation_replay", OfflineSource: s.Evaluation.Manifest.Source})
	}
	sim.Stages = append(sim.Stages, Stage{AdmissionEpoch: s.Release.RouteEpoch + 1, State: state, CohortID: cohort, WindowStartSeconds: sim.ClockSeconds, Assignments: a, Source: "simulation_replay", OfflineSource: s.Evaluation.Manifest.Source, ReportID: s.Report.ID, ReportHash: s.Report.Hash, CaseIDs: []string{}})
	return a
}

func simulate(s modelstore.SimulationSnapshot, req SimulationRequest) (modelstore.SimulationMutation, error) {
	m := modelstore.SimulationMutation{Decision: modelstore.GateDecision{Allowed: true, PolicyHash: s.Policy.Hash, EvidenceHash: s.Report.Hash}}
	sim, err := ReadSimulation(s.Release)
	if err != nil {
		return m, err
	}
	if req.VirtualSeconds < 0 || req.VirtualSeconds > 3600 || len(sim.Events) >= 512 {
		return m, modelstore.ErrInvalid
	}
	if req.Seed != 0 && req.Seed != sim.Seed {
		return m, modelstore.ErrInvalid
	}
	switch req.Scenario {
	case "replay_stage":
		if len(sim.Stages) == 0 || sim.Stages[len(sim.Stages)-1].State != s.Release.State || (!s.Release.AdmissionEnabled && s.Release.State != "SHADOW") {
			return m, modelstore.ErrInvalid
		}
		m.Decision = decision(s, "OFFLINE_EVAL")
		m.NeedsEvidence = true
		if !m.Decision.Allowed {
			return m, modelstore.ErrInsufficientEvidence
		}
		sim.ClockSeconds += req.VirtualSeconds
		if sim.ClockSeconds > 1_000_000_000 {
			return m, modelstore.ErrInvalid
		}
		stage := &sim.Stages[len(sim.Stages)-1]
		stage.Replayed = true
		stage.ReplayCount += 12
		stage.UniqueCases = 12
		stage.CaseIDs = nil
		for _, c := range s.Evaluation.Manifest.Dataset.Cases {
			stage.CaseIDs = append(stage.CaseIDs, c.ID)
		}
	case "inject_oom", "inject_failure":
		command := Pause
		if s.Release.State == "PAUSED" {
			if _, e := Next(s.Release, Rollback); e == nil {
				command = Rollback
			} else {
				return m, modelstore.ErrInvalid
			}
		}
		if _, err = Next(s.Release, command); err != nil {
			return m, err
		}
		m.Command = string(command)
		sim.Alerts = append(sim.Alerts, req.Scenario)
	default:
		return m, modelstore.ErrInvalid
	}
	source := "simulation_replay"
	if req.Scenario != "replay_stage" {
		source = "simulation_injected"
		if s.Release.State == "CANARY_5" || s.Release.State == "CANARY_25" || s.Release.State == "CANARY_50" {
			m.Command = string(Rollback)
		}
	}
	sim.Events = append(sim.Events, ScenarioEvent{Scenario: req.Scenario, Source: source, OfflineSource: s.Evaluation.Manifest.Source, ReportID: s.Report.ID, ClockSeconds: sim.ClockSeconds})
	m.Simulation = encodeSimulation(sim)
	return m, nil
}
func (s *Service) Simulate(ctx context.Context, ws, id string, req SimulationRequest, revision int64, key string) (modelstore.Release, error) {
	body, _ := json.Marshal(req)
	return s.store.ApplySimulation(ctx, modelstore.SimulationIntent{WorkspaceID: ws, ReleaseID: id, Operation: "simulation", ExpectedRevision: revision, Actor: "user", ReasonCode: req.Scenario, Payload: body, Idempotency: modelstore.Idempotency{Key: key}}, func(snap modelstore.SimulationSnapshot) (modelstore.SimulationMutation, error) {
		return s.simulationMutation(snap, req)
	})
}

func (s *Service) simulationMutation(snap modelstore.SimulationSnapshot, req SimulationRequest) (modelstore.SimulationMutation, error) {
	change, err := simulate(snap, req)
	if err == nil && snap.Release.State == "PROMOTED" && req.Scenario != "replay_stage" {
		until, e := time.Parse(time.RFC3339Nano, snap.Release.RollbackUntil)
		if e != nil {
			return change, modelstore.ErrInvalid
		}
		if !s.now().Before(until) {
			change.Command = ""
			change.Close = true
		}
	}
	return change, err
}
