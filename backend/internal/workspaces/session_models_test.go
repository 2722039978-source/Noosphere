package workspaces

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestSessionBindingRejectsStaleRunAdmission(t *testing.T) {
	m, err := Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.SelectSessionModel("local", "s", "old"); err != nil {
		t.Fatal(err)
	}
	if err = m.SelectSessionModel("local", "s", "new"); err != nil {
		t.Fatal(err)
	}
	_, _, err = m.Start(context.Background(), Run{WorkspaceID: "local", SessionID: "s", Kind: "chat", ModelID: "old"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("stale request admitted: %v", err)
	}
}

func TestImportedOpaqueSessionIDRetainsModelBinding(t *testing.T) {
	m, err := Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	// Existing backup validation permits opaque nonempty IDs, including Unicode.
	if err = m.SelectSessionModel("local", "导入 会话/1", "model"); err != nil {
		t.Fatal(err)
	}
	id, bound, err := m.SessionModel("local", "导入 会话/1")
	if err != nil || !bound || id != "model" {
		t.Fatalf("lost imported binding: %s %v %v", id, bound, err)
	}
}
