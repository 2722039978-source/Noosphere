package workspaces

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRestoredAgentRunsRemainInertAfterIndependentNewGrant(t *testing.T) {
	storage := t.TempDir()
	db := filepath.Join(storage, "workspace.sqlite")
	m, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	w, err := m.SaveWorkspace(Workspace{Name: "same project", ProjectPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	identify := func(s string) (string, error) { return s, nil }
	m.SetGrantIdentity(identify)
	if _, err = m.AgentGrant(w.ID, &w.ProjectBindingID); err != nil {
		t.Fatal(err)
	}
	run, err := m.createRun(Run{EngineVersion: 2, Generation: 1, WorkspaceID: w.ID, Kind: "agent", Title: "awaited"}, "waiting_approval", &agentAdmission{input: json.RawMessage("{}"), binding: w.ProjectBindingID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.db.Exec("UPDATE agent_sessions SET approval='previous-approval' WHERE run_id=?", run.ID); err != nil {
		t.Fatal(err)
	}
	const artifact = "test-artifact"
	if _, err = m.db.Exec("INSERT INTO agent_deliveries(run_id,id,body) VALUES(?,?,?)", run.ID, artifact, `{"id":"test-artifact"}`); err != nil {
		t.Fatal(err)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(storage, ".restored-agent-history"), []byte("1"), 0600); err != nil {
		t.Fatal(err)
	}
	m, err = Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.SetGrantIdentity(identify)
	granted, err := m.AgentGrant(w.ID, nil)
	if err != nil || granted != "" {
		t.Fatalf("restored grant %q %v", granted, err)
	}
	state, err := m.AgentState(run.ID, w.ID)
	if err != nil || !state.History || state.Approval != "" || state.Run.Status != "interrupted" || state.Run.Phase != "restored_history" {
		t.Fatal(state, err)
	}
	cards, err := m.AgentArtifacts(run.ID)
	if err != nil || len(cards) != 1 {
		t.Fatal(cards, err)
	}
	if _, err = os.Stat(filepath.Join(storage, ".restored-agent-history")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, err = m.AgentGrant(w.ID, &w.ProjectBindingID); err != nil {
		t.Fatal(err)
	}
	if _, err = m.ControlAgent(run.ID, w.ID, ControlRequest{RequestID: "new-confirm", Action: "approve", Expected: "previous-approval"}); !errors.Is(err, ErrConflict) {
		t.Fatal("restored approval reused", err)
	}
	if err = m.ValidateAgent(t.Context(), run.ID, w.ID); !errors.Is(err, ErrConflict) {
		t.Fatal("old run inherited new grant", err)
	}
	if err = m.UndoAgent(t.Context(), run.ID, w.ID, ControlRequest{RequestID: "new-undo", Expected: "0000000000000000000000000000000000000000000000000000000000000000"}, func(context.Context) error { t.Fatal("restored undo executed"); return nil }); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}
