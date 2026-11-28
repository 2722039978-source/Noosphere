package host

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestWithdrawInterruptedPublishLeavesUnappliedConflictsAlone(t *testing.T) {
	d, root, store := fixture(t)
	ctx := t.Context()
	must(t, d.Write(ctx, "a.txt", []byte("landed")))
	must(t, d.Write(ctx, "z.txt", []byte("draft only")))
	j, err := d.load()
	must(t, err)
	j.Actions, err = d.plan(ctx, j)
	must(t, err)
	j.Digest = ManifestDigest(changes(j.Actions))
	j.Status = "publishing"
	must(t, d.save(j))
	rr, err := checkedRoot(root)
	must(t, err)
	must(t, d.step(ctx, rr, j, 0, false))
	rr.Close()
	// External creation of the second target would prevent the forward commit.
	must(t, os.WriteFile(filepath.Join(root, "z.txt"), []byte("user created"), 0600))
	d, err = Open(ctx, store, "run-1", root, "binding")
	must(t, err)
	if err = d.Publish(ctx, j.Digest); err == nil {
		t.Fatal("expected conflict with external creation")
	}
	must(t, d.Undo(ctx, j.Digest))
	must(t, d.Undo(ctx, j.Digest))
	if _, err = os.Stat(filepath.Join(root, "a.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("landed change not withdrawn", err)
	}
	if textAt(t, root, "z.txt") != "user created" {
		t.Fatal("unapplied conflict was touched")
	}
}
func TestWithdrawDuringCreateIntent(t *testing.T) {
	for _, landed := range []bool{false, true} {
		t.Run(map[bool]string{false: "staged", true: "linked"}[landed], func(t *testing.T) {
			d, root, _ := fixture(t)
			ctx := t.Context()
			must(t, d.Write(ctx, "a.txt", []byte("draft")))
			j, err := d.load()
			must(t, err)
			j.Actions, err = d.plan(ctx, j)
			must(t, err)
			j.Digest = ManifestDigest(changes(j.Actions))
			j.Status = "publishing"
			rr, err := checkedRoot(root)
			must(t, err)
			defer rr.Close()
			op := &j.Actions[0]
			op.Temp = ".noosphere-fixture"
			must(t, os.WriteFile(filepath.Join(root, op.Temp), []byte("draft"), op.After.Mode))
			op.TempID, err = fileIdentity(rr, op.Temp)
			must(t, err)
			must(t, d.save(j))
			if landed {
				must(t, rr.Link(op.Temp, "a.txt"))
			} else {
				must(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("user revision"), 0600))
			}
			must(t, d.Undo(ctx, j.Digest))
			if landed {
				if _, err = os.Stat(filepath.Join(root, "a.txt")); !errors.Is(err, fs.ErrNotExist) {
					t.Fatal(err)
				}
			} else if textAt(t, root, "a.txt") != "user revision" {
				t.Fatal("external target overwritten")
			}
			if _, err = os.Stat(filepath.Join(root, op.Temp)); !errors.Is(err, fs.ErrNotExist) {
				t.Fatal(err)
			}
		})
	}
}
