package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/noosphere/backend/internal/agent/toolpack"
	"io"
	"runtime"
	"strings"
	"time"

	"github.com/noosphere/backend/internal/agent"
	"github.com/noosphere/backend/internal/agent/artifact"
	agentcore "github.com/noosphere/backend/internal/agent/core"
	"github.com/noosphere/backend/internal/agent/host"
	"github.com/noosphere/backend/internal/agent/sandbox"
	"github.com/noosphere/backend/internal/gateway"
	"github.com/noosphere/backend/internal/gateway/providers"
	"github.com/noosphere/backend/internal/workspaces"
)

type agentBudget struct {
	CommandSeconds int `json:"command_seconds"`
	DiskMiB        int `json:"disk_mib"`
	ModelCalls     int `json:"model_calls"`
	ToolCalls      int `json:"tool_calls"`
	Tokens         int `json:"tokens"`
	ActiveSeconds  int `json:"active_seconds"`
}
type deliverySession struct {
	ctx     context.Context
	manager *workspaces.Manager
	id      string
}

func (s deliverySession) Load() (json.RawMessage, error) { return s.manager.AgentCheckpoint(s.id) }
func (s deliverySession) Save(b json.RawMessage, phase, msg string, data any) error {
	err := s.manager.SaveAgentCheckpoint(s.ctx, s.id, b, phase, msg, data)
	if errors.Is(err, workspaces.ErrAgentControlPending) {
		return agentcore.ErrControlsPending
	}
	return err
}
func (s deliverySession) Controls(after int64) ([]agentcore.Control, error) {
	in, e := s.manager.AgentControls(s.id, after)
	if e != nil {
		return nil, e
	}
	out := make([]agentcore.Control, len(in))
	for i, c := range in {
		out[i] = agentcore.Control{Seq: c.Seq, Action: c.Action, Value: c.Value}
	}
	return out, nil
}
func runDeliveryAgent(ctx context.Context, m *workspaces.Manager, ws workspaces.Workspace, goal string, model *gateway.ModelConfig, gw *gateway.Gateway, registry *agent.Registry, mode string, budget agentBudget, systemEnabled bool) (any, error) {
	id := workspaces.RunID(ctx)
	validate := func() error { return m.ValidateAgent(ctx, id, ws.ID) }
	if e := validate(); e != nil {
		return nil, e
	}
	s, e := m.AgentState(id, ws.ID)
	if e != nil {
		return nil, e
	}
	if s.ModelHash != "" && gateway.AgentFingerprint(model) != s.ModelHash {
		return nil, workspaces.AgentWait{Status: "paused", Reason: "模型配置已变化，请恢复原配置后继续"}
	}
	d, e := host.Open(ctx, m.AgentDataDir(), id, ws.ProjectPath, s.Binding)
	if e != nil {
		return nil, e
	}
	o := agentcore.Options{MaxCommandSeconds: budget.CommandSeconds, Mode: mode, ContextWindow: model.ContextLength, MaxModelCalls: budget.ModelCalls, MaxToolCalls: budget.ToolCalls, MaxTokens: budget.Tokens, MaxActiveSeconds: budget.ActiveSeconds, Validate: validate}
	if toolpack.Status(m.AgentDataDir()).Installed {
		o.Generate = func(c context.Context, raw json.RawMessage) (any, error) {
			var spec toolpack.Spec
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&spec); err != nil {
				return nil, err
			}
			if dec.Decode(new(any)) != io.EOF {
				return nil, errors.New("生成参数有多余内容")
			}
			return toolpack.Generate(c, m.AgentDataDir(), d, spec)
		}
	}
	o.Protected = deliveryProtectedTools(m, ws, id, d, systemEnabled)
	o.Publish = func(c context.Context, cursor int64, _ string, apply func() error) error {
		return m.WithPublicationLease(c, id, ws.ID, cursor, apply)
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "linux" {
		o.CheckCommand = func(cwd, program string, args []string) error {
			return sandbox.CheckCommand(d.WorkingDir(), cwd, program, args, ws.ProjectPath)
		}
		o.Command = func(c context.Context, cwd, program string, args []string) (agentcore.CommandResult, error) {
			if program == "git" {
				if e := d.PrepareGit(c); e != nil {
					return agentcore.CommandResult{}, e
				}
			}
			result, e := sandbox.Run(c, d.WorkingDir(), cwd, program, args, sandbox.Limits{ProjectRoot: ws.ProjectPath, Timeout: time.Duration(budget.CommandSeconds) * time.Second, DiskBytes: int64(budget.DiskMiB) << 20})
			return agentcore.CommandResult{Output: result.Output, ExitCode: result.ExitCode, LogID: result.LogID, LogBytes: result.LogBytes, LogTruncated: result.LogTruncated}, e
		}
	}
	o.ReadLog = func(ctx context.Context, id string, offset int64, limit int) (string, error) {
		return sandbox.ReadLog(ctx, d.WorkingDir(), id, offset, limit)
	}
	o.Present = func(c context.Context, p string) (any, error) {
		return artifact.Present(c, d, p, m.AgentDataDir())
	}
	if model.SupportsImageInput() {
		o.PreviewImage = func(c context.Context, meta json.RawMessage) (string, error) {
			return artifact.ModelPreview(c, d, meta)
		}
	}
	for _, def := range registry.Definitions() {
		name := def.Function.Name
		if name == "project_context" || name == "project_trace" || name == "service_status" || strings.HasPrefix(name, "system_") {
			o.Definitions = append(o.Definitions, def)
		}
	}
	o.ReadOnly = func(c context.Context, name string, args json.RawMessage) (any, error) {
		out := registry.Execute(c, name, args)
		if !out.Success {
			return nil, fmt.Errorf("%s", out.Error)
		}
		return out, nil
	}
	call := func(c context.Context, msg []providers.ToolMessage, tools []providers.ToolDefinition) (*providers.ToolResponse, error) {
		return gw.ChatTools(c, model, ws.ID, msg, tools)
	}
	result, e := agentcore.Execute(ctx, goal, call, d, deliverySession{ctx, m, id}, o)
	var wait *agentcore.WaitError
	if errors.As(e, &wait) {
		return result, workspaces.AgentWait{Status: wait.Status, Reason: wait.Reason}
	}
	return result, e
}
