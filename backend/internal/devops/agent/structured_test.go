package agent

import (
	"context"
	"github.com/noosphere/backend/internal/devops/tools"
	"strings"
	"testing"
)

func TestStructuredPlanCarriesValidatedArgumentsThroughLoop(t *testing.T) {
	calls := 0
	a := New(nil, func(ctx context.Context, messages []map[string]string) (string, error) {
		calls++
		if calls == 1 {
			return `{"thought":"check service","action":"service_status","args":{"name":"Spooler"}}`, nil
		}
		if !strings.Contains(messages[0]["content"], "RUNNING") {
			t.Fatal("tool result missing from observation")
		}
		return `{"thought":"Service is running","action":"respond","args":{}}`, nil
	})
	a.executors["service_status"] = func(ctx context.Context, args map[string]any) tools.ToolResult {
		if args["name"] != "Spooler" {
			t.Fatalf("lost service name: %#v", args)
		}
		return tools.ToolResult{Success: true, Output: "RUNNING"}
	}
	result := a.LoopContext(context.Background(), "Check Spooler", 5, nil)
	if len(result.Steps) != 1 || !result.Steps[0].Result.Success || result.Answer != "Service is running" {
		t.Fatalf("incomplete execution: %+v", result)
	}
}

func TestCancelledLoopDoesNotExecuteTools(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a := New(nil, nil)
	result := a.LoopContext(ctx, "test", 1000, nil)
	if len(result.ToolsUsed) != 0 {
		t.Fatal("cancelled loop ran tools")
	}
}

func TestMalformedPlanAndMissingArgumentsDoNotRunTools(t *testing.T) {
	a := New(nil, nil)
	result := a.ExecuteTool("service_status", map[string]any{})
	if result.Success || !strings.Contains(result.Error, "name") {
		t.Fatalf("missing name not validated: %+v", result)
	}
	if p := parsePlanResponse(`{"action":"service_status","args":"invalid"}`); p.Action != "respond" {
		t.Fatal("malformed structured plan accepted")
	}
}
