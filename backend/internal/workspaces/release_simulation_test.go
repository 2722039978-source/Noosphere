package workspaces

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func TestReleaseSimulationRequiresTypedCancellationReference(t *testing.T) {
	m, e := Open(filepath.Join(t.TempDir(), "tasks.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	seen := make(chan TaskReference, 1)
	m.RegisterReferenced("release_simulation", "background", func(ctx context.Context, _ Workspace, _ json.RawMessage, _ func(string, string, any)) (any, error) {
		seen <- Reference(ctx)
		return nil, nil
	}, func(context.Context, Cancellation) error { return nil })
	ref := TaskReference{ID: "release-one", Generation: 7}
	if _, e = m.Submit("local", "release_simulation", "forged", nil); !errors.Is(e, ErrInvalid) {
		t.Fatal("generic submit accepted", e)
	}
	if _, _, e = m.Start(context.Background(), Run{WorkspaceID: "local", Kind: "release_simulation", ReferenceID: ref.ID, ReferenceGeneration: 7}); !errors.Is(e, ErrInvalid) {
		t.Fatal("external Start accepted", e)
	}
	if _, e = m.SubmitReferenced("local", "release_simulation", "replay", ref, nil); e != nil {
		t.Fatal(e)
	}
	if got := receive(t, seen); got != ref {
		t.Fatal("typed reference lost", got)
	}
}

func TestFindReferencedRunPreservesZeroGeneration(t *testing.T) {
	m, e := Open(filepath.Join(t.TempDir(), "tasks.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	m.RegisterReferenced("evaluation", "background", func(context.Context, Workspace, json.RawMessage, func(string, string, any)) (any, error) {
		return nil, nil
	}, func(context.Context, Cancellation) error { return nil })
	ref := TaskReference{ID: "zero-generation", Generation: 0}
	original, e := m.SubmitReferenced("local", "evaluation", "initial", ref, nil)
	if e != nil {
		t.Fatal(e)
	}
	resolved, e := m.FindReferencedRun("local", "evaluation", ref)
	if e != nil || resolved.ID != original.ID {
		t.Fatal("zero-generation reference missing", e)
	}
	if _, e = m.FindReferencedRun("local", "release_simulation", ref); !errors.Is(e, ErrNotFound) {
		t.Fatal("lookup crossed task kind", e)
	}
}
