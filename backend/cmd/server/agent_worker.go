package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/noosphere/backend/internal/agent"
	"github.com/noosphere/backend/internal/devops/tools"
	"github.com/noosphere/backend/internal/gateway"
	"github.com/noosphere/backend/internal/gateway/providers"
	"github.com/noosphere/backend/internal/knowledge"
	"github.com/noosphere/backend/internal/workspaces"
	"io"
)

func registerAgentWorker(manager *workspaces.Manager, gw *gateway.Gateway, knowledgeService *knowledge.Service, systemEnabled bool, actionStores ...*agent.Actions) {
	manager.Register("agent", "interactive", func(ctx context.Context, ws workspaces.Workspace, input json.RawMessage, emit func(string, string, any)) (any, error) {
		var args struct {
			Goal            string `json:"goal"`
			ModelID         string `json:"model_id"`
			SessionID       string `json:"session_id"`
			ReviewedActions bool   `json:"reviewed_actions"`
		}
		decoder := json.NewDecoder(bytes.NewReader(input))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&args); err != nil {
			return nil, fmt.Errorf("invalid Agent task input")
		}
		if decoder.Decode(new(any)) != io.EOF {
			return nil, fmt.Errorf("invalid trailing Agent input")
		}
		if gw == nil {
			return nil, fmt.Errorf("model gateway unavailable")
		}
		model, err := gw.WorkspaceModel(ws.ID, args.ModelID)
		if err != nil {
			return nil, err
		}
		if err = gw.ToolCapability(model); err != nil {
			return nil, err
		}
		validate := func() error {
			current, err := manager.Workspace(ws.ID)
			if err != nil {
				return err
			}
			if current.ProjectPath != ws.ProjectPath || current.ProjectID != ws.ProjectID {
				return fmt.Errorf("工作区项目绑定已变化，Agent 已停止；请在当前项目重新发起")
			}
			return nil
		}
		var lookup agent.ContextLookup
		if knowledgeService != nil {
			lookup = knowledgeService.Context
		}
		var system agent.SystemLookup
		if systemEnabled {
			system = func(ctx context.Context, name string, args map[string]any) (string, error) {
				var out tools.ToolResult
				switch name {
				case "system_processes":
					out = tools.ExecTaskListContext(ctx)
				case "system_connections":
					out = tools.ExecNetStatContext(ctx)
				case "system_services":
					out = tools.ExecServiceListContext(ctx)
				case "service_status":
					out = tools.ExecServiceStatusContext(ctx, args["name"].(string))
				default:
					return "", fmt.Errorf("unavailable system tool")
				}
				if !out.Success {
					return "", fmt.Errorf("%s", out.Error)
				}
				return out.Output, nil
			}
		}
		registry := agent.NewRegistry(ws, lookup, system)
		if args.ReviewedActions {
			if len(actionStores) == 0 || actionStores[0] == nil {
				return nil, fmt.Errorf("操作服务不可用")
			}
			registry.WithActions(actionStores[0], workspaces.RunID(ctx))
		}
		modeLabel := "只读 Agent 已启动"
		if args.ReviewedActions {
			modeLabel = "Agent 操作提案模式已启动，执行前需确认"
		}
		emit("agent_started", modeLabel, map[string]any{"model_id": model.ID, "workspace_id": ws.ID, "max_model_calls": 6, "max_tool_calls": 12, "timeout_seconds": 120})
		result, err := agent.Run(ctx, args.Goal, registry, func(ctx context.Context, messages []providers.ToolMessage, definitions []providers.ToolDefinition) (*providers.ToolResponse, error) {
			return gw.ChatTools(ctx, model, ws.ID, messages, definitions)
		}, emit, validate)
		result.ModelID = model.ID
		return result, err
	})
}
