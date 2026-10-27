package main

import (
	"context"
	"encoding/json"
	"github.com/noosphere/backend/internal/agent"
	"github.com/noosphere/backend/internal/gateway"
	"github.com/noosphere/backend/internal/workspaces"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentWorkerPersistsStructuredEvidence(t *testing.T) {
	m := workerManager(t)
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc start() {}\n"), 0600)
	ws, err := m.SaveWorkspace(workspaces.Workspace{Name: "Agent fixture", ProjectPath: root})
	if err != nil {
		t.Fatal(err)
	}
	turns := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		turns++
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if turns == 1 {
			w.Write([]byte(`{"choices":[{"finish_reason":"tool_calls","message":{"tool_calls":[{"id":"one","type":"function","function":{"name":"project_read_file","arguments":"{\"path\":\"main.go\"}"}}]}}]}`))
			return
		}
		messages := body["messages"].([]any)
		if messages[len(messages)-1].(map[string]any)["tool_call_id"] != "one" {
			t.Error("tool reply linkage missing")
		}
		w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"Verified source [E1]"}}]}`))
	}))
	defer upstream.Close()
	config := filepath.Join(t.TempDir(), "models.yaml")
	raw, _ := json.Marshal(map[string]any{"default_model": "fixture", "providers": []map[string]any{{"id": "fixture", "name": "fixture", "type": "deepseek", "base_url": upstream.URL, "model": "fixture", "enabled": true}}})
	os.WriteFile(config, raw, 0600)
	gw := gateway.New(config)
	gw.SetWorkspaceManager(m)
	registerAgentWorker(m, gw, nil, false)
	run, err := m.Submit(ws.ID, "agent", "inspect", json.RawMessage(`{"goal":"inspect startup","session_id":"s1"}`))
	if err != nil {
		t.Fatal(err)
	}
	done := terminalRun(t, m, run.ID)
	if done.Status != "completed" {
		t.Fatalf("%+v", done)
	}
	var result agent.Result
	if err = json.Unmarshal(done.Result, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Evidence) != 1 || result.ModelCalls != 2 || result.ModelID != "fixture" || done.SessionID != "s1" {
		t.Fatalf("missing result: %+v", result)
	}
	events, err := m.Events(run.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	started, completed := false, false
	for _, e := range events {
		started = started || e.Phase == "tool_started"
		completed = completed || e.Phase == "tool_completed"
	}
	if !started || !completed {
		t.Fatal("missing persisted tool events")
	}
}

func TestQueuedAgentPinsDefaultBeforeConfigurationChanges(t *testing.T) {
	m := workerManager(t)
	entered, release := make(chan struct{}, 2), make(chan struct{})
	m.Register("diagnosis", "interactive", func(ctx context.Context, ws workspaces.Workspace, input json.RawMessage, emit func(string, string, any)) (any, error) {
		entered <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil, nil
	})
	for i := 0; i < 2; i++ {
		if _, err := m.Submit("local", "diagnosis", "occupy", nil); err != nil {
			t.Fatal(err)
		}
	}
	<-entered
	<-entered
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.Model != "old" {
			t.Errorf("queued Agent silently changed model: %s", body.Model)
		}
		w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"OK"}}]}`))
	}))
	defer upstream.Close()
	config := filepath.Join(t.TempDir(), "models.yaml")
	raw, _ := json.Marshal(map[string]any{"default_model": "old", "providers": []map[string]any{{"id": "old", "type": "deepseek", "base_url": upstream.URL, "model": "old", "enabled": true}}})
	os.WriteFile(config, raw, 0600)
	gw := gateway.New(config)
	gw.SetWorkspaceManager(m)
	registerAgentWorker(m, gw, nil, false)
	run, err := m.Submit("local", "agent", "queued", json.RawMessage(`{"goal":"hello","session_id":"queued"}`))
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	gw.HandleAddModel(w, httptest.NewRequest("POST", "/api/gateway/models", strings.NewReader(`{"id":"new","name":"new","provider":"deepseek","base_url":"`+upstream.URL+`","model_name":"new","is_default":true}`)))
	if w.Code != 201 {
		t.Fatal(w.Body)
	}
	close(release)
	done := terminalRun(t, m, run.ID)
	var result agent.Result
	json.Unmarshal(done.Result, &result)
	if done.Status != "completed" || done.ModelID != "old" || result.ModelID != "old" {
		t.Fatalf("queued model not pinned: %+v %+v", done, result)
	}
}
