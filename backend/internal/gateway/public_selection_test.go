package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestPublicModelEditPreservesProtocolOverridesForSameTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.yaml")
	config := `default_model: relay
providers:
  - id: relay
    name: Before edit
    type: openai_compatible
    base_url: http://127.0.0.1:11434/v1
    model: fixture-model
    enabled: true
    max_tokens_field: max_completion_tokens
    supports_temperature: false
    tool_transport: responses
`
	if err := os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	g := New(path)
	if m := g.GetFirstModel(); m == nil || m.MaxTokensKey() != "max_completion_tokens" || m.TemperatureEnabled() || m.ToolTransport != "responses" {
		t.Fatalf("fixture overrides not loaded: %+v", m)
	}
	save := func(baseURL string) *ModelConfig {
		t.Helper()
		body := fmt.Sprintf(`{"id":"relay","name":"After edit","provider":"openai_compatible","base_url":%q,"model_name":"fixture-model","is_default":true,"strict_credential_origin":true}`, baseURL)
		response := httptest.NewRecorder()
		g.HandleAddModel(response, httptest.NewRequest(http.MethodPost, "/api/gateway/models", strings.NewReader(body)))
		if response.Code != http.StatusCreated {
			t.Fatalf("model edit returned %d: %s", response.Code, response.Body.String())
		}
		model := New(path).GetFirstModel()
		if model == nil || model.Name != "After edit" {
			t.Fatalf("model edit was not persisted: %+v", model)
		}
		return model
	}
	if m := save("http://127.0.0.1:11434/v1/"); m.MaxTokensKey() != "max_completion_tokens" || m.SupportsTemperature == nil || m.TemperatureEnabled() || m.ToolTransport != "responses" {
		t.Fatalf("rename lost the model's protocol overrides: %+v", m)
	}
	if m := save("http://127.0.0.1:11434/v2"); m.MaxTokensKey() != "" || m.SupportsTemperature != nil || m.ToolTransport != "" {
		t.Fatalf("new target inherited the old target's protocol overrides: %+v", m)
	}
}

func TestEditProbeUsesProtocolOverridesForExistingDeploymentOnly(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `{"data":[{"id":"fixture"}]}`)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		_, hasTemperature := payload["temperature"]
		if r.URL.Path == "/v1/chat/completions" {
			if hasTemperature || payload["max_completion_tokens"] == nil {
				t.Error("probe did not use the saved deployment's protocol overrides")
			}
		} else if r.URL.Path != "/v2/chat/completions" || !hasTemperature || payload["max_completion_tokens"] != nil {
			t.Error("probe reused protocol overrides on a different deployment")
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"4"}}]}`)
	}))
	defer upstream.Close()

	g := New(filepath.Join(t.TempDir(), "models.yaml"))
	disabledTemperature := false
	g.config.Models = []ModelConfig{{ID: "fixture", ModelName: "fixture", Provider: "openai_compatible", BaseURL: upstream.URL + "/v1", Enabled: true,
		SupportsTemperature: &disabledTemperature, MaxTokensField: "max_completion_tokens"}}
	for _, apiPath := range []string{"/v1/", "/v2"} {
		request := fmt.Sprintf(`{"model":{"id":"fixture","model_name":"fixture","provider":"openai_compatible","base_url":%q}}`, upstream.URL+apiPath)
		response := httptest.NewRecorder()
		g.HandleProbeModel(response, httptest.NewRequest(http.MethodPost, "/api/gateway/models/probe", strings.NewReader(request)))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"inference":"succeeded"`) {
			t.Fatalf("probe returned %d: %s", response.Code, response.Body.String())
		}
	}
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

func TestConnectionRejectsInferenceURLsAsBase(t *testing.T) {
	for _, tc := range []struct {
		provider, endpoint string
	}{
		{"openai", "/proxy/v1/chat/completions"},
		{"openai", "/proxy/v1/responses"},
		{"claude", "/proxy/v1/messages"},
		{"gemini", "/proxy/v1beta/models/gemini:generateContent"},
		{"gemini", "/proxy/v1beta/models/gemini:streamGenerateContent"},
	} {
		t.Run(tc.endpoint, func(t *testing.T) {
			model := &ModelConfig{Provider: tc.provider, ModelName: "fixture", BaseURL: "https://relay.example" + tc.endpoint + "/"}
			_, code := connectionEndpoints(model)
			if code != "BASE_URL_REQUIRED" {
				t.Errorf("connection validation returned %q", code)
			}
			g := New(filepath.Join(t.TempDir(), "models.yaml"))
			if _, err := g.getOrCreateProvider(model); err == nil {
				t.Error("saved model accepted an inference endpoint as a base URL")
			}
		})
	}
}

