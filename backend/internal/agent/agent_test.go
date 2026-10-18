package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/noosphere/backend/internal/gateway/providers"
	"github.com/noosphere/backend/internal/knowledge"
	"github.com/noosphere/backend/internal/workspaces"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureRegistry(t *testing.T) *Registry {
	t.Helper()
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc start() {}\napi_key = \"private-fixture-value\"\n"), 0600)
	return NewRegistry(workspaces.Workspace{ID: "ws-a", ProjectPath: root}, nil, nil)
}

func TestContextEnforcesSourceScopeAndCurrentHash(t *testing.T) {
	r := fixtureRegistry(t)
	r.lookup = func(context.Context, string, string, int) (knowledge.ContextResult, error) {
		return knowledge.ContextResult{WorkspaceID: r.workspace.ID, ProjectID: r.workspace.ProjectID, Citations: []knowledge.Citation{
			{Kind: "code", File: "main.go", SourceHash: "stale", Excerpt: "old source"},
			{Kind: "code", File: "missing.go", Excerpt: "not a real source"},
			{Kind: "memory", MemoryID: "m1", Revision: 2, Excerpt: "confirmed note"},
		}}, nil
	}
	out := r.Execute(context.Background(), "project_context", json.RawMessage(`{"query":"startup"}`))
	if !out.Success || len(out.Evidence) != 1 || out.Evidence[0].MemoryID != "m1" {
		t.Fatalf("unverified sources escaped: %+v", out)
	}
}
func TestSourceSizeBinaryLinksAndSearch(t *testing.T) {
	r := fixtureRegistry(t)
	ctx := context.Background()
	os.WriteFile(filepath.Join(r.workspace.ProjectPath, "large.go"), []byte(strings.Repeat("x", 1024*1024+1)), 0600)
	os.WriteFile(filepath.Join(r.workspace.ProjectPath, "binary.go"), []byte{0, 1, 2}, 0600)
	for _, name := range []string{"large.go", "binary.go"} {
		if out := r.Execute(ctx, "project_read_file", json.RawMessage(fmt.Sprintf(`{"path":%q}`, name))); out.Success {
			t.Fatal("invalid text accepted", name)
		}
	}
	out := r.Execute(ctx, "project_search", json.RawMessage(`{"query":"func start"}`))
	if !out.Success || len(out.Evidence) != 1 || out.Evidence[0].File != "main.go" {
		t.Fatalf("search evidence: %+v", out)
	}
	if err := os.Symlink(filepath.Join(r.workspace.ProjectPath, "main.go"), filepath.Join(r.workspace.ProjectPath, "linked.go")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if out := r.Execute(ctx, "project_read_file", json.RawMessage(`{"path":"linked.go"}`)); out.Success {
		t.Fatal("symlink accepted")
	}
}
func TestModelAndToolBudgets(t *testing.T) {
	for _, batch := range []int{1, 4} {
		t.Run(fmt.Sprint(batch), func(t *testing.T) {
			turn := 0
			call := func(context.Context, []providers.ToolMessage, []providers.ToolDefinition) (*providers.ToolResponse, error) {
				turn++
				calls := []providers.ToolCall{}
				for i := 0; i < batch; i++ {
					calls = append(calls, providers.ToolCall{ID: fmt.Sprintf("c%d-%d", turn, i), Type: "function", Function: providers.ToolFunction{Name: "project_search", Arguments: fmt.Sprintf(`{"query":"q%d-%d"}`, turn, i)}})
				}
				return &providers.ToolResponse{Message: providers.ToolMessage{Role: "assistant", ToolCalls: calls}}, nil
			}
			result, err := Run(context.Background(), "inspect", fixtureRegistry(t), call, nil, nil)
			if err == nil || result.ModelCalls > 6 || result.ToolCalls > 12 {
				t.Fatalf("unbounded %+v %v", result, err)
			}
			if batch == 1 && result.ModelCalls != 6 {
				t.Fatal("model budget not exercised")
			}
			if batch == 4 && result.ToolCalls != 12 {
				t.Fatal("tool budget not exercised")
			}
		})
	}
}
func TestScopedToolsRejectTraversalUnknownArgsAndRedactSecrets(t *testing.T) {
	r := fixtureRegistry(t)
	ctx := context.Background()
	for _, args := range []string{`{"path":"../outside.go"}`, `{"path":"C:/outside.go"}`, `{"path":"main.go","command":"whoami"}`, `null`, `{"path":".env"}`, `{"path":"main.go","start_line":-1}`} {
		if out := r.Execute(ctx, "project_read_file", json.RawMessage(args)); out.Success {
			t.Fatalf("accepted %s", args)
		}
	}
	out := r.Execute(ctx, "project_read_file", json.RawMessage(`{"path":"main.go"}`))
	if !out.Success || len(out.Evidence) != 1 {
		t.Fatalf("%+v", out)
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "private-fixture-value") || !strings.Contains(string(raw), "[redacted]") {
		t.Fatal("secret not redacted")
	}
	if out.Evidence[0].SourceHash == "" {
		t.Fatal("missing version")
	}
	if out := r.Execute(ctx, "delete_file", json.RawMessage(`{}`)); out.Success {
		t.Fatal("unknown write tool executed")
	}
}

func TestLoopUsesStructuredResultsAndStopsRepeatedFailure(t *testing.T) {
	r := fixtureRegistry(t)
	turn := 0
	events := []string{}
	call := func(ctx context.Context, m []providers.ToolMessage, d []providers.ToolDefinition) (*providers.ToolResponse, error) {
		turn++
		if turn == 1 {
			return &providers.ToolResponse{Message: providers.ToolMessage{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call-1", Type: "function", Function: providers.ToolFunction{Name: "project_read_file", Arguments: `{"path":"main.go"}`}}}}}, nil
		}
		if m[len(m)-1].Role != "tool" || m[len(m)-1].ToolCallID != "call-1" {
			t.Error("lost structured tool result")
		}
		return &providers.ToolResponse{Message: providers.ToolMessage{Role: "assistant", Content: "start is empty [E1]"}}, nil
	}
	result, err := Run(context.Background(), "inspect startup", r, call, func(phase, message string, data any) { events = append(events, phase) }, nil)
	if err != nil || len(result.Evidence) != 1 || len(result.Steps) != 1 || result.ModelCalls != 2 || !strings.Contains(result.Answer, "[E1]") {
		t.Fatalf("%+v %v", result, err)
	}
	if !strings.Contains(strings.Join(events, ","), "tool_started,tool_completed") {
		t.Fatal("no live tool events")
	}
	count := 0
	bad := func(ctx context.Context, m []providers.ToolMessage, d []providers.ToolDefinition) (*providers.ToolResponse, error) {
		count++
		return &providers.ToolResponse{Message: providers.ToolMessage{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: strings.Repeat("x", count), Type: "function", Function: providers.ToolFunction{Name: "missing", Arguments: `{}`}}}}}, nil
	}
	result, err = Run(context.Background(), "inspect", r, bad, nil, nil)
	if err == nil || count > 2 || len(result.Steps) != 2 {
		t.Fatalf("repeat not bounded %+v %v", result, err)
	}
}

func TestLoopCancellationAndWorkspaceChangePreventTool(t *testing.T) {
	r := fixtureRegistry(t)
	ctx, cancel := context.WithCancel(context.Background())
	call := func(context.Context, []providers.ToolMessage, []providers.ToolDefinition) (*providers.ToolResponse, error) {
		cancel()
		return &providers.ToolResponse{Message: providers.ToolMessage{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "a", Type: "function", Function: providers.ToolFunction{Name: "project_read_file", Arguments: `{"path":"main.go"}`}}}}}, nil
	}
	result, err := Run(ctx, "inspect", r, call, nil, nil)
	if err == nil || len(result.Steps) > 0 {
		t.Fatal("tool executed after cancellation")
	}
	_, err = Run(context.Background(), "inspect", r, call, nil, func() error { return os.ErrNotExist })
	if err == nil {
		t.Fatal("changed workspace accepted")
	}
}
