package modellab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/noosphere/backend/internal/evalrunner"
	"github.com/noosphere/backend/internal/gateway"
	"github.com/noosphere/backend/internal/gateway/providers"
	"github.com/noosphere/backend/internal/modelstore"
	"github.com/noosphere/backend/internal/workspaces"
)

func unknownMetric(unit, reason string) modelstore.Metric {
	return modelstore.Metric{Unit: unit, Source: "unavailable", Quality: "unknown", MissingReason: reason}
}
func observedMetric(value float64, unit, source string) modelstore.Metric {
	return modelstore.Metric{Value: modelstore.Float(value), Unit: unit, Source: source, Quality: "observed"}
}
func reportedMetric(v *int64, unit string) modelstore.Metric {
	if v == nil {
		return unknownMetric(unit, "engine did not report this field")
	}
	return modelstore.Metric{Value: modelstore.Float(float64(*v)), Unit: unit, Source: "ollama_native", Quality: "reported"}
}
func emptyMetrics() modelstore.Metrics {
	return modelstore.Metrics{WallNS: unknownMetric("ns", "formal response unavailable"), LoadNS: unknownMetric("ns", "formal response unavailable"), PromptNS: unknownMetric("ns", "formal response unavailable"), DecodeNS: unknownMetric("ns", "formal response unavailable"), Tokens: unknownMetric("tokens", "formal response unavailable"), CPU: unknownMetric("percent", "per-model CPU attribution unavailable"), RAM: unknownMetric("bytes", "per-model RAM attribution unavailable"), AvailableVRAM: unknownMetric("bytes", "physical available VRAM collector unavailable"), WarmupCost: unknownMetric("ns", "warmup response unavailable"), LoadingProtocol: LoadingProtocol}
}
func responseMetrics(m modelstore.Metrics, response *providers.EvaluationResponse) modelstore.Metrics {
	if response == nil {
		return m
	}
	m.WallNS = observedMetric(float64(response.WallDuration), "ns", "host_monotonic")
	m.LoadNS = reportedMetric(response.LoadDurationNS, "ns")
	m.PromptNS = reportedMetric(response.PromptEvalDurationNS, "ns")
	m.DecodeNS = reportedMetric(response.EvalDurationNS, "ns")
	m.Tokens = reportedMetric(response.EvalCount, "tokens")
	return m
}

type phaseEvidence struct {
	Kind        string                            `json:"kind"`
	Request     *providers.EvaluationRequest      `json:"request,omitempty"`
	Response    *providers.EvaluationResponse     `json:"response,omitempty"`
	Residency   *providers.ModelResidencySnapshot `json:"residency,omitempty"`
	ErrorCode   string                            `json:"error_code,omitempty"`
	ErrorReason string                            `json:"error_reason,omitempty"`
	StartedAt   string                            `json:"started_at"`
	FinishedAt  string                            `json:"finished_at"`
	WallNS      int64                             `json:"wall_ns"`
}

