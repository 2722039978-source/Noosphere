// Package core drives the durable Agent conversation. The workspace adapter
// stages all file and command effects in a private draft before publication.
package core

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/noosphere/backend/internal/agent/contract"
	"github.com/noosphere/backend/internal/gateway/providers"
)

type Session interface {
	Load() (json.RawMessage, error)
	Save(json.RawMessage, string, string, any) error
	Controls(after int64) ([]Control, error)
}

type Control struct {
	Seq           int64
	Action, Value string
}

type WaitError struct{ Status, Reason string }

func (e *WaitError) Error() string { return e.Reason }

type ModelCall = func(context.Context, []providers.ToolMessage, []providers.ToolDefinition) (*providers.ToolResponse, error)
type CommandResult struct {
	Output       string `json:"output"`
	ExitCode     int    `json:"exit_code"`
	LogID        string `json:"log_id,omitempty"`
	LogBytes     int64  `json:"log_bytes,omitempty"`
	LogTruncated bool   `json:"log_truncated,omitempty"`
}
type Options struct {
	PreviewImage                                                            func(context.Context, json.RawMessage) (string, error)
	MaxCommandSeconds                                                       int
	Protected                                                               []ProtectedTool
	ReadLog                                                                 func(context.Context, string, int64, int) (string, error)
	Publish                                                                 func(context.Context, int64, string, func() error) error
	Generate                                                                func(context.Context, json.RawMessage) (any, error)
	Mode                                                                    string
	ContextWindow, MaxModelCalls, MaxToolCalls, MaxTokens, MaxActiveSeconds int
	Command                                                                 func(context.Context, string, string, []string) (CommandResult, error)
	CheckCommand                                                            func(string, string, []string) error
	Present                                                                 func(context.Context, string) (any, error)
	Definitions                                                             []providers.ToolDefinition
	ReadOnly                                                                func(context.Context, string, json.RawMessage) (any, error)
	Validate                                                                func() error
}

type artifact struct {
	Path, Hash string
	Metadata   json.RawMessage
}
type checkpoint struct {
	PendingPreview                                          string
	ReviewEpoch                                             int
	Refreshing                                              bool
	RefreshCount, MissingAttempts                           int
	Checked                                                 map[string]string
	Recheck                                                 map[string]string
	RefreshChecks                                           map[string]string
	ConflictedPaths                                         []string
	OriginalGoal                                            string
	ContextBackoff                                          int
	StartedAtMS, StartedLimitMS                             int64
	ToolApproval, ToolApproved, ToolRejected                string
	Version                                                 int
	Mode                                                    string
	History                                                 []providers.ToolHistory
	Batch                                                   []providers.ToolCall
	Next                                                    int
	InFlight                                                bool
	NeedsInspection                                         bool
	Cursor                                                  int64
	BudgetUnits, ModelCalls, ToolCalls, TokensIn, TokensOut int
	ActiveMillis                                            int64
	Seen                                                    map[string]bool
	Recent                                                  []string
	FailedChecks                                            map[string]string
	Notes                                                   []string
	Summary                                                 string
	Artifacts                                               []artifact
	Answer, WaitingDigest, ApprovedDigest, PublishingDigest string
	Declined                                                bool
}

type engine struct {
	ctx         context.Context
	goal        string
	call        ModelCall
	d           contract.Workspace
	session     Session
	o           Options
	mutatedFile bool
	checkpoint
}

