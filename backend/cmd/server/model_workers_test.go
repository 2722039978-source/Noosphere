package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/noosphere/backend/internal/config"
	"github.com/noosphere/backend/internal/core"
	"github.com/noosphere/backend/internal/gateway"
	"github.com/noosphere/backend/internal/workspaces"
)

func TestModelRuntimeUsesTemporarySeparateStoreAndClosesAfterWorkers(t *testing.T) {
	dir := t.TempDir()
	manager, err := workspaces.Open(filepath.Join(dir, "workspace.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	cfg.App.DataDir = dir
	cfg.ModelLab.Enabled = true
	cfg.ModelLab.GoBinary = filepath.Join(dir, "missing-go.exe")
	app := &App{Config: cfg, Lifecycle: core.New(), Gateway: gateway.New(filepath.Join(dir, "models.yaml")), Workspaces: manager, Mux: http.NewServeMux()}
	initModelLab(app, dir)
	if app.ModelLab == nil || app.ModelStore == nil || app.ModelOps == nil {
		t.Fatal("separate engineering runtime not wired")
	}
	for _, path := range []string{"/api/model-lab/models", "/api/model-lab/profiles", "/api/model-ops/policies?mode=simulation"} {
		w := httptest.NewRecorder()
		app.Mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatalf("composed route %s: %d %s", path, w.Code, w.Body.String())
		}
	}
	if _, err = app.ModelStore.GetPolicy(context.Background(), "simulation-demo-v1"); err != nil {
		t.Fatal("ModelOps policy not in shared store", err)
	}
	if _, err = os.Stat(filepath.Join(dir, "model-engineering.sqlite")); err != nil {
		t.Fatal(err)
	}
	// A malformed or foreign generic task cannot become a durable evaluation.
	if _, err = manager.Submit("local", "evaluation", "unreferenced", json.RawMessage(`{"evaluation_id":"anything"}`)); err == nil {
		t.Fatal("unreferenced task admitted")
	}
	started, finished := make(chan struct{}), make(chan struct{})
	manager.Register("index", "background", func(ctx context.Context, ws workspaces.Workspace, in json.RawMessage, emit func(string, string, any)) (any, error) {
		close(started)
		<-ctx.Done()
		if _, err := app.ModelStore.ListEvaluations(context.Background()); err != nil {
			t.Errorf("store closed before handler drained: %v", err)
		}
		close(finished)
		return nil, ctx.Err()
	})
	if _, err = manager.Submit("local", "index", "shutdown fixture", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	<-started
	if err = app.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("handler outlived close")
	}
	if err = app.Close(); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	app.Mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/model-ops/policies?mode=simulation", nil))
	if w.Code != 503 {
		t.Fatalf("closed service admitted route: %d", w.Code)
	}
}
