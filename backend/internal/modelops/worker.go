package modelops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/noosphere/backend/internal/modelstore"
	"github.com/noosphere/backend/internal/workspaces"
	"time"
)

// RegisterWorkspace installs the typed background worker and cancellation hook.
// Production composition/HTTP remain the next milestone's responsibility.
func (s *Service) RegisterWorkspace(manager *workspaces.Manager) {
	manager.RegisterReferenced("release_simulation", "background", func(ctx context.Context, ws workspaces.Workspace, _ json.RawMessage, emit func(string, string, any)) (any, error) {
		ref := workspaces.Reference(ctx)
		r, e := s.ExecuteTask(ctx, ws.ID, ref.ID, workspaces.RunID(ctx), ref.Generation)
		if e == nil {
			emit("simulation", "synthetic replay checkpoint saved", map[string]any{"release_id": r.ID, "source": "simulation_replay"})
		}
		return r, e
	}, s.CancelTask)
}

// Schedule persists intent before dispatch. The cross-database dispatch gap is
// recoverable, never a reason to automatically replay a task after restart.
func (s *Service) Schedule(ctx context.Context, manager *workspaces.Manager, ws, id string, request SimulationRequest, revision int64, key string) (workspaces.Run, error) {
	s.dispatchMu.Lock()
	defer s.dispatchMu.Unlock()
	payload, _ := json.Marshal(request)
	r, err := s.store.ApplySimulation(ctx, modelstore.SimulationIntent{WorkspaceID: ws, ReleaseID: id, Operation: "queue", ExpectedRevision: revision, Actor: "user", ReasonCode: "SIMULATION_QUEUED", Payload: payload, Idempotency: modelstore.Idempotency{Key: key}}, func(snap modelstore.SimulationSnapshot) (modelstore.SimulationMutation, error) {
		// Validate scenario scope without consuming replay samples or virtual time.
		if _, e := s.simulationMutation(snap, request); e != nil {
			return modelstore.SimulationMutation{}, e
		}
		sim, e := ReadSimulation(snap.Release)
		if e != nil {
			return modelstore.SimulationMutation{}, e
		}
		if sim.Task != nil && (sim.Task.Status == "queued" || sim.Task.Status == "running") {
			return modelstore.SimulationMutation{}, modelstore.ErrInvalid
		}
		sim.Task = &SimulationTask{Generation: snap.Release.Revision + 1, Status: "queued", Request: request}
		return modelstore.SimulationMutation{Simulation: encodeSimulation(sim)}, nil
	})
	if err != nil {
		return workspaces.Run{}, err
	}
	sim, err := ReadSimulation(r)
	if err != nil || sim.Task == nil {
		return workspaces.Run{}, modelstore.ErrInvalid
	}
	run, err := manager.FindReferencedRun(ws, "release_simulation", workspaces.TaskReference{ID: id, Generation: sim.Task.Generation})
	if err == nil {
		return run, nil
	}
	if !errors.Is(err, workspaces.ErrNotFound) {
		return workspaces.Run{}, err
	}
	current, e := s.store.GetRelease(ctx, id)
	if e != nil {
		return workspaces.Run{}, e
	}
	checkpoint, e := ReadSimulation(current)
	if e != nil || !taskOwned(checkpoint, "", sim.Task.Generation) || current.ClosedAt != "" {
		return workspaces.Run{}, modelstore.ErrStaleLease
	}
	input, _ := json.Marshal(map[string]string{"release_id": id})
	return manager.SubmitReferenced(ws, "release_simulation", "Synthetic workflow release replay", workspaces.TaskReference{ID: id, Generation: sim.Task.Generation}, input)
}

