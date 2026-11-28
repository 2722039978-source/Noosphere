package host

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDependencyChecksumAndPublicationExclusion(t *testing.T) {
	d, root, _ := fixture(t)
	ctx := t.Context()
	data := []byte("test package bytes")
	hash := sum(string(data))
	name := "package-1.tgz"
	p, err := d.StoreDependency(ctx, name, hash, bytes.NewReader(data))
	must(t, err)
	if got := textAt(t, d.WorkingDir(), p); got != string(data) {
		t.Fatal(got)
	}
	if _, err = d.StoreDependency(ctx, name, hash, bytes.NewBufferString("tampered")); err == nil {
		t.Fatal("checksum mismatch accepted")
	}
	if _, err = d.StoreDependency(ctx, "../escape", hash, bytes.NewReader(data)); err == nil {
		t.Fatal("escaped package name")
	}
	changes, err := d.Changes(ctx)
	must(t, err)
	if len(changes) != 0 {
		t.Fatal(changes)
	}
	if _, err = os.Stat(filepath.Join(root, ".noosphere-deps")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
