package host

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func realTemp(t *testing.T) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func fixture(t *testing.T) (*Draft, string, string) {
	t.Helper()
	root, store := realTemp(t), realTemp(t)
	for name, txt := range map[string]string{"用户.txt": "my uncommitted text\n", "module.go": "package example\n", "old.txt": "remove me\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(txt), 0600); err != nil {
			t.Fatal(err)
		}
	}
	d, err := Open(t.Context(), store, "run-1", root, "binding")
	if err != nil {
		t.Fatal(err)
	}
	return d, root, store
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func textAt(t *testing.T, root, p string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, p))
	must(t, err)
	return string(b)
}
func manifest(t *testing.T, d *Draft) string {
	t.Helper()
	cs, err := d.Changes(t.Context())
	must(t, err)
	return ManifestDigest(cs)
}
func TestPublishAndUndoRound(t *testing.T) {
	d, root, store := fixture(t)
	ctx := t.Context()
	must(t, d.Replace(ctx, "用户.txt", sum("my uncommitted text\n"), []byte("revision\n")))
	must(t, d.Delete(ctx, "old.txt"))
	binary := bytes.Repeat([]byte{0, 0xab, 0xff, 0x0a}, 80<<10)
	must(t, d.Write(ctx, "deliverables/sub/演示.pptx", binary))
	if textAt(t, root, "用户.txt") != "my uncommitted text\n" {
		t.Fatal("live tree modified before publish")
	}
	digest := manifest(t, d)
	if err := d.Publish(ctx, sum("stale")); err == nil {
		t.Fatal("stale manifest accepted")
	}
	var err error
	d, err = Open(ctx, store, "run-1", root, "binding")
	must(t, err)
	must(t, d.Publish(ctx, digest))
	must(t, d.Publish(ctx, digest))
	if textAt(t, root, "用户.txt") != "revision\n" || !bytes.Equal([]byte(textAt(t, root, "deliverables/sub/演示.pptx")), binary) {
		t.Fatal("published content mismatch")
	}
	if _, err := os.Stat(filepath.Join(root, "old.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("delete: %v", err)
	}
	hash, err := d.Archive(ctx, "deliverables/sub/演示.pptx")
	must(t, err)
	f, err := d.OpenBlob(ctx, hash)
	must(t, err)
	got, err := io.ReadAll(f)
	must(t, err)
	f.Close()
	if !bytes.Equal(got, binary) {
		t.Fatal("bad artifact")
	}
	must(t, os.WriteFile(filepath.Join(root, "用户.txt"), []byte("editor"), 0600))
	if err = d.Undo(ctx, digest); err == nil {
		t.Fatal("undo overwrote editor change")
	}
	if _, err := os.Stat(filepath.Join(root, "deliverables/sub/演示.pptx")); err != nil {
		t.Fatalf("partial rollback: %v", err)
	}
	must(t, os.WriteFile(filepath.Join(root, "用户.txt"), []byte("revision\n"), 0600))
	must(t, d.Undo(ctx, digest))
	must(t, d.Undo(ctx, digest))
	if textAt(t, root, "用户.txt") != "my uncommitted text\n" || textAt(t, root, "old.txt") != "remove me\n" {
		t.Fatal("dirty state was not restored")
	}
	if _, err := os.Stat(filepath.Join(root, "deliverables")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("new directory retained: %v", err)
	}
	if _, err := Open(ctx, store, "run-1", root, "another-binding"); err == nil {
		t.Fatal("stale grant opened")
	}
	if status, err := d.Status(ctx); err != nil || status != "undone" {
		t.Fatalf("status=%s error=%v", status, err)
	}
}
func TestStaleInputsAndConcurrentDrafts(t *testing.T) {
	d, root, store := fixture(t)
	ctx := t.Context()
	other, err := Open(ctx, store, "run-2", root, "binding")
	must(t, err)
	must(t, d.Write(ctx, "a.txt", []byte("first")))
	must(t, other.Write(ctx, "b.txt", []byte("second")))
	must(t, d.Publish(ctx, manifest(t, d)))
	if err := other.Publish(ctx, manifest(t, other)); err == nil || !strings.Contains(err.Error(), "input added") {
		t.Fatalf("stale verification accepted: %v", err)
	}
	must(t, d.Undo(ctx, manifest(t, d)))
	must(t, os.WriteFile(filepath.Join(root, "module.go"), []byte("editor changes dependency"), 0600))
	if err := other.Publish(ctx, manifest(t, other)); err == nil || !strings.Contains(err.Error(), "input changed") {
		t.Fatalf("changed input: %v", err)
	}
}
func TestRecoverInterruptedCreate(t *testing.T) {
	d, root, store := fixture(t)
	ctx := t.Context()
	must(t, d.Write(ctx, "a.txt", []byte("result")))
	j, err := d.load()
	must(t, err)
	j.Actions, err = d.plan(ctx, j)
	must(t, err)
	j.Digest = ManifestDigest(changes(j.Actions))
	j.Status = "publishing"
	op := &j.Actions[0]
	op.Temp = ".noosphere-fixture"
	rr, err := checkedRoot(root)
	must(t, err)
	must(t, os.WriteFile(filepath.Join(root, op.Temp), []byte("result"), op.After.Mode))
	op.TempID, err = fileIdentity(rr, op.Temp)
	must(t, err)
	must(t, d.save(j))
	// Loss of power after link but before removing the staging name or writing done.
	must(t, rr.Link(op.Temp, "a.txt"))
	rr.Close()
	d, err = Open(ctx, store, "run-1", root, "binding")
	must(t, err)
	must(t, d.Publish(ctx, j.Digest))
	if textAt(t, root, "a.txt") != "result" {
		t.Fatal("incorrect recovery")
	}
	if _, err := os.Stat(filepath.Join(root, op.Temp)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	must(t, d.Undo(ctx, j.Digest))
}
func TestSensitiveFilesAndRootBoundary(t *testing.T) {
	root, store := realTemp(t), realTemp(t)
	ctx := context.Background()
	for name, raw := range map[string]string{".env": "token", "secret.json": "data", "regular.txt": "Bearer abcdefghijklmnopqrstuvwxyz", "okay.go": "public contents"} {
		must(t, os.WriteFile(filepath.Join(root, name), []byte(raw), 0600))
	}
	d, err := Open(ctx, store, "r", root, "b")
	must(t, err)
	names, err := d.List(ctx)
	must(t, err)
	if strings.Join(names, ",") != "okay.go" {
		t.Fatalf("sensitive file in draft: %v", names)
	}
	if err = d.Write(ctx, "../escaped", []byte("escape")); err == nil {
		t.Fatal("escaped")
	}
	if err = d.Write(ctx, "dir/key.txt", []byte("Bearer abcdefghijklmnopqrstuvwxyz")); err == nil {
		t.Fatal("credential written")
	}
	if _, err = Open(ctx, filepath.Join(root, "state"), "r", root, "b"); err == nil {
		t.Fatal("overlap accepted")
	}
	if _, err = os.Stat(filepath.Join(root, "state")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("created state in real project")
	}
}
