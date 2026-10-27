package modelops

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/noosphere/backend/internal/modelstore"
	"github.com/noosphere/backend/internal/workspaces"
	"path/filepath"
	"testing"
	"time"
)

func TestQueuedWorkspaceCancellationPersistsAndFencesStaleRun(t *testing.T) {
	s, st, report, _ := fixture(t, false, "local")
	r := canary(t, s, report)
	manager, e := workspaces.Open(filepath.Join(t.TempDir(), "workspace.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer manager.Close()
	if _, e = manager.SaveWorkspace(workspaces.Workspace{ID: "local", Name: "simulation fixture"}); e != nil {
		t.Fatal(e)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	manager.Register("index", "background", func(ctx context.Context, _ workspaces.Workspace, _ json.RawMessage, _ func(string, string, any)) (any, error) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil, nil
	})
	if _, e = manager.Submit("local", "index", "hold worker", nil); e != nil {
		t.Fatal(e)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("worker not blocked")
	}
	s.RegisterWorkspace(manager)
	run, e := s.Schedule(context.Background(), manager, "local", r.ID, SimulationRequest{Scenario: "replay_stage", VirtualSeconds: 60}, r.Revision, "queue")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = manager.Cancel(run.ID); e != nil {
		t.Fatal(e)
	}
	current, _ := st.GetRelease(context.Background(), r.ID)
	if current.State != "PAUSED" || current.AdmissionEnabled {
		t.Fatal("queued cancel failed", current.State)
	}
	if e = s.CancelTask(context.Background(), workspaces.Cancellation{RunID: run.ID, WorkspaceID: "local", Reference: workspaces.TaskReference{ID: r.ID, Generation: run.ReferenceGeneration}}); e != nil {
		t.Fatal(e)
	}
	oldRevision := current.Revision
	current = apply(t, s, current, Resume)
	if e = s.CancelTask(context.Background(), workspaces.Cancellation{RunID: run.ID, WorkspaceID: "local", Reference: workspaces.TaskReference{ID: r.ID, Generation: run.ReferenceGeneration}}); e != nil {
		t.Fatal(e)
	}
	after, _ := st.GetRelease(context.Background(), r.ID)
	if after.State != "CANARY_5" || after.Revision <= oldRevision || after.Revision != current.Revision {
		t.Fatal("stale cancel affected resumed release")
	}
	if _, e = s.ExecuteTask(context.Background(), "local", r.ID, run.ID, run.ReferenceGeneration); !errors.Is(e, modelstore.ErrStaleLease) {
		t.Fatal("stale worker accepted", e)
	}
}

func TestWorkspaceWorkerPersistsReplay(t *testing.T) {
	s, st, report, _ := fixture(t, false, "local")
	r := canary(t, s, report)
	manager, e := workspaces.Open(filepath.Join(t.TempDir(), "workspace.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer manager.Close()
	if _, e = manager.SaveWorkspace(workspaces.Workspace{ID: "local", Name: "simulation"}); e != nil {
		t.Fatal(e)
	}
	s.RegisterWorkspace(manager)
	run, e := s.Schedule(context.Background(), manager, "local", r.ID, SimulationRequest{Scenario: "replay_stage", VirtualSeconds: 60}, r.Revision, "work")
	if e != nil {
		t.Fatal(e)
	}
	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		got, e := manager.Run(run.ID)
		if e != nil {
			t.Fatal(e)
		}
		if got.Status == "completed" {
			break
		}
		if got.Status == "failed" || got.Status == "cancelled" {
			t.Fatalf("worker %s %s", got.Status, got.Error)
		}
		select {
		case <-deadline:
			t.Fatal("simulation worker timeout")
		case <-ticker.C:
		}
	}
	current, _ := st.GetRelease(context.Background(), r.ID)
	sim, e := ReadSimulation(current)
	if e != nil || sim.Task.Status != "completed" || sim.Stages[len(sim.Stages)-1].ReplayCount != 12 {
		t.Fatal("worker checkpoint missing", e)
	}
}
