package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/noosphere/backend/internal/workspaces"
)

// Only the remote inference server is substituted. Gateway resolution,
// provider serialization, credential headers and SQLite persistence are real.
func TestSessionSelectionActualProviderRequests(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		mu.Lock()
		calls = append(calls, r.URL.Path+"|"+body.Model+"|"+r.Header.Get("Authorization"))
		mu.Unlock()
		if body.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\ndata: [DONE]\n\n"))
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}]}`))
		}
	}))
	defer upstream.Close()
	dir := t.TempDir()
	manager, err := workspaces.Open(filepath.Join(dir, "workspaces.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { manager.Close() }()
	g := New(filepath.Join(dir, "models.yaml"))
	g.SetWorkspaceManager(manager)
	g.config = &GatewayConfig{DefaultModel: "local", Models: []ModelConfig{
		{ID: "local", Provider: "ollama", BaseURL: upstream.URL, ModelName: "fixture-local", Enabled: true},
		{ID: "api", Provider: "deepseek", BaseURL: upstream.URL, APIKey: "fixture-api", ModelName: "deepseek-chat", Enabled: true},
		{ID: "api-two", Provider: "deepseek", BaseURL: upstream.URL, APIKey: "fixture-api-two", ModelName: "deepseek-reasoner", Enabled: true},
	}}
	if err := g.saveConfig(); err != nil {
		t.Fatal(err)
	}
	selectModel := func(scope, session, id string) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"scope": scope, "workspace_id": "local", "session_id": session, "model_id": id})
		w := httptest.NewRecorder()
		g.HandleSelectModel(w, httptest.NewRequest("POST", "/api/gateway/selection", strings.NewReader(string(body))))
		return w
	}
	infer := func(session, modelID string, stream bool) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"request_id": "", "workspace_id": "local", "session_id": session, "model_id": modelID, "messages": []map[string]string{{"role": "user", "content": "hello"}}})
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/gateway/chat", strings.NewReader(string(body)))
		if stream {
			g.HandleChatStream(w, req)
		} else {
			g.HandleChat(w, req)
		}
		if w.Code != 200 || strings.Contains(w.Body.String(), `"error"`) {
			t.Fatalf("inference: %d %s", w.Code, w.Body)
		}
	}
	for _, tc := range []struct{ name, from, to, want string }{
		{"local to API", "local", "api", "/chat/completions|deepseek-chat|Bearer fixture-api"},
		{"API to local", "api", "local", "/chat/completions|fixture-local|"},
		{"same Provider different model", "api", "api-two", "/chat/completions|deepseek-reasoner|Bearer fixture-api-two"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, id := range []string{tc.from, tc.to} {
				if w := selectModel("session", "target", id); w.Code != 200 {
					t.Fatalf("select: %d %s", w.Code, w.Body)
				}
			}
			for _, stream := range []bool{false, true} {
				infer("target", "", stream)
				mu.Lock()
				got := calls[len(calls)-1]
				mu.Unlock()
				if got != tc.want {
					t.Fatalf("actual upstream got %q want %q", got, tc.want)
				}
			}
		})
	}
	// Global/workspace changes affect new sessions, not an existing binding.
	if w := selectModel("global", "", "api"); w.Code != 200 {
		t.Fatal(w.Body)
	}
	infer("new-global", "", false)
	if m, err := g.SessionModel("local", "new-global", ""); err != nil || m.ID != "api" {
		t.Fatalf("new global: %v %v", m, err)
	}
	if w := selectModel("workspace", "", "local"); w.Code != 200 {
		t.Fatal(w.Body)
	}
	infer("new-workspace", "", false)
	if m, err := g.SessionModel("local", "new-workspace", ""); err != nil || m.ID != "local" {
		t.Fatalf("new workspace: %v %v", m, err)
	}
	if m, err := g.SessionModel("local", "target", ""); err != nil || m.ID != "api-two" {
		t.Fatalf("old session changed: %v %v", m, err)
	}
	manager.Close()
	manager, err = workspaces.Open(filepath.Join(dir, "workspaces.db"))
	if err != nil {
		t.Fatal(err)
	}
	g = New(filepath.Join(dir, "models.yaml"))
	g.SetWorkspaceManager(manager)
	infer("target", "", true)
	mu.Lock()
	got := calls[len(calls)-1]
	mu.Unlock()
	if got != "/chat/completions|deepseek-reasoner|Bearer fixture-api-two" {
		t.Fatalf("restart lost binding/credential: %q", got)
	}
	if g.config.DefaultModel != "api" {
		t.Fatal("global default lost on restart")
	}
	runs, err := manager.Runs("local")
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range runs {
		if run.Status != "completed" {
			t.Fatalf("successful inference task not completed: %+v", run)
		}
	}
}

func TestSelectionRechecksConfigurationAndTaskAdmissionAfterProbe(t *testing.T) {
	for _, scenario := range []string{"configuration edit", "task starts"} {
		t.Run(scenario, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				<-release
				w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}]}`))
			}))
			defer upstream.Close()
			g := newTestGateway(t)
			m, err := workspaces.Open(filepath.Join(t.TempDir(), "workspace.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			g.SetWorkspaceManager(m)
			g.config.Models = []ModelConfig{{ID: "old", Provider: "ollama", BaseURL: upstream.URL, ModelName: "old", Enabled: true}, {ID: "new", Provider: "deepseek", BaseURL: upstream.URL, ModelName: "new", Enabled: true}}
			if err = m.SelectSessionModel("local", "s", "old"); err != nil {
				t.Fatal(err)
			}
			result := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				w := httptest.NewRecorder()
				g.HandleSelectModel(w, httptest.NewRequest("POST", "/api/gateway/selection", strings.NewReader(`{"scope":"session","workspace_id":"local","session_id":"s","model_id":"new"}`)))
				result <- w
			}()
			<-entered
			if scenario == "configuration edit" {
				g.mu.Lock()
				g.config.Models[1].ModelName = "edited"
				g.mu.Unlock()
			} else {
				_, ex, e := m.Start(context.Background(), workspaces.Run{WorkspaceID: "local", SessionID: "s", Kind: "chat", ModelID: "old"})
				if e != nil {
					t.Fatal(e)
				}
				defer ex.Finish(nil, nil)
			}
			close(release)
			w := <-result
			if w.Code != 409 {
				t.Fatalf("race accepted: %d %s", w.Code, w.Body)
			}
			id, _, err := m.SessionModel("local", "s")
			if err != nil || id != "old" {
				t.Fatalf("race mutated binding: %s %v", id, err)
			}
		})
	}
}

