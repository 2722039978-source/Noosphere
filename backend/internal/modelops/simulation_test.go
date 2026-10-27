package modelops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/noosphere/backend/internal/modellab"
	"github.com/noosphere/backend/internal/modelstore"
)

func fixture(t *testing.T, bad bool, workspace ...string) (*Service, *modelstore.Store, modelstore.Report, string) {
	return fixtureInput(t, bad, []byte("synthetic input/output"), workspace...)
}

func fixtureInput(t *testing.T, bad bool, input []byte, workspace ...string) (*Service, *modelstore.Store, modelstore.Report, string) {
	t.Helper()
	ws := "ws"
	if len(workspace) > 0 {
		ws = workspace[0]
	}
	return fixtureInputAt(t, bad, input, filepath.Join(t.TempDir(), "ops.sqlite"), ws, 12)
}

// fixtureInputAt uses production storage/report code with explicitly synthetic
// evidence. A caller may retain a fresh directory for opt-in native UI proof.
func fixtureInputAt(t *testing.T, bad bool, input []byte, path, workspace string, completedPairs int) (*Service, *modelstore.Store, modelstore.Report, string) {
	t.Helper()
	ctx := context.Background()
	st, err := modelstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	m := modelstore.Manifest{SchemaVersion: 1, EncoderVersion: modelstore.EncoderVersion, WorkspaceID: "ws", WorkflowID: Workflow, Source: "offline_fixture", MemoryMode: "not_applicable", Stable: modelstore.ModelRevision{ID: "stable", GatewayModelID: "local", Provider: "ollama", ModelName: "s", IdentityLevel: "digest", Digest: strings.Repeat("a", 64)}, Candidate: modelstore.ModelRevision{ID: "candidate", GatewayModelID: "local", Provider: "ollama", ModelName: "c", IdentityLevel: "digest", Digest: strings.Repeat("b", 64)}, Profile: modelstore.InferenceProfile{ID: "profile"}, Dataset: modelstore.DatasetRevision{ID: "dataset", BenchmarkID: "go", Version: "v1", ContentHash: "synthetic-hash", ScorerVersion: "host-v1"}, Environment: modelstore.EnvironmentSnapshot{ID: "env", OS: "windows", Arch: "amd64", CollectorVersion: "fixture"}}
	m.WorkspaceID = workspace
	m.Stable.ModelName = "TEST_OFFLINE_FIXTURE_STABLE"
	m.Candidate.ModelName = "TEST_OFFLINE_FIXTURE_CANDIDATE"
	m.Environment.OS = "unknown"
	m.Environment.Arch = "unknown"
	m.Environment.VRAM = modelstore.Metric{Unit: "bytes", Source: "offline_fixture", Quality: "unknown", MissingReason: "Synthetic fixture does not collect hardware"}
	a, err := st.PublishArtifact(ctx, input, modelstore.ArtifactMetadata{Provenance: "offline_fixture", MediaType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		m.Dataset.Cases = append(m.Dataset.Cases, modelstore.Case{ID: fmt.Sprintf("case%02d", i), Category: []string{"logic", "boundary", "repair"}[i/4], InputArtifact: a.ID, Expected: "ok"})
	}
	ev, err := st.CreateEvaluation(ctx, modelstore.CreateEvaluationRequest{ID: "eval", Manifest: m, Idempotency: modelstore.Idempotency{Key: "eval"}})
	if err != nil {
		t.Fatal(err)
	}
	ev, err = st.ClaimEvaluation(ctx, ev.ID, "run", ev.Revision)
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range m.Dataset.Cases {
		for _, arm := range []string{"stable", "candidate"} {
			if arm == "candidate" && i >= completedPairs {
				continue
			}
			id := c.ID + arm
			body, _ := json.Marshal(map[string]any{"kind": "warmup", "attempt_id": id, "response": map[string]int{"total_duration_ns": 100, "load_duration_ns": 20}})
			warm, e := st.PublishArtifact(ctx, body, modelstore.ArtifactMetadata{Provenance: "offline_fixture", MediaType: "application/json"})
			if e != nil {
				t.Fatal(e)
			}
			verdict := "pass"
			if bad && arm == "candidate" && i == 0 {
				verdict = "functional_fail"
			}
			_, e = st.AppendAttempt(ctx, modelstore.Attempt{ID: id, EvaluationID: ev.ID, CaseID: c.ID, Arm: arm, AttemptNo: 1, LeaseGeneration: ev.LeaseGeneration, State: "completed", Verdict: verdict, InputArtifact: a.ID, OutputArtifact: a.ID, Result: modelstore.Result{InferenceCompleted: true, ScorerVersion: "host-v1"}, Metrics: modelstore.Metrics{WallNS: modelstore.Metric{Value: modelstore.Float(100), Unit: "ns", Source: "host_monotonic", Quality: "observed"}, WarmVerified: true, LoadingProtocol: modellab.LoadingProtocol, WarmupArtifact: warm.ID}})
			if e != nil {
				t.Fatal(e)
			}
		}
	}
	report, err := modellab.New(st, nil).BuildReport(ctx, ev.ID)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	return svc, st, report, path
}
func create(t *testing.T, s *Service, r modelstore.Report) modelstore.Release {
	t.Helper()
	ev, _ := s.store.GetEvaluation(context.Background(), r.EvaluationID)
	v, e := s.Create(context.Background(), ev.Manifest.WorkspaceID, CreateSpec{Mode: "simulation", WorkflowID: Workflow, TaskTypeFilter: TaskType, ReportID: r.ID}, "create")
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func apply(t *testing.T, s *Service, r modelstore.Release, c Command) modelstore.Release {
	t.Helper()
	v, e := s.Apply(context.Background(), r.WorkspaceID, r.ID, c, r.Revision, fmt.Sprintf("%s-%d", c, r.Revision))
	if e != nil {
		t.Fatalf("%s at %s: %v", c, r.State, e)
	}
	return v
}
func replay(t *testing.T, s *Service, r modelstore.Release) modelstore.Release {
	t.Helper()
	v, e := s.Simulate(context.Background(), r.WorkspaceID, r.ID, SimulationRequest{Scenario: "replay_stage", Seed: 7, VirtualSeconds: 60}, r.Revision, fmt.Sprintf("replay-%d", r.Revision))
	if e != nil {
		t.Fatal(e)
	}
	return v
}

func TestSimulationCannotMutateLiveOrGatewayDefault(t *testing.T) {
	s, st, report, _ := fixture(t, false)
	ctx := context.Background()
	for _, spec := range []CreateSpec{{Mode: "live", WorkflowID: Workflow, TaskTypeFilter: TaskType, ReportID: report.ID}, {Mode: "simulation", WorkflowID: "chat", TaskTypeFilter: TaskType, ReportID: report.ID}, {Mode: "simulation", WorkflowID: Workflow, TaskTypeFilter: "chat", ReportID: report.ID}} {
		if _, err := s.Create(ctx, "ws", spec, "bad"+spec.Mode+spec.WorkflowID+spec.TaskTypeFilter); err == nil {
			t.Fatal("unsupported scope accepted")
		}
	}
	r := create(t, s, report)
	r = apply(t, s, r, StartOffline)
	r = apply(t, s, r, Advance)
	for _, stage := range []string{"SHADOW", "CANARY_5", "CANARY_25", "CANARY_50", "PROMOTED"} {
		if r.State != stage {
			t.Fatal(r.State)
		}
		r = replay(t, s, r)
		sim, err := ReadSimulation(r)
		if err != nil {
			t.Fatal(err)
		}
		curr := sim.Stages[len(sim.Stages)-1]
		count := 0
		for _, a := range curr.Assignments {
			if a.Arm == "candidate" {
				count++
			}
		}
		want := map[string]int{"SHADOW": 0, "CANARY_5": 1, "CANARY_25": 5, "CANARY_50": 10, "PROMOTED": 20}[stage]
		if count != want || len(curr.Assignments) != 20 || curr.UniqueCases != 12 || curr.RealInferenceCount != 0 || curr.Source != "simulation_replay" || curr.OfflineSource != "offline_fixture" {
			t.Fatalf("%s %+v", stage, curr)
		}
		if stage != "PROMOTED" {
			r = apply(t, s, r, Advance)
		}
	}
	if r.StableRevisionID != "stable" || r.CandidateRevisionID != "candidate" || r.ServingRevisionID != "candidate" {
		t.Fatalf("frozen identity changed %+v", r)
	}
	original, e := st.GetReport(ctx, report.ID)
	if e != nil || original.Hash != report.Hash || string(original.Analysis) != string(report.Analysis) {
		t.Fatal("report changed")
	}
	r = apply(t, s, r, Rollback)
	if r.AdmissionEnabled || r.ServingRevisionID != "stable" {
		t.Fatal("rollback unsafe")
	}
}

func TestManualAdvanceCannotBypassSafety(t *testing.T) {
	s, _, report, _ := fixture(t, false)
	r := create(t, s, report)
	r = apply(t, s, r, StartOffline)
	r = apply(t, s, r, Advance)
	ctx := context.Background()
	p, e := s.Preview(ctx, "ws", r.ID, Advance)
	if e != nil || p.Decision.Allowed {
		t.Fatalf("missing shadow evidence %+v %v", p, e)
	}
	if _, e = s.Apply(ctx, "ws", r.ID, Advance, r.Revision, "denied"); !errors.Is(e, modelstore.ErrInsufficientEvidence) {
		t.Fatal(e)
	}
	deniedRevision := r.Revision
	r = replay(t, s, r)
	if _, e = s.Apply(ctx, "ws", r.ID, Advance, deniedRevision, "denied"); !errors.Is(e, modelstore.ErrInsufficientEvidence) {
		t.Fatal("same intent refusal must persist", e)
	}
	r = apply(t, s, r, Advance)
	r, e = s.Simulate(ctx, "ws", r.ID, SimulationRequest{Scenario: "inject_oom"}, r.Revision, "oom")
	if e != nil || r.State != "ROLLED_BACK" || r.AdmissionEnabled {
		t.Fatalf("injection %+v %v", r, e)
	}
}
func TestConcurrentAdvanceCASAndIdempotency(t *testing.T) {
	s, st, report, _ := fixture(t, false)
	r := create(t, s, report)
	r = apply(t, s, r, StartOffline)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	ready := make(chan struct{}, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ready <- struct{}{}
			<-start
			_, e := s.Apply(ctx, "ws", r.ID, Advance, r.Revision, fmt.Sprintf("race%d", i))
			errs <- e
		}(i)
	}
	<-ready
	<-ready
	close(start)
	wg.Wait()
	close(errs)
	wins, conflicts := 0, 0
	for e := range errs {
		if e == nil {
			wins++
		} else if errors.Is(e, modelstore.ErrRevisionConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("wins %d conflicts %d", wins, conflicts)
	}
	current, _ := st.GetRelease(ctx, r.ID)
	old, e := s.Apply(ctx, "ws", r.ID, Advance, r.Revision, "race0")
	old2, e2 := s.Apply(ctx, "ws", r.ID, Advance, r.Revision, "race0")
	if !errors.Is(e, e2) || old.Revision != old2.Revision || current.Revision != r.Revision+1 {
		t.Fatal("receipt lost", e, e2)
	}
}
func TestPromotedRetentionUsesWallClock(t *testing.T) {
	s, st, report, _ := fixture(t, false)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	r := create(t, s, report)
	r = apply(t, s, r, StartOffline)
	r = apply(t, s, r, Advance)
	for r.State != "PROMOTED" {
		r = replay(t, s, r)
		r = apply(t, s, r, Advance)
	}
	closed, e := s.CloseExpired(context.Background(), "ws", r.ID)
	if e != nil || closed.ClosedAt != "" {
		t.Fatal("early close", e)
	}
	now = now.Add(7 * 24 * time.Hour)
	_, mutation, previewErr := st.PreviewSimulation(context.Background(), "ws", r.ID, func(snap modelstore.SimulationSnapshot) (modelstore.SimulationMutation, error) {
		return s.simulationMutation(snap, SimulationRequest{Scenario: "inject_failure"})
	})
	if previewErr != nil || !mutation.Close || mutation.Command != "" {
		t.Fatal("expired safety fault must close retained history without late rollback", previewErr)
	}
	closed, e = s.CloseExpired(context.Background(), "ws", r.ID)
	if e != nil || closed.State != "PROMOTED" || closed.ClosedAt == "" {
		t.Fatal("retention", e)
	}
	if _, e = s.Apply(context.Background(), "ws", closed.ID, Rollback, closed.Revision, "late"); e == nil {
		t.Fatal("closed release rollback")
	}
	if _, e = s.Create(context.Background(), "ws", CreateSpec{Mode: "simulation", WorkflowID: Workflow, TaskTypeFilter: TaskType, ReportID: report.ID}, "old-baseline"); !errors.Is(e, modelstore.ErrInsufficientEvidence) {
		t.Fatal("closed promotion lost candidate baseline", e)
	}
}
