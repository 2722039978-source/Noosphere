package host

import (
	"context"
	"github.com/noosphere/backend/internal/ownedprocess"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) { ownedprocess.RunSupervisor(); os.Exit(m.Run()) }
func TestGitUsesPrivateBaseline(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git not installed")
	}
	root := t.TempDir()
	data := t.TempDir()
	name := "说明.md"
	if e := os.WriteFile(filepath.Join(root, name), []byte("unsaved in Git\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(filepath.Join(root, ".git"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, ".git", "private-marker"), []byte("original Git state"), 0600); e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	d, e := Open(ctx, data, "baseline", root, "bound")
	if e != nil {
		t.Fatal(e)
	}
	if e = d.Write(ctx, name, []byte("draft revision\n")); e != nil {
		t.Fatal(e)
	}
	if e = d.PrepareGit(ctx); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command("git", "-C", d.WorkingDir(), "diff", "--", name)
	b, e := cmd.CombinedOutput()
	if e != nil || !strings.Contains(string(b), "-unsaved in Git") || !strings.Contains(string(b), "+draft revision") {
		t.Fatalf("%s %v", b, e)
	}
	if _, e := os.Stat(filepath.Join(d.WorkingDir(), ".git", "private-marker")); !os.IsNotExist(e) {
		t.Fatal("original repository exposed")
	}
	c, e := d.Changes(ctx)
	if len(c) != 1 || e != nil {
		t.Fatalf("%+v %v", c, e)
	}
}
