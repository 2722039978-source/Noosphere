package host

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"github.com/noosphere/backend/internal/agent/contract"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

type Change = contract.Change

func tempName() (string, error) {
	var b [12]byte
	_, err := rand.Read(b[:])
	return ".noosphere-" + hex.EncodeToString(b[:]), err
}
func ManifestDigest(changes []Change) string { return contract.ManifestDigest(changes) }
func changes(ops []operation) []Change {
	out := make([]Change, 0, len(ops))
	for _, op := range ops {
		out = append(out, op.Change)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
func (d *Draft) Changes(ctx context.Context) ([]Change, error) {
	unlock, err := d.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	j, err := d.load()
	if err != nil {
		return nil, err
	}
	if j.Status != "draft" || d.history {
		return changes(j.Actions), nil
	}
	ops, err := d.plan(ctx, j)
	if err != nil {
		return nil, err
	}
	return changes(ops), nil
}
func dirVersion(tr tree, rootID, name string) version {
	id := tr.Dirs[name]
	if id == "" {
		return version{}
	}
	parent := rootID
	if p := path.Dir(name); p != "." {
		parent = tr.Dirs[p]
	}
	return version{Hash: "directory", ID: id, Parent: parent}
}
func (d *Draft) plan(ctx context.Context, j *journal) ([]operation, error) {
	work, err := checkedRoot(d.WorkingDir())
	if err != nil {
		return nil, err
	}
	defer work.Close()
	current, err := d.scan(ctx, work, false, false)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for p := range current.Files {
		names[p] = true
	}
	for p := range j.Base.Files {
		names[p] = true
	}
	for p := range current.Dirs {
		names[p] = true
	}
	for p := range j.Base.Dirs {
		names[p] = true
	}
	var ops []operation
	for p := range names {
		before, after := j.Base.Files[p], current.Files[p]
		dir := j.Base.Dirs[p] != "" || current.Dirs[p] != ""
		if dir {
			if before.Hash != "" || after.Hash != "" {
				return nil, fmt.Errorf("cannot replace a directory with a file or vice versa: %s", p)
			}
			before = dirVersion(j.Base, j.RootID, p)
			after = dirVersion(current, "", p)
		}
		if sameContent(before, after) {
			continue
		}
		c := Change{Path: p, Kind: "modified", IsDir: dir, BeforeHash: before.Hash, AfterHash: after.Hash, BeforeMode: before.Mode, AfterMode: after.Mode, BeforeSize: before.Size, Size: after.Size}
		if before.Hash == "" {
			c.Kind = "created"
		}
		if after.Hash == "" {
			c.Kind = "deleted"
		}
		if !dir {
			if after.Hash != "" {
				captured, err := d.capture(ctx, work, p, true)
				if err != nil {
					return nil, err
				}
				if !sameVersion(captured, after) {
					return nil, fmt.Errorf("draft changed during verification: %s", p)
				}
			}
			c.Diff = d.diff(before, after)
		}
		ops = append(ops, operation{Change: c, Before: before, After: after})
	}
	// Make parents before their children and remove directories after their contents.
	rank := func(op operation) int {
		if !op.Change.IsDir {
			return 1
		}
		if op.After.Hash == "" {
			return 2
		}
		return 0
	}
	sort.Slice(ops, func(i, k int) bool {
		a, b := ops[i], ops[k]
		if rank(a) != rank(b) {
			return rank(a) < rank(b)
		}
		if rank(a) == 2 {
			return a.Change.Path > b.Change.Path
		}
		return a.Change.Path < b.Change.Path
	})
	return ops, nil
}

func (d *Draft) diff(before, after version) string {
	if before.Size > 128<<10 || after.Size > 128<<10 {
		return ""
	}
	read := func(v version) ([]string, bool) {
		if v.Hash == "" {
			return nil, true
		}
		b, err := os.ReadFile(filepath.Join(d.blobs(), v.Hash))
		if err != nil || !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
			return nil, false
		}
		return strings.SplitAfter(string(b), "\n"), true
	}
	a, ok := read(before)
	if !ok {
		return ""
	}
	b, ok := read(after)
	if !ok {
		return ""
	}
	head := 0
	for head < min(len(a), len(b)) && a[head] == b[head] {
		head++
	}
	ai, bi := len(a), len(b)
	for ai > head && bi > head && a[ai-1] == b[bi-1] {
		ai--
		bi--
	}
	if ai == head && bi == head {
		return ""
	}
	var s strings.Builder
	fmt.Fprintf(&s, "@@ -%d,%d +%d,%d @@\n", head+1, ai-head, head+1, bi-head)
	for pass, lines := range [][]string{a[head:ai], b[head:bi]} {
		prefix := []string{"-", "+"}[pass]
		for _, line := range lines {
			if s.Len()+len(line) > 4096 {
				s.WriteString("… [diff truncated]\n")
				return s.String()
			}
			s.WriteString(prefix)
			s.WriteString(line)
			if !strings.HasSuffix(line, "\n") {
				s.WriteByte('\n')
			}
		}
	}
	return s.String()
}
