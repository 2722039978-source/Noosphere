package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noosphere/backend/internal/agent/host"
	"github.com/noosphere/backend/internal/gateway/providers"
)

type memorySession struct {
	raw       json.RawMessage
	input     []Control
	phases    []string
	intercept func(string) error
}

func (s *memorySession) Load() (json.RawMessage, error) { return s.raw, nil }
func (s *memorySession) Save(raw json.RawMessage, phase, msg string, data any) error {
	if s.intercept != nil {
		if err := s.intercept(phase); err != nil {
			return err
		}
	}
	s.raw = append(json.RawMessage{}, raw...)
	s.phases = append(s.phases, phase)
	return nil
}
func (s *memorySession) Controls(after int64) ([]Control, error) {
	var out []Control
	for _, c := range s.input {
		if c.Seq > after {
			out = append(out, c)
		}
	}
	return out, nil
}
func fixture(t *testing.T) (string, *host.Draft) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d, err := host.Open(t.Context(), t.TempDir(), "test-run", root, "test-binding")
	if err != nil {
		t.Fatal(err)
	}
	return root, d
}
func call(id, name, args string) providers.ToolCall {
	return providers.ToolCall{ID: id, Type: "function", Function: providers.ToolFunction{Name: name, Arguments: args}}
}
func reply(text string, calls ...providers.ToolCall) *providers.ToolResponse {
	return &providers.ToolResponse{Message: providers.ToolMessage{Role: "assistant", Content: text, ToolCalls: calls}, TokensIn: 15, TokensOut: 8}
}
func waitFor(t *testing.T, err error, status string) {
	t.Helper()
	var w *WaitError
	if !errors.As(err, &w) || w.Status != status {
		t.Fatalf("want %s: %v", status, err)
	}
}

