package host

import (
	"context"
	"errors"
	"fmt"
	"github.com/noosphere/backend/internal/agent/contract"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ErrWorkspaceChanged is returned before any item has been written live. A
// frozen, partially published manifest must be resumed or withdrawn instead.
var ErrWorkspaceChanged = contract.ErrWorkspaceChanged
var workVersion = regexp.MustCompile(`^work-rebase-[0-9]+$`)

type FileConflict = contract.FileConflict
type RefreshResult = contract.RefreshResult

func (d *Draft) materialize(ctx context.Context, dir string, content tree) error {
	for name := range content.Dirs {
		if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(name)), 0700); err != nil {
			return err
		}
	}
	for name, v := range content.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		f, err := os.OpenFile(filepath.Join(dir, filepath.FromSlash(name)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		err = d.copyBlob(ctx, v.Hash, f)
		if err == nil {
			err = f.Chmod(v.Mode)
		}
		if err == nil {
			err = f.Sync()
		}
		err = errors.Join(err, f.Close())
		if err != nil {
			return err
		}
	}
	return syncPath(dir)
}
func workspaceDelta(a, b tree) (map[string]bool, map[string]bool) {
	changed := map[string]bool{}
	parents := map[string]bool{}
	for p, v := range a.Files {
		if !sameVersion(v, b.Files[p]) {
			changed[p] = true
		}
	}
	for p := range b.Files {
		if _, ok := a.Files[p]; !ok {
			changed[p] = true
		}
	}
	for p, id := range a.Dirs {
		if b.Dirs[p] != id {
			changed[p] = true
		}
	}
	for p := range b.Dirs {
		if _, ok := a.Dirs[p]; !ok {
			changed[p] = true
		}
	}
	for p := range changed {
		for s := path.Dir(p); s != "."; s = path.Dir(s) {
			parents[s] = true
		}
	}
	return changed, parents
}
func overlaps(name string, isDir bool, changed, parents, blocked map[string]bool) bool {
	if isDir && parents[name] {
		return true
	}
	for p := name; p != "."; p = path.Dir(p) {
		if changed[p] || blocked[p] {
			return true
		}
	}
	return false
}
func cloneConflicts(m map[string]FileConflict) []FileConflict {
	out := make([]FileConflict, 0, len(m))
	for _, c := range m {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
func (d *Draft) FileConflicts(ctx context.Context) ([]FileConflict, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	j, err := d.load()
	if err != nil {
		return nil, err
	}
	return cloneConflicts(j.Conflicts), nil
}

func (d *Draft) ReadConflict(ctx context.Context, name, revision string, offset int64, limit int) ([]byte, error) {
	if !eligible(name) || offset < 0 || limit < 0 || limit > 8<<20 {
		return nil, fmt.Errorf("invalid conflict range or path")
	}
	j, err := d.load()
	if err != nil {
		return nil, err
	}
	c, ok := j.Conflicts[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	var hash string
	switch revision {
	case "base":
		hash = c.BeforeHash
	case "proposal":
		hash = c.ProposalHash
	case "current":
		hash = c.CurrentHash
	default:
		return nil, fmt.Errorf("unknown conflict version")
	}
	if hash == "" || hash == "directory" {
		return nil, fs.ErrNotExist
	}
	f, err := d.OpenBlob(ctx, hash)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b := make([]byte, limit)
	n, err := f.ReadAt(b, offset)
	if errors.Is(err, io.EOF) {
		err = nil
	}
	return b[:n], err
}

// Refresh re-anchors an unpublished draft to the workspace. Uncontested
// changes are carried forward. Overlapping paths take the user's live version;
// their earlier baseline and proposal remain independently inspectable.
func (d *Draft) Refresh(ctx context.Context) (RefreshResult, error) {
	var result RefreshResult
	if d.history {
		return result, fmt.Errorf("historical draft")
	}
	unlock, err := d.lock(ctx)
	if err != nil {
		return result, err
	}
	defer unlock()
	j, err := d.load()
	if err != nil {
		return result, err
	}
	if j.Status != "draft" {
		return result, fmt.Errorf("refresh requires an unpublished draft")
	}
	scope, err := checkedRoot(d.root)
	if err != nil {
		return result, err
	}
	defer scope.Close()
	id, err := dirID(scope, ".")
	if err != nil || id != j.RootID {
		return result, fmt.Errorf("project identity changed")
	}
	ops, err := d.plan(ctx, j)
	if err != nil {
		return result, err
	}
	live, err := d.scan(ctx, scope, true, true)
	if err != nil {
		return result, err
	}
	modified, parents := workspaceDelta(j.Base, live)
	result.ChangedInputs = len(modified)
	if len(modified) == 0 {
		// The host may have switched the draft just before a crash prevented
		// the core from saving its refresh checkpoint. Its durable conflict blobs
		// and new baseline already represent this input revision.
		result.Conflicts = cloneConflicts(j.Conflicts)
		return result, nil
	}
	desired := tree{Files: maps.Clone(live.Files), Dirs: maps.Clone(live.Dirs)}
	if j.Conflicts == nil {
		j.Conflicts = map[string]FileConflict{}
	}
	blocked := map[string]bool{}
	// Precompute blocked directory edits before their children, including deletes.
	for _, op := range ops {
		if op.Change.IsDir && overlaps(op.Change.Path, true, modified, parents, nil) {
			blocked[op.Change.Path] = true
		}
	}
	for name, c := range j.Conflicts {
		c.CurrentHash = live.Files[name].Hash
		if live.Dirs[name] != "" {
			c.CurrentHash = "directory"
		}
		j.Conflicts[name] = c
	}
	for _, op := range ops {
		p := op.Change.Path
		if overlaps(p, op.Change.IsDir, modified, parents, blocked) {
			currentHash := live.Files[p].Hash
			if live.Dirs[p] != "" {
				currentHash = "directory"
			}
			j.Conflicts[p] = FileConflict{Path: p, BeforeHash: op.Before.Hash, ProposalHash: op.After.Hash, CurrentHash: currentHash, Diff: op.Change.Diff}
			continue
		}
		if op.Change.IsDir {
			if op.After.Hash == "" {
				delete(desired.Dirs, p)
			} else {
				desired.Dirs[p] = op.After.ID
			}
		} else if op.After.Hash == "" {
			delete(desired.Files, p)
		} else {
			desired.Files[p] = op.After
		}
	}
	previous := d.WorkingDir()
	next, err := os.MkdirTemp(d.state, "work-rebase-")
	if err != nil {
		return result, err
	}
	committed := false
	defer func() {
		if !committed {
			d.removeWork(next)
		}
	}()
	if err = d.materialize(ctx, next, desired); err != nil {
		return result, err
	}
	if err = d.copyApprovedCache(ctx, next); err != nil {
		return result, err
	}
	j.Work = filepath.Base(next)
	j.Base = live
	j.Actions = nil
	j.Digest = ""
	if err = d.save(j); err != nil {
		// A directory sync can fail after the journal rename. Preserve a tree
		// already named by the visible journal for recovery.
		if current, e := d.load(); e == nil && current.Work == j.Work {
			committed = true
		}
		return result, err
	}
	committed = true
	d.removeWork(previous)
	result.Conflicts = cloneConflicts(j.Conflicts)
	return result, nil
}

func (d *Draft) removeWork(dir string) {
	if filepath.Dir(dir) != d.state {
		return
	}
	base := filepath.Base(dir)
	if base != "work" && !workVersion.MatchString(base) {
		return
	}
	live, err := os.Lstat(dir)
	if err != nil || !live.IsDir() || live.Mode()&os.ModeSymlink != 0 {
		return
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err == nil && resolved == dir {
		_ = os.RemoveAll(dir)
	}
}

// The cache directory is writable by the isolated command. Carry forward only
// regular, verified archives with the expected path and digest.
func (d *Draft) copyApprovedCache(ctx context.Context, next string) error {
	// Use the app-owned archive registry; forged cache names in project code
	// do not grant network access or create new approved dependency references.
	rows, err := os.ReadDir(filepath.Join(d.state, "dependencies"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var total int64
	returnDir, err := os.OpenRoot(next)
	if err != nil {
		return err
	}
	defer returnDir.Close()
	output, err := realSubdir(returnDir, ".noosphere-deps")
	if err != nil {
		return err
	}
	defer output.Close()
	for _, e := range rows {
		digest, name, ok := strings.Cut(e.Name(), "_")
		if !ok || !digestRE.MatchString(digest) || !packageName.MatchString(name) {
			continue
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		blob, err := d.OpenBlob(ctx, digest)
		if err != nil {
			return err
		}
		info, err := blob.Stat()
		blob.Close()
		if err != nil {
			return err
		}
		total += info.Size()
		if total > maxTreeBytes {
			return fmt.Errorf("dependency cache exceeds draft limit")
		}
		sub, err := realSubdir(output, digest)
		if err != nil {
			return err
		}
		f, err := sub.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			err = d.copyBlob(ctx, digest, f)
			if err == nil {
				err = f.Sync()
			}
			err = errors.Join(err, f.Close())
		}
		sub.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