func phase(kind string, request *providers.EvaluationRequest, response *providers.EvaluationResponse, residency *providers.ModelResidencySnapshot, err error, start time.Time) phaseEvidence {
	out := phaseEvidence{Kind: kind, Request: request, Response: response, Residency: residency, StartedAt: start.UTC().Format(time.RFC3339Nano), FinishedAt: time.Now().UTC().Format(time.RFC3339Nano), WallNS: int64(time.Since(start))}
	if err != nil {
		out.ErrorCode = errorCode(err)
		out.ErrorReason = safeReason(err)
	}
	return out
}
func (s *Service) Execute(ctx context.Context, evaluationID, runID string, emit func(string, string, any)) (returned error) {
	ctx, done, err := s.operation(ctx)
	if err != nil {
		return err
	}
	defer done()
	r := s.runtime
	defer func() {
		if returned != nil && r.ctx.Err() != nil {
			returned = identityError("APP_RESTARTED", "application stopped; explicit resume required")
		}
	}()
	ref := workspaces.Reference(ctx)
	if ref.ID != evaluationID || runID == "" || workspaces.RunID(ctx) != runID {
		return modelstore.ErrStaleLease
	}
	run, err := r.config.Workspaces.Run(runID)
	if err != nil {
		return err
	}
	if run.Kind != "evaluation" || run.ReferenceID != ref.ID || run.ReferenceGeneration != ref.Generation {
		return modelstore.ErrStaleLease
	}
	select {
	case r.serial <- struct{}{}:
		defer func() { <-r.serial }()
	case <-ctx.Done():
		return ctx.Err()
	}
	e, err := s.store.GetEvaluation(ctx, evaluationID)
	if err != nil {
		return err
	}
	if e.Manifest.WorkspaceID != run.WorkspaceID {
		return modelstore.ErrNotFound
	}
	if e.Status == "completed" || e.Status == "incomplete" {
		return nil
	}
	if e.Status == "queued" {
		e, err = s.store.ClaimDispatchedEvaluation(ctx, evaluationID, runID, ref.Generation)
	} else {
		e, err = s.store.BindResumedRun(ctx, evaluationID, runID, ref.Generation)
	}
	if err != nil {
		return err
	}
	// Every exit before report commit revokes this lease. Explicit user cancel
	// has already revoked it and therefore makes this cleanup a harmless stale op.
	finished := false
	defer func() {
		if !finished {
			persist, cancel := context.WithTimeout(context.Background(), r.config.Budget.CleanupTimeout)
			defer cancel()
			code := errorCode(returned)
			if r.ctx.Err() != nil || code == "USER_CANCELLED" {
				code = "APP_RESTARTED"
			}
			_ = s.store.InterruptEvaluation(persist, e.ID, runID, e.LeaseGeneration, code)
		}
	}()
	dataset, err := LoadDataset(e.Manifest.Dataset.BenchmarkID)
	if err != nil {
		return err
	}
	if e.Manifest.Source != s.source() {
		return identityError("INSUFFICIENT_EVIDENCE", "runtime provenance differs from frozen manifest")
	}
	if err = s.preflightFrozen(ctx, e, dataset); err != nil {
		if errorCode(err) == "MODEL_DRIFT" {
			if persistErr := s.recordPreflightDrift(ctx, e); persistErr != nil {
				return persistErr
			}
		}
		return err
	}
	scorer, err := NewScorer(r.config.Runner)
	if err != nil {
		return err
	}
	all, err := s.store.ListAttempts(ctx, e.ID)
	if err != nil {
		return err
	}
	latest := map[string]modelstore.Attempt{}
	for _, a := range all {
		latest[a.CaseID+"/"+a.Arm] = a
	}
	selectedRetry := false
	for _, a := range latest {
		if a.LeaseGeneration == e.LeaseGeneration && a.State == "queued" && a.SupersedesReason == "explicit_retry_case" {
			selectedRetry = true
		}
	}
	for index, c := range e.Manifest.Dataset.Cases {
		in, err := s.readInput(ctx, dataset, c)
		if err != nil {
			return err
		}
		refCase := dataset.Cases()[index]
		if refCase.ID() != c.ID {
			return identityError("INSUFFICIENT_EVIDENCE", "case order changed")
		}
		arms := []string{"stable", "candidate"}
		if index%2 == 1 {
			arms[0], arms[1] = arms[1], arms[0]
		}
		for _, arm := range arms {
			previous := latest[c.ID+"/"+arm]
			if selectedRetry && !(previous.LeaseGeneration == e.LeaseGeneration && previous.State == "queued" && previous.SupersedesReason == "explicit_retry_case") {
				continue
			}
			if previous.State == "completed" {
				continue
			}
			if err = ctx.Err(); err != nil {
				return err
			}
			model := e.Manifest.Stable
			if arm == "candidate" {
				model = e.Manifest.Candidate
			}
			attemptID := newEvaluationID(e.ID, c.ID+"/"+arm+fmt.Sprint(previous.AttemptNo+1))
			a := modelstore.Attempt{ID: attemptID, EvaluationID: e.ID, CaseID: c.ID, Arm: arm, AttemptNo: previous.AttemptNo + 1, LeaseGeneration: e.LeaseGeneration, State: "running", Verdict: "not_scored", InputArtifact: c.InputArtifact, Metrics: emptyMetrics()}
			if previous.ID != "" {
				a.SupersedesReason = "explicit_resume_unfinished"
			}
			if selectedRetry {
				a = previous // The control transaction already created this exact lineage.
				a.Metrics = emptyMetrics()
			} else {
				a, err = s.store.AppendAttempt(ctx, a)
				if err != nil {
					return err
				}
			}
			if emit != nil {
				emit("evaluating", "CodeTaskBench case", map[string]any{"evaluation_id": e.ID, "case_id": c.ID, "arm": arm, "attempt_no": a.AttemptNo, "planned": 24, "completed": countCompleted(latest)})
			}
			var outcome modelstore.AttemptOutcome
			if previous.OutputArtifact != "" && previous.Result.InferenceCompleted && !previous.Result.Truncated {
				// A durable formal output is never sent to the provider a second time.
				raw, readErr := s.store.ReadArtifact(ctx, previous.OutputArtifact)
				if readErr != nil {
					return readErr
				}
				outcome = modelstore.AttemptOutcome{State: "running", Verdict: "not_scored", OutputArtifact: previous.OutputArtifact, Result: previous.Result, Metrics: previous.Metrics}
				outcome.Metrics.WarmVerified = false
				if _, err = s.store.CheckpointAttempt(ctx, a.ID, a.LeaseGeneration, outcome); err != nil {
					return err
				}
				outcome, err = s.score(ctx, scorer, refCase, string(raw), outcome)
			} else {
				outcome, err = s.runArm(ctx, e, model, in, a, scorer, refCase)
			}
			persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.config.Budget.CleanupTimeout)
			if err != nil && outcome.State == "" {
				outcome.State = "failed"
				outcome.Verdict = "infra_error"
				outcome.ErrorCode = errorCode(err)
				outcome.Result.ErrorReason = safeReason(err)
			}
			saved, saveErr := s.store.FinalizeAttempt(persist, a.ID, a.LeaseGeneration, outcome)
			cancel()
			if saveErr != nil {
				return saveErr
			}
			latest[c.ID+"/"+arm] = saved
			if err != nil && fatalInference(err) {
				return err
			}
		}
	}
	if err = s.preflightFrozen(ctx, e, dataset); err != nil {
		if errorCode(err) == "MODEL_DRIFT" {
			if persistErr := s.recordPreflightDrift(ctx, e); persistErr != nil {
				return persistErr
			}
		}
		return err
	}
	fresh, err := s.store.GetEvaluation(ctx, e.ID)
	if err != nil {
		return err
	}
	report, err := s.buildReportAt(ctx, e.ID, e.LeaseGeneration, fresh.Revision)
	if err != nil {
		return err
	}
	finished = true
	if emit != nil {
		emit("evaluated", "CodeTaskBench evidence saved", map[string]any{"evaluation_id": e.ID, "report_id": report.ID, "planned": report.Counts.Planned, "scored": report.Counts.Scored, "infra": report.Counts.Infra})
	}
	return nil
}