func TestReviewResumeRequiresCurrentManifestAndNeverRerunsModel(t *testing.T) {
	root, d := fixture(t)
	s := &memorySession{}
	calls := 0
	model := func(ctx context.Context, h []providers.ToolMessage, defs []providers.ToolDefinition) (*providers.ToolResponse, error) {
		calls++
		if calls == 1 {
			return reply("", call("edit-1", "write_file", `{"path":"out.txt","content":"verified"}`)), nil
		}
		if calls != 2 || h[len(h)-1].Role != "tool" {
			t.Fatalf("missing tool feedback: %v", h)
		}
		return reply("交付草稿"), nil
	}
	run := func() (any, error) {
		return Execute(t.Context(), "create out.txt", model, d, s, Options{Mode: "review"})
	}
	_, err := run()
	waitFor(t, err, "waiting_approval")
	if _, err = os.Stat(filepath.Join(root, "out.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("early publication")
	}
	changes, err := d.Changes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	s.input = append(s.input, Control{Seq: 1, Action: "approve", Value: strings.Repeat("0", 64)})
	_, err = run()
	waitFor(t, err, "waiting_approval")
	s.input = append(s.input, Control{Seq: 2, Action: "approve", Value: host.ManifestDigest(changes)})
	result, err := run()
	if err != nil || result.(map[string]any)["published"] != true {
		t.Fatal(result, err)
	}
	data, err := os.ReadFile(filepath.Join(root, "out.txt"))
	if err != nil || string(data) != "verified" || calls != 2 {
		t.Fatal(string(data), calls, err)
	}
}

func TestCommandCrashDoesNotReplayAndBudgetCanResume(t *testing.T) {
	_, d := fixture(t)
	s := &memorySession{}
	dispatches := 0
	options := Options{MaxModelCalls: 1, Command: func(context.Context, string, string, []string) (CommandResult, error) {
		dispatches++
		return CommandResult{Output: "done"}, nil
	}}
	turns := 0
	model := func(ctx context.Context, h []providers.ToolMessage, defs []providers.ToolDefinition) (*providers.ToolResponse, error) {
		turns++
		if turns == 1 {
			return reply("", call("cmd", "run_command", `{"program":"go","args":["test"]}`)), nil
		}
		if h[len(h)-1].Role != "tool" || !strings.Contains(h[len(h)-1].Content, "unknown") {
			t.Fatal(h)
		}
		return reply("已核对历史"), nil
	}
	s.intercept = func(phase string) error {
		if phase == "tool_completed" {
			return errors.New("power lost")
		}
		return nil
	}
	run := func() error { _, err := Execute(t.Context(), "test", model, d, s, options); return err }
	if err := run(); err == nil || err.Error() != "power lost" {
		t.Fatal(err)
	}
	s.intercept = nil
	waitFor(t, run(), "paused")
	waitFor(t, run(), "budget_exhausted")
	s.input = append(s.input, Control{Seq: 1, Action: "extend"})
	if err := run(); err != nil || dispatches != 1 || turns != 2 {
		t.Fatal(dispatches, turns, err)
	}
}
func TestSteerSkipsUndispatchedBatchAtSafePoint(t *testing.T) {
	root, d := fixture(t)
	s := &memorySession{}
	turn := 0
	s.intercept = func(phase string) error {
		if phase == "tool_completed" && len(s.input) == 0 {
			s.input = append(s.input, Control{Seq: 1, Action: "steer", Value: "skip second"})
		}
		return nil
	}
	model := func(ctx context.Context, h []providers.ToolMessage, defs []providers.ToolDefinition) (*providers.ToolResponse, error) {
		turn++
		if turn == 1 {
			return reply("", call("one", "list_files", "{}"), call("two", "write_file", `{"path":"wrong.txt","content":"x"}`)), nil
		}
		if n := len(h); h[n-3].ToolCallID != "one" || h[n-2].ToolCallID != "two" || h[n-1].Content != "skip second" {
			t.Fatal(h)
		}
		return reply("done"), nil
	}
	if _, err := Execute(t.Context(), "inspect", model, d, s); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "wrong.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}
func TestCompactionPreservesCallPairsAndLatestSteering(t *testing.T) {
	e := engine{session: &memorySession{}, goal: "build", o: Options{ContextWindow: 5000}}
	e.add(providers.ToolMessage{Role: "user", Content: "keep PDF"})
	for i := 0; i < 30; i++ {
		id := fmt.Sprint(i)
		e.add(reply("", call(id, "read_file", "{}")).Message)
		e.add(providers.ToolMessage{Role: "tool", ToolCallID: id, Content: strings.Repeat("x", 800)})
	}
	h, err := e.messages(nil)
	if err != nil {
		t.Fatal(err)
	}
	if e.Summary == "" || len(e.Notes) != 1 || e.Notes[0] != "keep PDF" {
		t.Fatal(e.Summary, e.Notes)
	}
	for i, m := range h {
		if m.Role == "tool" && (i == 0 || len(h[i-1].ToolCalls) != 1 || h[i-1].ToolCalls[0].ID != m.ToolCallID) {
			t.Fatal("orphaned tool call")
		}
	}
}

func TestRepairedCommandReadsLogAndRetests(t *testing.T) {
	root, d := fixture(t)
	s := &memorySession{}
	turn := 0
	commands := 0
	const testArgs = `{"program":"go","args":["test","./..."],"cwd":"."}`
	options := Options{
		Command: func(ctx context.Context, cwd, program string, argv []string) (CommandResult, error) {
			commands++
			if commands == 1 {
				return CommandResult{Output: "failed", ExitCode: 1, LogID: "first", LogBytes: 36000}, nil
			}
			return CommandResult{Output: "ok", LogID: "second"}, nil
		},
		ReadLog: func(ctx context.Context, id string, offset int64, limit int) (string, error) {
			if id != "first" || offset != 32768 {
				t.Fatal(id, offset)
			}
			return "missing generated file", nil
		},
	}
	model := func(ctx context.Context, history []providers.ToolMessage, defs []providers.ToolDefinition) (*providers.ToolResponse, error) {
		turn++
		switch turn {
		case 1:
			return reply("", call("cmd1", "run_command", testArgs)), nil
		case 2:
			if !strings.Contains(history[len(history)-1].Content, "\"exit_code\":1") {
				t.Fatal(history)
			}
			return reply("", call("log", "read_command_log", `{"log_id":"first","offset":32768}`)), nil
		case 3:
			if !strings.Contains(history[len(history)-1].Content, "missing generated file") {
				t.Fatal(history)
			}
			return reply("", call("fix", "write_file", `{"path":"generated.txt","content":"resolved"}`)), nil
		case 4:
			return reply("", call("cmd2", "run_command", testArgs)), nil
		default:
			return reply("修复并复测通过"), nil
		}
	}
	result, err := Execute(t.Context(), "verify", model, d, s, options)
	if err != nil || result.(map[string]any)["published"] != true || commands != 2 {
		t.Fatal(result, err, commands)
	}
	if _, err = os.Stat(filepath.Join(root, "generated.txt")); err != nil {
		t.Fatal(err)
	}
}
func TestFailedVerificationRequiresReview(t *testing.T) {
	_, d := fixture(t)
	s := &memorySession{}
	turn := 0
	model := func(ctx context.Context, history []providers.ToolMessage, defs []providers.ToolDefinition) (*providers.ToolResponse, error) {
		turn++
		if turn == 1 {
			return reply("", call("write", "write_file", `{"path":"changed.txt","content":"pending"}`), call("check", "run_command", `{"program":"go","args":["test"]}`)), nil
		}
		return reply("准备交付"), nil
	}
	_, err := Execute(t.Context(), "edit and verify", model, d, s, Options{Command: func(context.Context, string, string, []string) (CommandResult, error) {
		return CommandResult{ExitCode: 1, Output: "failed"}, nil
	}})
	waitFor(t, err, "waiting_approval")
	if !strings.Contains(err.Error(), "验证失败") {
		t.Fatal(err)
	}
}
func TestMoveSourceVersionAndDestination(t *testing.T) {
	_, d := fixture(t)
	if err := d.Write(t.Context(), "old.txt", []byte("value")); err != nil {
		t.Fatal(err)
	}
	_, hash, err := d.Stat(t.Context(), "old.txt")
	if err != nil {
		t.Fatal(err)
	}
	e := engine{d: d}
	if _, err = e.dispatch(t.Context(), call("stale", "move_file", `{"path":"old.txt","to":"new.txt","expected_hash":"stale"}`)); err == nil {
		t.Fatal("accepted stale source")
	}
	raw := fmt.Sprintf(`{"path":"old.txt","to":"new.txt","expected_hash":"%s"}`, hash)
	if _, err = e.dispatch(t.Context(), call("move", "move_file", raw)); err != nil {
		t.Fatal(err)
	}
	data, err := d.Read(t.Context(), "new.txt", 0, 20)
	if err != nil || string(data) != "value" {
		t.Fatal(err, string(data))
	}
}

func TestLongTurnExceedsLegacyCeilingsAndCompactsByModelWindow(t *testing.T) {
	for _, window := range []int{192000, 12000} {
		t.Run(fmt.Sprint(window), func(t *testing.T) {
			_, d := fixture(t)
			session := &memorySession{}
			if err := d.Write(t.Context(), "large.txt", []byte(strings.Repeat("0123456789", 24000))); err != nil {
				t.Fatal(err)
			}
			turn, total, maxRequest := 0, 0, 0
			model := func(ctx context.Context, m []providers.ToolMessage, defs []providers.ToolDefinition) (*providers.ToolResponse, error) {
				raw, _ := json.Marshal(m)
				maxRequest = max(maxRequest, len(raw))
				turn++
				if turn == 1 && !strings.Contains(m[1].Content, "read every part") {
					t.Fatal("lost initial goal")
				}
				if turn <= 8 {
					var requests []providers.ToolCall
					for j := 0; j < 3; j++ {
						requests = append(requests, call(fmt.Sprint(total), "read_file", fmt.Sprintf(`{"path":"large.txt","offset":%d,"limit":10000}`, total*10000)))
						total++
					}
					return reply("", requests...), nil
				}
				return reply("observed all 24 parts"), nil
			}
			result, err := Execute(t.Context(), "read every part", model, d, session, Options{ContextWindow: window})
			if err != nil || total != 24 || turn != 9 || result.(map[string]any)["tool_calls"] != 24 {
				t.Fatal(result, err, total, turn)
			}
			compacted := false
			for _, phase := range session.phases {
				if phase == "context_compacted" {
					compacted = true
				}
			}
			if window == 192000 && maxRequest <= 160<<10 {
				t.Fatalf("fixture never exercised >160KiB request: %d", maxRequest)
			}
			if window == 12000 && !compacted {
				t.Fatal("smaller window did not compact a complete batch")
			}
		})
	}
}
