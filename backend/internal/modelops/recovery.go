package modelops

import (
	"context"
	"crypto/rand"
	"fmt"
	"github.com/noosphere/backend/internal/modelstore"
	"time"
)

func (s *Service) Recover(ctx context.Context) error {
	if err := s.store.RecoverInterrupted(ctx); err != nil {
		return err
	}
	releases, err := s.store.OpenSimulationReleases(ctx)
	if err != nil {
		return err
	}
	for _, r := range releases {
		if r.State != "PROMOTED" {
			continue
		}
		until, e := time.Parse(time.RFC3339Nano, r.RollbackUntil)
		if e == nil && !s.now().Before(until) {
			if _, e = s.CloseExpired(ctx, r.WorkspaceID, r.ID); e != nil {
				return e
			}
			continue
		}
		var nonce [16]byte
		if _, e = rand.Read(nonce[:]); e != nil {
			return e
		}
		_, e = s.store.ApplySimulation(ctx, modelstore.SimulationIntent{WorkspaceID: r.WorkspaceID, ReleaseID: r.ID, Operation: "restart_promoted", ExpectedRevision: r.Revision, Actor: "system", ReasonCode: "APP_RESTARTED_PROMOTED_CHECK", Idempotency: modelstore.Idempotency{Key: fmt.Sprintf("restart-promoted-%x", nonce)}}, func(snap modelstore.SimulationSnapshot) (modelstore.SimulationMutation, error) {
			sim, simErr := ReadSimulation(snap.Release)
			m := modelstore.SimulationMutation{Decision: modelstore.GateDecision{Allowed: true, PolicyHash: snap.Policy.Hash, EvidenceHash: snap.Report.Hash}}
			if simErr == nil && interruptTask(&sim) {
				m.Simulation = encodeSimulation(sim)
			}
			if identity(snap) && snap.EvidenceError == nil {
				m.Noop = len(m.Simulation) == 0
				return m, nil
			}
			m.Command = string(Rollback)
			m.Decision.Reasons = []string{"promoted simulation evidence/identity unavailable after restart"}
			return m, nil
		})
		if e != nil {
			return e
		}
	}
	return nil
}

// CloseExpired is a trusted wall-clock lifecycle action, never a client command
// or a comparison against the user-controlled virtual observation clock.
func (s *Service) CloseExpired(ctx context.Context, ws, id string) (modelstore.Release, error) {
	r, err := s.store.GetRelease(ctx, id)
	if err != nil {
		return r, err
	}
	if r.WorkspaceID != ws {
		return modelstore.Release{}, modelstore.ErrNotFound
	}
	if r.ClosedAt != "" {
		return r, nil
	}
	if until, e := time.Parse(time.RFC3339Nano, r.RollbackUntil); e == nil && s.now().Before(until) {
		return r, nil
	}
	return s.store.ApplySimulation(ctx, modelstore.SimulationIntent{WorkspaceID: ws, ReleaseID: id, Operation: "retention", ExpectedRevision: r.Revision, Actor: "system", ReasonCode: "ROLLBACK_RETENTION_EXPIRED", Idempotency: modelstore.Idempotency{Key: fmt.Sprintf("expiry-%d", r.Revision)}}, func(snap modelstore.SimulationSnapshot) (modelstore.SimulationMutation, error) {
		if snap.Release.State != "PROMOTED" {
			return modelstore.SimulationMutation{}, modelstore.ErrInvalid
		}
		until, e := time.Parse(time.RFC3339Nano, snap.Release.RollbackUntil)
		if e != nil {
			return modelstore.SimulationMutation{}, modelstore.ErrInvalid
		}
		if s.now().Before(until) {
			return modelstore.SimulationMutation{Noop: true}, nil
		}
		m := modelstore.SimulationMutation{Close: true}
		if sim, e := ReadSimulation(snap.Release); e == nil && interruptTask(&sim) {
			m.Simulation = encodeSimulation(sim)
		}
		return m, nil
	})
}

func interruptTask(sim *Simulation) bool {
	if sim.Task == nil || (sim.Task.Status != "queued" && sim.Task.Status != "running") {
		return false
	}
	sim.Task.Status = "interrupted"
	sim.Task.Generation++
	return true
}
