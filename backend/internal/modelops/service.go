package modelops

import (
	"context"
	"github.com/noosphere/backend/internal/modellab"
	"github.com/noosphere/backend/internal/modelstore"
	"sync"
	"time"
)

type Service struct {
	store            *modelstore.Store
	now              func() time.Time
	dispatchMu       sync.Mutex
	admissionMu      sync.Mutex
	admissionStopped bool
	httpWork         sync.WaitGroup
}
type CreateSpec struct{ Mode, WorkflowID, TaskTypeFilter, ReportID string }
type DecisionPreview struct {
	ReleaseID    string                  `json:"release_id"`
	Revision     int64                   `json:"revision"`
	NextState    string                  `json:"next_state"`
	Decision     modelstore.GateDecision `json:"decision"`
	EvidenceHash string                  `json:"evidence_hash"`
}

func New(ctx context.Context, store *modelstore.Store) (*Service, error) {
	if store == nil {
		return nil, modelstore.ErrInvalid
	}
	_, err := store.SavePolicy(ctx, DemoPolicy())
	if err != nil {
		return nil, err
	}
	return &Service{store: store, now: time.Now}, nil
}
func (s *Service) Create(ctx context.Context, ws string, spec CreateSpec, key string) (modelstore.Release, error) {
	if spec.Mode != "simulation" || spec.WorkflowID != Workflow || spec.TaskTypeFilter != TaskType {
		return modelstore.Release{}, modelstore.ErrInvalid
	}
	// A deterministic server-generated identifier makes response-loss create
	// retries resolve the same store receipt; the hash contains all caller intent.
	return s.store.CreateSimulationRelease(ctx, modelstore.CreateReleaseRequest{WorkspaceID: ws, WorkflowID: Workflow, FilterHash: TaskType, ReportID: spec.ReportID, PolicyID: PolicyID, Idempotency: modelstore.Idempotency{Key: key}}, func(snap modelstore.SimulationSnapshot) error {
		comparison, err := modellab.ReadComparison(snap.Report)
		if err != nil || !identity(snap) || comparison.ManifestHash != snap.Report.ManifestHash || comparison.AttemptSetHash != snap.Report.AttemptSetHash || comparison.Source != snap.Evaluation.Manifest.Source {
			return modelstore.ErrInsufficientEvidence
		}
		return nil
	})
}
func (s *Service) command(snap modelstore.SimulationSnapshot, c Command) (modelstore.SimulationMutation, error) {
	m := modelstore.SimulationMutation{Command: string(c)}
	next, err := Next(snap.Release, c)
	if err != nil {
		return m, err
	}
	m.Decision = modelstore.GateDecision{Allowed: true, PolicyHash: snap.Policy.Hash, EvidenceHash: snap.Report.Hash}
	if snap.Release.State == "PROMOTED" && (c == Pause || c == Rollback) {
		until, e := time.Parse(time.RFC3339Nano, snap.Release.RollbackUntil)
		if e != nil || !s.now().Before(until) {
			return m, modelstore.ErrInvalid
		}
	}
	if c == Advance {
		m.Decision = decision(snap, snap.Release.State)
		if !m.Decision.Allowed {
			return m, modelstore.ErrInsufficientEvidence
		}
	}
	if c == Resume {
		m.Decision = decision(snap, "OFFLINE_EVAL")
		if !m.Decision.Allowed {
			return m, modelstore.ErrInsufficientEvidence
		}
	}
	if c == StartOffline {
		m.Decision = modelstore.GateDecision{Allowed: identity(snap) && snap.EvidenceError == nil, PolicyHash: snap.Policy.Hash, EvidenceHash: snap.Report.Hash}
		if !m.Decision.Allowed {
			return m, modelstore.ErrInsufficientEvidence
		}
	}
	sim, err := ReadSimulation(snap.Release)
	if err != nil {
		if c != StartOffline && c != Reject && c != Pause && c != Rollback {
			return m, err
		}
		sim = freshSimulation()
	}
	sim.Source = "simulation_replay"
	sim.OfflineSource = snap.Evaluation.Manifest.Source
	if c == Resume {
		sim.Alerts = nil
	}
	m.Assignments = enterStage(&sim, snap, next)
	if sim.Task != nil {
		sim.Task.Status = "revoked"
		sim.Task.Generation++
	}
	m.Simulation = encodeSimulation(sim)
	if next == "PROMOTED" {
		stamp := s.now().UTC()
		m.PromotedAt = stamp.Format(time.RFC3339Nano)
		m.RollbackUntil = stamp.Add(time.Duration(snap.Policy.RollbackRetentionNS)).Format(time.RFC3339Nano)
	}
	return m, nil
}
func (s *Service) Preview(ctx context.Context, ws, id string, c Command) (DecisionPreview, error) {
	snap, m, err := s.store.PreviewSimulation(ctx, ws, id, func(v modelstore.SimulationSnapshot) (modelstore.SimulationMutation, error) { return s.command(v, c) })
	p := DecisionPreview{ReleaseID: id, Revision: snap.Release.Revision, Decision: m.Decision, EvidenceHash: snap.Report.Hash}
	p.NextState, _ = Next(snap.Release, c)
	if err == modelstore.ErrInsufficientEvidence {
		return p, nil
	}
	return p, err
}
func (s *Service) Apply(ctx context.Context, ws, id string, c Command, revision int64, key string) (modelstore.Release, error) {
	return s.store.ApplySimulation(ctx, modelstore.SimulationIntent{WorkspaceID: ws, ReleaseID: id, Operation: "command", Command: string(c), ExpectedRevision: revision, Actor: "user", ReasonCode: string(c), Idempotency: modelstore.Idempotency{Key: key}}, func(v modelstore.SimulationSnapshot) (modelstore.SimulationMutation, error) { return s.command(v, c) })
}
func (s *Service) CloneEvaluation(ctx context.Context, ws, id, key string) (modelstore.Evaluation, error) {
	ev, err := s.store.GetEvaluation(ctx, id)
	if err != nil {
		return ev, err
	}
	if ev.Manifest.WorkspaceID != ws {
		return modelstore.Evaluation{}, modelstore.ErrNotFound
	}
	m := ev.Manifest
	m.ParentEvaluationID = id
	return s.store.CreateClonedEvaluation(ctx, id, modelstore.CreateEvaluationRequest{Manifest: m, Idempotency: modelstore.Idempotency{Key: key}})
}
