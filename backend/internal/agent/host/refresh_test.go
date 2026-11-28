package host

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestBaselineOmitsExplicitIgnoredDistributionTrees(t *testing.T) {
	root, store := realTemp(t), realTemp(t)
	must(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte("release/\ncache/\n!cache/source.txt\n*.md\n"), 0600))
	for _, dir := range []string{"release", "cache"} {
		must(t, os.Mkdir(filepath.Join(root, dir), 0700))
		must(t, os.WriteFile(filepath.Join(root, dir, "source.txt"), []byte("kept only when explicitly included"), 0600))
	}
	must(t, os.WriteFile(filepath.Join(root, "untracked.md"), []byte("my notes"), 0600))
	d, err := Open(t.Context(), store, "fixture", root, "binding")
	must(t, err)
	for _, p := range []string{"cache/source.txt", "untracked.md"} {
		if _, _, err := d.Stat(t.Context(), p); err != nil {
			t.Fatal(p, err)
		}
	}
	if _, _, err := d.Stat(t.Context(), "release/source.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("copied excluded release", err)
	}
	must(t, d.Write(t.Context(), "release/new-report.md", []byte("new artifact")))
	must(t, d.Publish(t.Context(), manifest(t, d)))
	if textAt(t, root, "release/new-report.md") != "new artifact" || textAt(t, root, "release/source.txt") != "kept only when explicitly included" {
		t.Fatal("output tree lost user data")
	}
}
func TestRefreshRetainsCleanEditsAndRebasesConflicts(t *testing.T) {
	d, root, store := fixture(t)
	ctx := t.Context()
	must(t, d.Write(ctx, "用户.txt", []byte("agent proposal\n")))
	must(t, d.Write(ctx, "generated.txt", []byte("uncontested")))
	const pkg = "package bytes"
	cached, err := d.StoreDependency(ctx, "pkg.tgz", sum(pkg), bytes.NewBufferString(pkg))
	must(t, err)
	must(t, os.WriteFile(filepath.Join(root, "用户.txt"), []byte("new user input\n"), 0600))
	must(t, os.WriteFile(filepath.Join(root, "module.go"), []byte("new build input\n"), 0600))
	must(t, os.WriteFile(filepath.Join(d.WorkingDir(), filepath.FromSlash(cached)), []byte("tampered sandbox cache"), 0600))
	err = d.Publish(ctx, manifest(t, d))
	if !errors.Is(err, ErrWorkspaceChanged) {
		t.Fatalf("no refresh signal: %v", err)
	}
	refreshed, err := d.Refresh(ctx)
	must(t, err)
	if refreshed.ChangedInputs != 2 || len(refreshed.Conflicts) != 1 || refreshed.Conflicts[0].Path != "用户.txt" {
		t.Fatal(refreshed)
	}
	if textAt(t, d.WorkingDir(), "用户.txt") != "new user input\n" || textAt(t, d.WorkingDir(), "module.go") != "new build input\n" || textAt(t, d.WorkingDir(), cached) != pkg {
		t.Fatal("stale rebase files")
	}
	d, err = Open(ctx, store, "run-1", root, "binding")
	must(t, err)
	again, err := d.Refresh(ctx)
	must(t, err)
	if len(again.Conflicts) != 1 {
		t.Fatal("refresh checkpoint did not survive restart", again)
	}
	for ver, want := range map[string]string{"base": "my uncommitted text\n", "proposal": "agent proposal\n", "current": "new user input\n"} {
		b, err := d.ReadConflict(ctx, "用户.txt", ver, 0, 128)
		must(t, err)
		if string(b) != want {
			t.Fatal(ver, string(b))
		}
	}
	c, err := d.Changes(ctx)
	must(t, err)
	if len(c) != 1 || c[0].Path != "generated.txt" {
		t.Fatalf("unresolved work was not preserved: %+v", c)
	}
	must(t, d.Replace(ctx, "用户.txt", sum("new user input\n"), []byte("new user input\nagent changes\n")))
	hash := manifest(t, d)
	must(t, d.Publish(ctx, hash))
	must(t, d.Undo(ctx, hash))
	if textAt(t, root, "用户.txt") != "new user input\n" || textAt(t, root, "module.go") != "new build input\n" {
		t.Fatal("undo dropped external edits")
	}
}
func TestRefreshProtectsExternallyAddedChildrenDuringDirectoryDeletion(t *testing.T) {
	root, store := realTemp(t), realTemp(t)
	ctx := t.Context()
	must(t, os.Mkdir(filepath.Join(root, "folder"), 0700))
	must(t, os.WriteFile(filepath.Join(root, "folder", "existing.txt"), []byte("old"), 0600))
	d, err := Open(ctx, store, "r", root, "b")
	must(t, err)
	// Simulate a script deleting a subtree from the private working directory.
	must(t, d.Delete(ctx, "folder/existing.txt"))
	must(t, os.Remove(filepath.Join(d.WorkingDir(), "folder")))
	must(t, os.WriteFile(filepath.Join(root, "folder", "user.txt"), []byte("new"), 0600))
	if !errors.Is(d.Publish(ctx, manifest(t, d)), ErrWorkspaceChanged) {
		t.Fatal("new file wasn't protected")
	}
	refresh, err := d.Refresh(ctx)
	must(t, err)
	if len(refresh.Conflicts) != 2 {
		t.Fatal(refresh)
	}
	if textAt(t, d.WorkingDir(), "folder/user.txt") != "new" || textAt(t, d.WorkingDir(), "folder/existing.txt") != "old" {
		t.Fatal("unsafe directory merge")
	}
}
