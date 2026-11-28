package workspaces

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentDeliveryCommitsOnlyCurrentArtifactsAndTerminalResult(t *testing.T) {
	for _, declined := range []bool{false, true} {
		t.Run(fmt.Sprint(declined), func(t *testing.T) {
			db := filepath.Join(t.TempDir(), "workspace.db")
			m, err := Open(db)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			ws, err := m.SaveWorkspace(Workspace{Name: "p", ProjectPath: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			m.SetGrantIdentity(func(s string) (string, error) { return s, nil })
			if _, err = m.AgentGrant(ws.ID, &ws.ProjectBindingID); err != nil {
				t.Fatal(err)
			}
			run, err := m.createRun(Run{EngineVersion: 2, Generation: 1, WorkspaceID: ws.ID, Kind: "agent", Title: "deliver"}, "running", &agentAdmission{input: json.RawMessage("{}"), binding: ws.ProjectBindingID})
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.WithValue(t.Context(), agentGenerationKey{}, int64(1))
			item := func(path string, hash string) map[string]any {
				return map[string]any{"Path": path, "Hash": hash, "Metadata": map[string]string{"id": fmt.Sprintf("%x", sha256.Sum256([]byte(path))), "path": path, "sha256": hash}}
			}
			old := item("old.pdf", strings.Repeat("1", 64))
			latest := item("new.pdf", strings.Repeat("2", 64))
			state := map[string]any{"Artifacts": []any{old, latest}, "History": []any{map[string]any{"message": map[string]string{"role": "tool", "content": "private full observation"}}}}
			cp, _ := json.Marshal(state)
			if err = m.SaveAgentCheckpoint(ctx, run.ID, cp, "tool_completed", "present", map[string]string{"tool": "present"}); err != nil {
				t.Fatal(err)
			}
			artifacts, err := m.AgentArtifacts(run.ID)
			if err != nil || len(artifacts) != 2 {
				t.Fatal(artifacts, err)
			}
			if _, err = m.ControlAgent(run.ID, ws.ID, ControlRequest{Action: "steer", Value: "Remove old file", RequestID: "steer-1"}); err != nil {
				t.Fatal(err)
			}
			controls, err := m.AgentControls(run.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			state["Artifacts"] = []any{latest}
			state["Declined"] = declined
			cp, _ = json.Marshal(state)
			result := map[string]any{"answer": "final", "delivery_declined": declined}
			if err = m.SaveAgentCheckpoint(ctx, run.ID, cp, "agent_delivered", "done", result); !errors.Is(err, ErrAgentControlPending) {
				t.Fatalf("missed steer: %v", err)
			}
			state["Cursor"] = controls[0].Seq
			cp, _ = json.Marshal(state)
			if err = m.SaveAgentCheckpoint(ctx, run.ID, cp, "agent_delivered", "done", result); err != nil {
				t.Fatal(err)
			}
			if err = m.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(db)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			r, err := reopened.Run(run.ID)
			if err != nil || r.Status != "completed" || !strings.Contains(string(r.Result), "final") {
				t.Fatal(r, err)
			}
			artifacts, err = reopened.AgentArtifacts(run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if declined && len(artifacts) != 0 || !declined && (len(artifacts) != 1 || !strings.Contains(string(artifacts[0]), "new.pdf")) {
				t.Fatalf("stale projection: %v", artifacts)
			}
			var frame string
			if err = reopened.db.QueryRow("SELECT body FROM agent_transcript WHERE run_id=?", run.ID).Scan(&frame); err != nil || !strings.Contains(frame, "private full observation") {
				t.Fatal(frame, err)
			}
			events, err := reopened.Events(run.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, event := range events {
				if strings.Contains(string(event.Data), "private full observation") {
					t.Fatal("private frame in public event")
				}
			}
		})
	}
}