func TestOllamaCompatibilityKeepsReverseProxyPrefix(t *testing.T) {
	model := &ModelConfig{Provider: "ollama", BaseURL: "http://127.0.0.1:11434/engines/ollama/api/", ModelName: "fixture"}
	endpoints, code := connectionEndpoints(model)
	if code != "" || endpoints.catalog != "http://127.0.0.1:11434/engines/ollama/api/tags" ||
		endpoints.inference != "http://127.0.0.1:11434/engines/ollama/v1/chat/completions" {
		t.Errorf("connection endpoints = %+v (%s)", endpoints, code)
	}
}

func TestConnectionDiagnosticReportsUnsupportedTemperatureFallback(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `{"data":[{"id":"fixture"}]}`)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if _, found := payload["temperature"]; found {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":{"message":"Unsupported parameter: temperature"}}`)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"4"}}]}`)
	}))
	defer upstream.Close()

	g := New(filepath.Join(t.TempDir(), "models.yaml"))
	model := &ModelConfig{ID: "fixture", Provider: "openai_compatible", BaseURL: upstream.URL + "/v1", ModelName: "fixture", Enabled: true}
	result := g.testModelContext(context.Background(), model)
	raw, err := json.Marshal(result)
	if err != nil || result.Inference != "succeeded" || !strings.Contains(string(raw), `"temperature_omitted":true`) {
		t.Fatalf("connection diagnostic = %s (%v)", raw, err)
	}
}

func TestProviderCallLogRedactsEchoedCredential(t *testing.T) {
	const credential = "test-credential-kept-out-of-call-logs"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, strings.Repeat("x", 195)+credential)
	}))
	defer upstream.Close()

	g, _, _ := publicSelectionGateway(t)
	g.sem = make(chan struct{}, 1)
	g.providers = make(map[string]providers.LLMProvider)
	g.config.Models[0].BaseURL = upstream.URL + "/v1"
	g.config.Models[0].APIKey = credential
	body := fmt.Sprintf(`{"model_id":"explicit","messages":[{"role":"user","content":%q}]}`, credential)
	response := httptest.NewRecorder()
	g.HandleChat(response, httptest.NewRequest(http.MethodPost, "/api/gateway/chat", strings.NewReader(body)))
	if response.Code != http.StatusOK {
		t.Fatalf("chat returned %d: %s", response.Code, response.Body.String())
	}
	logs := httptest.NewRecorder()
	g.HandleLogs(logs, httptest.NewRequest(http.MethodGet, "/api/gateway/logs", nil))
	if contents := logs.Body.String(); strings.Contains(contents, credential[:5]) || !strings.Contains(contents, "[redacted]") {
		t.Error("request or response previews retained a full or truncated credential")
	}
}

func TestPublicModelSelectionProviderRouting(t *testing.T) {
	type providerRequest struct {
		Model  string
		Stream bool
	}
	var mu sync.Mutex
	var calls []providerRequest
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, "{\"data\":[{\"id\":\"explicit\"},{\"id\":\"global\"},{\"id\":\"unauthorized\"},{\"id\":\"empty\"},{\"id\":\"slow\"}]}")
			return
		}
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
			fmt.Fprint(w, "{\"error\":{\"code\":\"model_not_found\",\"message\":\"Model not found\"}}")
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

	for _, failure := range []struct{ modelID, code string }{
		{"unauthorized", "AUTHENTICATION_FAILED"},
		{"not-found", "MODEL_NOT_FOUND"},
		{"empty", "EMPTY_ANSWER"},
		{"slow", "REQUEST_TIMEOUT"},
	} {
		t.Run(failure.modelID, func(t *testing.T) {
			ctx := context.Background()
			if failure.modelID == "slow" {
				deadline, cancel := context.WithTimeout(ctx, 2*time.Second)
				defer cancel()
				ctx = deadline
			}
			w := selectModel(ctx, failure.modelID)
			var response struct {
				Diagnostic ModelDiagnostic
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || w.Code != http.StatusBadGateway || response.Diagnostic.Code != failure.code {
				t.Fatalf("意外的连接验证结果：%d %s", w.Code, w.Body.String())
			}
			requirePublicSessionModel(t, g, "local", "routing", "", "global")
		})
	}
	if w := selectModel(context.Background(), "explicit"); w.Code != http.StatusOK {
		t.Fatalf("切回原会话模型失败：%d %s", w.Code, w.Body.String())
	}
	chat("explicit", true)
	requirePublicSessionModel(t, g, "local", "routing", "", "explicit")
	mu.Lock()
	defer mu.Unlock()
	want := []providerRequest{
		{Model: "explicit"}, {Model: "global"}, {Model: "global", Stream: true},
		{Model: "unauthorized"}, {Model: "not-found"}, {Model: "empty"}, {Model: "slow"},
		{Model: "explicit"}, {Model: "explicit", Stream: true},
	}
	if !slices.Equal(calls, want) {
		t.Fatalf("本地推理服务收到 %+v，预期 %+v", calls, want)
	}
}