func (s *Service) recordPreflightDrift(ctx context.Context, e modelstore.Evaluation) error {
	all, err := s.store.ListAttempts(ctx, e.ID)
	if err != nil {
		return err
	}
	c := e.Manifest.Dataset.Cases[0]
	n := 1
	for _, a := range all {
		if a.CaseID == c.ID && a.Arm == "stable" && a.AttemptNo >= n {
			n = a.AttemptNo + 1
		}
	}
	_, err = s.store.AppendAttempt(ctx, modelstore.Attempt{ID: newEvaluationID(e.ID, fmt.Sprintf("preflight-drift-%d", e.LeaseGeneration)), EvaluationID: e.ID, CaseID: c.ID, Arm: "stable", AttemptNo: n, LeaseGeneration: e.LeaseGeneration, State: "failed", Verdict: "infra_error", ErrorCode: "MODEL_DRIFT", InputArtifact: c.InputArtifact, Result: modelstore.Result{ErrorStage: "identity_preflight", ErrorReason: "model identity drift requires a new evaluation"}, Metrics: emptyMetrics(), SupersedesReason: "model_drift_invalidates_evaluation"})
	return err
}
func countCompleted(latest map[string]modelstore.Attempt) int {
	n := 0
	for _, a := range latest {
		if a.State == "completed" {
			n++
		}
	}
	return n
}
func fatalInference(err error) bool {
	if err == nil {
		return false
	}
	// A semantic provider error owns classification of its underlying transport
	// cause. Joined independent phase failures must each be checked, however:
	// the first nonfatal inference failure cannot hide unsafe cleanup/storage.
	switch e := err.(type) {
	case interface{ Unwrap() []error }:
		for _, cause := range e.Unwrap() {
			if fatalInference(cause) {
				return true
			}
		}
		return false
	case *providers.EvaluationError:
	case *evalrunner.Error:
	case interface{ Unwrap() error }:
		return fatalInference(e.Unwrap())
	}
	switch errorCode(err) {
	case "MODEL_DRIFT", "OUT_OF_MEMORY", "RESOURCE_EXHAUSTION_UNKNOWN", "RELEASE_UNCONFIRMED", "MODEL_BUSY", "FOREGROUND_PREEMPTED", "STORAGE_UNAVAILABLE", "ARTIFACT_MISSING", "USER_CANCELLED", "APP_RESTARTED":
		return true
	}
	return errors.Is(err, gateway.ErrForegroundPreempted)
}

