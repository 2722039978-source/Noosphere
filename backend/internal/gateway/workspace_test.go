package gateway

import (
	"context"
	"encoding/json"
	"github.com/noosphere/backend/internal/gateway/providers"
	"github.com/noosphere/backend/internal/workspaces"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceStreamRecordsResultAndUsesWorkspaceDefault(t *testing.T) {
	manager, err := workspaces.Open(filepath.Join(t.TempDir(), "tasks.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	ws, err := manager.SaveWorkspace(workspaces.Workspace{Name: "A", DefaultModelID: "workspace-model"})
	if err != nil {
		t.Fatal(err)
	}
	g := streamGateway(chunksProvider(providers.StreamChunk{Done: true, Content: "workspace answer"}))
	g.config.Models[0].ID = "workspace-model"
	g.config.DefaultModel = "missing"
	model := g.config.Models[0]
	g.providers = map[string]providers.LLMProvider{providerCacheKey(&model): chunksProvider(providers.StreamChunk{Done: true, Content: "workspace answer"})}
	g.SetWorkspaceManager(manager)
	body, _ := json.Marshal(map[string]any{"request_id": "run-fixture", "workspace_id": ws.ID, "session_id": "session-A", "messages": []map[string]string{{"role": "user", "content": "hello"}}})
	w := httptest.NewRecorder()
	g.HandleChatStream(w, httptest.NewRequest("POST", "/api/gateway/chat/stream", strings.NewReader(string(body))))
	run, err := manager.Run("run-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "completed" || run.WorkspaceID != ws.ID || run.SessionID != "session-A" || run.ModelID != "workspace-model" || !strings.Contains(string(run.Result), "workspace answer") {
		t.Fatalf("incorrect task %+v", run)
	}
	events, err := manager.Events(run.ID, 0)
	if err != nil || len(events) < 3 {
		t.Fatalf("events=%v err=%v", events, err)
	}
}

func TestWorkspaceCancellationStopsUpstreamAndCannotComplete(t *testing.T) {
	manager, err := workspaces.Open(filepath.Join(t.TempDir(), "tasks.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	started, stopped := make(chan struct{}), make(chan struct{})
	g := streamGateway(mockStreamProvider{stream: func(ctx context.Context) (<-chan providers.StreamChunk, error) {
		ch := make(chan providers.StreamChunk)
		close(started)
		go func() { <-ctx.Done(); close(ch); close(stopped) }()
		return ch, nil
	}})
	g.SetWorkspaceManager(manager)
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		g.HandleChatStream(w, httptest.NewRequest("POST", "/api/gateway/chat/stream", strings.NewReader(`{"request_id":"cancel-fixture","workspace_id":"local","messages":[{"role":"user","content":"hello"}]}`)))
	}()
	awaitSignal(t, started, "stream start")
	if _, err = manager.Cancel("cancel-fixture"); err != nil {
		t.Fatal(err)
	}
	awaitSignal(t, done, "stream end")
	awaitSignal(t, stopped, "upstream end")
	run, _ := manager.Run("cancel-fixture")
	if run.Status != "cancelled" {
		t.Fatalf("wrong state %+v", run)
	}
	if !strings.Contains(w.Body.String(), `"status":"cancelled"`) {
		t.Fatalf("missing cancelled event: %s", w.Body.String())
	}
}

func TestUnknownWorkspaceCannotFallBackToLocal(t *testing.T) {
	manager, err := workspaces.Open(filepath.Join(t.TempDir(), "tasks.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	g := streamGateway(chunksProvider(providers.StreamChunk{Done: true, Content: "must not run"}))
	g.SetWorkspaceManager(manager)
	w := httptest.NewRecorder()
	g.HandleChatStream(w, httptest.NewRequest("POST", "/api/gateway/chat/stream", strings.NewReader(`{"workspace_id":"missing","messages":[{"role":"user","content":"hello"}]}`)))
	if w.Code < 400 {
		t.Fatalf("missing workspace accepted: %s", w.Body.String())
	}
}

func TestChatResultStorageFailureIsNotReportedAsCancellation(t *testing.T) {
	manager, err := workspaces.Open(filepath.Join(t.TempDir(), "tasks.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	g := streamGateway(chunksProvider(providers.StreamChunk{Done: true, Content: strings.Repeat("x", 2*1024*1024)}))
	g.SetWorkspaceManager(manager)
	w := httptest.NewRecorder()
	g.HandleChatStream(w, httptest.NewRequest("POST", "/api/gateway/chat/stream", strings.NewReader(`{"request_id":"oversized","workspace_id":"local","messages":[{"role":"user","content":"hello"}]}`)))
	run, err := manager.Run("oversized")
	if err != nil || run.Status != "failed" {
		t.Fatalf("run: %+v %v", run, err)
	}
	if !strings.Contains(w.Body.String(), `"status":"failed"`) || strings.Contains(w.Body.String(), `"type":"done"`) {
		t.Fatal("failed commit did not report failure")
	}
}
