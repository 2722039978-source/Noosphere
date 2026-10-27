package modellab

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/noosphere/backend/internal/evalrunner"
	"github.com/noosphere/backend/internal/gateway/providers"
	"github.com/noosphere/backend/internal/modelstore"
	"github.com/noosphere/backend/internal/workspaces"
)

type executionFixture struct {
	mu       sync.Mutex
	registry *registryFixture
	calls    []string
	resident bool
	builds   int
	formal   int
	failCode string
}

func orchestrationLab(t *testing.T) (*Service, *modelstore.Store, *workspaces.Manager, *executionFixture, EvaluationSpec, string) {
	t.Helper()
	lab, store, _, native, path, _ := registryLab(t)
	stable := registerAlpha(t, lab)
	discovery, err := lab.Discover(context.Background(), "source")
	if err != nil {
		t.Fatal(err)
	}
	var candidate modelstore.ModelRevision
	for _, m := range discovery.Models {
		if m.Name == "beta:1" {
			candidate, err = lab.Register(context.Background(), "source", m.Name, m.MetadataHash)
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	p, err := lab.SaveProfile(context.Background(), testProfile())
	if err != nil {
		t.Fatal(err)
	}
	manager, err := workspaces.Open(filepath.Join(t.TempDir(), "workspace.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(runtime.GOROOT(), "bin", "go")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	runner, err := evalrunner.New(evalrunner.Config{GoBinary: binary, TempRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err = lab.ConfigureRuntime(RuntimeConfig{Workspaces: manager, Runner: runner, TempParent: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	f := &executionFixture{registry: native}
	r := lab.runtime
	r.fixture = true
	r.build = func(ctx context.Context, d *Dataset, ref CaseRef) (FrozenContext, error) {
		f.mu.Lock()
		f.builds++
		f.mu.Unlock()
		e, _ := d.Evidence(ref)
		pub := PublicContext{File: "cases/" + e.ID + "/faulty.go", Language: "go", CodeLensVersion: "0.6.0", Parser: "tree-sitter", HostFixtureHash: e.FixtureHash}
		raw, _ := json.Marshal(pub)
		analysis := json.RawMessage(`{"fixture":"analysis"}`)
		health := json.RawMessage(`{"fixture":"health"}`)
		prompt := buildPrompt(e, pub)
		return FrozenContext{Public: pub, ContextHash: digest(raw), Prompt: prompt, PromptHash: digest([]byte(prompt)), PromptVersion: PromptVersion, PromptTemplateHash: d.Revision().PromptTemplateHash, DatasetHash: d.Revision().ContentHash, CaseHash: e.CaseHash, AnalysisResponse: analysis, HealthResponse: health, AnalysisResponseHash: digest(analysis), HealthResponseHash: digest(health)}, nil
	}
	r.begin = func(ctx context.Context) (context.Context, func(), error) { return ctx, func() {}, nil }
	r.inspect = func(context.Context, string, providers.EvaluationRequest) (providers.ModelResidencySnapshot, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return providers.ModelResidencySnapshot{TargetResident: f.resident}, nil
	}
	r.release = func(context.Context, string, providers.EvaluationRequest) (providers.ModelResidencySnapshot, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.resident = false
		return providers.ModelResidencySnapshot{}, nil
	}
	r.infer = func(ctx context.Context, id string, req providers.EvaluationRequest) (*providers.EvaluationResponse, error) {
		f.mu.Lock()
		f.calls = append(f.calls, req.Kind+":"+req.ModelName+":"+req.AttemptID)
		f.resident = true
		code := f.failCode
		if req.Kind != "warmup" {
			f.formal++
		}
		f.mu.Unlock()
		if code != "" {
			return nil, identityError(code, "injected provider failure")
		}
		out := ""
		if req.Kind != "warmup" {
			d, _ := LoadDataset(DatasetID)
			for _, ref := range d.Cases() {
				e, _ := d.Evidence(ref)
				if strings.Contains(req.Messages[0].Content, "Task: "+e.Task+"\n") {
					out = e.Reference
					break
				}
			}
			if out == "" {
				return nil, errors.New("unexpected fixture prompt")
			}
		}
		return &providers.EvaluationResponse{Output: out, RawResponse: json.RawMessage(`{"done":true,"fixture":true}`), WallDuration: time.Millisecond, LoadDurationNS: modelstore.Int(10), EvalCount: modelstore.Int(1), EvalDurationNS: modelstore.Int(50), RequestedOptions: req.Options}, nil
	}
	manager.RegisterReferenced("evaluation", "background", func(ctx context.Context, ws workspaces.Workspace, raw json.RawMessage, emit func(string, string, any)) (any, error) {
		var in struct {
			EvaluationID string `json:"evaluation_id"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		err := lab.Execute(ctx, in.EvaluationID, workspaces.RunID(ctx), emit)
		return map[string]string{"evaluation_id": in.EvaluationID}, err
	}, func(ctx context.Context, c workspaces.Cancellation) error { return lab.CancelRun(ctx, c) })
	t.Cleanup(func() { lab.Stop(); manager.Close(); lab.Close() })
	return lab, store, manager, f, EvaluationSpec{StableRevisionID: stable.ID, CandidateRevisionID: candidate.ID, ProfileID: p.ID, IdempotencyKey: "fixture-full"}, path
}

// Only context construction and provenance are scripted. The runtime keeps the
// real Gateway admission, identity checks and native HTTP lifecycle throughout.
func TestServiceGatewayNativeLifecycle(t *testing.T) {
	lab, store, _, fixture, spec, _ := orchestrationLab(t)
	native := &nativeLifecycleFixture{}
	fixture.registry.mu.Lock()
	fixture.registry.native = native
	fixture.registry.mu.Unlock()
	r := lab.runtime
	r.begin = lab.gateway.BeginEvaluationSession
	r.infer = lab.gateway.Evaluate
	r.inspect = lab.gateway.InspectEvaluationResidency
	r.release = lab.gateway.ReleaseEvaluationModel
	e, err := lab.CreateEvaluation(context.Background(), "local", spec)
	if err != nil {
		t.Fatal(err)
	}
	e = awaitEvaluation(t, store, e.ID)
	attempts, err := store.ListAttempts(context.Background(), e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != "completed" || e.Manifest.Source != "offline_fixture" || len(attempts) != 24 {
		t.Fatalf("actual Gateway lifecycle did not complete: status=%s source=%s attempts=%d", e.Status, e.Manifest.Source, len(attempts))
	}
	ctx := context.Background()
	report, err := store.LatestReport(ctx, e.ID)
	if err != nil || report.Counts.Scored != 24 || report.Counts.Fail != 24 || report.Counts.Infra != 0 || report.Counts.Pass != 0 || report.Counts.PairedComplete != 12 {
		t.Fatalf("invalid-source report: %+v %v", report.Counts, err)
	}
	for _, a := range attempts {
		if a.State != "completed" || a.Verdict != "invalid_output" || !a.Result.InferenceCompleted || !a.Metrics.WarmVerified || a.Metrics.WarmupCost.Value == nil || *a.Metrics.WarmupCost.Value < 0 || a.Metrics.Tokens.Value == nil || *a.Metrics.Tokens.Value != 1 || a.Metrics.LoadNS.Value == nil || *a.Metrics.LoadNS.Value != 5 || a.Metrics.AvailableVRAM.Value != nil {
			t.Fatalf("native lifecycle evidence lost: %+v", a)
		}
		raw, err := store.ReadArtifact(ctx, a.OutputArtifact)
		if err != nil || string(raw) != nativeInvalidSource {
			t.Fatalf("formal output not preserved: %q %v", raw, err)
		}
		kinds := map[string]phaseEvidence{}
		var score ScoreResult
		for _, id := range a.Result.EvidenceArtifacts {
			raw, err := store.ReadArtifact(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			var p phaseEvidence
			if err = json.Unmarshal(raw, &p); err != nil {
				t.Fatal(err)
			}
			if p.Kind != "" {
				kinds[p.Kind] = p
			} else if err = json.Unmarshal(raw, &score); err != nil {
				t.Fatal(err)
			}
		}
		for _, kind := range []string{"planned_formal", "initial_residency", "planned_warmup", "warmup", "warm_residency", "formal", "release"} {
			if _, ok := kinds[kind]; !ok {
				t.Fatalf("missing persisted %s phase", kind)
			}
		}
		if score.ErrorCode != "INVALID_OUTPUT" || score.StaticValid || score.CompileSucceeded || len(score.Assertions) != 0 {
			t.Fatalf("invalid source reached compiler/assertions: %+v", score)
		}
		if kinds["initial_residency"].Residency.TargetResident || !kinds["warm_residency"].Residency.TargetResident || kinds["release"].Residency.TargetResident || kinds["release"].ErrorCode != "" {
			t.Fatal("residency/release evidence is inconsistent")
		}
		for _, kind := range []string{"warmup", "formal"} {
			p := kinds[kind]
			if p.Response == nil || len(p.Response.RawResponse) == 0 || p.Request == nil || p.Request.ExpectedSourceFingerprint == "" || p.Response.IdentityBefore.Digest != p.Request.ExpectedDigest || p.Response.IdentityAfter.MetadataHash != p.Request.ExpectedMetadataHash || p.Response.IdentityAfter.EngineVersion != p.Request.ExpectedEngineVersion || p.Response.RequestedOptions.KeepAlive != p.Request.Options.KeepAlive {
				t.Fatalf("missing real native identity/raw/options evidence: %+v", p)
			}
		}
	}
	native.mu.Lock()
	defer native.mu.Unlock()
	if len(native.events) != 24*8 || len(native.prompts) != 24 || native.resident != "" {
		t.Fatalf("hidden retry or leaked model: events=%d prompts=%d resident=%q", len(native.events), len(native.prompts), native.resident)
	}
	for i, c := range e.Manifest.Dataset.Cases {
		names := []string{"alpha:1", "beta:1"}
		if i%2 == 1 {
			names[0], names[1] = names[1], names[0]
		}
		var input frozenInput
		raw, err := store.ReadArtifact(ctx, c.InputArtifact)
		if err != nil || json.Unmarshal(raw, &input) != nil {
			t.Fatal("frozen context artifact unreadable", err)
		}
		for arm, name := range names {
			want := []string{"ps:empty", "ps:empty", "warmup:" + name, "ps:" + name, "formal:" + name, "ps:" + name, "release:" + name, "ps:empty"}
			position := i*2 + arm
			for j, event := range want {
				if got := native.events[position*8+j]; got != event {
					t.Fatalf("native arm %d phase %d: got=%s want=%s", position, j, got, event)
				}
			}
			if native.prompts[position] != input.Context.Prompt {
				t.Fatal("native formal prompt differs from shared frozen input")
			}
		}
	}
	// A fresh reservation after all 24 arms proves the real admission capability
	// was released. The fresh session cannot unload the previous arm's model.
	deadline, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	session, release, err := lab.gateway.BeginEvaluationSession(deadline)
	if err != nil {
		t.Fatal("Gateway admission leaked", err)
	}
	defer release()
	m := e.Manifest.Stable
	_, err = lab.gateway.ReleaseEvaluationModel(session, m.GatewayModelID, providers.EvaluationRequest{ModelName: m.ModelName, ExpectedDigest: m.Digest, ExpectedMetadataHash: m.MetadataHash, ExpectedEngineVersion: m.EngineVersion, ExpectedSourceFingerprint: m.EndpointFingerprint})
	if errorCode(err) != "MODEL_BUSY" {
		t.Fatalf("fresh session inherited another arm's ownership: %v", err)
	}
	t.Logf("actual Service->Gateway->native HTTP: source=%s pairs=12 warmup=24 formal=24 owned_unload=24 ps=120 invalid_output=24 compiler_jobs=0", e.Manifest.Source)
}
func awaitEvaluation(t *testing.T, s *modelstore.Store, id string) modelstore.Evaluation {
	t.Helper()
	deadline := time.Now().Add(180 * time.Second)
	for time.Now().Before(deadline) {
		e, err := s.GetEvaluation(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if e.Status != "queued" && e.Status != "running" {
			return e
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("evaluation did not finish")
	return modelstore.Evaluation{}
}
func TestSameManifestAndAlternatingSerialArms(t *testing.T) {
	lab, store, manager, f, spec, path := orchestrationLab(t)
	ctx := context.Background()
	e, err := lab.CreateEvaluation(ctx, "local", spec)
	if err != nil {
		t.Fatal(err)
	}
	e = awaitEvaluation(t, store, e.ID)
	if e.Status != "completed" || e.Manifest.Source != "offline_fixture" {
		t.Fatalf("unexpected completion %+v", e)
	}
	report, err := store.LatestReport(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts.Scored != 24 || report.Counts.PairedComplete != 12 || report.Counts.Pass != 24 {
		t.Fatalf("host report %+v", report.Counts)
	}
	attempts, err := store.ListAttempts(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 24 {
		t.Fatal(len(attempts))
	}
	byCase := map[string]string{}
	for _, a := range attempts {
		if !a.Metrics.WarmVerified || a.Metrics.WarmupArtifact == "" || a.OutputArtifact == "" {
			t.Fatalf("missing native lifecycle evidence %+v", a)
		}
		if old := byCase[a.CaseID]; old != "" && old != a.InputArtifact {
			t.Fatal("arm input differs")
		}
		byCase[a.CaseID] = a.InputArtifact
		for _, id := range append(a.Result.EvidenceArtifacts, a.InputArtifact, a.OutputArtifact, a.Metrics.WarmupArtifact) {
			if _, err := store.ReadArtifact(ctx, id); err != nil {
				t.Fatal(err)
			}
		}
	}
	f.mu.Lock()
	if f.builds != 12 || f.formal != 24 || len(f.calls) != 48 {
		t.Fatalf("builds=%d formal=%d calls=%d", f.builds, f.formal, len(f.calls))
	}
	for i := 0; i < 12; i++ {
		names := []string{"alpha:1", "beta:1"}
		if i%2 == 1 {
			names[0], names[1] = names[1], names[0]
		}
		for j, name := range names {
			offset := i*4 + j*2
			if !strings.HasPrefix(f.calls[offset], "warmup:"+name+":") || !strings.HasPrefix(f.calls[offset+1], "evaluation:"+name+":") {
				t.Fatalf("order %d: %v", i, f.calls)
			}
		}
	}
	f.mu.Unlock()
	duplicate, err := lab.CreateEvaluation(ctx, "local", spec)
	if err != nil || duplicate.ID != e.ID {
		t.Fatal("idempotent create", err)
	}
	f.mu.Lock()
	if f.builds != 12 || f.formal != 24 {
		t.Fatal("idempotency replay")
	}
	f.mu.Unlock()
	t.Logf("trace evaluation=%s manifest=%s cases=12 formal=24 warmup=24 status=%s report=%s", e.ID, e.ManifestHash, e.Status, report.ID)
	lab.Stop()
	if err = manager.Close(); err != nil {
		t.Fatal(err)
	}
	if err = lab.Close(); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := modelstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	saved, err := reopened.GetEvaluation(ctx, e.ID)
	if err != nil || saved.ManifestHash != e.ManifestHash {
		t.Fatal("reopen manifest", err)
	}
	if _, err = reopened.GetReport(ctx, report.ID); err != nil {
		t.Fatal(err)
	}
}