func taskOwned(sim Simulation, run string, generation int64) bool {
	return sim.Task != nil && sim.Task.Generation == generation && (sim.Task.RunID == "" || sim.Task.RunID == run) && (sim.Task.Status == "queued" || sim.Task.Status == "running")
}
func (s *Service) ExecuteTask(ctx context.Context, ws, id, run string, generation int64) (modelstore.Release, error) {
	claimed, err := s.store.ApplySimulation(ctx, modelstore.SimulationIntent{WorkspaceID: ws, ReleaseID: id, Operation: "claim", ExpectedRevision: generation, Actor: "worker", ReasonCode: "SIMULATION_CLAIMED", Payload: json.RawMessage(fmt.Sprintf(`{"run_id":%q,"generation":%d}`, run, generation)), Idempotency: modelstore.Idempotency{Key: fmt.Sprintf("claim-%s-%d", run, generation)}}, func(snap modelstore.SimulationSnapshot) (modelstore.SimulationMutation, error) {
		sim, e := ReadSimulation(snap.Release)
		if e != nil || !taskOwned(sim, run, generation) || sim.Task.Status != "queued" {
			return modelstore.SimulationMutation{}, modelstore.ErrStaleLease
		}
		sim.Task.RunID = run
		sim.Task.Status = "running"
		return modelstore.SimulationMutation{Simulation: encodeSimulation(sim)}, nil
	})
	if err != nil {
		if errors.Is(err, modelstore.ErrRevisionConflict) {
			err = modelstore.ErrStaleLease
		}
		return claimed, err
	}
	if err = ctx.Err(); err != nil {
		return claimed, err
	}
	return s.store.ApplySimulation(ctx, modelstore.SimulationIntent{WorkspaceID: ws, ReleaseID: id, Operation: "checkpoint", ExpectedRevision: claimed.Revision, Actor: "worker", ReasonCode: "SIMULATION_CHECKPOINT", Payload: json.RawMessage(fmt.Sprintf(`{"run_id":%q,"generation":%d}`, run, generation)), Idempotency: modelstore.Idempotency{Key: fmt.Sprintf("finish-%s-%d", run, generation)}}, func(snap modelstore.SimulationSnapshot) (modelstore.SimulationMutation, error) {
		sim, e := ReadSimulation(snap.Release)
		if e != nil || !taskOwned(sim, run, generation) || sim.Task.Status != "running" {
			return modelstore.SimulationMutation{}, modelstore.ErrStaleLease
		}
		change, e := s.simulationMutation(snap, sim.Task.Request)
		if e != nil {
			return change, e
		}
		updated, e := ReadSimulation(modelstore.Release{Simulation: change.Simulation})
		if e != nil {
			return change, e
		}
		updated.Task.Status = "completed"
		change.Simulation = encodeSimulation(updated)
		return change, nil
	})
}

// CancelTask runs outside the Manager mutex. Current queued/running references
// persist safe release state before Manager marks the task cancelled. Old runs
// are successful no-ops; they cannot borrow a new release generation.
func (s *Service) CancelTask(ctx context.Context, c workspaces.Cancellation) error {
	for attempt := 0; attempt < 3; attempt++ {
		r, e := s.store.GetRelease(ctx, c.Reference.ID)
		if e != nil {
			return e
		}
		if r.WorkspaceID != c.WorkspaceID {
			return modelstore.ErrNotFound
		}
		sim, e := ReadSimulation(r)
		if e != nil {
			return e
		}
		if !taskOwned(sim, c.RunID, c.Reference.Generation) || r.ClosedAt != "" {
			return nil
		}
		_, e = s.store.ApplySimulation(ctx, modelstore.SimulationIntent{WorkspaceID: c.WorkspaceID, ReleaseID: r.ID, Operation: "cancel", ExpectedRevision: r.Revision, Actor: "task_center", ReasonCode: "USER_CANCELLED", Payload: json.RawMessage(fmt.Sprintf(`{"run_id":%q,"generation":%d}`, c.RunID, c.Reference.Generation)), Idempotency: modelstore.Idempotency{Key: fmt.Sprintf("cancel-%s-%d", c.RunID, r.Revision)}}, func(snap modelstore.SimulationSnapshot) (modelstore.SimulationMutation, error) {
			state, e := ReadSimulation(snap.Release)
			if e != nil {
				return modelstore.SimulationMutation{}, e
			}
			if !taskOwned(state, c.RunID, c.Reference.Generation) {
				return modelstore.SimulationMutation{Noop: true}, nil
			}
			m, e := s.command(snap, Pause)
			if e != nil && snap.Release.State == "PROMOTED" {
				if until, parseErr := time.Parse(time.RFC3339Nano, snap.Release.RollbackUntil); parseErr == nil && !s.now().Before(until) {
					m = modelstore.SimulationMutation{Close: true, Simulation: encodeSimulation(state)}
					e = nil
				}
			}
			if e != nil {
				return m, e
			}
			state, e = ReadSimulation(modelstore.Release{Simulation: m.Simulation})
			if e != nil {
				return m, e
			}
			state.Task.Status = "cancelled"
			state.Task.Generation++
			m.Simulation = encodeSimulation(state)
			return m, nil
		})
		if errors.Is(e, modelstore.ErrRevisionConflict) {
			continue
		}
		return e
	}
	return modelstore.ErrRevisionConflict
}
