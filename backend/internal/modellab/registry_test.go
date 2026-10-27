package modellab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/noosphere/backend/internal/gateway"
	"github.com/noosphere/backend/internal/gateway/providers"
	"github.com/noosphere/backend/internal/modelstore"
)

type registryFixture struct {
	mu          sync.Mutex
	native      *nativeLifecycleFixture
	template    string
	calls       int
	lastOptions map[string]any
}

func registryLab(t *testing.T) (*Service, *modelstore.Store, *gateway.Gateway, *registryFixture, string, string) {
	t.Helper()
	f := &registryFixture{template: "{{ .Prompt }}"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		f.mu.Lock()
		native := f.native
		f.mu.Unlock()
		if native != nil && native.serve(t, w, r) {
			return
		}
		switch r.URL.Path {
		case "/api/version":
			json.NewEncoder(w).Encode(map[string]string{"version": "0.20.0-fixture"})
		case "/api/tags":
			models := []map[string]any{}
			for _, name := range []string{"alpha:1", "beta:1"} {
				models = append(models, map[string]any{"name": name, "model": name, "digest": strings.Repeat("a", 64), "size": 1024, "details": map[string]string{"format": "gguf", "family": "fixture", "parameter_size": "1B", "quantization_level": "Q4_K_M"}})
			}
			json.NewEncoder(w).Encode(map[string]any{"models": models})
		case "/api/show":
			f.mu.Lock()
			template := f.template
			f.mu.Unlock()
			json.NewEncoder(w).Encode(map[string]any{"template": template, "parameters": "temperature 0.7\nnum_ctx 8192", "details": map[string]string{"format": "gguf", "family": "fixture", "parameter_size": "1B", "quantization_level": "Q4_K_M"}, "model_info": map[string]any{"general.architecture": "fixture", "fixture.context_length": 8192}, "capabilities": []string{"completion"}})
		case "/api/chat":
			var req struct {
				Model   string         `json:"model"`
				Options map[string]any `json:"options"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			f.mu.Lock()
			f.calls++
			f.lastOptions = req.Options
			f.mu.Unlock()
			json.NewEncoder(w).Encode(map[string]any{"model": req.Model, "message": map[string]string{"role": "assistant", "content": "OK"}, "done": true, "done_reason": "stop", "total_duration": 100, "load_duration": 5, "eval_count": 1, "eval_duration": 50})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "models.yaml")
	config := "default_model: source\nproviders:\n  - id: source\n    name: fixture\n    type: ollama\n    base_url: " + server.URL + "/v1\n    model: alpha:1\n    max_tokens: 8192\n    enabled: true\n"
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	g := gateway.New(configPath)
	dbPath := filepath.Join(dir, "engineering.sqlite")
	s, err := modelstore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return New(s, g), s, g, f, dbPath, configPath
}

func registerAlpha(t *testing.T, lab *Service) modelstore.ModelRevision {
	t.Helper()
	discovery, err := lab.Discover(context.Background(), "source")
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Models) != 2 {
		t.Fatalf("expected installed models, got %+v", discovery)
	}
	for _, m := range discovery.Models {
		if m.Name == "alpha:1" {
			revision, err := lab.Register(context.Background(), "source", m.Name, m.MetadataHash)
			if err != nil {
				t.Fatal(err)
			}
			return revision
		}
	}
	t.Fatal("alpha missing")
	return modelstore.ModelRevision{}
}

func testProfile() modelstore.InferenceProfile {
	return modelstore.InferenceProfile{Requested: modelstore.Options{Temperature: modelstore.Float(0), Seed: modelstore.Int(7), NumCtx: modelstore.Int(256), MaxOutput: modelstore.Int(32), RetryLimit: modelstore.Int(0)}}
}

func TestDiscoverRegisterAndReopenModelRevision(t *testing.T) {
	lab, s, g, f, dbPath, _ := registryLab(t)
	revision := registerAlpha(t, lab)
	if revision.IdentityLevel != "digest" || len(revision.Digest) != 64 || revision.EngineVersion == "" || revision.ConfigHash == "" || revision.EndpointFingerprint == "" {
		t.Fatalf("incomplete registered revision: %+v", revision)
	}
	if _, err := lab.Register(context.Background(), "source", "alpha:1", "stale"); err == nil {
		t.Fatal("stale discovery registered")
	}
	f.mu.Lock()
	f.template = "changed template"
	f.mu.Unlock()
	if _, err := lab.Register(context.Background(), "source", "alpha:1", revision.MetadataHash); err == nil {
		t.Fatal("changed metadata registered using old confirmation")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := modelstore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := New(reopened, g).GetModel(context.Background(), revision.ID)
	if err != nil || !reflect.DeepEqual(got, revision) {
		t.Fatalf("revision did not survive reopen: %+v %v", got, err)
	}
}

func TestProfilePreservesZeroAndRegisteredEvaluation(t *testing.T) {
	lab, _, _, f, _, _ := registryLab(t)
	revision := registerAlpha(t, lab)
	profile, err := lab.SaveProfile(context.Background(), testProfile())
	if err != nil {
		t.Fatal(err)
	}
	if profile.ID == "" || profile.Requested.Temperature == nil || *profile.Requested.Temperature != 0 || profile.Effective != nil || profile.Reported != nil {
		t.Fatalf("invented/lost options: %+v", profile)
	}
	result, err := lab.Evaluate(context.Background(), InferenceSpec{ModelRevisionID: revision.ID, ProfileID: profile.ID, AttemptID: "fixture-attempt", Messages: []providers.Message{{Role: "user", Content: "Reply OK."}}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != "OK" {
		t.Fatalf("unexpected output %+v", result)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls != 1 || f.lastOptions["temperature"] != float64(0) || f.lastOptions["seed"] != float64(7) || f.lastOptions["num_predict"] != float64(32) {
		t.Fatalf("hidden retry or legacy override: calls=%d options=%v", f.calls, f.lastOptions)
	}
}

func TestRegistryRejectsChangedSourceAndUnsupportedProfile(t *testing.T) {
	lab, _, g, _, _, configPath := registryLab(t)
	revision := registerAlpha(t, lab)
	bad := testProfile()
	bad.Requested.RetryLimit = modelstore.Int(1)
	if _, err := lab.SaveProfile(context.Background(), bad); err == nil {
		t.Fatal("accepted hidden retry profile")
	}
	bad = testProfile()
	bad.Effective = &modelstore.Options{}
	if _, err := lab.SaveProfile(context.Background(), bad); err == nil {
		t.Fatal("accepted caller-asserted effective parameters")
	}
	profile, err := lab.SaveProfile(context.Background(), testProfile())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("providers:\n  - id: source\n    type: ollama\n    base_url: http://127.0.0.1:1/v1\n    model: alpha:1\n    enabled: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	g.ReloadConfig()
	_, err = lab.Evaluate(context.Background(), InferenceSpec{ModelRevisionID: revision.ID, ProfileID: profile.ID, Messages: []providers.Message{{Role: "user", Content: "OK"}}})
	if err == nil || !strings.Contains(err.Error(), "MODEL_DRIFT") {
		t.Fatalf("changed endpoint not rejected before inference: %v", err)
	}
}

func TestProfileNativeBoundsBeforePersistence(t *testing.T) {
	lab, store, _, _, _, _ := registryLab(t)
	ctx := context.Background()
	cases := []struct {
		name   string
		change func(*modelstore.Options)
	}{
		{"top_k", func(o *modelstore.Options) { o.TopK = modelstore.Int(100001) }},
		{"keep_alive_negative", func(o *modelstore.Options) { o.KeepAlive = "-1" }},
		{"keep_alive_long", func(o *modelstore.Options) { o.KeepAlive = "25h" }},
		{"stop_count", func(o *modelstore.Options) { o.Stop = make([]string, 65) }},
		{"stop_bytes", func(o *modelstore.Options) { o.Stop = []string{strings.Repeat("x", 1025)} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := testProfile()
			tc.change(&p.Requested)
			if _, err := lab.SaveProfile(ctx, p); err == nil {
				t.Fatal("Lab persisted profile rejected by native execution")
			}
			p.ID = "historical_" + tc.name
			if err := store.SaveInferenceProfile(ctx, p); err != nil {
				t.Fatalf("native restriction leaked into historical store domain: %v", err)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		change func(*modelstore.Options)
	}{
		{"upper_bounds", func(o *modelstore.Options) {
			o.TopK = modelstore.Int(100000)
			o.KeepAlive = "24h"
			o.Stop = make([]string, 64)
			o.Stop[0] = strings.Repeat("x", 1024)
			o.MaxOutput = modelstore.Int(65536)
		}},
		{"zero_options", func(o *modelstore.Options) {
			o.Temperature = modelstore.Float(0)
			o.Seed = modelstore.Int(0)
			o.TopK = modelstore.Int(0)
			o.TopP = modelstore.Float(0)
			o.KeepAlive = "0"
			o.RetryLimit = modelstore.Int(0)
			o.Parallelism = modelstore.Int(1)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := testProfile()
			tc.change(&p.Requested)
			saved, err := lab.SaveProfile(ctx, p)
			if err != nil {
				t.Fatal(err)
			}
			got, err := lab.GetProfile(ctx, saved.ID)
			if err != nil || !reflect.DeepEqual(saved, got) {
				t.Fatalf("valid executable boundary lost: %+v %v", got, err)
			}
		})
	}
}
func TestRegistrationMetadataRepeatAndReopen(t *testing.T) {
	lab, store, g, _, path, _ := registryLab(t)
	ctx := context.Background()
	first := registerAlpha(t, lab)
	var metadata struct {
		Capabilities      []string `json:"capabilities"`
		ParameterSize     string   `json:"parameter_size"`
		SizeBytes         *int64   `json:"size_bytes"`
		FirstDiscoveredAt string   `json:"first_discovered_at"`
	}
	raw, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &metadata); err != nil {
		t.Fatal(err)
	}
	stamp, err := time.Parse(time.RFC3339Nano, metadata.FirstDiscoveredAt)
	if err != nil || stamp.Location() != time.UTC || !reflect.DeepEqual(metadata.Capabilities, []string{"completion"}) || metadata.ParameterSize != "1B" || metadata.SizeBytes == nil || *metadata.SizeBytes != 1024 || first.ParameterCount != nil {
		t.Fatalf("reported metadata/time missing or invented: %s %v", raw, err)
	}
	repeated, err := lab.Register(ctx, "source", "alpha:1", first.MetadataHash)
	if err != nil || !reflect.DeepEqual(first, repeated) {
		t.Fatalf("same identity changed revision or first timestamp: %+v %v", repeated, err)
	}
	collision := first
	collision.Format = "different-format"
	if err = store.SaveModelRevision(ctx, collision); err == nil {
		t.Fatal("same-ID metadata collision overwritten")
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := modelstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	again, err := New(reopened, g).GetModel(ctx, first.ID)
	if err != nil || !reflect.DeepEqual(first, again) {
		t.Fatalf("metadata lost at reopen: %+v %v", again, err)
	}
	// New optional fields must not become mandatory for historical registry rows.
	var legacy modelstore.ModelRevision
	if err = json.Unmarshal([]byte(`{"id":"legacy","gateway_model_id":"source","provider":"ollama","model_name":"alpha:old","identity_level":"digest","digest":"`+strings.Repeat("b", 64)+`"}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if err = reopened.SaveModelRevision(ctx, legacy); err != nil {
		t.Fatalf("legacy absent metadata rejected: %v", err)
	}
	if _, err = reopened.GetModelRevision(ctx, legacy.ID); err != nil {
		t.Fatal(err)
	}
}
func TestConcurrentIdenticalRegistrationUsesOriginalRecord(t *testing.T) {
	lab, _, _, _, _, _ := registryLab(t)
	ctx := context.Background()
	d, err := lab.Discover(ctx, "source")
	if err != nil {
		t.Fatal(err)
	}
	hash := ""
	for _, m := range d.Models {
		if m.Name == "alpha:1" {
			hash = m.MetadataHash
		}
	}
	type result struct {
		model modelstore.ModelRevision
		err   error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() { m, e := lab.Register(ctx, "source", "alpha:1", hash); results <- result{m, e} }()
	}
	a, b := <-results, <-results
	if a.err != nil || b.err != nil || !reflect.DeepEqual(a.model, b.model) {
		t.Fatalf("same confirmed identity raced: %+v/%v %+v/%v", a.model, a.err, b.model, b.err)
	}
}
