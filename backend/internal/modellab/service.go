package modellab

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"

	"github.com/noosphere/backend/internal/codelens"
	"github.com/noosphere/backend/internal/evalrunner"
	"github.com/noosphere/backend/internal/gateway"
	"github.com/noosphere/backend/internal/gateway/providers"
	"github.com/noosphere/backend/internal/modelstore"
	"github.com/noosphere/backend/internal/workspaces"
)

type RuntimeConfig struct {
	Workspaces         *workspaces.Manager
	ContextAdapter     func() *codelens.Adapter
	Runner             *evalrunner.Runner
	TempParent         string
	Budget             Budget
	CollectEnvironment func(context.Context) (modelstore.EnvironmentSnapshot, error)
}
type EvaluationSpec struct {
	StableRevisionID    string `json:"stable_revision_id"`
	CandidateRevisionID string `json:"candidate_revision_id"`
	ProfileID           string `json:"profile_id"`
	DatasetID           string `json:"dataset_id"`
	WorkflowID          string `json:"workflow_id"`
	IdempotencyKey      string `json:"idempotency_key"`
}
type evaluationRuntime struct {
	config           RuntimeConfig
	source           string
	ctx              context.Context
	cancel           context.CancelFunc
	mu               sync.Mutex
	control          sync.Mutex
	closed           bool
	admissionStopped bool
	work             sync.WaitGroup
	serial           chan struct{}
	closeOnce        sync.Once
	closeErr         error
	// These package-private test seams always force fixture provenance.
	fixture bool
	build   func(context.Context, *Dataset, CaseRef) (FrozenContext, error)
	verify  func(context.Context, modelstore.ModelRevision) error
	begin   func(context.Context) (context.Context, func(), error)
	infer   func(context.Context, string, providers.EvaluationRequest) (*providers.EvaluationResponse, error)
	inspect func(context.Context, string, providers.EvaluationRequest) (providers.ModelResidencySnapshot, error)
	release func(context.Context, string, providers.EvaluationRequest) (providers.ModelResidencySnapshot, error)
}
type frozenInput struct {
	SchemaVersion   int           `json:"schema_version"`
	Case            CaseEvidence  `json:"case"`
	FaultySource    string        `json:"faulty_source"`
	Context         FrozenContext `json:"context"`
	Budget          Budget        `json:"budget"`
	LoadingProtocol string        `json:"loading_protocol"`
}

