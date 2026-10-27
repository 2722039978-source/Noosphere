package modelops

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/noosphere/backend/internal/modelstore"
	"github.com/noosphere/backend/internal/workspaces"
)

func blockedManager(t *testing.T, s *Service) *workspaces.Manager {
	t.Helper()
	m, e := workspaces.Open(filepath.Join(t.TempDir(), "tasks.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { m.Close() })
	started := make(chan struct{})
	m.Register("index", "background", func(ctx context.Context, _ workspaces.Workspace, _ json.RawMessage, _ func(string, string, any)) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, nil
	})
	if _, e = m.Submit("local", "index", "hold lane", nil); e != nil {
		t.Fatal(e)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("lane barrier timeout")
	}
	s.RegisterWorkspace(m)
	return m
}
func promote(t *testing.T, s *Service, report modelstore.Report) modelstore.Release {
	t.Helper()
	r := canary(t, s, report)
	for r.State != "PROMOTED" {
		r = replay(t, s, r)
		r = apply(t, s, r, Advance)
	}
	return r
}

func TestResumeRequiresFreshAuthoritativeCohort(t *testing.T) {
	s, st, report, path := fixture(t, false)
	ctx := context.Background()
	r := canary(t, s, report)
	r = replay(t, s, r)
	before, _ := ReadSimulation(r)
	old := before.Stages[len(before.Stages)-1]
	r = apply(t, s, r, Pause)
	r = apply(t, s, r, Resume)
	after, _ := ReadSimulation(r)
	current := after.Stages[len(after.Stages)-1]
	if len(after.Stages) != len(before.Stages)+1 || current.CohortID == old.CohortID || current.State != "CANARY_5" || current.Replayed {
		t.Fatal("resume did not establish a fresh observation cohort")
	}
	rows, e := st.ListSimulationAssignments(ctx, "ws", r.ID)
	if e != nil {
		t.Fatal(e)
	}
	oldCandidates, newCandidates, newTotal := 0, 0, 0
	for _, a := range rows {
		if a.CohortID == old.CohortID && a.Arm == "candidate" {
			if a.RevokedAt == "" {
				t.Fatal("old candidate resurrected")
			}
			oldCandidates++
		}
		if a.CohortID == current.CohortID {
			newTotal++
			if a.Arm == "candidate" && a.RevokedAt == "" && a.AdmittedEpoch == r.RouteEpoch {
				newCandidates++
			}
		}
	}
	if oldCandidates != 1 || newTotal != 20 || newCandidates != 1 {
		t.Fatalf("cohort truth old=%d total=%d candidate=%d", oldCandidates, newTotal, newCandidates)
	}
	p, e := s.Preview(ctx, "ws", r.ID, Advance)
	if e != nil || p.Decision.Allowed {
		t.Fatal("fresh window bypass", e)
	}
	r = replay(t, s, r)
	p, e = s.Preview(ctx, "ws", r.ID, Advance)
	if e != nil || !p.Decision.Allowed {
		t.Fatal("valid fresh cohort blocked", e)
	}
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec("UPDATE release_assignments SET revoked_at='test-revoked' WHERE release_id=? AND cohort_id=? AND arm='candidate'", r.ID, current.CohortID); e != nil {
		t.Fatal(e)
	}
	p, e = s.Preview(ctx, "ws", r.ID, Advance)
	if e != nil || p.Decision.Allowed {
		t.Fatal("gate trusted JSON over revoked authoritative admission", e)
	}
	if _, e = s.Apply(ctx, "ws", r.ID, Advance, r.Revision, "revoked-current-cohort"); !errors.Is(e, modelstore.ErrInsufficientEvidence) {
		t.Fatal("revoked cohort advanced", e)
	}
	if _, e = db.Exec("DELETE FROM release_assignments WHERE release_id=? AND cohort_id=?", r.ID, current.CohortID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Apply(ctx, "ws", r.ID, Advance, r.Revision, "missing-current-cohort"); !errors.Is(e, modelstore.ErrInsufficientEvidence) {
		t.Fatal("missing cohort advanced", e)
	}
}

func TestPromotedRestartFencesQueuedAndClaimedTasks(t *testing.T) {
	for _, status := range []string{"queued", "running"} {
		t.Run(status, func(t *testing.T) {
			s, st, report, _ := fixture(t, false, "local")
			ctx := context.Background()
			r := promote(t, s, report)
			m := blockedManager(t, s)
			request := SimulationRequest{Scenario: "inject_failure"}
			run, e := s.Schedule(ctx, m, "local", r.ID, request, r.Revision, "pending")
			if e != nil {
				t.Fatal(e)
			}
			current, _ := st.GetRelease(ctx, r.ID)
			if status == "running" {
				current, e = st.ApplySimulation(ctx, modelstore.SimulationIntent{WorkspaceID: "local", ReleaseID: r.ID, Operation: "claim", ExpectedRevision: run.ReferenceGeneration, Actor: "worker", ReasonCode: "SIMULATION_CLAIMED", Payload: json.RawMessage(fmt.Sprintf(`{"run_id":%q,"generation":%d}`, run.ID, run.ReferenceGeneration)), Idempotency: modelstore.Idempotency{Key: fmt.Sprintf("claim-%s-%d", run.ID, run.ReferenceGeneration)}}, func(snap modelstore.SimulationSnapshot) (modelstore.SimulationMutation, error) {
					sim, _ := ReadSimulation(snap.Release)
					sim.Task.RunID = run.ID
					sim.Task.Status = "running"
					return modelstore.SimulationMutation{Simulation: encodeSimulation(sim)}, nil
				})
				if e != nil {
					t.Fatal(e)
				}
			}
			before, _ := ReadSimulation(current)
			if e = s.Recover(ctx); e != nil {
				t.Fatal(e)
			}
			recovered, _ := st.GetRelease(ctx, r.ID)
			sim, _ := ReadSimulation(recovered)
			if recovered.State != "PROMOTED" || recovered.ServingRevisionID != "candidate" || !recovered.AdmissionEnabled || sim.Task.Status != "interrupted" || sim.Task.Generation <= before.Task.Generation || recovered.Revision <= current.Revision {
				t.Fatalf("promoted %s task remained valid: state=%s task=%+v", status, recovered.State, sim.Task)
			}
			if sim.ClockSeconds != before.ClockSeconds || len(sim.Events) != len(before.Events) {
				t.Fatal("restart replayed old fault")
			}
			if _, e = s.ExecuteTask(ctx, "local", r.ID, run.ID, run.ReferenceGeneration); e == nil {
				t.Fatal("stale worker accepted")
			}
			if e = s.CancelTask(ctx, workspaces.Cancellation{RunID: run.ID, WorkspaceID: "local", Reference: workspaces.TaskReference{ID: r.ID, Generation: run.ReferenceGeneration}}); e != nil {
				t.Fatal(e)
			}
			after, _ := st.GetRelease(ctx, r.ID)
			if after.Revision != recovered.Revision || after.State != "PROMOTED" {
				t.Fatal("stale cancellation affected recovered promotion")
			}
			fresh, e := s.Schedule(ctx, m, "local", r.ID, SimulationRequest{Scenario: "replay_stage", VirtualSeconds: 60}, recovered.Revision, "fresh-after-restart")
			if e != nil || fresh.ID == run.ID || fresh.ReferenceGeneration <= run.ReferenceGeneration {
				t.Fatal("explicit fresh schedule blocked", e)
			}
		})
	}
}

func TestScheduleRetryFindsOriginalBeyondRecentHistory(t *testing.T) {
	s, _, report, _ := fixture(t, false, "local")
	ctx := context.Background()
	r := canary(t, s, report)
	m := blockedManager(t, s)
	request := SimulationRequest{Scenario: "replay_stage", VirtualSeconds: 60}
	original, e := s.Schedule(ctx, m, "local", r.ID, request, r.Revision, "same-dispatch")
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 101; i++ {
		_, ex, e := m.Start(ctx, workspaces.Run{WorkspaceID: "local", Kind: "diagnosis", Title: fmt.Sprintf("unrelated-%d", i)})
		if e != nil {
			t.Fatal(e)
		}
		if e = ex.Finish(nil, nil); e != nil {
			t.Fatal(e)
		}
	}
	recent, e := m.Runs("local")
	if e != nil {
		t.Fatal(e)
	}
	for _, run := range recent {
		if run.ID == original.ID {
			t.Fatal("fixture failed to age original out of recent100")
		}
	}
	// Reconstruct the service as after a response-loss/reconnect; no in-memory
	// run cache can supply the original dispatch identity.
	s2, e := New(ctx, s.store)
	if e != nil {
		t.Fatal(e)
	}
	retried, e := s2.Schedule(ctx, m, "local", r.ID, request, r.Revision, "same-dispatch")
	if e != nil || retried.ID != original.ID || retried.ReferenceGeneration != original.ReferenceGeneration {
		t.Fatalf("dispatch receipt lost: original=%s retried=%s error=%v", original.ID, retried.ID, e)
	}
}
