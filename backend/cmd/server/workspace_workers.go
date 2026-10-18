package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	devopsAgent "github.com/noosphere/backend/internal/devops/agent"
	"github.com/noosphere/backend/internal/gateway"
	"github.com/noosphere/backend/internal/nebula/engine"
	"github.com/noosphere/backend/internal/workspaces"
	"net/http"
	"strings"
	"time"
)

// Internal module requests use a fixed route and the job's context, never an
// arbitrary URL supplied by a model or the UI.
type moduleResponse struct {
	header    http.Header
	body      bytes.Buffer
	status    int
	oversized bool
}

func (r *moduleResponse) Header() http.Header { return r.header }
func (r *moduleResponse) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}
func (r *moduleResponse) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = 200
	}
	if r.body.Len()+len(b) > 2*1024*1024 {
		r.oversized = true
		return 0, fmt.Errorf("module result exceeds limit")
	}
	return r.body.Write(b)
}
func requestModule(ctx context.Context, handler http.Handler, path string, input any) (map[string]any, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	out := &moduleResponse{header: make(http.Header)}
	handler.ServeHTTP(out, req)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if out.oversized {
		return nil, fmt.Errorf("module result exceeds limit")
	}
	if out.status < 200 || out.status >= 300 {
		return nil, fmt.Errorf("module request failed (HTTP %d); check module availability and project path", out.status)
	}
	var result map[string]any
	if err = json.Unmarshal(out.body.Bytes(), &result); err != nil {
		return nil, fmt.Errorf("invalid module response")
	}
	if result["error"] != nil {
		return nil, fmt.Errorf("module reported an error")
	}
	return result, nil
}

func registerWorkspaceWorkers(m *workspaces.Manager, mux http.Handler, eng *engine.Engine, gw *gateway.Gateway, indexEnabled, diagnosisEnabled bool) {
	m.Register("index", "background", func(parent context.Context, ws workspaces.Workspace, input json.RawMessage, emit func(string, string, any)) (any, error) {
		if !indexEnabled {
			return nil, fmt.Errorf("CodeLens is disabled")
		}
		if ws.ProjectPath == "" {
			return nil, fmt.Errorf("请先为工作区设置项目目录")
		}
		var args struct {
			Exclude []string `json:"exclude"`
		}
		if len(input) > 0 && string(input) != "null" {
			if err := json.Unmarshal(input, &args); err != nil {
				return nil, fmt.Errorf("invalid index input")
			}
		}
		if args.Exclude == nil {
			args.Exclude = []string{}
		}
		ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
		defer cancel()
		current, err := m.Workspace(ws.ID)
		if err != nil {
			return nil, err
		}
		if current.ProjectPath != ws.ProjectPath {
			return nil, fmt.Errorf("workspace project changed while task was queued")
		}
		emit("indexing", "正在解析项目并更新索引", nil)
		result, err := requestModule(ctx, mux, "/api/v1/codelens/index", map[string]any{"project_path": ws.ProjectPath, "exclude": args.Exclude})
		if err != nil {
			return nil, err
		}
		projectID, ok := result["project_id"].(string)
		if !ok || projectID == "" || result["retrieval_ready"] != true {
			return nil, fmt.Errorf("index did not publish a retrieval-ready project")
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err = m.BindProject(ws.ID, ws.ProjectPath, projectID); err != nil {
			return nil, err
		}
		emit("indexed", "项目索引已保存", map[string]any{"project_id": projectID, "indexed_chunks": result["indexed_chunks"]})
		return result, nil
	})
	m.Register("diagnosis", "interactive", func(ctx context.Context, ws workspaces.Workspace, input json.RawMessage, emit func(string, string, any)) (any, error) {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		if !diagnosisEnabled {
			return nil, fmt.Errorf("DevOps is disabled")
		}
		var args struct {
			Issue string `json:"issue"`
		}
		if err := json.Unmarshal(input, &args); err != nil {
			return nil, fmt.Errorf("invalid diagnosis input")
		}
		args.Issue = strings.TrimSpace(args.Issue)
		if args.Issue == "" || len(args.Issue) > 16000 {
			return nil, fmt.Errorf("diagnosis requires an issue up to 4000 characters")
		}
		// Freeze the chosen model when execution starts; a later settings edit cannot
		// silently route the next diagnostic step to another provider.
		model, err := gw.WorkspaceModel(ws.ID, ws.DefaultModelID)
		if err != nil {
			return nil, err
		}
		var modelErr error
		agent := devopsAgent.New(eng, func(callCtx context.Context, messages []map[string]string) (string, error) {
			content, _, _, err := gw.ChatContext(callCtx, model, messages, "", 0.1, 1024)
			if err != nil {
				modelErr = err
			}
			return content, err
		})
		emit("planning", "正在分析诊断问题", nil)
		result := agent.LoopContextWithEvents(ctx, args.Issue, 5, nil, func(step devopsAgent.LoopStep) { emit("tool", "诊断步骤 · "+step.Action, step) })
		output := map[string]any{"issue": args.Issue, "answer": result.Answer, "trace": result.Steps, "tools_used": result.ToolsUsed, "steps": len(result.Steps)}
		if ctx.Err() != nil {
			return output, ctx.Err()
		}
		if modelErr != nil {
			return output, modelErr
		}
		return output, nil
	})
}