func (s *Service) ConfigureRuntime(c RuntimeConfig) error {
	if s.runtime != nil {
		return identityError("INVALID_REQUEST", "evaluation runtime already configured")
	}
	if s.store == nil || s.gateway == nil || c.Workspaces == nil {
		return identityError("INVALID_REQUEST", "model store, Gateway and Workspace manager required")
	}
	b, err := c.Budget.normalized()
	if err != nil {
		return err
	}
	c.Budget = b
	if c.CollectEnvironment == nil {
		c.CollectEnvironment = func(context.Context) (modelstore.EnvironmentSnapshot, error) {
			return modelstore.EnvironmentSnapshot{OS: runtime.GOOS, Arch: runtime.GOARCH, Cores: modelstore.Int(int64(runtime.NumCPU())), CollectorVersion: "modellab-host-v1", VRAM: unknownMetric("bytes", "physical GPU memory collector unavailable")}, nil
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &evaluationRuntime{config: c, source: "offline_real", ctx: ctx, cancel: cancel, serial: make(chan struct{}, 1)}
	r.begin = s.gateway.BeginEvaluationSession
	r.infer = s.gateway.Evaluate
	r.inspect = s.gateway.InspectEvaluationResidency
	r.release = s.gateway.ReleaseEvaluationModel
	r.verify = s.verifyModel
	s.runtime = r
	return nil
}
func (s *Service) runtimeReady() (*evaluationRuntime, error) {
	r := s.runtime
	if r == nil {
		return nil, identityError("INSUFFICIENT_EVIDENCE", "evaluation runtime unavailable")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, identityError("APP_RESTARTED", "evaluation runtime shutting down")
	}
	if r.config.Runner == nil {
		return nil, identityError("INSUFFICIENT_EVIDENCE", "trusted Go/WASI runner unavailable")
	}
	return r, nil
}
func (s *Service) operation(ctx context.Context) (context.Context, func(), error) {
	r, err := s.runtimeReady()
	if err != nil {
		return nil, nil, err
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, nil, context.Canceled
	}
	r.work.Add(1)
	r.mu.Unlock()
	child, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(r.ctx, cancel)
	return child, func() { stop(); cancel(); r.work.Done() }, nil
}
func (s *Service) source() string {
	if s.runtime.fixture {
		return "offline_fixture"
	}
	return "offline_real"
}
func newEvaluationID(ws, key string) string {
	if key != "" {
		return "eval_" + digest([]byte(ws + "\x00" + key))[:32]
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return "eval_" + hex.EncodeToString(b[:])
}
func (s *Service) publish(ctx context.Context, v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	a, err := s.store.PublishArtifact(ctx, b, modelstore.ArtifactMetadata{MediaType: "application/json", Provenance: s.source()})
	return a.ID, err
}
func (s *Service) publishSource(ctx context.Context, source string) (string, error) {
	a, err := s.store.PublishArtifact(ctx, []byte(source), modelstore.ArtifactMetadata{MediaType: "text/x-go", Provenance: s.source()})
	return a.ID, err
}
func normalizeSpec(spec EvaluationSpec) EvaluationSpec {
	if spec.DatasetID == "" {
		spec.DatasetID = DatasetID
	}
	if spec.WorkflowID == "" {
		spec.WorkflowID = "codelens.codetask.go.v1"
	}
	return spec
}
func sameSpec(e modelstore.Evaluation, spec EvaluationSpec) bool {
	return e.Manifest.Stable.ID == spec.StableRevisionID && e.Manifest.Candidate.ID == spec.CandidateRevisionID && e.Manifest.Profile.ID == spec.ProfileID && e.Manifest.Dataset.BenchmarkID == spec.DatasetID && e.Manifest.WorkflowID == spec.WorkflowID
}

func (s *Service) CreateEvaluation(ctx context.Context, workspaceID string, spec EvaluationSpec) (modelstore.Evaluation, error) {
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
	spec = normalizeSpec(spec)
	if len(spec.IdempotencyKey) > 128 || len(spec.WorkflowID) > 128 {
		return empty, modelstore.ErrInvalid
	}
	if _, err = r.config.Workspaces.Workspace(workspaceID); err != nil {
		return empty, err
	}
	id := newEvaluationID(workspaceID, spec.IdempotencyKey)
	if spec.IdempotencyKey != "" {
		if old, e := s.store.GetEvaluation(ctx, id); e == nil {
			if !sameSpec(old, spec) {
				return empty, modelstore.ErrIdempotencyConflict
			}
			return old, nil
		} else if !errors.Is(e, modelstore.ErrNotFound) {
			return empty, e
		}
	}
	dataset, err := LoadDataset(spec.DatasetID)
	if err != nil {
		return empty, err
	}
	stable, err := s.GetModel(ctx, spec.StableRevisionID)
	if err != nil {
		return empty, err
	}
	candidate, err := s.GetModel(ctx, spec.CandidateRevisionID)
	if err != nil {
		return empty, err
	}
	if stable.ID == candidate.ID {
		return empty, modelstore.ErrInvalid
	}
	for _, model := range []modelstore.ModelRevision{stable, candidate} {
		if err = r.verify(ctx, model); err != nil {
			return empty, err
		}
	}
	profile, err := s.GetProfile(ctx, spec.ProfileID)
	if err != nil {
		return empty, err
	}
	if err = r.config.Budget.validate(profile); err != nil {
		return empty, err
	}
	env, err := r.config.CollectEnvironment(ctx)
	if err != nil {
		return empty, err
	}
	if s.source() == "offline_real" && strings.TrimSpace(env.ToolchainVersion) == "" {
		return empty, identityError("INSUFFICIENT_EVIDENCE", "trusted Go toolchain version was not collected")
	}
	env, err = freezeEnvironment(env)
	if err != nil {
		return empty, err
	}
	build := r.build
	var builder *ContextBuilder
	if build == nil {
		if r.config.ContextAdapter == nil || r.config.ContextAdapter() == nil {
			return empty, identityError("CODELENS_UNAVAILABLE", "CodeLens worker is not ready")
		}
		builder, err = NewContextBuilder(r.config.ContextAdapter(), r.config.TempParent)
		if err != nil {
			return empty, err
		}
		defer builder.Close()
		build = builder.BuildContext
	}
	revision := dataset.Revision()
	contexts := []string{}
	for i, ref := range dataset.Cases() {
		e, err := dataset.Evidence(ref)
		if err != nil {
			return empty, err
		}
		frozen, err := build(ctx, dataset, ref)
		if err != nil {
			return empty, identityError("CODELENS_UNAVAILABLE", "CodeLens context could not be frozen")
		}
		if err = validateFrozen(dataset, ref, frozen); err != nil {
			return empty, err
		}
		in := frozenInput{SchemaVersion: 1, Case: e, FaultySource: e.Faulty, Context: frozen, Budget: r.config.Budget, LoadingProtocol: LoadingProtocol}
		inputID, err := s.publish(ctx, in)
		if err != nil {
			return empty, err
		}
		fixtureID, err := s.publishSource(ctx, e.Faulty)
		if err != nil {
			return empty, err
		}
		revision.Cases[i].InputArtifact = inputID
		revision.Cases[i].FixtureArtifact = fixtureID
		contexts = append(contexts, frozen.ContextHash)
		revision.CodeLensVersion = frozen.Public.CodeLensVersion
	}
	raw, _ := json.Marshal(contexts)
	revision.ContextHash = digest(raw)
	revision.ID = ""
	revision.ContentHash = ""
	raw, _ = json.Marshal(revision)
	revision.ContentHash = digest(raw)
	revision.ID = DatasetID + "." + revision.ContentHash[:24]
	manifest := modelstore.Manifest{SchemaVersion: 1, EncoderVersion: modelstore.EncoderVersion, WorkspaceID: workspaceID, WorkflowID: spec.WorkflowID, Source: s.source(), Stable: stable, Candidate: candidate, Profile: profile, Dataset: revision, Environment: env, MemoryMode: "not_applicable", ToolRegistryVersion: "not_applicable"}
	key := spec.IdempotencyKey
	if key == "" {
		key = id
	}
	ev, err := s.store.CreateEvaluation(ctx, modelstore.CreateEvaluationRequest{ID: id, Manifest: manifest, Idempotency: modelstore.Idempotency{Key: key}})
	if err != nil {
		return empty, err
	}
	return s.dispatch(ctx, ev)
}
func (s *Service) dispatch(ctx context.Context, e modelstore.Evaluation) (modelstore.Evaluation, error) {
	if err := s.checkAdmission(); err != nil {
		p, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.runtime.config.Budget.CleanupTimeout)
		defer cancel()
		return e, errors.Join(err, s.store.InterruptEvaluation(p, e.ID, "", e.LeaseGeneration, "APP_RESTARTED"))
	}
	input, _ := json.Marshal(struct {
		EvaluationID string `json:"evaluation_id"`
	}{e.ID})
	run, err := s.runtime.config.Workspaces.SubmitReferenced(e.Manifest.WorkspaceID, "evaluation", "CodeTaskBench paired evaluation", workspaces.TaskReference{ID: e.ID, Generation: e.LeaseGeneration}, input)
	if err != nil {
		persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.runtime.config.Budget.CleanupTimeout)
		defer cancel()
		mark := s.store.InterruptEvaluation(persistCtx, e.ID, "", e.LeaseGeneration, "DISPATCH_INTERRUPTED")
		return e, errors.Join(err, mark)
	}
	_ = run
	return s.store.GetEvaluation(ctx, e.ID)
}

func (s *Service) checkAdmission() error {
	r := s.runtime
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.admissionStopped {
		return identityError("APP_RESTARTED", "evaluation admission stopped")
	}
	return nil
}
func freezeEnvironment(e modelstore.EnvironmentSnapshot) (modelstore.EnvironmentSnapshot, error) {
	e.ID = ""
	raw, err := json.Marshal(e)
	if err != nil {
		return e, err
	}
	if e.OS == "" || e.Arch == "" || e.CollectorVersion == "" {
		return e, identityError("INSUFFICIENT_EVIDENCE", "environment identity missing")
	}
	e.ID = "env_" + digest(raw)[:32]
	return e, nil
}
func (s *Service) verifyModel(ctx context.Context, m modelstore.ModelRevision) error {
	if m.IdentityLevel != "digest" || m.MetadataHash == "" || m.EndpointFingerprint == "" || m.EngineVersion == "" {
		return identityError("INSUFFICIENT_EVIDENCE", "exact local registered model identity required")
	}
	fingerprint, err := s.gateway.EvaluationSourceFingerprint(m.GatewayModelID)
	if err != nil {
		return err
	}
	if fingerprint != m.EndpointFingerprint {
		return identityError("MODEL_DRIFT", "configured model source changed")
	}
	d, err := s.gateway.DiscoverEvaluationModels(ctx, m.GatewayModelID)
	if err != nil {
		return err
	}
	for _, actual := range d.Models {
		if actual.Name == m.ModelName && actual.LocalVerified && actual.Digest == m.Digest && actual.MetadataHash == m.MetadataHash && actual.EngineVersion == m.EngineVersion {
			return nil
		}
	}
	return identityError("MODEL_DRIFT", "registered model identity changed or is unavailable")
}
func validateFrozen(d *Dataset, ref CaseRef, c FrozenContext) error {
	e, err := d.Evidence(ref)
	if err != nil {
		return err
	}
	pub, _ := json.Marshal(c.Public)
	if c.DatasetHash != d.Revision().ContentHash || c.CaseHash != e.CaseHash || c.ContextHash != digest(pub) || c.PromptHash != digest([]byte(c.Prompt)) || c.Prompt != buildPrompt(e, c.Public) || c.PromptTemplateHash != d.Revision().PromptTemplateHash || c.Public.HostFixtureHash != e.FixtureHash || c.AnalysisResponseHash != digest(c.AnalysisResponse) || c.HealthResponseHash != digest(c.HealthResponse) || c.Public.CodeLensVersion != "0.6.0" {
		return identityError("INSUFFICIENT_EVIDENCE", "frozen CodeLens or prompt evidence mismatch")
	}
	return nil
}
func (s *Service) readInput(ctx context.Context, d *Dataset, c modelstore.Case) (frozenInput, error) {
	var in frozenInput
	raw, err := s.store.ReadArtifact(ctx, c.InputArtifact)
	if err != nil {
		return in, err
	}
	if err = strictJSON(raw, &in); err != nil {
		return in, err
	}
	var ref CaseRef
	for _, x := range d.Cases() {
		if x.ID() == c.ID {
			ref = x
		}
	}
	if err = validateFrozen(d, ref, in.Context); err != nil {
		return in, err
	}
	e, err := d.Evidence(ref)
	if err != nil {
		return in, err
	}
	fixture, err := s.store.ReadArtifact(ctx, c.FixtureArtifact)
	if err != nil {
		return in, err
	}
	expected, _ := json.Marshal(e.Assertions)
	// Audit DTOs never replace authoritative dataset assertions.
	if in.SchemaVersion != 1 || in.LoadingProtocol != LoadingProtocol || in.FaultySource != e.Faulty || !bytes.Equal(fixture, []byte(e.Faulty)) || c.FixtureHash != e.FixtureHash || c.Expected != string(expected) || in.Case.ID != c.ID || in.Case.CaseHash != "" {
		return in, identityError("INSUFFICIENT_EVIDENCE", "frozen input differs from installed benchmark")
	}
	if in.Budget != s.runtime.config.Budget {
		return in, identityError("INSUFFICIENT_EVIDENCE", "evaluation budget changed")
	}
	return in, nil
}
func errorCode(err error) string {
	if errors.Is(err, gateway.ErrForegroundPreempted) {
		return "FOREGROUND_PREEMPTED"
	}
	var p *providers.EvaluationError
	if errors.As(err, &p) {
		return p.Code
	}
	var r *evalrunner.Error
	if errors.As(err, &r) {
		return r.Code
	}
	switch {
	case errors.Is(err, context.Canceled):
		return "USER_CANCELLED"
	case errors.Is(err, context.DeadlineExceeded):
		return "INFERENCE_TIMEOUT"
	case errors.Is(err, modelstore.ErrArtifactMissing):
		return "ARTIFACT_MISSING"
	default:
		return "STORAGE_UNAVAILABLE"
	}
}
func safeReason(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%s: evaluation phase failed; retained evidence contains bounded diagnostics", errorCode(err))
}
func environmentEqual(a, b modelstore.EnvironmentSnapshot) bool {
	// Older snapshots never recorded the new inventory. Compare the facts they
	// actually froze, without rewriting their body/hash or treating enrichment
	// as a hardware change. Hardware-aware history remains strictly compared.
	if b.Hardware == nil && a.Hardware != nil {
		a.Hardware = nil
		if b.CPU == "" {
			a.CPU = ""
		}
		if b.GPU == "" {
			a.GPU = ""
		}
		if b.Driver == "" {
			a.Driver = ""
		}
		// Only this old explanation changed as part of adding total-VRAM
		// inventory. Preserve the legacy unknown fact, never a measured value.
		if b.VRAM.Value == nil && a.VRAM.Value == nil && b.VRAM.Unit == "bytes" && a.VRAM.Unit == b.VRAM.Unit && b.VRAM.Source == "unavailable" && a.VRAM.Source == b.VRAM.Source && b.VRAM.Quality == "unknown" && a.VRAM.Quality == b.VRAM.Quality && b.VRAM.MissingReason == "physical GPU/driver/VRAM collection unavailable; daemon residency is recorded separately" && a.VRAM.MissingReason == "physical available GPU memory collection unavailable; total adapter VRAM is recorded separately in hardware inventory" {
			a.VRAM.MissingReason = b.VRAM.MissingReason
		}
		var err error
		a, err = freezeEnvironment(a)
		if err != nil {
			return false
		}
	}
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return bytes.Equal(left, right)
}
func normalizedDigest(s string) string { return strings.TrimPrefix(s, "sha256:") }
