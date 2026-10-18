package main

import (
	"encoding/json"
	"github.com/noosphere/backend/internal/workspaces"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func workerManager(t *testing.T) *workspaces.Manager {
	t.Helper()
	m, e := workspaces.Open(filepath.Join(t.TempDir(), "tasks.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { m.Close() })
	return m
}
func terminalRun(t *testing.T, m *workspaces.Manager, id string) workspaces.Run {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r, e := m.Run(id)
		if e != nil {
			t.Fatal(e)
		}
		switch r.Status {
		case "completed", "failed", "cancelled", "interrupted":
			return r
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("task did not finish")
	return workspaces.Run{}
}

func TestIndexWorkerUsesWorkspaceRootAndArrayExclude(t *testing.T) {
	m := workerManager(t)
	ws, e := m.SaveWorkspace(workspaces.Workspace{Name: "project", ProjectPath: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/codelens/index", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		json.NewDecoder(r.Body).Decode(&in)
		if in["project_path"] != ws.ProjectPath {
			t.Error("index escaped workspace root")
		}
		if _, ok := in["exclude"].([]any); !ok {
			http.Error(w, "exclude must be an array", 422)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"project_id": "code-fixture", "retrieval_ready": true, "indexed_chunks": 2})
	})
	registerWorkspaceWorkers(m, mux, nil, nil, true, false)
	run, e := m.Submit(ws.ID, "index", "index", json.RawMessage(`{}`))
	if e != nil {
		t.Fatal(e)
	}
	done := terminalRun(t, m, run.ID)
	if done.Status != "completed" {
		t.Fatalf("index failed: %+v", done)
	}
	saved, _ := m.Workspace(ws.ID)
	if saved.ProjectID != "code-fixture" {
		t.Fatal("index not bound")
	}
	local, _ := m.Runs("local")
	if len(local) != 0 {
		t.Fatal("task attached to another workspace")
	}
}

func TestIndexWorkerCannotBindLateResultAfterRootChange(t *testing.T) {
	m := workerManager(t)
	ws, e := m.SaveWorkspace(workspaces.Workspace{Name: "project", ProjectPath: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-release:
		case <-r.Context().Done():
		}
		json.NewEncoder(w).Encode(map[string]any{"project_id": "stale", "retrieval_ready": true})
	})
	registerWorkspaceWorkers(m, mux, nil, nil, true, false)
	run, e := m.Submit(ws.ID, "index", "index", nil)
	if e != nil {
		t.Fatal(e)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker not started")
	}
	ws.ProjectPath = t.TempDir()
	if _, e = m.SaveWorkspace(ws); e != nil {
		t.Fatal(e)
	}
	release <- struct{}{}
	if terminalRun(t, m, run.ID).Status != "failed" {
		t.Fatal("stale root must fail")
	}
	saved, _ := m.Workspace(ws.ID)
	if saved.ProjectID != "" {
		t.Fatal("stale binding published")
	}
}

func TestIndexCancellationReachesModuleAndRejectsLateSuccess(t *testing.T) {
	m := workerManager(t)
	ws, e := m.SaveWorkspace(workspaces.Workspace{Name: "project", ProjectPath: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	started, stopped := make(chan struct{}), make(chan struct{})
	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		json.NewEncoder(w).Encode(map[string]any{"project_id": "cancelled", "retrieval_ready": true})
		close(stopped)
	})
	registerWorkspaceWorkers(m, mux, nil, nil, true, false)
	run, e := m.Submit(ws.ID, "index", "index", nil)
	if e != nil {
		t.Fatal(e)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker not started")
	}
	if _, e = m.Cancel(run.ID); e != nil {
		t.Fatal(e)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("module did not cancel")
	}
	if terminalRun(t, m, run.ID).Status != "cancelled" {
		t.Fatal("cancellation overwritten")
	}
	saved, _ := m.Workspace(ws.ID)
	if saved.ProjectID != "" {
		t.Fatal("cancelled index published")
	}
}