func TestDefaultSelectionRollsBackWhenDiskSaveFails(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}]}`))
	}))
	defer upstream.Close()
	g := newTestGateway(t)
	m, err := workspaces.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	g.SetWorkspaceManager(m)
	g.configPath = filepath.Join(t.TempDir(), "missing", "models.yaml")
	g.config = &GatewayConfig{DefaultModel: "old", Models: []ModelConfig{{ID: "old", Provider: "ollama", Enabled: true, IsDefault: true}, {ID: "new", Provider: "deepseek", BaseURL: upstream.URL, ModelName: "new", Enabled: true}}}
	w := httptest.NewRecorder()
	g.HandleSelectModel(w, httptest.NewRequest("POST", "/api/gateway/selection", strings.NewReader(`{"scope":"global","workspace_id":"local","model_id":"new"}`)))
	if w.Code < 500 || g.config.DefaultModel != "old" || !g.config.Models[0].IsDefault {
		t.Fatalf("failed save changed default: %d %+v", w.Code, g.config)
	}
}

func TestSelectionRejectsInvalidAndBusyTargetsWithoutChangingBinding(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Model string `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&b)
		w.Header().Set("Content-Type", "application/json")
		if b.Model == "bad" {
			w.WriteHeader(401)
			w.Write([]byte(`{"error":"fixture-secret https://private.invalid"}`))
			return
		}
		if b.Model == "empty" {
			w.Write([]byte(`{"choices":[{"message":{"content":""}}]}`))
			return
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}]}`))
	}))
	defer upstream.Close()
	g := newTestGateway(t)
	m, err := workspaces.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	g.SetWorkspaceManager(m)
	for _, id := range []string{"good", "bad", "empty", "disabled"} {
		g.config.Models = append(g.config.Models, ModelConfig{ID: id, Provider: "openai_compatible", ModelName: id, BaseURL: upstream.URL, APIKey: "fixture-secret", Enabled: id != "disabled"})
	}
	selectID := func(id string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		g.HandleSelectModel(w, httptest.NewRequest("POST", "/api/gateway/selection", strings.NewReader(`{"scope":"session","workspace_id":"local","session_id":"s","model_id":"`+id+`"}`)))
		return w
	}
	if w := selectID("good"); w.Code != 200 {
		t.Fatal(w.Body)
	}
	for _, id := range []string{"bad", "empty", "disabled", "missing"} {
		t.Run(id, func(t *testing.T) {
			w := selectID(id)
			if w.Code < 400 {
				t.Fatalf("false success: %s", w.Body)
			}
			if strings.Contains(w.Body.String(), "fixture-secret") || strings.Contains(w.Body.String(), "private.invalid") {
				t.Fatalf("credential leaked: %s", w.Body)
			}
			model, e := g.SessionModel("local", "s", "")
			if e != nil || model.ID != "good" {
				t.Fatalf("failed selection mutated binding: %v %v", model, e)
			}
		})
	}
	// A real queued Agent is sufficient to block a switch, including while waiting.
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	m.Register("agent", "interactive", func(ctx context.Context, ws workspaces.Workspace, input json.RawMessage, emit func(string, string, any)) (any, error) {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil, nil
	})
	if _, err = m.Submit("local", "agent", "busy", json.RawMessage(`{"session_id":"s","model_id":"good"}`)); err != nil {
		t.Fatal(err)
	}
	<-entered
	if w := selectID("bad"); w.Code != 409 {
		t.Fatalf("active task switch: %d %s", w.Code, w.Body)
	}
}

func TestConfigEditDoesNotEnableDisabledModel(t *testing.T) {
	g := newTestGateway(t)
	g.config.Models = []ModelConfig{{ID: "disabled", Enabled: false, Provider: "openai_compatible", ModelName: "disabled"}}
	w := httptest.NewRecorder()
	g.HandleAddModel(w, httptest.NewRequest("POST", "/api/gateway/models", strings.NewReader(`{"id":"disabled","name":"edited","provider":"openai_compatible","base_url":"http://localhost:1234","model_name":"disabled"}`)))
	if w.Code != 201 {
		t.Fatal(w.Body)
	}
	var response struct {
		Added ModelConfig `json:"added"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Added.Enabled {
		t.Fatal("edit response contradicts disabled configuration")
	}
	if g.findModel("disabled") != nil {
		t.Fatal("editing configuration silently enabled disabled model")
	}
}

