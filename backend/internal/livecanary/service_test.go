package livecanary

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/noosphere/backend/internal/gateway"
	"github.com/noosphere/backend/internal/gateway/secure"
	"github.com/noosphere/backend/internal/middleware"
	"github.com/noosphere/backend/internal/modellab"
	"github.com/noosphere/backend/internal/modelstore"
	"github.com/noosphere/backend/internal/workspaces"
)

// These are real HTTP provider dispatches against protocol fixtures, not model performance evidence.
type liveFixture struct {
	mu            sync.Mutex
	calls         []string
	failCandidate bool
	block         chan struct{}
	entered       chan struct{}
	store         *modelstore.Store
	lab           *modellab.Service
	gateway       *gateway.Gateway
	service       *Service
	spec          CreateSpec
	path          string
	clock         time.Time
}

type liveFixtureSecretStore []byte

func (s liveFixtureSecretStore) Provider() string { return "fixture" }
func (s liveFixtureSecretStore) Load(context.Context) ([]byte, error) {
	return append([]byte(nil), s...), nil
}
func (s liveFixtureSecretStore) Create(context.Context, []byte) error {
	return errors.New("fixture key already exists")
}

func newFixture(t *testing.T) *liveFixture {
	t.Helper()
	f := &liveFixture{clock: time.Now().UTC(), entered: make(chan struct{}, 10)}
	dir := t.TempDir()
	endpoint := func(name, digest string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/api/version":
				json.NewEncoder(w).Encode(map[string]any{"version": "0.20.0-live-fixture"})
			case "/api/tags":
				json.NewEncoder(w).Encode(map[string]any{"models": []any{map[string]any{"name": name, "model": name, "digest": strings.Repeat(digest, 64), "size": 1024, "details": map[string]any{"format": "gguf", "family": "fixture", "parameter_size": "1B", "quantization_level": "Q4_K_M"}}}})
			case "/api/show":
				json.NewEncoder(w).Encode(map[string]any{"template": "{{ .Prompt }}", "parameters": "temperature 0.7\nnum_ctx 8192", "details": map[string]any{"format": "gguf", "family": "fixture", "parameter_size": "1B", "quantization_level": "Q4_K_M"}, "model_info": map[string]any{"general.architecture": "fixture", "fixture.context_length": 8192}, "capabilities": []string{"completion"}})
			case "/api/chat":
				if r.Header.Get("Authorization") != "Bearer fixture-live-key" {
					t.Error("configured provider auth lost")
				}
				var body struct {
					Model    string `json:"model"`
					Messages []struct {
						Content string `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				if body.Model != name || len(body.Messages) != 1 {
					t.Error("wrong explicit model or request context", body)
				}
				f.mu.Lock()
				f.calls = append(f.calls, name)
				fail := f.failCandidate && name == "candidate:1"
				block := f.block
				f.mu.Unlock()
				if block != nil {
					f.entered <- struct{}{}
					select {
					case <-block:
					case <-r.Context().Done():
						return
					}
				}
				if fail {
					w.WriteHeader(503)
					json.NewEncoder(w).Encode(map[string]string{"error": "synthetic provider failure"})
					return
				}
				// Provide a measurable latency even on Windows runners with a
				// coarser monotonic clock. The gate still uses real elapsed time.
				time.Sleep(5 * time.Millisecond)
				json.NewEncoder(w).Encode(map[string]any{"model": name, "message": map[string]string{"role": "assistant", "content": "fixture answer from " + name}, "done": true, "done_reason": "stop", "eval_count": 4, "eval_duration": 1000, "total_duration": 2000})
			default:
				w.WriteHeader(404)
			}
		}))
	}
	stable, candidate := endpoint("stable:1", "a"), endpoint("candidate:1", "b")
	t.Cleanup(stable.Close)
	t.Cleanup(candidate.Close)
	configPath := filepath.Join(dir, "models.yaml")
	key, err := secure.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	f.gateway = gateway.NewWithSecretStore(configPath, liveFixtureSecretStore(key))
	for _, entry := range []struct{ id, url string }{{"stable", stable.URL}, {"candidate", candidate.URL}} {
		payload, err := json.Marshal(map[string]any{
			"id": entry.id, "name": entry.id, "provider": "ollama",
			"base_url": entry.url + "/v1", "model_name": entry.id + ":1",
			"api_key": "fixture-live-key", "is_default": entry.id == "stable",
		})
		if err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		f.gateway.HandleAddModel(response, httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(payload)))
		if response.Code != http.StatusCreated {
			t.Fatal("model configuration failed", response.Code, response.Body.String())
		}
	}
	f.gateway.ReloadConfig()
	f.path = filepath.Join(dir, "evidence.sqlite")
	f.store, err = modelstore.Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.store.Close() })
	f.lab = modellab.New(f.store, f.gateway)
	register := func(id string) modelstore.ModelRevision {
		d, e := f.lab.Discover(t.Context(), id)
		if e != nil {
			t.Fatal(e)
		}
		r, e := f.lab.Register(t.Context(), id, d.Models[0].Name, d.Models[0].MetadataHash)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	a, b := register("stable"), register("candidate")
	profile, err := f.lab.SaveProfile(t.Context(), modelstore.InferenceProfile{Requested: modelstore.Options{Temperature: modelstore.Float(0), Seed: modelstore.Int(7), NumCtx: modelstore.Int(256), MaxOutput: modelstore.Int(32), Parallelism: modelstore.Int(1), RetryLimit: modelstore.Int(0)}})
	if err != nil {
		t.Fatal(err)
	}
	f.service, err = New(t.Context(), f.store, f.lab, f.gateway)
	if err != nil {
		t.Fatal(err)
	}
	f.service.now = func() time.Time { return f.clock }
	policy := DefaultPolicy()
	policy.MinimumSamples = 2
	policy.MinimumFeedback = 1
	policy.MaximumLatencyRatio = 10
	f.spec = CreateSpec{Title: "Protocol fixture, not real model evidence", StableID: a.ID, CandidateID: b.ID, ProfileID: profile.ID, TargetBPS: 5000, Policy: policy}
	t.Cleanup(func() { _ = f.service.Close() })
	return f
}
func (f *liveFixture) start(t *testing.T) modelstore.LiveRelease {
	t.Helper()
	r, err := f.service.Create(t.Context(), "local", "create", f.spec)
	if err != nil {
		t.Fatal(err)
	}
	r, err = f.service.Command(t.Context(), "local", r.ID, "start", "start", r.Revision)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func (f *liveFixture) request(t *testing.T, r modelstore.LiveRelease, key string) modelstore.LiveRequest {
	t.Helper()
	result, err := f.service.Request(t.Context(), "local", r.ID, key, "Actual user input to protocol fixture")
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestLiveTwoHTTPProvidersIsolationAndIdempotency(t *testing.T) {
	f := newFixture(t)
	draft, err := f.service.Create(t.Context(), "local", "create", f.spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.Request(t.Context(), "local", draft.ID, "premature", "x"); !errors.Is(err, modelstore.ErrInvalid) {
		t.Fatal("draft dispatched", err)
	}
	r := f.start(t)
	one, two := f.request(t, r, "one"), f.request(t, r, "two")
	if one.Arm != "stable" || two.Arm != "candidate" || one.Status != "succeeded" || two.Status != "succeeded" || one.IdentityAfterHash == "" || two.OutputHash == "" {
		t.Fatal(one, two)
	}
	again := f.request(t, r, "two")
	if again.ID != two.ID || again.Status != "succeeded" {
		t.Fatal(again)
	}
	f.mu.Lock()
	calls := append([]string{}, f.calls...)
	f.mu.Unlock()
	if strings.Join(calls, ",") != "stable:1,candidate:1" {
		t.Fatal("not exactly two provider calls", calls)
	}
	if _, err = f.service.Request(t.Context(), "foreign", r.ID, "foreign", "x"); !errors.Is(err, modelstore.ErrNotFound) {
		t.Fatal("workspace leaked", err)
	}
	if _, err = f.service.Request(t.Context(), "local", r.ID, "two", "changed prompt"); !errors.Is(err, modelstore.ErrIdempotencyConflict) {
		t.Fatal("key reused with new prompt", err)
	}
	first, next, err := f.store.ListLiveRequests(t.Context(), "local", r.ID, "", 1)
	if err != nil || len(first) != 1 || next == "" {
		t.Fatal(first, next, err)
	}
	second, _, err := f.store.ListLiveRequests(t.Context(), "local", r.ID, next, 1)
	if err != nil || len(second) != 1 || first[0].ID == second[0].ID {
		t.Fatal(second, err)
	}
	w := httptest.NewRecorder()
	f.gateway.HandleListModels(w, httptest.NewRequest("GET", "/api/gateway/models", nil))
	var gatewayState struct {
		Default string `json:"default_model"`
	}
	json.Unmarshal(w.Body.Bytes(), &gatewayState)
	if gatewayState.Default != "stable" {
		t.Fatal("live path changed ordinary default")
	}
}
func TestLiveGateRequiresWallClockBothArmsAndFeedback(t *testing.T) {
	f := newFixture(t)
	r := f.start(t)
	for i := 0; i < 4; i++ {
		req := f.request(t, r, fmt.Sprint(i))
		if req.Status != "succeeded" {
			t.Fatal(req)
		}
	}
	d, _ := f.service.Get(t.Context(), "local", r.ID)
	if d.Decision.Allowed {
		t.Fatal("missing feedback/window passed")
	}
	f.clock = f.clock.Add(61 * time.Second)
	if _, err := f.service.Command(t.Context(), "local", r.ID, "advance", "no-feedback", r.Revision); !errors.Is(err, modelstore.ErrInsufficientEvidence) {
		t.Fatal("advanced without feedback", err)
	}
	requests, _, _ := f.store.ListLiveRequests(t.Context(), "local", r.ID, "", 20)
	for i, req := range requests {
		if _, err := f.service.Feedback(t.Context(), "local", r.ID, req.ID, "good", "explicit review", fmt.Sprint("feedback", i)); err != nil {
			t.Fatal(err)
		}
	}
	f.clock = f.clock.Add(-45 * time.Second)
	d, _ = f.service.Get(t.Context(), "local", r.ID)
	if d.Decision.Allowed {
		t.Fatal("virtual/incomplete time accepted")
	}
	if _, err := f.service.Command(t.Context(), "local", r.ID, "advance", "early", r.Revision); !errors.Is(err, modelstore.ErrInsufficientEvidence) {
		t.Fatal("advanced too early", err)
	}
	f.clock = f.clock.Add(60 * time.Second)
	d, _ = f.service.Get(t.Context(), "local", r.ID)
	if !d.Decision.Allowed {
		t.Fatalf("all bounded conditions should pass: %+v", d.Decision)
	}
	promoted, err := f.service.Command(t.Context(), "local", r.ID, "advance", "advance", r.Revision)
	if err != nil || promoted.State != "PROMOTED" || promoted.TargetBPS != 10000 || promoted.RouteEpoch != 2 {
		t.Fatal(promoted, err)
	}
	next := f.request(t, promoted, "after-promotion")
	if next.Arm != "candidate" {
		t.Fatal(next)
	}
	if _, err = f.service.Feedback(t.Context(), "local", r.ID, next.ID, "bad", "explicit bad answer", "bad-new-stage"); err != nil {
		t.Fatal(err)
	}
	d, _ = f.service.Get(t.Context(), "local", r.ID)
	if d.Release.State != "PAUSED" || d.EffectiveBPS != 0 {
		t.Fatal("quality guard did not pause", d)
	}
	rolled, err := f.service.Command(t.Context(), "local", r.ID, "rollback", "rollback", d.Release.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if f.request(t, rolled, "after-rollback").Arm != "stable" {
		t.Fatal("rollback did not serve stable")
	}
	events, err := f.store.LiveEvents(t.Context(), "local", r.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	previous := ""
	for _, event := range events {
		if event.PreviousHash != previous {
			t.Fatal("broken event chain", event)
		}
		if event.Decision.EvidenceHash != "" && modelstore.LiveHash(event.Decision.Observations) != event.Decision.EvidenceHash {
			t.Fatal("frozen gate observations are not verifiable", event)
		}
		if event.Command == "advance" && (len(event.Decision.Observations) != 4 || event.Decision.RouteEpoch != 1) {
			t.Fatal("advanced gate lost original phase evidence", event)
		}
		previous = event.Hash
	}
	if events[len(events)-1].Command != "rollback" || events[len(events)-1].ConfigurationHash != r.ConfigurationHash {
		t.Fatal(events)
	}
}
func TestLiveAutoPauseErrorsAndFeedbackIntegrity(t *testing.T) {
	f := newFixture(t)
	f.spec.Policy.ConsecutiveErrors = 1
	r := f.start(t)
	f.request(t, r, "one")
	f.mu.Lock()
	f.failCandidate = true
	f.mu.Unlock()
	failed := f.request(t, r, "two")
	if failed.Status != "failed" {
		t.Fatal(failed)
	}
	d, _ := f.service.Get(t.Context(), "local", r.ID)
	if d.Release.State != "PAUSED" || d.EffectiveBPS != 0 {
		t.Fatal(d)
	}
	if _, err := f.service.Feedback(t.Context(), "local", r.ID, failed.ID, "good", "must not score failures", "invalid-feedback"); !errors.Is(err, modelstore.ErrInvalid) {
		t.Fatal("failed request got quality feedback", err)
	}
	stable := f.request(t, r, "stable-fallback")
	if stable.Arm != "stable" || stable.Status != "succeeded" {
		t.Fatal(stable)
	}
	if _, err := f.service.Feedback(t.Context(), "foreign", r.ID, stable.ID, "good", "", "foreign-feedback"); !errors.Is(err, modelstore.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := f.service.Feedback(t.Context(), "local", r.ID, stable.ID, "good", "", "real-feedback"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Feedback(t.Context(), "local", r.ID, stable.ID, "bad", "overwrite", "another-key"); !errors.Is(err, modelstore.ErrInvalid) {
		t.Fatal("feedback overwritten", err)
	}
	read, err := f.store.GetLiveRequest(t.Context(), "local", r.ID, stable.ID)
	if err != nil || read.Feedback == nil || read.FeedbackRating != "good" || read.Feedback.Hash == "" {
		t.Fatal("quality evidence cannot be inspected", read, err)
	}
	hash := read.Feedback.Hash
	read.Feedback.Hash = ""
	if modelstore.LiveHash(read.Feedback) != hash {
		t.Fatal("feedback evidence hash mismatch")
	}
}

func TestLiveCommandReplayCannotCancelResumedTraffic(t *testing.T) {
	f := newFixture(t)
	r := f.start(t)
	paused, err := f.service.Command(t.Context(), "local", r.ID, "pause", "pause-once", r.Revision)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := f.service.Command(t.Context(), "local", r.ID, "resume", "resume-once", paused.Revision)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.block = make(chan struct{})
	f.mu.Unlock()
	completed := make(chan modelstore.LiveRequest, 1)
	go func() {
		req, _ := f.service.Request(context.Background(), "local", r.ID, "resumed-request", "real resumed request")
		completed <- req
	}()
	select {
	case <-f.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("not dispatched")
	}
	replay, err := f.service.Command(t.Context(), "local", r.ID, "pause", "pause-once", r.Revision)
	if err != nil || replay.State != "PAUSED" {
		t.Fatal(replay, err)
	}
	current, _ := f.service.Get(t.Context(), "local", r.ID)
	if current.Release.State != "ACTIVE" || current.Release.Revision != resumed.Revision {
		t.Fatal("replayed command mutated current state", current)
	}
	f.mu.Lock()
	close(f.block)
	f.block = nil
	f.mu.Unlock()
	select {
	case req := <-completed:
		if req.Status != "succeeded" {
			t.Fatal("old pause cancelled resumed request", req)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request did not complete")
	}
	if _, err = f.service.Command(t.Context(), "local", r.ID, "rollback", "stale", r.Revision); !errors.Is(err, modelstore.ErrRevisionConflict) {
		t.Fatal("stale revision accepted", err)
	}
}

func TestLiveRetentionAndFivePercentDispatch(t *testing.T) {
	f := newFixture(t)
	f.spec.TargetBPS = 500
	r := f.start(t)
	for i := 0; i < 20; i++ {
		req := f.request(t, r, fmt.Sprintf("ratio-%d", i))
		if req.Status != "succeeded" {
			t.Fatal(req)
		}
	}
	f.mu.Lock()
	candidate := 0
	for _, model := range f.calls {
		if model == "candidate:1" {
			candidate++
		}
	}
	f.mu.Unlock()
	if candidate != 1 {
		t.Fatal("5 percent must route one of twenty admitted requests", candidate)
	}
	_, _, err := f.store.MutateLive(t.Context(), "local", r.ID, "test_retention_fixture", "", "", func(snap modelstore.LiveSnapshot) (modelstore.LiveMutation, error) {
		snap.Release.NextSequence = modelstore.LiveRequestLimit - 1
		return modelstore.LiveMutation{Release: snap.Release}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	last := f.request(t, r, "last-retained")
	if last.Sequence != modelstore.LiveRequestLimit {
		t.Fatal(last)
	}
	if _, err = f.service.Request(t.Context(), "local", r.ID, "beyond-limit", "x"); !errors.Is(err, modelstore.ErrInvalid) {
		t.Fatal("storage limit did not reject new inference", err)
	}
	if again := f.request(t, r, "last-retained"); again.ID != last.ID {
		t.Fatal("retention blocked safe replay")
	}
	for i := 1; i < modelstore.LiveReleaseLimit; i++ {
		if _, err = f.service.Create(t.Context(), "local", fmt.Sprintf("release-%d", i), f.spec); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = f.service.Create(t.Context(), "local", "release-overflow", f.spec); !errors.Is(err, modelstore.ErrInvalid) {
		t.Fatal("release storage unbounded", err)
	}
}

func TestLiveLatencyUnknownAndRegressionAreNotQualitySuccess(t *testing.T) {
	now := time.Now()
	p := DefaultPolicy()
	p.MinimumSamples = 2
	p.MinimumFeedback = 1
	snap := modelstore.LiveSnapshot{Release: modelstore.LiveRelease{State: "ACTIVE", Policy: p, WindowStartedAt: now.Add(-time.Minute).Format(time.RFC3339Nano)}, Observations: []modelstore.LiveObservation{{ID: "s1", Arm: "stable", Status: "succeeded", WallNS: 1000, Rating: "good"}, {ID: "s2", Arm: "stable", Status: "succeeded", WallNS: 1000}, {ID: "c1", Arm: "candidate", Status: "succeeded", WallNS: 3000, Rating: "good"}, {ID: "c2", Arm: "candidate", Status: "succeeded", WallNS: 3000}}}
	if decision(snap, now).Allowed || autoPause(snap) != "candidate_latency_regression" {
		t.Fatal("latency regression did not prevent promotion")
	}
	snap.Observations[2].WallNS = 0
	if decision(snap, now).Allowed {
		t.Fatal("one latency sample passed a two-sample gate")
	}
	snap.Observations[3].WallNS = 0
	if stats(snap)["candidate"].MeanLatencyMS != nil || decision(snap, now).Allowed {
		t.Fatal("missing latency fabricated pass")
	}
	snap.Observations[2].ErrorCode = "MODEL_DRIFT"
	if autoPause(snap) != "candidate_identity_drift" {
		t.Fatal("model identity drift did not pause")
	}
}
func TestLiveConcurrentAdmissionAndRollbackCancelInFlight(t *testing.T) {
	f := newFixture(t)
	r := f.start(t)
	f.mu.Lock()
	f.block = make(chan struct{})
	f.mu.Unlock()
	result := make(chan modelstore.LiveRequest, 1)
	errs := make(chan error, 1)
	go func() {
		out, err := f.service.Request(context.Background(), "local", r.ID, "blocked", "blocked user request")
		result <- out
		errs <- err
	}()
	select {
	case <-f.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("provider was not dispatched")
	}
	if _, err := f.service.Request(t.Context(), "local", r.ID, "second", "another user request"); !errors.Is(err, workspaces.ErrBusy) {
		t.Fatal("parallel request admitted", err)
	}
	retry, err := f.service.Request(t.Context(), "local", r.ID, "blocked", "blocked user request")
	if err != nil || retry.Status != "running" {
		t.Fatal(retry, err)
	}
	if _, err = f.service.Command(t.Context(), "local", r.ID, "rollback", "rollback", r.Revision); err != nil {
		t.Fatal(err)
	}
	select {
	case req := <-result:
		if req.Status != "interrupted" {
			t.Fatal(req)
		}
		if err = <-errs; err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("rollback did not cancel in-flight request")
	}
	f.mu.Lock()
	f.block = nil
	f.mu.Unlock()
	after := f.request(t, r, "after")
	if after.Arm != "stable" {
		t.Fatal(after)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) != 2 {
		t.Fatal("retry/blocked admission dispatched provider", f.calls)
	}
}
func TestLiveRestartPreservesEvidenceAndPausesCandidate(t *testing.T) {
	f := newFixture(t)
	r := f.start(t)
	f.request(t, r, "completed")
	// Persist an admitted lease with no terminal record, as after a process crash.
	_, _, err := f.store.MutateLive(t.Context(), "local", r.ID, "request", "crash", modelstore.LiveHash("crash prompt"), func(snap modelstore.LiveSnapshot) (modelstore.LiveMutation, error) {
		req := modelstore.LiveRequest{ID: "crash-request", ReleaseID: r.ID, WorkspaceID: "local", Source: "live_provider", Sequence: 2, RouteEpoch: r.RouteEpoch, Arm: "candidate", ModelRevisionID: r.Candidate.ID, Status: "running", Prompt: "crash prompt", StartedAt: f.clock.Format(time.RFC3339Nano)}
		next := snap.Release
		next.NextSequence = 2
		return modelstore.LiveMutation{Release: next, Request: &req}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.store.Close(); err != nil {
		t.Fatal(err)
	}
	f.store, err = modelstore.Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	f.lab = modellab.New(f.store, f.gateway)
	f.service, err = New(t.Context(), f.store, f.lab, f.gateway)
	if err != nil {
		t.Fatal(err)
	}
	d, err := f.service.Get(t.Context(), "local", r.ID)
	if err != nil || d.Release.State != "PAUSED" || d.Running != 0 || d.EffectiveBPS != 0 {
		t.Fatal(d, err)
	}
	retry, err := f.service.Request(t.Context(), "local", r.ID, "crash", "crash prompt")
	if err != nil || retry.Status != "interrupted" || retry.ErrorCode != "PROCESS_RESTART" {
		t.Fatal(retry, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) != 1 {
		t.Fatal("restart replay sent new inference", f.calls)
	}
}
func TestLiveHTTPAuthenticationScopeAndBounds(t *testing.T) {
	f := newFixture(t)
	r := f.start(t)
	manager, err := workspaces.Open(filepath.Join(t.TempDir(), "workspaces.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	foreign, err := manager.SaveWorkspace(workspaces.Workspace{Name: "Foreign"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	f.service.RegisterRoutes(mux, manager)
	handler := middleware.BearerAuth("fixture-secret")(mux)
	send := func(method, path, body, auth string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", auth)
		req.Header.Set("Idempotency-Key", "http-key")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	root := "/api/workspaces/local/live-canaries/" + r.ID
	if w := send("GET", root, "", ""); w.Code != 401 {
		t.Fatal("unauthenticated access", w.Code)
	}
	if w := send("GET", "/api/workspaces/"+foreign.ID+"/live-canaries/"+r.ID, "", "Bearer fixture-secret"); w.Code != 404 {
		t.Fatal("foreign access", w.Code, w.Body.String())
	}
	if w := send("POST", root+"/requests", `{"prompt":"real input","model_id":"injected"}`, "Bearer fixture-secret"); w.Code != 400 {
		t.Fatal("injected model accepted", w.Code)
	}
	if w := send("GET", root+"/events?after=-1", "", "Bearer fixture-secret"); w.Code != 400 {
		t.Fatal("bad cursor accepted", w.Code)
	}
	if w := send("POST", root+"/requests", `{"prompt":"`+strings.Repeat("x", 16385)+`"}`, "Bearer fixture-secret"); w.Code != 409 {
		t.Fatal("oversized prompt accepted", w.Code)
	}
	for _, prompt := range []string{"authenticated live stable request", "authenticated live candidate request"} {
		body, _ := json.Marshal(map[string]string{"prompt": prompt})
		req := httptest.NewRequest("POST", root+"/requests", strings.NewReader(string(body)))
		req.Header.Set("Authorization", "Bearer fixture-secret")
		req.Header.Set("Idempotency-Key", fmt.Sprintf("public-request-%d", len(f.calls)))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		var result modelstore.LiveRequest
		if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 200 || result.Status != "succeeded" || result.Source != "live_provider" {
			t.Fatal("authenticated real provider request failed", w.Code, w.Body.String(), err)
		}
	}
	if len(f.calls) != 2 || f.calls[0] != "stable:1" || f.calls[1] != "candidate:1" {
		t.Fatal("public route did not dispatch both providers", f.calls)
	}
	if w := send("POST", root+"/commands", `{"command":"advance","expected_revision":2,"virtual_seconds":86400}`, "Bearer fixture-secret"); w.Code != 400 {
		t.Fatal("virtual time entered live gate", w.Code)
	}
}
