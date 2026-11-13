package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/noosphere/backend/internal/gateway/providers"
	"github.com/noosphere/backend/internal/workspaces"
	"gopkg.in/yaml.v3"
)

func publicSelectionGateway(t *testing.T) (*Gateway, *workspaces.Manager, workspaces.Workspace) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "models.yaml")
	config := GatewayConfig{DefaultModel: "global", Models: []ModelConfig{
		{ID: "explicit", Provider: "openai_compatible", BaseURL: "http://127.0.0.1:1/v1", ModelName: "explicit", Enabled: true},
		{ID: "global", Provider: "openai_compatible", BaseURL: "http://127.0.0.1:1/v1", ModelName: "global", Enabled: true},
		{ID: "workspace", Provider: "openai_compatible", BaseURL: "http://127.0.0.1:1/v1", ModelName: "workspace", Enabled: true},
		{ID: "disabled", Provider: "openai_compatible", BaseURL: "http://127.0.0.1:1/v1", ModelName: "disabled", Enabled: false},
	}}
	data, err := yaml.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := workspaces.Open(filepath.Join(dir, "workspace.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Error(err)
		}
	})
	workspace, err := manager.SaveWorkspace(workspaces.Workspace{Name: "模型选择检查", DefaultModelID: "workspace"})
	if err != nil {
		t.Fatal(err)
	}
	// 模型选择仅使用配置和 SQLite，不读取系统凭据或发起推理。
	g := &Gateway{configPath: configPath}
	g.ReloadConfig()
	g.SetWorkspaceManager(manager)
	return g, manager, workspace
}

func requirePublicSessionModel(t *testing.T, g *Gateway, workspaceID, sessionID, requested, want string) {
	t.Helper()
	model, err := g.SessionModel(workspaceID, sessionID, requested)
	if err != nil || model == nil || model.ID != want {
		t.Fatalf("工作区 %q 会话 %q 选择结果 %+v，错误 %v；预期 %q", workspaceID, sessionID, model, err, want)
	}
}

func TestPublicModelSelectionScopes(t *testing.T) {
	g, manager, workspace := publicSelectionGateway(t)
	requirePublicSessionModel(t, g, "local", "global-session", "", "global")
	requirePublicSessionModel(t, g, workspace.ID, "workspace-session", "", "workspace")
	requirePublicSessionModel(t, g, workspace.ID, "explicit-session", "explicit", "explicit")
	requirePublicSessionModel(t, g, "", "default-workspace", "", "global")
	requirePublicSessionModel(t, g, workspace.ID, "shared-session", "", "workspace")
	requirePublicSessionModel(t, g, "local", "shared-session", "", "global")
	if _, err := g.SessionModel(workspace.ID, "explicit-session", "global"); err == nil {
		t.Fatal("过期的请求改变了已保存的会话模型")
	}
	id, bound, err := manager.SessionModel(workspace.ID, "explicit-session")
	if err != nil || !bound || id != "explicit" {
		t.Fatalf("会话绑定发生变化: %s %t %v", id, bound, err)
	}
	for _, requested := range []string{"", "global"} {
		if _, err := g.SessionModel("unknown-workspace", "session", requested); !errors.Is(err, workspaces.ErrNotFound) {
			t.Fatalf("未知工作区未被拒绝: %v", err)
		}
	}
}