func TestDefaultFlagsFollowPersistedDefaultOnRestart(t *testing.T) {
	g := newTestGateway(t)
	g.config = &GatewayConfig{DefaultModel: "chosen", Models: []ModelConfig{{ID: "chosen", Enabled: true}, {ID: "other", Enabled: true, IsDefault: true}}}
	if err := g.saveConfig(); err != nil {
		t.Fatal(err)
	}
	restarted := New(g.configPath)
	for _, m := range restarted.config.Models {
		if m.IsDefault != (m.ID == "chosen") {
			t.Fatalf("UI default differs from routing after restart: %+v", m)
		}
	}
}

func TestNonstreamChatDoesNotReportUncommittedResultAsSuccess(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": strings.Repeat("a", 2*1024*1024)}}}})
	}))
	defer upstream.Close()
	g := newTestGateway(t)
	m, err := workspaces.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	g.SetWorkspaceManager(m)
	g.config = &GatewayConfig{DefaultModel: "m", Models: []ModelConfig{{ID: "m", Provider: "ollama", BaseURL: upstream.URL, ModelName: "m", Enabled: true}}}
	w := httptest.NewRecorder()
	g.HandleChat(w, httptest.NewRequest("POST", "/api/gateway/chat", strings.NewReader(`{"workspace_id":"local","session_id":"s","messages":[{"role":"user","content":"hi"}]}`)))
	if w.Code < 400 || strings.Contains(w.Body.String(), `"content"`) {
		t.Fatalf("uncommitted result reported success: HTTP %d, body size %d", w.Code, w.Body.Len())
	}
	if len(g.logs) != 1 || g.logs[0].Status != "error" {
		t.Fatalf("failed chat result was not recorded as an error: %+v", g.logs)
	}
}
