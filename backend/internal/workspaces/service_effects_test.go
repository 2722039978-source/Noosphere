package workspaces

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func TestServiceEffectReceiptAndConflictRecovery(t *testing.T) {
	db := filepath.Join(t.TempDir(), "state.db")
	m, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	w, err := m.SaveWorkspace(Workspace{Name: "project", ProjectPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	m.SetGrantIdentity(func(s string) (string, error) { return s, nil })
	if _, err = m.AgentGrant(w.ID, &w.ProjectBindingID); err != nil {
		t.Fatal(err)
	}
	r, err := m.createRun(Run{EngineVersion: 2, Generation: 1, WorkspaceID: w.ID, Kind: "agent", Title: "effect"}, "running", &agentAdmission{input: json.RawMessage("{}"), binding: w.ProjectBindingID})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(t.Context(), agentGenerationKey{}, int64(1))
	state := "running"
	calls := 0
	apply := func(c context.Context, n, a, b string) error {
		if n != "NoosphereFixture" || a != state {
			return ErrConflict
		}
		state = b
		calls++
		return nil
	}
	e, err := m.ChangeService(ctx, r.ID, w.ID, "NoosphereFixture", "running", "stopped", apply)
	if err != nil || e.Status != "applied" || calls != 1 {
		t.Fatal(e, err)
	}
	if _, err = m.RestoreService(ctx, r.ID, w.ID, e.ID, "bad digest", apply); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	state = "pending"
	if _, err = m.RestoreService(ctx, r.ID, w.ID, e.ID, e.Digest, apply); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	state = "stopped"
	restored, err := m.RestoreService(ctx, r.ID, w.ID, e.ID, e.Digest, apply)
	if err != nil || restored.Status != "restored" || calls != 2 || state != "running" {
		t.Fatal(restored, err)
	}
	if _, err = m.RestoreService(ctx, r.ID, w.ID, e.ID, e.Digest, apply); err != nil || calls != 2 {
		t.Fatal(err)
	}
	records, err := m.ServiceEffects(r.ID)
	if err != nil || len(records) != 1 || records[0] != restored {
		t.Fatal(records, err)
	}
}
