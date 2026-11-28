package core

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/noosphere/backend/internal/gateway/providers"
)

// Prepare describes the specific effect, including any live precondition. Execute receives the
// exact approved description and must recheck that precondition at the point of use.
type ProtectedTool struct {
	Definition providers.ToolDefinition
	Prepare    func(context.Context, json.RawMessage) (string, error)
	Execute    func(context.Context, json.RawMessage, string) (any, error)
}

func (e *engine) protected(name string) *ProtectedTool {
	for i := range e.o.Protected {
		t := &e.o.Protected[i]
		if t.Definition.Function.Name == name && t.Prepare != nil && t.Execute != nil {
			return t
		}
	}
	return nil
}
func (e *engine) clearToolApproval() { e.ToolApproval = ""; e.ToolApproved = ""; e.ToolRejected = "" }
func (e *engine) authorizeTool(call providers.ToolCall, t *ProtectedTool) (string, error) {
	raw := json.RawMessage(call.Function.Arguments)
	if !json.Valid(raw) || len(raw) > 32000 {
		return "", fmt.Errorf("操作参数无效")
	}
	scope, err := t.Prepare(e.ctx, raw)
	if err != nil {
		return "", err
	}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(call.ID+"\x00"+call.Function.Name+"\x00"+string(raw)+"\x00"+scope)))
	if e.ToolRejected == digest {
		e.clearToolApproval()
		return "", fmt.Errorf("用户拒绝了此次操作，请调整方案")
	}
	if e.ToolApproved == digest {
		e.clearToolApproval()
		return scope, nil
	}
	e.ToolApproval = digest
	e.ToolApproved = ""
	e.ToolRejected = ""
	return "", e.wait("waiting_approval", safeText(scope, 4096), map[string]string{"digest": digest, "reason": safeText(scope, 4096), "tool": call.Function.Name})
}