func (s *Service) runArm(ctx context.Context, e modelstore.Evaluation, m modelstore.ModelRevision, in frozenInput, a modelstore.Attempt, scorer *Scorer, ref CaseRef) (out modelstore.AttemptOutcome, returned error) {
	r := s.runtime
	out = modelstore.AttemptOutcome{State: "running", Verdict: "not_scored", Metrics: emptyMetrics()}
	persist := func(v any, kind string) error {
		p, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.config.Budget.CleanupTimeout)
		defer cancel()
		id, err := s.publish(p, v)
		if err != nil {
			return err
		}
		out.Result.EvidenceArtifacts = append(out.Result.EvidenceArtifacts, id)
		if kind == "warmup" {
			out.Metrics.WarmupArtifact = id
		}
		_, err = s.store.CheckpointAttempt(p, a.ID, a.LeaseGeneration, out)
		return err
	}
	fail := func(err error) (modelstore.AttemptOutcome, error) {
		if errorCode(err) == "USER_CANCELLED" && ctx.Err() != nil {
			p, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.config.Budget.CleanupTimeout)
			current, readErr := s.store.GetEvaluation(p, e.ID)
			cancel()
			if readErr != nil || !current.CancelRequested {
				err = identityError("APP_RESTARTED", "evaluation context stopped without durable user cancellation")
			}
		}
		out.State = "failed"
		out.Verdict = "infra_error"
		out.ErrorCode = errorCode(err)
		out.Result.ErrorStage = "inference"
		out.Result.ErrorReason = safeReason(err)
		if errors.Is(err, context.Canceled) || errors.Is(err, gateway.ErrForegroundPreempted) || errorCode(err) == "FOREGROUND_PREEMPTED" || errorCode(err) == "APP_RESTARTED" {
			out.State = "interrupted"
			out.Verdict = "not_scored"
		}
		return out, err
	}
	if err := r.verify(ctx, m); err != nil {
		return fail(err)
	}
	options, timeout, err := evaluationOptions(e.Manifest.Profile)
	if err != nil {
		return fail(err)
	}
	req := providers.EvaluationRequest{ModelName: m.ModelName, ExpectedDigest: m.Digest, ExpectedMetadataHash: m.MetadataHash, ExpectedEngineVersion: m.EngineVersion, ExpectedSourceFingerprint: m.EndpointFingerprint, AttemptID: a.ID, Kind: "evaluation", Messages: []providers.Message{{Role: "user", Content: in.Context.Prompt}}, Options: options, Timeout: timeout}
	req.Options.KeepAlive = "0s"
	if err = persist(phase("planned_formal", &req, nil, nil, nil, time.Now()), ""); err != nil {
		return fail(err)
	}
	session, release, err := r.begin(ctx)
	if err != nil {
		return fail(err)
	}
	closed := false
	closeSession := func() error {
		if !closed {
			release()
			closed = true
		}
		// Gateway close freezes the winning cancellation cause. Observe after
		// close so foreground arriving at the end of cleanup is not lost;
		// intentional normal cancellation is not foreground preemption.
		return sessionError(session, nil)
	}
	// Bounded detached cleanup keeps the owned Gateway reservation until the
	// daemon has confirmed release, even if foreground cancelled the session.
	owned := false
	cleanupOwned := func() error {
		if !owned {
			return nil
		}
		owned = false
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(session), r.config.Budget.CleanupTimeout)
		defer cancel()
		start := time.Now()
		snapshot, cleanupErr := r.release(cleanup, m.GatewayModelID, req)
		recordErr := persist(phase("release", &req, nil, &snapshot, cleanupErr, start), "")
		if recordErr != nil {
			recordErr = errors.Join(identityError("STORAGE_UNAVAILABLE", "release evidence could not be persisted"), recordErr)
			out.Metrics.WarmVerified = false
			out.State = "failed"
			out.Verdict = "infra_error"
			out.ErrorCode = "STORAGE_UNAVAILABLE"
		}
		if cleanupErr != nil {
			out.Metrics.WarmVerified = false
			out.State = "failed"
			out.Verdict = "infra_error"
			out.ErrorCode = "RELEASE_UNCONFIRMED"
			out.Result.ErrorStage = "release"
			out.Result.ErrorReason = "owned model release could not be confirmed"
			return errors.Join(identityError("RELEASE_UNCONFIRMED", "owned model release could not be confirmed"), recordErr)
		}
		return recordErr
	}
	defer func() {
		cleanupErr := cleanupOwned()
		closeErr := closeSession()
		if closeErr != nil {
			out, _ = fail(closeErr)
		}
		returned = errors.Join(returned, cleanupErr, closeErr)
	}()
	start := time.Now()
	before, err := r.inspect(session, m.GatewayModelID, req)
	err = sessionError(session, err)
	if recordErr := persist(phase("initial_residency", &req, nil, &before, err, start), ""); recordErr != nil {
		return fail(recordErr)
	}
	if err != nil {
		return fail(err)
	}
	if before.TargetResident || len(before.Models) > 0 {
		return fail(identityError("MODEL_BUSY", "preexisting model residency is shared or externally owned"))
	}
	warm := req
	warm.Kind = "warmup"
	warm.Messages = nil
	warm.Options.MaxOutputTokens = new(int)
	*warm.Options.MaxOutputTokens = 1
	warm.Options.KeepAlive = "30s"
	if err = persist(phase("planned_warmup", &warm, nil, nil, nil, time.Now()), ""); err != nil {
		return fail(err)
	}
	owned = true
	start = time.Now()
	warmed, warmErr := r.infer(session, m.GatewayModelID, warm)
	warmErr = sessionError(session, warmErr)
	out.Metrics.WarmupCost = observedMetric(float64(time.Since(start)), "ns", "host_monotonic")
	if err = persist(phase("warmup", &warm, warmed, nil, warmErr, start), "warmup"); err != nil {
		return fail(err)
	}
	if warmErr != nil {
		return fail(warmErr)
	}
	if warmed == nil {
		return fail(identityError("INSUFFICIENT_EVIDENCE", "warmup terminal response missing"))
	}
	start = time.Now()
	resident, err := r.inspect(session, m.GatewayModelID, req)
	err = sessionError(session, err)
	if recordErr := persist(phase("warm_residency", &req, nil, &resident, err, start), ""); recordErr != nil {
		return fail(recordErr)
	}
	if err != nil {
		return fail(err)
	}
	if !resident.TargetResident {
		return fail(identityError("INSUFFICIENT_EVIDENCE", "warm target digest was not observed resident"))
	}
	out.Metrics.WarmVerified = true
	start = time.Now()
	response, inferErr := r.infer(session, m.GatewayModelID, req)
	inferErr = sessionError(session, inferErr)
	out.Result.InferenceCompleted = response != nil && inferErr == nil && !response.Truncated && response.DoneReason != "length"
	out.Metrics = responseMetrics(out.Metrics, response)
	if response != nil {
		out.Result.Truncated = response.Truncated || response.DoneReason == "length"
		if response.Output != "" {
			out.OutputArtifact, err = s.publishSource(context.WithoutCancel(ctx), response.Output)
			if err != nil {
				return fail(err)
			}
		}
	}
	if err = persist(phase("formal", &req, response, nil, inferErr, start), ""); err != nil {
		return fail(err)
	}
	if inferErr != nil {
		return fail(inferErr)
	}
	if response == nil {
		return fail(identityError("INSUFFICIENT_EVIDENCE", "formal response missing"))
	}
	// Release the model/admission window before potentially lengthy compilation.
	if err = cleanupOwned(); err != nil {
		return fail(err)
	}
	if err = closeSession(); err != nil {
		return fail(err)
	}
	if out.Result.Truncated {
		out.State = "completed"
		out.Verdict = "invalid_output"
		out.ErrorCode = "INVALID_OUTPUT"
		out.Result.ErrorStage = "output"
		out.Result.ErrorReason = "provider marked output truncated"
		if out.OutputArtifact == "" {
			out.OutputArtifact, err = s.publishSource(context.WithoutCancel(ctx), response.Output)
		}
		return out, err
	}
	out, err = s.score(ctx, scorer, ref, response.Output, out)
	return out, err
}

