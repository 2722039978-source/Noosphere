package main

import (
	"encoding/json"
	"github.com/noosphere/backend/internal/agent"
	"github.com/noosphere/backend/internal/gateway"
	"github.com/noosphere/backend/internal/workspaces"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
