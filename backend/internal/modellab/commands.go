package modellab

import (
	"context"
	"errors"
	"github.com/noosphere/backend/internal/gateway/providers"
	"github.com/noosphere/backend/internal/modelstore"
	"github.com/noosphere/backend/internal/workspaces"
)

// Control executes keyed human intent; duplicate receipts never redispatch.
func (s *Service) Control(ctx context.Context, r modelstore.EvaluationControl) (modelstore.Evaluation, error) {
	if s.runtime == nil {
		return modelstore.Evaluation{}, identityError("PROVIDER_UNAVAILABLE", "runtime unavailable")
	}
	rt := s.runtime
	rt.control.Lock()
	defer rt.control.Unlock()
	rt.mu.Lock()
	if rt.closed || rt.admissionStopped {
		rt.mu.Unlock()
		return modelstore.Evaluation{}, identityError("APP_RESTARTED", "admission stopped")
	}
	rt.work.Add(1)
	rt.mu.Unlock()
	defer rt.work.Done()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(rt.ctx, cancel)
	defer func() { stop(); cancel() }()
	if e, found, err := s.store.EvaluationControlReceipt(ctx, r); found || err != nil {
		if err == nil && r.Command == "cancel" {
			return e, s.cancelPersisted(ctx, e)
		}
		return e, err
	}
	previous, err := s.store.GetEvaluation(ctx, r.EvaluationID)
	if err != nil {
		return previous, err
	}
	if previous.Manifest.WorkspaceID != r.WorkspaceID {
		return modelstore.Evaluation{}, modelstore.ErrNotFound
	}
	verified := ""
	if r.Command != "cancel" && previous.Revision == r.ExpectedRevision {
		if _, err = s.runtimeReady(); err != nil {
			return previous, identityError("PROVIDER_UNAVAILABLE", "trusted runtime unavailable")
		}
		dataset, err := LoadDataset(previous.Manifest.Dataset.BenchmarkID)
		if err != nil {
			// Embedded dataset/validation failures are fixed compatibility facts,
			// not a temporary provider or filesystem availability condition.
			return s.store.RefuseEvaluationControl(ctx, r, previous.ManifestHash, "INSUFFICIENT_EVIDENCE")
		}
		if err = s.preflightFrozen(ctx, previous, dataset); err != nil {
			if code := deterministicPreflightCode(err); code != "" {
				return s.store.RefuseEvaluationControl(ctx, r, previous.ManifestHash, code)
			}
			return previous, err
		}
		verified = previous.ManifestHash
	}
	next, replayed, err := s.store.ControlEvaluation(ctx, r, verified)
	if err != nil || replayed {
		return next, err
	}
	if r.Command == "cancel" {
		return next, s.cancelPersisted(ctx, next)
	}
	return s.dispatch(ctx, next)
}

func deterministicPreflightCode(err error) string {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ""
	}
	var provider *providers.EvaluationError
	if errors.As(err, &provider) && (provider.Code == "MODEL_DRIFT" || provider.Code == "INSUFFICIENT_EVIDENCE") {
		return provider.Code
	}
	if errors.Is(err, modelstore.ErrInsufficientEvidence) || errors.Is(err, modelstore.ErrArtifactMissing) {
		return "INSUFFICIENT_EVIDENCE"
	}
	return ""
}

// Repeating the same persisted cancel receipt also retries stopping its exact
// external run if the first response or Manager cancellation was interrupted.
func (s *Service) cancelPersisted(ctx context.Context, e modelstore.Evaluation) error {
	if !e.CancelRequested || e.Status != "cancelled" {
		return nil
	}
	manager := s.runtime.config.Workspaces
	var run workspaces.Run
	var err error
	if e.RunID != "" {
		run, err = manager.Run(e.RunID)
	} else {
		run, err = manager.FindReferencedRun(e.Manifest.WorkspaceID, "evaluation", workspaces.TaskReference{ID: e.ID, Generation: e.LeaseGeneration - 1})
	}
	if errors.Is(err, workspaces.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.runtime.config.Budget.CleanupTimeout)
	defer cancel()
	_, err = manager.CancelContext(persist, run.ID)
	return err
}