func sessionError(ctx context.Context, err error) error {
	if errors.Is(context.Cause(ctx), gateway.ErrForegroundPreempted) {
		return identityError("FOREGROUND_PREEMPTED", "foreground request interrupted evaluation; explicit resume required")
	}
	return err
}
func (s *Service) score(ctx context.Context, scorer *Scorer, ref CaseRef, source string, out modelstore.AttemptOutcome) (modelstore.AttemptOutcome, error) {
	result, scoreErr := scorer.Score(ctx, ref, source)
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.runtime.config.Budget.CleanupTimeout)
	defer cancel()
	id, err := s.publish(persist, result)
	if err != nil {
		return out, err
	}
	out.Result.EvidenceArtifacts = append(out.Result.EvidenceArtifacts, id)
	out.Result.ScorerVersion = result.ScorerVersion
	out.Result.ErrorStage = result.ErrorStage
	out.Result.ErrorReason = result.Reason
	out.ErrorCode = result.ErrorCode
	if out.OutputArtifact == "" {
		out.OutputArtifact, err = s.publishSource(persist, source)
		if err != nil {
			return out, err
		}
	}
	switch result.Status {
	case "passed":
		out.State = "completed"
		out.Verdict = "pass"
	case "cancelled":
		out.State = "interrupted"
		out.Verdict = "not_scored"
	case "infrastructure":
		out.State = "failed"
		out.Verdict = "infra_error"
	default:
		out.State = "completed"
		out.Verdict = "functional_fail"
		switch result.ErrorCode {
		case "INVALID_OUTPUT":
			out.Verdict = "invalid_output"
		case "COMPILE_ERROR":
			out.Verdict = "compile_fail"
		case "RUNNER_TIMEOUT", "OUT_OF_MEMORY", "SANDBOX_VIOLATION":
			out.Verdict = "resource_fail"
		}
	}
	return out, scoreErr
}