func TestScopedImageAndProtocolOptionsAcrossReload(t *testing.T) {
	requests := make(chan map[string]any, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		requests <- body
		fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"content":"green"}}]}`)
	}))
	defer server.Close()
	base, manager, ws := publicSelectionGateway(t)
	gw := NewWithSecretStore(base.configPath, nil)
	gw.SetWorkspaceManager(manager)
	update := func(options map[string]any, code int) {
		payload := map[string]any{"id": "explicit", "name": "Local", "provider": "openai_compatible", "base_url": server.URL + "/v1", "model_name": "test-model", "temperature": 0, "protocol_options": options}
		data, _ := json.Marshal(payload)
		rec := httptest.NewRecorder()
		gw.HandleAddModel(rec, httptest.NewRequest("POST", "/", bytes.NewReader(data)))
		if rec.Code != code {
			t.Fatalf("update status %d: %s", rec.Code, rec.Body)
		}
	}
	update(map[string]any{"top_p": 1.5}, 400)
	update(map[string]any{"streaming": false, "tools": false, "top_p": 0.7}, 201)
	gw.ReloadConfig()
	selected := gw.findModelByID("explicit")
	if selected.Temperature == nil || *selected.Temperature != 0 || gw.ToolCapability(selected) == nil || selected.visionEnabled() {
		t.Fatal("explicit zero/tool block/unknown vision changed on reload")
	}
	if gw.config.DefaultModel != "global" {
		t.Fatal("update changed default selection")
	}
	mux := http.NewServeMux()
	manager.RegisterRoutes(mux)
	var raw bytes.Buffer
	if err := png.Encode(&raw, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	meta, _ := json.Marshal(map[string]any{"workspace_id": ws.ID, "session_id": "test-session", "project_path": ws.ProjectPath, "size": raw.Len(), "id": "image-one", "name": "a.png"})
	upload := httptest.NewRequest("POST", "/api/chat-attachments/stage", bytes.NewReader(raw.Bytes()))
	upload.Header.Set("X-Noosphere-Attachment", url.PathEscape(string(meta)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, upload)
	if rec.Code != 200 {
		t.Fatalf("stage %d: %s", rec.Code, rec.Body)
	}
	turn := workspaces.AttachmentTurn{MessageID: "user-1", IDs: []string{"image-one"}, Question: "Color?"}
	if _, err := manager.PrepareModelAttachments(context.Background(), ws.ID, "other", turn, true); err == nil {
		t.Fatal("cross-session image disclosure")
	}
	send := func() *httptest.ResponseRecorder {
		data, _ := json.Marshal(map[string]any{"model_id": "explicit", "workspace_id": ws.ID, "session_id": "test-session", "request_id": "img-run", "attachment_turn": turn, "messages": []map[string]string{{"role": "user", "content": "Color?"}}})
		recorder := httptest.NewRecorder()
		gw.HandleChatStream(recorder, httptest.NewRequest("POST", "/", bytes.NewReader(data)))
		return recorder
	}
	denied := send()
	if denied.Code != 400 || len(requests) != 0 {
		t.Fatalf("unknown vision sent data: %d %s", denied.Code, denied.Body)
	}
	update(map[string]any{"vision": true, "streaming": false, "tools": false, "top_p": 0.7}, 201)
	result := send()
	if result.Code != 200 || !strings.Contains(result.Body.String(), "green") {
		t.Fatalf("chat %d: %s", result.Code, result.Body)
	}
	select {
	case payload := <-requests:
		if payload["temperature"] != float64(0) || payload["top_p"] != 0.7 || payload["stream"] != false {
			t.Fatalf("bad protocol options: %v", payload)
		}
		data, _ := json.Marshal(payload["messages"])
		imageURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw.Bytes())
		if bytes.Contains(data, []byte("File excerpts")) {
			t.Fatal("image-only turn received synthetic text excerpts")
		}
		if !bytes.Contains(data, []byte(imageURL)) {
			t.Fatal("provider did not receive staged image")
		}
		if strings.Contains(result.Body.String(), imageURL) {
			t.Fatal("image body leaked in result")
		}
	default:
		t.Fatal("provider was not called")
	}
}