func Execute(ctx context.Context, goal string, model ModelCall, draft contract.Workspace, session Session, options ...Options) (any, error) {
	if model == nil || draft == nil || session == nil {
		return nil, errors.New("Agent 执行依赖不可用")
	}
	e := engine{ctx: ctx, goal: goal, call: model, d: draft, session: session}
	if len(options) > 0 {
		e.o = options[0]
	}
	e.o.ContextWindow = positive(e.o.ContextWindow, 16384)
	e.o.MaxModelCalls = positive(e.o.MaxModelCalls, 48)
	e.o.MaxToolCalls = positive(e.o.MaxToolCalls, 160)
	e.o.MaxTokens = positive(e.o.MaxTokens, 200000)
	e.o.MaxActiveSeconds = positive(e.o.MaxActiveSeconds, 1800)
	e.checkpoint = checkpoint{Version: 1, Mode: e.o.Mode, BudgetUnits: 1, Seen: map[string]bool{}}
	if e.Mode == "" {
		e.Mode = "auto"
	}
	raw, err := session.Load()
	if err != nil {
		return nil, err
	}
	if len(raw) > 0 && string(raw) != "null" && string(raw) != "{}" {
		if err = json.Unmarshal(raw, &e.checkpoint); err != nil {
			return nil, fmt.Errorf("读取 Agent 检查点: %w", err)
		}
	}
	if e.Version != 1 || e.Seen == nil || e.Next < 0 || e.Next > len(e.Batch) || e.BudgetUnits < 1 || (e.Mode != "auto" && e.Mode != "review") {
		return nil, errors.New("Agent 检查点版本、权限或工具位置无效")
	}
	e.upgradeVerificationKeys()
	if e.OriginalGoal == "" {
		e.OriginalGoal = goal
	}
	e.goal = e.OriginalGoal
	// A previous process might have exited while its model or host request
	// was outstanding. Charge at most that individual request's reserved time.
	if e.StartedAtMS > 0 {
		e.ActiveMillis += min(max(int64(0), time.Now().UnixMilli()-e.StartedAtMS), e.StartedLimitMS)
		e.StartedAtMS, e.StartedLimitMS = 0, 0
		if err = e.save("execution_recovered", "已恢复最近检查点与用量", e.usage()); err != nil {
			return nil, err
		}
	}
	return e.run()
}

