package core

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/noosphere/backend/internal/gateway/providers"
)

func TestProtectedScopeMustMatchAtExecution(t *testing.T) {
	_, draft := fixture(t)
	session := &memorySession{}
	scope := "dependency version A"
	execCount, calls := 0, 0
	tool := ProtectedTool{Definition: definition("download_fixture", "read checked fixture", ""), Prepare: func(context.Context, json.RawMessage) (string, error) { return scope, nil }, Execute: func(_ context.Context, _ json.RawMessage, approved string) (any, error) {
		if approved != scope {
			t.Fatalf("stale approval %q", approved)
		}
		execCount++
		return "done", nil
	}}
	model := func(context.Context, []providers.ToolMessage, []providers.ToolDefinition) (*providers.ToolResponse, error) {
		calls++
		if calls == 1 {
			return reply("", call("effect", "download_fixture", "{}")), nil
		}
		return reply("done"), nil
	}
	run := func() error {
		_, e := Execute(t.Context(), "use fixture", model, draft, session, Options{Mode: "auto", Protected: []ProtectedTool{tool}})
		return e
	}
	waitFor(t, run(), "waiting_approval")
	var cp checkpoint
	if e := json.Unmarshal(session.raw, &cp); e != nil {
		t.Fatal(e)
	}
	old := cp.ToolApproval
	scope = "dependency version B"
	session.input = append(session.input, Control{Seq: 1, Action: "approve", Value: old})
	waitFor(t, run(), "waiting_approval")
	if e := json.Unmarshal(session.raw, &cp); e != nil {
		t.Fatal(e)
	}
	if execCount != 0 || old == cp.ToolApproval {
		t.Fatal("stale scope accepted")
	}
	session.input = append(session.input, Control{Seq: 2, Action: "approve", Value: cp.ToolApproval})
	if e := run(); e != nil {
		t.Fatal(e)
	}
	if execCount != 1 || calls != 2 {
		t.Fatalf("executions=%d model requests=%d", execCount, calls)
	}
}
