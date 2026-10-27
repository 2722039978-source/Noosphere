package modellab

import (
	"context"
	"errors"
	"github.com/noosphere/backend/internal/modelstore"
	"github.com/noosphere/backend/internal/workspaces"
)

func (s *Service) Resume(ctx context.Context, workspaceID, evaluationID string, expectedRevision int64) (modelstore.Evaluation, error) {
	var empty modelstore.Evaluation
	ctx, done, err := s.operation(ctx)
	if err != nil {
		return empty, err
	}
	defer done()
	r := s.runtime
	r.control.Lock()
	defer r.control.Unlock()
	if err := s.checkAdmission(); err != nil {
		return empty, err
	}
	if _, err = r.config.Workspaces.Workspace(workspaceID); err != nil {
		return empty, err
	}
	e, err := s.store.GetEvaluation(ctx, evaluationID)
	if err != nil {
		return empty, err
	}
	if e.Manifest.WorkspaceID != workspaceID {
		return empty, modelstore.ErrNotFound
	}
	if e.Revision != expectedRevision {
		return empty, modelstore.ErrRevisionConflict
	}
	if e.Status != "interrupted" && e.Status != "incomplete" && e.Status != "cancelled" {
		return empty, modelstore.ErrInvalid
	}
	all, err := s.store.ListAttempts(ctx, e.ID)
	if err != nil {
		return empty, err
	}
	for _, a := range all {
		if a.ErrorCode == "MODEL_DRIFT" {
			return empty, identityError("MODEL_DRIFT", "identity drift was observed; create a new evaluation")
		}
	}
	dataset, err := LoadDataset(e.Manifest.Dataset.BenchmarkID)
	if err != nil {
		return empty, err
	}
	if err = s.preflightFrozen(ctx, e, dataset); err != nil {
		return empty, err
	}
	e, err = s.store.ResumeEvaluation(ctx, evaluationID, expectedRevision, e.ManifestHash)
	if err != nil {
		return empty, err
	}
	return s.dispatch(ctx, e)
}

// CancelRun is called outside Manager/store locks for every referenced task
// cancellation. Its immutable reference is never upgraded from a current read.
func (s *Service) CancelRun(ctx context.Context, c workspaces.Cancellation) error {
	if c.Kind != "evaluation" || c.Reference.ID == "" || c.RunID == "" || c.Reference.Generation < 0 {
		return modelstore.ErrInvalid
	}
	return s.store.CancelDispatchedEvaluation(ctx, c.Reference.ID, c.WorkspaceID, c.RunID, c.Reference.Generation)
}
func (s *Service) Cancel(ctx context.Context, workspaceID, evaluationID string, expectedRevision int64) error {
	if s.runtime == nil {
		return identityError("INSUFFICIENT_EVIDENCE", "evaluation runtime unavailable")
	}
	e, err := s.store.GetEvaluation(ctx, evaluationID)
	if err != nil {
		return err
	}
	if e.Manifest.WorkspaceID != workspaceID {
		return modelstore.ErrNotFound
	}
	if e.Revision != expectedRevision {
		return modelstore.ErrRevisionConflict
	}
	if e.Status == "completed" || e.Status == "incomplete" || e.Status == "cancelled" {
		return nil
	}
	var run workspaces.Run
	if e.RunID != "" {
		run, err = s.runtime.config.Workspaces.Run(e.RunID)
	} else {
		run, err = s.runtime.config.Workspaces.FindReferencedRun(workspaceID, "evaluation", workspaces.TaskReference{ID: e.ID, Generation: e.LeaseGeneration})
	}
	if err == nil {
		_, err = s.runtime.config.Workspaces.CancelContext(ctx, run.ID)
		return err
	}
	if !errors.Is(err, workspaces.ErrNotFound) {
		return err
	}
	// The creation->Run crash window has no external Run to cancel.
	return s.store.CancelDispatchedEvaluation(ctx, e.ID, workspaceID, "undispatched", e.LeaseGeneration)
}

// Reconcile is startup-only; it never submits a new task or calls inference.
func (s *Service) Reconcile(ctx context.Context) error {
	if err := s.ReconcileCancellations(ctx); err != nil {
		return err
	}
	return s.store.RecoverInterrupted(ctx)
}

// ReconcileCancellations precedes the shared ModelOps/store recovery in server
// composition, so recovery only revokes each generation once.
func (s *Service) ReconcileCancellations(ctx context.Context) error {
	if s.runtime == nil {
		return identityError("INSUFFICIENT_EVIDENCE", "evaluation runtime unavailable")
	}
	evaluations, err := s.store.ListEvaluations(ctx)
	if err != nil {
		return err
	}
	for _, e := range evaluations {
		if e.Status != "queued" && e.Status != "running" {
			continue
		}
		var run workspaces.Run
		if e.RunID != "" {
			run, err = s.runtime.config.Workspaces.Run(e.RunID)
		} else {
			run, err = s.runtime.config.Workspaces.FindReferencedRun(e.Manifest.WorkspaceID, "evaluation", workspaces.TaskReference{ID: e.ID, Generation: e.LeaseGeneration})
		}
		if err != nil && !errors.Is(err, workspaces.ErrNotFound) {
			return err
		}
		if err == nil {
			if run.Kind == "evaluation" && run.ReferenceID == e.ID && run.Status == "cancelled" {
				if err = s.CancelRun(ctx, workspaces.Cancellation{RunID: run.ID, WorkspaceID: run.WorkspaceID, Kind: run.Kind, Reference: workspaces.TaskReference{ID: run.ReferenceID, Generation: run.ReferenceGeneration}}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Stop closes service admission and cancels owned work without destroying its
// runner/store dependencies. The application drains Workspace handlers next.
func (s *Service) Stop() {
	if r := s.runtime; r != nil {
		r.mu.Lock()
		r.closed = true
		r.cancel()
		r.mu.Unlock()
	}
}

// StopAdmission rejects new control/dispatch work while Manager cancels and
// drains its already admitted handlers with their original contexts.
func (s *Service) StopAdmission() {
	s.httpMu.Lock()
	s.httpStopped = true
	s.httpMu.Unlock()
	if r := s.runtime; r != nil {
		r.mu.Lock()
		r.admissionStopped = true
		r.mu.Unlock()
	}
}

// DrainHTTP is separate so the application can stop both services' admission
// before waiting for either service's currently admitted HTTP controls.
func (s *Service) DrainHTTP() { s.httpWork.Wait() }
func (s *Service) Close() error {
	r := s.runtime
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		s.Stop()
		r.work.Wait()
		ctx, cancel := context.WithTimeout(context.Background(), r.config.Budget.CleanupTimeout)
		defer cancel()
		r.closeErr = s.store.RecoverInterrupted(ctx)
		if r.config.Runner != nil {
			r.closeErr = errors.Join(r.closeErr, r.config.Runner.Close())
		}
	})
	return r.closeErr
}