func positive(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func (e *engine) save(phase, message string, data any) error {
	raw, err := json.Marshal(e.checkpoint)
	if err != nil {
		return err
	}
	return e.session.Save(raw, phase, message, data)
}
func (e *engine) check() error {
	if err := e.ctx.Err(); err != nil {
		return err
	}
	if e.o.Validate != nil {
		return e.o.Validate()
	}
	return nil
}
func (e *engine) wait(status, reason string, data any) error {
	if err := e.save(status, reason, data); err != nil {
		return err
	}
	return &WaitError{status, reason}
}
func (e *engine) add(m providers.ToolMessage) {
	e.History = append(e.History, providers.CheckpointToolMessage(m))
}
func (e *engine) observation(id string, payload string) {
	e.add(providers.ToolMessage{Role: "tool", ToolCallID: id, Content: payload})
	e.Next++
}
func (e *engine) skipBatch() {
	for e.Next < len(e.Batch) {
		e.observation(e.Batch[e.Next].ID, `{"skipped":"用户更新了任务，这次工具调用未分发"}`)
	}
}
func (e *engine) controls() error {
	controls, err := e.session.Controls(e.Cursor)
	if err != nil {
		return err
	}
	for _, c := range controls {
		if c.Seq <= e.Cursor {
			continue
		}
		e.Cursor = c.Seq
		switch c.Action {
		case "steer":
			e.clearToolApproval()
			if strings.TrimSpace(c.Value) == "" {
				continue
			}
			e.skipBatch()
			e.add(providers.ToolMessage{Role: "user", Content: c.Value})
			e.Answer, e.WaitingDigest, e.ApprovedDigest = "", "", ""
			e.Declined = false
			e.Recent = nil
			e.RefreshCount, e.MissingAttempts = 0, 0
		case "mode":
			e.clearToolApproval()
			if c.Value == "auto" || c.Value == "review" {
				e.Mode = c.Value
				e.ApprovedDigest, e.WaitingDigest = "", ""
			}
		case "approve":
			if c.Value != "" && c.Value == e.ToolApproval {
				e.ToolApproved = c.Value
			}
			if c.Value != "" && c.Value == e.WaitingDigest {
				e.ApprovedDigest = c.Value
			}
		case "reject":
			if c.Value != "" && c.Value == e.ToolApproval {
				e.ToolRejected = c.Value
			}
			if c.Value != "" && c.Value == e.WaitingDigest {
				e.Declined = true
				e.ApprovedDigest = ""
			}
		case "extend":
			e.BudgetUnits++
			e.Recent = nil
		case "pause", "resume":
			// These controls are applied immediately by the queue, including
			// cancelling the active context. Replaying an old pause here would
			// pause the freshly resumed generation a second time.
			if c.Action == "resume" {
				e.RefreshCount, e.MissingAttempts = 0, 0
			}
		}
	}
	if len(controls) > 0 {
		if err := e.save("controls_applied", "已处理本轮用户指令", map[string]any{"cursor": e.Cursor, "mode": e.Mode}); err != nil {
			return err
		}
		if len(controls) >= 200 {
			return e.controls()
		}
	}
	return nil
}

func (e *engine) run() (any, error) {
	if e.InFlight {
		if e.Next >= len(e.Batch) {
			return nil, errors.New("执行检查点缺少工具调用")
		}
		call := e.Batch[e.Next]
		e.InFlight = false
		e.NeedsInspection = true
		e.observation(call.ID, `{"ok":false,"unknown":true,"error":"工具执行期间进程退出，执行结果未知；请先检查草稿文件和产物，再决定下一步。"}`)
		for e.Next < len(e.Batch) {
			e.observation(e.Batch[e.Next].ID, `{"skipped":"上一项结果未知，请重新检查草稿后决定"}`)
		}
		// Persist uncertainty before yielding. A subsequent resume consumes the
		// observation and never dispatches the same logical operation twice.
		detail := map[string]string{"tool": call.Function.Name, "call_id": call.ID}
		controls, err := e.session.Controls(e.Cursor)
		if err != nil {
			return nil, err
		}
		resumed := false
		for _, c := range controls {
			if c.Action == "resume" {
				resumed = true
			}
		}
		if !resumed {
			return nil, e.wait("paused", "上一次工具执行结果未知；已保留草稿，恢复后先核查", detail)
		}
		if err = e.save("tool_unknown", "已恢复，下一步先核查草稿或日志", detail); err != nil {
			return nil, err
		}
	}
	for {
		if err := e.check(); err != nil {
			return nil, err
		}
		if e.Refreshing {
			if err := e.refresh(); err != nil {
				return nil, err
			}
		}
		if e.PublishingDigest != "" {
			status, err := e.d.Status(e.ctx)
			if err != nil {
				return nil, err
			}
			if status == "undone" || status == "undoing_partial" || status == "undoing" {
				return nil, e.wait("paused", "本轮正在撤回或文件已恢复，请从交付记录完成撤销", map[string]string{"digest": e.PublishingDigest})
			}
			if status == "draft" {
				// Intent was journaled before the host froze any files. Re-evaluate pending
				// steering, review mode and approvals against the still-mutable draft.
				e.PublishingDigest = ""
			} else {
				controls, err := e.session.Controls(e.Cursor)
				if err != nil {
					return nil, err
				}
				for _, c := range controls {
					if c.Action == "steer" || c.Action == "mode" {
						return nil, e.wait("paused", "提交过程中收到新指令，请先撤回已提交的文件", map[string]string{"digest": e.PublishingDigest})
					}
				}
				if err = e.controls(); err != nil {
					return nil, err
				}
				return e.publish(e.PublishingDigest)
			}
		}
		if err := e.controls(); err != nil {
			return nil, err
		}
		if e.Answer != "" {
			return e.finish()
		}
		if e.Next < len(e.Batch) {
			if e.exhausted(true) {
				return nil, e.wait("budget_exhausted", "本轮工具或时间预算已用尽，可增加预算继续", e.usage())
			}
			if err := e.tool(); err != nil {
				return nil, err
			}
			continue
		}
		e.Next, e.Batch = 0, nil
		if e.exhausted(false) {
			return nil, e.wait("budget_exhausted", "本轮模型、令牌或时间预算已用尽，可增加预算继续", e.usage())
		}
		defs := e.definitions()
		messages, err := e.messages(defs)
		if err != nil {
			return nil, err
		}
		e.ModelCalls++
		e.StartedAtMS = time.Now().UnixMilli()
		e.StartedLimitMS = e.timeout(120 * time.Second).Milliseconds()
		if err = e.save("agent_model", "正在分析下一步", e.usage()); err != nil {
			return nil, err
		}
		if err = e.check(); err != nil {
			return nil, err
		}
		start := time.Now()
		ctx, cancel := context.WithTimeout(e.ctx, e.timeout(120*time.Second))
		resp, err := e.call(ctx, messages, defs)
		cancel()
		e.ActiveMillis += time.Since(start).Milliseconds()
		e.StartedAtMS, e.StartedLimitMS = 0, 0
		if err != nil {
			if check := e.check(); check != nil {
				return nil, check
			}
			reason := strings.ToLower(err.Error())
			if e.ContextBackoff < 2 && (strings.Contains(reason, "context_length_exceeded") || strings.Contains(reason, "context window") || strings.Contains(reason, "context length") || strings.Contains(reason, "model request exceeds")) {
				e.ContextBackoff++
				if saveErr := e.save("context_retry", "模型报告上下文超限，将缩短下一次请求", e.usage()); saveErr != nil {
					return nil, saveErr
				}
				continue
			}
			return nil, e.wait("paused", "模型请求未取得完整结果，可从本步继续", map[string]string{"error": safeText(err.Error(), 1000)})
		}
		if resp == nil {
			return nil, errors.New("模型没有返回结果")
		}
		e.PendingPreview = ""
		inputTokens, outputTokens := resp.TokensIn, resp.TokensOut
		if inputTokens <= 0 {
			raw, _ := json.Marshal([]any{messages, defs})
			inputTokens = estimatedTokens(raw)
			for _, msg := range messages {
				if msg.ImageURL != "" {
					inputTokens += 1024
				}
			}
		}
		if outputTokens <= 0 {
			raw, _ := json.Marshal(resp.Message)
			outputTokens = estimatedTokens(raw)
		}
		e.TokensIn += inputTokens
		e.TokensOut += outputTokens
		m := resp.Message
		for _, tc := range m.ToolCalls {
			if tc.ID == "" || tc.Type != "function" || e.Seen[tc.ID] {
				return nil, errors.New("模型返回了无效或重复的工具调用 ID")
			}
			e.Seen[tc.ID] = true
		}
		if len(m.ToolCalls) == 0 && strings.TrimSpace(m.Content) == "" {
			return nil, errors.New("模型没有返回行动或答复")
		}
		e.add(m)
		e.Batch = m.ToolCalls
		if len(m.ToolCalls) == 0 {
			e.Answer = safeText(m.Content, 32000)
		}
		if err = e.save("agent_model_completed", "模型已返回", e.usage()); err != nil {
			return nil, err
		}
	}
}

func (e *engine) timeout(perCall time.Duration) time.Duration {
	remaining := time.Duration(int64(e.o.MaxActiveSeconds)*int64(e.BudgetUnits)*1000-e.ActiveMillis) * time.Millisecond
	return min(perCall, max(remaining, time.Millisecond))
}
func (e *engine) exhausted(tool bool) bool {
	if e.ActiveMillis >= int64(e.o.MaxActiveSeconds)*int64(e.BudgetUnits)*1000 {
		return true
	}
	if tool {
		return e.ToolCalls >= e.o.MaxToolCalls*e.BudgetUnits
	}
	return e.ModelCalls >= e.o.MaxModelCalls*e.BudgetUnits || e.TokensIn+e.TokensOut >= e.o.MaxTokens*e.BudgetUnits
}
func (e *engine) usage() map[string]any {
	return map[string]any{"model_calls": e.ModelCalls, "tool_calls": e.ToolCalls, "tokens_in": e.TokensIn, "tokens_out": e.TokensOut, "active_ms": e.ActiveMillis}
}

func (e *engine) tool() error {
	call := e.Batch[e.Next]
	policy := e.toolPolicy(call)
	protected := e.protected(call.Function.Name)
	var scope string
	var prepErr error
	if protected != nil && !e.NeedsInspection {
		scope, prepErr = e.authorizeTool(call, protected)
		var pending *WaitError
		if errors.As(prepErr, &pending) {
			return prepErr
		}
	}
	toolTimeout := 3 * time.Minute
	if call.Function.Name == "run_command" {
		toolTimeout = time.Duration(positive(e.o.MaxCommandSeconds, 120)) * time.Second
	}
	e.ToolCalls++
	e.InFlight = true
	e.StartedAtMS = time.Now().UnixMilli()
	e.StartedLimitMS = e.timeout(toolTimeout).Milliseconds()
	event := map[string]any{"call_id": call.ID, "tool": call.Function.Name, "number": e.ToolCalls, "policy": policy}
	startMessage := "执行 " + call.Function.Name
	if policy.Decision == deny {
		startMessage = "策略拒绝 " + call.Function.Name
	}
	if err := e.save("tool_started", startMessage, event); err != nil {
		return err
	}
	if err := e.check(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(e.ctx, e.timeout(toolTimeout))
	start := time.Now()
	var out any
	var err error
	state := ""
	e.mutatedFile = false
	commandChanged := false
	if policy.Decision == deny {
		err = fmt.Errorf("%s: %s", call.Function.Name, policy.Reason)
	} else if e.NeedsInspection && !inspectionTool(call.Function.Name) {
		err = errors.New("上次执行的结果未知，请先查询草稿、日志或相关服务状态")
	} else {
		if protected != nil {
			err = prepErr
			if err == nil {
				out, err = protected.Execute(ctx, json.RawMessage(call.Function.Arguments), scope)
			}
		} else {
			if call.Function.Name == "run_command" {
				out, state, commandChanged, err = e.runDraftCommand(ctx, call)
			} else {
				out, err = e.dispatch(ctx, call)
			}
		}
		if e.NeedsInspection && err == nil {
			e.NeedsInspection = false
		}
	}
	cancel()
	e.ActiveMillis += time.Since(start).Milliseconds()
	e.StartedAtMS, e.StartedLimitMS = 0, 0
	if check := e.check(); check != nil {
		return check
	}
	e.InFlight = false
	var payload []byte
	if err != nil {
		message := safeText(err.Error(), 1500)
		event["error"] = message
		failure := map[string]any{"ok": false, "error": message}
		if res, ok := out.(CommandResult); ok && res.LogID != "" {
			failure["command"] = res
		}
		payload, _ = json.Marshal(failure)
	} else {
		payload, err = json.Marshal(out)
		if err != nil {
			return err
		}
	}
	content := boundedObservation(payload)
	e.observation(call.ID, content)
	var parsed any
	_ = json.Unmarshal([]byte(call.Function.Arguments), &parsed)
	arguments, _ := json.Marshal(parsed)
	progress := content
	if res, ok := out.(CommandResult); ok && err == nil {
		// Each invocation gets a new log ID even when the exit status and
		// workspace are identical. Log storage is not evidence of progress.
		progress = fmt.Sprintf("exit=%d\n%s", res.ExitCode, res.Output)
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(call.Function.Name+"\x00"+string(arguments)+"\x00"+state+"\x00"+progress)))
	key := verificationKey(call)
	if key != "" {
		if e.Checked == nil {
			e.Checked = map[string]string{}
		}
		e.Checked[key] = safeText(call.Function.Name+" "+call.Function.Arguments, 1000)
		delete(e.Recheck, key)
		failure := ""
		if err != nil {
			failure = safeText(err.Error(), 360)
		}
		if res, ok := out.(CommandResult); ok && res.ExitCode != 0 {
			failure = fmt.Sprintf("退出码 %d：%s", res.ExitCode, safeText(res.Output, 300))
		}
		if failure != "" {
			if e.FailedChecks == nil {
				e.FailedChecks = map[string]string{}
			}
			e.FailedChecks[key] = failure
			event["error"] = failure
		} else {
			delete(e.FailedChecks, key)
		}
	}
	if e.mutatedFile || commandChanged {
		e.invalidateCommandChecks(key)
	}
	e.Recent = append(e.Recent, fingerprint)
	if len(e.Recent) > 12 {
		e.Recent = e.Recent[len(e.Recent)-12:]
	}
	event["ok"] = event["error"] == nil
	event["observation"] = safeText(content, 6500)
	finalMessage := "完成 " + call.Function.Name
	if policy.Decision == deny {
		finalMessage = startMessage
	}
	if err = e.save("tool_completed", finalMessage, event); err != nil {
		return err
	}
	repeated := 0
	for _, f := range e.Recent {
		if f == fingerprint {
			repeated++
		}
	}
	if repeated >= 3 {
		e.Recent = nil
		event["summary"] = fmt.Sprintf("%s 在同一状态下三次得到相同观察：%s", call.Function.Name, safeText(content, 450))
		// Other calls in this assistant message still need matching results.
		// Keep the transcript valid before adding a new explanatory turn.
		for e.Next < len(e.Batch) {
			e.observation(e.Batch[e.Next].ID, `{"skipped":"工具返回相同结果，当前批次已停止"}`)
		}
		e.Summary = clip(e.Summary+"\n前一策略多次未取得进展。请利用已有观察，改用其他验证或修正方法。", 3000)
		return e.wait("paused", "工具在同一草稿状态下多次返回相同结果，可补充方向后继续", event)
	}
	return nil
}
