package core

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noosphere/backend/internal/agent/host"
	"github.com/noosphere/backend/internal/gateway/providers"
)

func TestVerificationCommandKeysPreserveLongArguments(t *testing.T) {
	args := func(target string) string {
		raw, err := json.Marshal(toolArgs{Program: "go", Args: []string{"test", "-args", strings.Repeat("x", 450), target}})
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	first := call("first", "run_command", args("first"))
	second := call("second", "run_command", args("second"))
	firstKey, secondKey := verificationKey(first), verificationKey(second)
	if firstKey == "" || secondKey == "" || firstKey == secondKey {
		t.Fatal("distinct verification commands have the same identity")
	}
	var parsed toolArgs
	if err := json.Unmarshal([]byte(first.Function.Arguments), &parsed); err != nil {
		t.Fatal(err)
	}
	legacyKey := safeText(verificationCommandArgs(parsed), 400)
	legacyDescription := "run_command " + first.Function.Arguments
	cp := checkpoint{
		Checked:       map[string]string{legacyKey: legacyDescription},
		Recheck:       map[string]string{legacyKey: legacyDescription},
		RefreshChecks: map[string]string{legacyKey: legacyDescription},
		FailedChecks:  map[string]string{legacyKey: "original failure"},
	}
	cp.upgradeVerificationKeys()
	if cp.Checked[firstKey] != legacyDescription || cp.Recheck[firstKey] != legacyDescription ||
		cp.RefreshChecks[firstKey] != legacyDescription || cp.FailedChecks[firstKey] != "original failure" {
		t.Fatal("legacy verification identity did not migrate")
	}
	if _, exists := cp.Recheck[secondKey]; exists {
		t.Fatal("migration accepted a different command")
	}

	redacted := call("redacted", "run_command", `{"program":"go","args":["test","--password=example-value"]}`)
	exactKey := verificationKey(redacted)
	if strings.Contains(exactKey, "example-value") {
		t.Fatal("verification identity stored an unredacted argument")
	}
	if err := json.Unmarshal([]byte(redacted.Function.Arguments), &parsed); err != nil {
		t.Fatal(err)
	}
	oldRedacted := safeText(verificationCommandArgs(parsed), 400)
	description := safeText("run_command "+redacted.Function.Arguments, 1000)
	cp.Checked[oldRedacted], cp.Recheck[oldRedacted] = description, description
	cp.upgradeVerificationKeys()
	if _, exists := cp.Recheck[oldRedacted]; exists || cp.FailedChecks[oldRedacted] == "" {
		t.Fatal("ambiguous legacy identity did not fall back to publication review")
	}
}

func TestFileEditAfterVerificationRequiresNewCheck(t *testing.T) {
	_, draft := fixture(t)
	session := &memorySession{}
	turn, checks := 0, 0
	check := func(id string) providers.ToolCall {
		return call(id, "run_command", `{"program":"go","args":["test"]}`)
	}
	model := func(_ context.Context, history []providers.ToolMessage, _ []providers.ToolDefinition) (*providers.ToolResponse, error) {
		turn++
		switch turn {
		case 1:
			return reply("", check("initial"), call("write", "write_file", `{"path":"edited.txt","content":"changed"}`)), nil
		case 2:
			return reply("ready"), nil
		case 3:
			if !strings.Contains(history[len(history)-1].Content, "run_command") {
				t.Fatal("the model was not shown the pending verification")
			}
			return reply("", check("updated")), nil
		default:
			return reply("verified"), nil
		}
	}
	_, err := Execute(t.Context(), "write and verify", model, draft, session, Options{
		Command: func(context.Context, string, string, []string) (CommandResult, error) {
			checks++
			return CommandResult{Output: "ok"}, nil
		},
	})
	if err != nil || checks != 2 {
		t.Fatalf("checks = %d, err = %v", checks, err)
	}
}

func TestBuildManifestChangeKeepsItsOwnVerification(t *testing.T) {
	_, draft := fixture(t)
	session := &memorySession{}
	turn, tests, builds := 0, 0, 0
	check := func(id, verb string) providers.ToolCall {
		return call(id, "run_command", fmt.Sprintf(`{"program":"go","args":[%q]}`, verb))
	}
	model := func(_ context.Context, history []providers.ToolMessage, _ []providers.ToolDefinition) (*providers.ToolResponse, error) {
		turn++
		switch turn {
		case 1:
			return reply("", check("test", "test"), check("build", "build")), nil
		case 2:
			return reply("ready"), nil
		case 3:
			prompt := history[len(history)-1].Content
			if !strings.Contains(prompt, `"test"`) || strings.Contains(prompt, `"build"`) {
				t.Fatalf("unexpected pending checks: %q", prompt)
			}
			return reply("", check("test-rerun", "test")), nil
		default:
			return reply("verified"), nil
		}
	}
	_, err := Execute(t.Context(), "build and test", model, draft, session, Options{
		Command: func(_ context.Context, _, _ string, args []string) (CommandResult, error) {
			if args[0] == "build" {
				builds++
				return CommandResult{}, os.WriteFile(filepath.Join(draft.WorkingDir(), "build-output.bin"), []byte("fixed"), 0600)
			}
			tests++
			return CommandResult{}, nil
		},
	})
	if err != nil || tests != 2 || builds != 1 {
		t.Fatalf("tests = %d, builds = %d, err = %v", tests, builds, err)
	}
}

func TestExternalEditRefreshesDraftRetestsAndAsksForOverlap(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "main.txt")
	if err = os.WriteFile(file, []byte("base"), 0600); err != nil {
		t.Fatal(err)
	}
	d, err := host.Open(t.Context(), t.TempDir(), "refresh", root, "binding")
	if err != nil {
		t.Fatal(err)
	}
	s := &memorySession{}
	turn, verified, presented := 0, 0, 0
	hash := func(text string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(text))) }
	edit := func(id, from, to string) providers.ToolCall {
		return call(id, "write_file", fmt.Sprintf(`{"path":"main.txt","expected_hash":"%s","content":"%s"}`, hash(from), to))
	}
	test := func(id string) providers.ToolCall { return call(id, "run_command", `{"program":"go","args":["test"]}`) }
	present := func(id string) providers.ToolCall { return call(id, "present", `{"path":"main.txt"}`) }
	model := func(ctx context.Context, h []providers.ToolMessage, defs []providers.ToolDefinition) (*providers.ToolResponse, error) {
		turn++
		switch turn {
		case 1:
			return reply("", edit("write", "base", "proposal"), test("check"), present("card")), nil
		case 2:
			if err := os.WriteFile(file, []byte("user"), 0600); err != nil {
				t.Fatal(err)
			}
			return reply("ready"), nil
		case 3:
			return reply("", call("review", "read_conflict", `{"path":"main.txt","version":"proposal"}`)), nil
		case 4:
			if !strings.Contains(h[len(h)-1].Content, "proposal") {
				t.Fatal(h[len(h)-1])
			}
			return reply("", edit("merge", "user", "user+proposal")), nil
		case 5:
			return reply("ready"), nil
		case 6:
			return reply("", test("recheck"), present("newcard")), nil
		default:
			return reply("merged and verified"), nil
		}
	}
	options := Options{Command: func(context.Context, string, string, []string) (CommandResult, error) {
		verified++
		return CommandResult{Output: "ok"}, nil
	}, Present: func(ctx context.Context, p string) (any, error) {
		presented++
		_, v, err := d.Stat(ctx, p)
		return map[string]string{"path": p, "sha256": v}, err
	}}
	run := func() error { _, err := Execute(t.Context(), "update main.txt", model, d, s, options); return err }
	if err := run(); err == nil {
		t.Fatal("auto-overwrote overlapping changes")
	} else {
		waitFor(t, err, "waiting_approval")
	}
	var cp checkpoint
	if err := json.Unmarshal(s.raw, &cp); err != nil {
		t.Fatal(err)
	}
	if cp.ReviewEpoch != 1 || cp.WaitingDigest == "" || verified != 2 || presented != 2 {
		t.Fatal(cp.ReviewEpoch, verified, presented, cp.WaitingDigest)
	}
	if data, _ := os.ReadFile(file); string(data) != "user" {
		t.Fatal(string(data))
	}
	s.input = append(s.input, Control{Seq: 1, Action: "approve", Value: cp.WaitingDigest})
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(file); string(data) != "user+proposal" {
		t.Fatal(string(data))
	}
}