func TestPublicSessionSelectionPersistsAcrossDefaultsAndRestart(t *testing.T) {
	g, manager, workspace := publicSelectionGateway(t)
	requirePublicSessionModel(t, g, workspace.ID, "existing", "", "workspace")
	request := httptest.NewRequest(http.MethodPost, "/api/gateway/models", strings.NewReader(`{"id":"explicit","provider":"openai_compatible","model_name":"explicit","is_default":true}`))
	response := httptest.NewRecorder()
	g.HandleAddModel(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("保存全局默认模型失败: %d %s", response.Code, response.Body.String())
	}
	if err := manager.SelectWorkspaceModel(workspace.ID, "global"); err != nil {
		t.Fatal(err)
	}
	requirePublicSessionModel(t, g, "local", "new-global", "", "explicit")
	requirePublicSessionModel(t, g, workspace.ID, "new-workspace", "", "global")
	requirePublicSessionModel(t, g, workspace.ID, "existing", "", "workspace")
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := workspaces.Open(filepath.Join(filepath.Dir(g.configPath), "workspace.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reloaded := &Gateway{configPath: g.configPath}
	reloaded.ReloadConfig()
	reloaded.SetWorkspaceManager(reopened)
	requirePublicSessionModel(t, reloaded, workspace.ID, "existing", "", "workspace")
	requirePublicSessionModel(t, reloaded, workspace.ID, "after-restart", "", "global")
	requirePublicSessionModel(t, reloaded, "local", "after-restart", "", "explicit")
}

func TestPublicModelSelectionRejectsUnavailableModels(t *testing.T) {
	g, manager, workspace := publicSelectionGateway(t)
	for _, requested := range []string{"missing", "disabled"} {
		t.Run(requested, func(t *testing.T) {
			if _, err := g.SessionModel(workspace.ID, "new-"+requested, requested); err == nil {
				t.Fatal("无效的显式模型被接受")
			}
			if _, bound, err := manager.SessionModel(workspace.ID, "new-"+requested); err != nil || bound {
				t.Fatalf("失败请求创建了会话绑定: %t %v", bound, err)
			}
			if err := manager.SelectSessionModel(workspace.ID, "saved-"+requested, requested); err != nil {
				t.Fatal(err)
			}
			if _, err := g.SessionModel(workspace.ID, "saved-"+requested, ""); err == nil {
				t.Fatal("已保存但不可用的模型被替换")
			}
			if err := manager.SelectWorkspaceModel(workspace.ID, requested); err != nil {
				t.Fatal(err)
			}
			if _, err := g.SessionModel(workspace.ID, "workspace-"+requested, ""); err == nil {
				t.Fatal("不可用的工作区默认模型被接受")
			}
		})
	}
	requirePublicSessionModel(t, g, workspace.ID, "valid-explicit", "explicit", "explicit")
	g.config.DefaultModel = "disabled"
	requirePublicSessionModel(t, g, "local", "implicit-enabled", "", "explicit")
	for i := range g.config.Models {
		g.config.Models[i].Enabled = false
	}
	if _, err := g.SessionModel("local", "none-enabled", ""); err == nil {
		t.Fatal("没有启用的模型时仍然创建了会话")
	}
}

func TestPublicModelSelectionRejectsSwitchDuringActiveTask(t *testing.T) {
	g, manager, workspace := publicSelectionGateway(t)
	_, execution, err := manager.Start(context.Background(), workspaces.Run{WorkspaceID: workspace.ID, SessionID: "active", Kind: "chat", ModelID: "workspace"})
	if err != nil {
		t.Fatal(err)
	}
	defer execution.Finish(nil, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/gateway/selection", strings.NewReader(`{"scope":"session","workspace_id":"`+workspace.ID+`","session_id":"active","model_id":"explicit"}`))
	response := httptest.NewRecorder()
	g.HandleSelectModel(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("运行中的会话模型切换请求未被拒绝: %d %s", response.Code, response.Body.String())
	}
	requirePublicSessionModel(t, g, workspace.ID, "active", "", "workspace")
	if err := execution.Finish(nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := manager.SelectSessionModel(workspace.ID, "active", "explicit"); err != nil {
		t.Fatal(err)
	}
	requirePublicSessionModel(t, g, workspace.ID, "active", "", "explicit")
}

func TestPublicModelSelectionProviderRouting(t *testing.T) {
	type providerRequest struct {
		Model  string
		Stream bool
	}
	var mu sync.Mutex
	var calls []providerRequest
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			t.Errorf("意外的本地请求：%s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var call providerRequest
		if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		calls = append(calls, call)
		mu.Unlock()
		content := "OK"
		switch call.Model {
		case "unauthorized":
			w.WriteHeader(http.StatusUnauthorized)
			return
		case "not-found":
			w.WriteHeader(http.StatusNotFound)
			return
		case "empty":
			content = ""
		case "slow":
			<-r.Context().Done()
			return
		}
		if call.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\ndata: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, content)
	}))
	defer upstream.Close()

	g, manager, _ := publicSelectionGateway(t)
	g.sem = make(chan struct{}, 1)
	g.providers = make(map[string]providers.LLMProvider)
	g.config.DefaultModel = ""
	for i := range g.config.Models {
		g.config.Models[i].BaseURL = upstream.URL + "/v1"
		g.config.Models[i].IsDefault = false
	}
	for _, id := range []string{"unauthorized", "not-found", "empty", "slow"} {
		g.config.Models = append(g.config.Models, ModelConfig{
			ID: id, Provider: "openai_compatible", BaseURL: upstream.URL + "/v1", ModelName: id, Enabled: true,
		})
	}

	selectModel := func(ctx context.Context, modelID string) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"scope":"session","workspace_id":"local","session_id":"routing","model_id":%q}`, modelID)
		w := httptest.NewRecorder()
		g.HandleSelectModel(w, httptest.NewRequest(http.MethodPost, "/api/gateway/selection", strings.NewReader(body)).WithContext(ctx))
		return w
	}
	chat := func(modelID string, stream bool) {
		t.Helper()
		body := fmt.Sprintf(`{"workspace_id":"local","session_id":"routing","model_id":%q,"messages":[{"role":"user","content":"Hello"}]}`, modelID)
		req := httptest.NewRequest(http.MethodPost, "/api/gateway/chat", strings.NewReader(body))
		w := httptest.NewRecorder()
		if stream {
			g.HandleChatStream(w, req)
		} else {
			g.HandleChat(w, req)
		}
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "OK") || strings.Contains(w.Body.String(), `"error"`) {
			t.Fatalf("对话失败：%d %s", w.Code, w.Body.String())
		}
	}

	chat("", false)
	requirePublicSessionModel(t, g, "local", "routing", "", "explicit")
	if w := selectModel(context.Background(), "global"); w.Code != http.StatusOK {
		t.Fatalf("切换会话模型失败：%d %s", w.Code, w.Body.String())
	}
	chat("global", true)
	runs, err := manager.Runs("local")
	if err != nil || len(runs) != 2 {
		t.Fatalf("读取对话任务失败：%+v %v", runs, err)
	}
	completed := make(map[string]bool)
	for _, run := range runs {
		if run.Status == "completed" && run.SessionID == "routing" {
			completed[run.ModelID] = true
		}
	}
	if !completed["explicit"] || !completed["global"] {
		t.Fatalf("对话任务未记录切换前后的模型 ID：%+v", runs)
	}

	for _, failure := range []struct{ modelID, message string }{
		{"unauthorized", "HTTP 401"},
		{"not-found", "HTTP 404"},
		{"empty", "模型连接验证失败"},
		{"slow", "模型连接验证超时"},
	} {
		t.Run(failure.modelID, func(t *testing.T) {
			ctx := context.Background()
			if failure.modelID == "slow" {
				deadline, cancel := context.WithTimeout(ctx, 2*time.Second)
				defer cancel()
				ctx = deadline
			}
			w := selectModel(ctx, failure.modelID)
			if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), failure.message) {
				t.Fatalf("意外的连接验证结果：%d %s", w.Code, w.Body.String())
			}
			requirePublicSessionModel(t, g, "local", "routing", "", "global")
		})
	}
	mu.Lock()
	defer mu.Unlock()
	want := []providerRequest{
		{Model: "explicit"}, {Model: "global"}, {Model: "global", Stream: true},
		{Model: "unauthorized"}, {Model: "not-found"}, {Model: "empty"}, {Model: "slow"},
	}
	if !slices.Equal(calls, want) {
		t.Fatalf("本地推理服务收到 %+v，预期 %+v", calls, want)
	}
}
