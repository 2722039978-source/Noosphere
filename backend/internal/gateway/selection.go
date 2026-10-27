package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/noosphere/backend/internal/gateway/providers"
	"github.com/noosphere/backend/internal/workspaces"
)

func selectionReply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
func selectionFailure(w http.ResponseWriter, err error) {
	status, message := 503, "模型绑定保存失败，请重试；原绑定保持不变"
	switch {
	case errors.Is(err, workspaces.ErrConflict):
		status, message = 409, "会话模型或任务状态已变化，请等待任务结束并刷新后重试"
	case errors.Is(err, workspaces.ErrInvalid):
		status, message = 400, "无效的工作区或会话标识"
	case errors.Is(err, workspaces.ErrNotFound):
		status, message = 404, "工作区不存在"
	}
	selectionReply(w, status, map[string]string{"error": message})
}

// SessionModel is shared by chat, streaming chat and Agent. Existing backend
// bindings are authoritative; a stale explicit request is rejected, never rerouted.
func (g *Gateway) SessionModel(workspaceID, sessionID, requested string) (*ModelConfig, error) {
	if workspaceID == "" {
		workspaceID = "local"
	}
	if sessionID == "" {
		return g.WorkspaceModel(workspaceID, requested)
	}
	if g.workspaceManager == nil {
		return nil, fmt.Errorf("session model storage unavailable")
	}
	id, bound, err := g.workspaceManager.SessionModel(workspaceID, sessionID)
	if err != nil {
		return nil, err
	}
	if bound {
		if requested != "" && requested != id {
			return nil, fmt.Errorf("会话模型已变化，请刷新后重试")
		}
		return g.WorkspaceModel(workspaceID, id)
	}
	model, err := g.WorkspaceModel(workspaceID, requested)
	if err != nil {
		return nil, err
	}
	id, err = g.workspaceManager.PinSessionModel(workspaceID, sessionID, model.ID)
	if err != nil {
		return nil, err
	}
	if id != model.ID {
		return nil, fmt.Errorf("会话模型已变化，请刷新后重试")
	}
	return model, nil
}

func (g *Gateway) HandleModelSelection(w http.ResponseWriter, r *http.Request) {
	ws, session := r.URL.Query().Get("workspace_id"), r.URL.Query().Get("session_id")
	if ws == "" {
		ws = "local"
	}
	if g.workspaceManager == nil {
		selectionFailure(w, workspaces.ErrClosed)
		return
	}
	id, bound, err := g.workspaceManager.SessionModel(ws, session)
	if err != nil {
		selectionFailure(w, err)
		return
	}
	selectionReply(w, 200, map[string]any{"model_id": id, "bound": bound, "workspace_id": ws, "session_id": session})
}

func safeSelectionProbeError(err error) string {
	s := strings.ToLower(err.Error())
	for _, status := range []string{"401", "403", "402", "404", "429"} {
		if strings.Contains(s, status) {
			return "模型连接验证失败（HTTP " + status + "），请检查权限、余额或模型配置"
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(s, "timeout") {
		return "模型连接验证超时，请检查服务是否已启动"
	}
	return "模型连接验证失败，请检查 Provider 服务、模型名称与连接配置"
}

func (g *Gateway) HandleSelectModel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Scope       string `json:"scope"`
		WorkspaceID string `json:"workspace_id"`
		SessionID   string `json:"session_id"`
		ModelID     string `json:"model_id"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF {
		selectionReply(w, 400, map[string]string{"error": "无效的模型选择请求"})
		return
	}
	if body.WorkspaceID == "" {
		body.WorkspaceID = "local"
	}
	if body.Scope != "session" && body.Scope != "workspace" && body.Scope != "global" {
		selectionReply(w, 400, map[string]string{"error": "请选择会话、工作区默认或全局默认作用域"})
		return
	}
	if g.workspaceManager == nil {
		selectionFailure(w, workspaces.ErrClosed)
		return
	}
	if body.Scope == "session" {
		if _, _, err := g.workspaceManager.SessionModel(body.WorkspaceID, body.SessionID); err != nil {
			selectionFailure(w, err)
			return
		}
	}
	idleWS, idleSession := body.WorkspaceID, ""
	if body.Scope == "session" {
		idleSession = body.SessionID
	}
	if body.Scope == "global" {
		idleWS = ""
	}
	if err := g.workspaceManager.WithIdleModelTasks(idleWS, idleSession, func() error { return nil }); err != nil {
		selectionFailure(w, err)
		return
	}
	model, err := g.WorkspaceModel(body.WorkspaceID, body.ModelID)
	if body.Scope == "workspace" && body.ModelID == "" {
		model = g.findModel("")
		if model == nil {
			err = fmt.Errorf("no enabled global model")
		} else {
			err = nil
		}
	}
	if err != nil {
		selectionReply(w, 400, map[string]string{"error": "模型不存在、已禁用或工作区不可用"})
		return
	}
	if providers.Get(model.Provider) == nil {
		selectionReply(w, 400, map[string]string{"error": "Provider 协议不受支持，请检查模型配置"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	content, _, _, err := g.callLLMContext(ctx, model, []map[string]string{{"role": "user", "content": "Reply with OK only."}}, "", 0, 128)
	if err != nil {
		selectionReply(w, 502, map[string]string{"error": safeSelectionProbeError(err)})
		return
	}
	if strings.TrimSpace(content) == "" {
		selectionReply(w, 502, map[string]string{"error": "模型未返回正文，未切换模型"})
		return
	}
	if ctx.Err() != nil {
		selectionReply(w, 504, map[string]string{"error": "模型验证已取消或超时，未切换模型"})
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	unchanged := false
	for _, current := range g.config.Models {
		if current.ID == model.ID && current == *model {
			unchanged = true
			break
		}
	}
	if !unchanged {
		selectionReply(w, 409, map[string]string{"error": "验证期间模型配置已变化，请重试"})
		return
	}
	switch body.Scope {
	case "session":
		err = g.workspaceManager.SelectSessionModel(body.WorkspaceID, body.SessionID, model.ID)
	case "workspace":
		err = g.workspaceManager.SelectWorkspaceModel(body.WorkspaceID, body.ModelID)
	case "global":
		err = g.workspaceManager.WithIdleModelTasks("", "", func() error {
			previous := g.config
			next := *previous
			next.Models = append([]ModelConfig(nil), previous.Models...)
			next.DefaultModel = model.ID
			for i := range next.Models {
				next.Models[i].IsDefault = next.Models[i].ID == model.ID
			}
			g.config = &next
			if e := g.saveConfig(); e != nil {
				g.config = previous
				return e
			}
			return nil
		})
	}
	if err != nil {
		selectionFailure(w, err)
		return
	}
	selectionReply(w, 200, map[string]any{"model_id": model.ID, "bound": true, "scope": body.Scope, "workspace_id": body.WorkspaceID, "session_id": body.SessionID})
}
