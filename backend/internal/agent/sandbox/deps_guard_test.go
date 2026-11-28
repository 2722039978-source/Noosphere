package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafeContainedRealDirBlocksSymlinkParentAndLeafEscapes(t *testing.T) {
	base := t.TempDir()
	project := filepath.Join(base, "project")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(filepath.Join(project, "frontend", "node_modules"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(outside, "node_modules"), 0700); err != nil {
		t.Fatal(err)
	}

	// Legitimate nested directory is allowed.
	got, ok := safeContainedRealDir(project, "frontend/node_modules")
	if !ok || got == "" {
		t.Fatal("expected legitimate nested node_modules to be allowed")
	}

	// Traversal is rejected.
	if _, ok := safeContainedRealDir(project, "../outside/node_modules"); ok {
		t.Fatal("expected parent traversal to be rejected")
	}

	// Symlink / junction as parent component pointing outside project MUST be rejected.
	escapeParent := filepath.Join(project, "escape_parent")
	createDirLinkForTest(t, escapeParent, outside)
	if _, ok := safeContainedRealDir(project, "escape_parent/node_modules"); ok {
		t.Fatal("expected symlink/junction parent escaping projectRoot to be rejected")
	}

	// Symlink / junction as leaf node_modules pointing outside project MUST be rejected.
	escapeLeaf := filepath.Join(project, "node_modules")
	createDirLinkForTest(t, escapeLeaf, filepath.Join(outside, "node_modules"))
	if _, ok := safeContainedRealDir(project, "node_modules"); ok {
		t.Fatal("expected symlink/junction leaf node_modules to be rejected")
	}

	// safeContainedChildPath must also reject symlink/junction parent inside work.
	work := filepath.Join(base, "work")
	if err := os.MkdirAll(filepath.Join(work, "real_sub"), 0700); err != nil {
		t.Fatal(err)
	}
	createDirLinkForTest(t, filepath.Join(work, "linked_sub"), outside)
	if _, ok := safeContainedChildPath(work, "linked_sub", "node_modules"); ok {
		t.Fatal("expected safeContainedChildPath to reject symlink/junction parent")
	}
	if child, ok := safeContainedChildPath(work, "real_sub", "node_modules"); !ok || filepath.Base(child) != "node_modules" {
		t.Fatalf("expected valid child path inside real_sub, got %q %v", child, ok)
	}
}