func (s *Service) preflightFrozen(ctx context.Context, e modelstore.Evaluation, d *Dataset) error {
	r := s.runtime
	if e.Manifest.Source != s.source() || len(e.Manifest.Dataset.Cases) != 12 || e.Manifest.Dataset.ScorerVersion != ScorerVersion || e.Manifest.Dataset.RunnerVersion != d.Revision().RunnerVersion {
		return identityError("INSUFFICIENT_EVIDENCE", "installed benchmark/scorer/runner differs from frozen evaluation")
	}
	current, err := r.config.CollectEnvironment(ctx)
	if err != nil {
		return err
	}
	current, err = freezeEnvironment(current)
	if err != nil {
		return err
	}
	if !environmentEqual(current, e.Manifest.Environment) {
		return identityError("INSUFFICIENT_EVIDENCE", "environment changed; create a new evaluation")
	}
	if err = r.config.Budget.validate(e.Manifest.Profile); err != nil {
		return err
	}
	for _, model := range []modelstore.ModelRevision{e.Manifest.Stable, e.Manifest.Candidate} {
		if err = r.verify(ctx, model); err != nil {
			return err
		}
	}
	for i, c := range e.Manifest.Dataset.Cases {
		if d.Cases()[i].ID() != c.ID {
			return identityError("INSUFFICIENT_EVIDENCE", "dataset order changed")
		}
		if _, err = s.readInput(ctx, d, c); err != nil {
			return err
		}
	}
	return nil
}

// ResultReference keeps large outputs in the engineering evidence store.
func (s *Service) ResultReference(ctx context.Context, id string) (json.RawMessage, error) {
	e, err := s.store.GetEvaluation(ctx, id)
	if err != nil {
		return nil, err
	}
	report, reportErr := s.store.LatestReport(ctx, id)
	if reportErr != nil && !errors.Is(reportErr, modelstore.ErrNotFound) {
		return nil, reportErr
	}
	return json.Marshal(struct {
		EvaluationID string `json:"evaluation_id"`
		ReportID     string `json:"report_id,omitempty"`
		Status       string `json:"status"`
	}{id, report.ID, e.Status})
}
