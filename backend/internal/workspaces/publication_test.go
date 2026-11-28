package workspaces

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func TestPublicationLeaseChecksGenerationCursorAndBinding(t *testing.T) {
	m, err := Open(filepath.Join(t.TempDir(), "state.db"))
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
	r, err := m.createRun(Run{EngineVersion: 2, Generation: 1, WorkspaceID: w.ID, Kind: "agent", Title: "test"}, "running", &agentAdmission{input: json.RawMessage("{}"), binding: w.ProjectBindingID})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(t.Context(), agentGenerationKey{}, int64(1))
	_, err = m.ControlAgent(r.ID, w.ID, ControlRequest{Action: "steer", RequestID: "steer-1", Value: "New direction"})
	if err != nil {
		t.Fatal(err)
	}
	controls, err := m.AgentControls(r.ID, 0)
	if err != nil || len(controls) != 1 {
		t.Fatal(controls, err)
	}
	shouldNotRun := func() error { t.Fatal("stale operation executed"); return nil }
	if err = m.WithPublicationLease(ctx, r.ID, w.ID, 0, shouldNotRun); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	stale := context.WithValue(ctx, agentGenerationKey{}, int64(0))
	if err = m.WithPublicationLease(stale, r.ID, w.ID, controls[0].Seq, shouldNotRun); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	rebound := w
	rebound.ProjectPath = t.TempDir()
	err = m.WithPublicationLease(ctx, r.ID, w.ID, controls[0].Seq, func() error {
		if _, err := m.SaveWorkspace(rebound); !errors.Is(err, ErrBusy) {
			t.Fatalf("rebind during publication: %v", err)
		}
		if _, err := m.Cancel(r.ID); !errors.Is(err, ErrConflict) {
			t.Fatalf("cancel during publication: %v", err)
		}
		if _, err := m.ControlAgent(r.ID, w.ID, ControlRequest{Action: "steer", RequestID: "steer-2", Value: "New turn"}); !errors.Is(err, ErrConflict) {
			t.Fatalf("steer during publication: %v", err)
		}
		if _, err := m.Workspace(w.ID); err != nil {
			t.Fatal(err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.SaveWorkspace(rebound); err != nil {
		t.Fatal(err)
	}
	if err = m.WithPublicationLease(ctx, r.ID, w.ID, controls[0].Seq, shouldNotRun); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}
