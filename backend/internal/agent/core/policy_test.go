package core

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/noosphere/backend/internal/agent/sandbox"
	"github.com/noosphere/backend/internal/gateway/providers"
)

type policySession struct {
	memorySession
	events map[string]map[string]policyResult
}

func TestToolPolicyDeniesUnregisteredCommandBeforeDispatch(t *testing.T) {
	_, draft := fixture(t)
	session := &policySession{events: map[string]map[string]policyResult{
		"tool_started": {}, "tool_completed": {},
	}}
	engine := engine{
		ctx: t.Context(), d: draft, session: session,
		o: Options{
			Command: func(context.Context, string, string, []string) (CommandResult, error) {
				t.Error("unregistered command dispatched")
				return CommandResult{}, nil
			},
			CheckCommand: func(cwd, program string, args []string) error {
				return sandbox.CheckCommand(draft.WorkingDir(), cwd, program, args, "")
			},
		},
		checkpoint: checkpoint{Batch: []providers.ToolCall{
			call("unregistered", "run_command", `{"program":"unregistered-noosphere-test","args":[]}`),
		}},
	}
	if err := engine.tool(); err != nil {
		t.Fatal(err)
	}
	for phase, events := range session.events {
		if got := events["unregistered"]; got.Decision != deny || got.Reason == "" {
			t.Errorf("%s policy = %+v, want deny with reason", phase, got)
		}
	}
}

func (s *policySession) Save(raw json.RawMessage, phase, message string, data any) error {
	if phase == "tool_started" || phase == "tool_completed" {
		event := data.(map[string]any)
		s.events[phase][event["call_id"].(string)] = event["policy"].(policyResult)
	}
	return s.memorySession.Save(raw, phase, message, data)
}

func TestToolPolicyDeniesForbiddenPathsBeforeDispatch(t *testing.T) {
	_, draft := fixture(t)
	session := &policySession{events: map[string]map[string]policyResult{
		"tool_started": {}, "tool_completed": {},
	}}
	cases := []providers.ToolCall{
		call("traversal", "read_file", `{"path":"../outside.txt"}`),
		call("protected", "write_file", `{"path":"notes/credentials.json","content":"dummy"}`),
		call("destination", "move_file", `{"path":"note.txt","to":".git/config","expected_hash":"irrelevant"}`),
		call("generation", "generate_artifact", `{"path":".env","title":"Denied"}`),
		call("allowed", "write_file", `{"path":"note.txt","content":"ordinary notes"}`),
	}
	modelCalls := 0
	model := func(context.Context, []providers.ToolMessage, []providers.ToolDefinition) (*providers.ToolResponse, error) {
		modelCalls++
		if modelCalls == 1 {
			return reply("", cases...), nil
		}
		return reply("完成"), nil
	}
	generated := false
	_, err := Execute(t.Context(), "test policy", model, draft, session, Options{Generate: func(context.Context, json.RawMessage) (any, error) {
		generated = true
		return nil, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	for phase, events := range session.events {
		for _, request := range cases[:len(cases)-1] {
			if got := events[request.ID]; got.Decision != deny || got.Reason == "" {
				t.Errorf("%s %s policy = %+v, want deny with reason", phase, request.ID, got)
			}
		}
		if got := events["allowed"]; got.Decision != allow {
			t.Errorf("%s allowed policy = %+v, want allow", phase, got)
		}
	}
	if generated {
		t.Error("forbidden generation dispatched")
	}
}
