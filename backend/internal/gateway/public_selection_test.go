package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
