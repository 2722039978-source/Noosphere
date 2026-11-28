package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
)

// Reissuing the same digest resumes a partially applied manifest. An intent
// recorded before a filesystem rename contains the exact staged inode/file ID.
func (d *Draft) Publish(ctx context.Context, expectedDigest string) error {
	return d.apply(ctx, expectedDigest, false)
}
func (d *Draft) Undo(ctx context.Context, expectedDigest string) error {
	return d.apply(ctx, expectedDigest, true)
}

func (d *Draft) apply(ctx context.Context, digest string, undo bool) error {
	if d.history {
		return fmt.Errorf("historical draft is read-only")
	}
	if !digestRE.MatchString(digest) {
		return fmt.Errorf("expected manifest hash required")
	}
	release, err := d.lock(ctx)
	if err != nil {
		return err
	}
	defer release()
	j, err := d.load()
	if err != nil {
		return err
	}
	rr, err := checkedRoot(d.root)
	if err != nil {
		return err
	}
	defer rr.Close()
	rid, err := dirID(rr, ".")
	if err != nil || rid != d.rootID {
		return fmt.Errorf("bound project moved or changed identity")
	}
	if j.Status == "draft" {
		if undo {
			return fmt.Errorf("draft has not been published")
		}
		j.Actions, err = d.plan(ctx, j)
		if err != nil {
			return err
		}
		j.Digest = ManifestDigest(changes(j.Actions))
	}
	if digest != j.Digest {
		return fmt.Errorf("manifest has changed; review the new diff")
	}
	if (undo && j.Status == "undone") || (!undo && j.Status == "published") {
		return nil
	}
	start, progress, end := "draft", "publishing", "published"
	if undo {
		start, progress, end = "published", "undoing", "undone"
	}
	partial := undo && (j.Status == "publishing" || j.Status == "undoing_partial")
	if partial {
		start, progress = "publishing", "undoing_partial"
		if j.Status == start {
			j.Status = progress
			if err = d.save(j); err != nil {
				return err
			}
		}
		if !j.PartialPrepared {
			if err = d.preparePartial(ctx, rr, j); err != nil {
				return err
			}
			j.PartialPrepared = true
			if err = d.save(j); err != nil {
				return err
			}
		}
	}
	if j.Status != start && j.Status != progress {
		return fmt.Errorf("cannot apply manifest in state %s", j.Status)
	}
	if !undo {
		if err = d.checkInputs(ctx, rr, j); err != nil {
			if j.Status == "draft" && ctx.Err() == nil {
				return fmt.Errorf("%w: %w", ErrWorkspaceChanged, err)
			}
			return err
		}
	}
	// Always preflight the whole remaining group. Edits during apply are checked
	// again immediately before their respective filesystem commits.
	for i := range j.Actions {
		op := &j.Actions[i]
		if partial && !op.Done {
			continue
		}
		if op.Temp != "" {
			continue
		} // reconcile a recorded intent in step()
		expected := op.Before
		if op.Done {
			expected = op.Published
		}
		if op.Reverted {
			expected = op.Restored
		}
		if err = checkCurrent(ctx, rr, op, expected); err != nil {
			if j.Status == "draft" && ctx.Err() == nil {
				return fmt.Errorf("%w: %w", ErrWorkspaceChanged, err)
			}
			return err
		}
	}
	if j.Status == start {
		j.Status = progress
		if err = d.save(j); err != nil {
			return err
		}
	}
	for n := range j.Actions {
		if err = ctx.Err(); err != nil {
			return err
		}
		i := n
		if undo {
			i = len(j.Actions) - 1 - n
		}
		if partial && !j.Actions[i].Done {
			continue
		}
		if err = d.step(ctx, rr, j, i, undo); err != nil {
			return fmt.Errorf("%s: %w", j.Actions[i].Change.Path, err)
		}
	}
	j.Status = end
	return d.save(j)
}

func (d *Draft) checkInputs(ctx context.Context, r *os.Root, j *journal) error {
	// Native commands may read any file visible in the draft. Validate all
	// eligible baseline inputs, including newly added source, before publication.
	touched := map[string]bool{}
	for _, op := range j.Actions {
		touched[op.Change.Path] = true
	}
	tr, err := d.scan(ctx, r, true, false, touched)
	if err != nil {
		return err
	}
	for p, v := range j.Base.Files {
		if !touched[p] && !sameVersion(v, tr.Files[p]) {
			return fmt.Errorf("verification input changed: %s", p)
		}
	}
	for p := range tr.Files {
		if !touched[p] && j.Base.Files[p].Hash == "" {
			return fmt.Errorf("verification input added: %s", p)
		}
	}
	for p, v := range j.Base.Dirs {
		if !touched[p] && v != tr.Dirs[p] {
			return fmt.Errorf("verification directory changed: %s", p)
		}
	}
	for p := range tr.Dirs {
		if !touched[p] && j.Base.Dirs[p] == "" {
			return fmt.Errorf("verification directory added: %s", p)
		}
	}
	return nil
}

func current(ctx context.Context, r *os.Root, op *operation) (version, error) {
	if !op.Change.IsDir {
		return inspect(ctx, r, op.Change.Path, nil)
	}
	name := op.Change.Path
	parent, err := parentAt(r, name)
	if errors.Is(err, fs.ErrNotExist) {
		return version{}, nil
	}
	if err != nil {
		return version{}, err
	}
	defer parent.Close()
	leaf := path.Base(name)
	entry, err := parent.Lstat(leaf)
	if errors.Is(err, fs.ErrNotExist) {
		return version{}, nil
	}
	if err != nil {
		return version{}, err
	}
	if !entry.IsDir() || entry.Mode()&os.ModeSymlink != 0 {
		return version{}, fmt.Errorf("directory changed: %s", name)
	}
	id, err := dirID(parent, leaf)
	if err != nil {
		return version{}, err
	}
	pid, err := dirID(parent, ".")
	return version{Hash: "directory", ID: id, Parent: pid}, err
}
func checkCurrent(ctx context.Context, r *os.Root, op *operation, expected version) error {
	if expected.Hash != "" {
		parts := strings.Split(op.Change.Path, "/")
		dir := "."
		for _, part := range parts {
			f, err := r.Open(dir)
			if err != nil {
				return err
			}
			found := false
			for !found {
				names, e := f.Readdirnames(256)
				for _, n := range names {
					if n == part {
						found = true
						break
					}
				}
				if e != nil {
					if !errors.Is(e, io.EOF) {
						f.Close()
						return e
					}
					break
				}
			}
			f.Close()
			if !found {
				return fmt.Errorf("path spelling changed: %s", op.Change.Path)
			}
			dir = path.Join(dir, part)
		}
	}
	live, err := current(ctx, r, op)
	if err != nil {
		return err
	}
	if !sameVersion(expected, live) {
		return fmt.Errorf("project conflict: %s", op.Change.Path)
	}
	return nil
}

// A user may replace an unchanged parent directory or insert a case alias.
// Bind the parent used by every step to the originally observed or journaled ID.
func expectedParent(j *journal, name string, undo bool) string {
	p := path.Dir(name)
	if p == "." {
		return j.RootID
	}
	if id := j.Base.Dirs[p]; id != "" {
		if undo {
			for _, op := range j.Actions {
				if op.Change.Path == p && op.Reverted {
					return op.Restored.ID
				}
			}
		}
		return id
	}
	for _, op := range j.Actions {
		if op.Change.Path == p && op.Done {
			return op.Published.ID
		}
	}
	return ""
}
func fileIdentity(r *os.Root, name string) (string, error) {
	entry, err := r.Lstat(name)
	if err != nil {
		return "", err
	}
	if entry.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("linked file")
	}
	f, err := r.OpenFile(name, os.O_RDONLY|readFlags, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !os.SameFile(info, entry) {
		return "", fmt.Errorf("path changed")
	}
	id, _, err := identity(f)
	return id, err
}
func nonempty(r *os.Root, name string) (bool, error) {
	f, err := r.Open(name)
	if err != nil {
		return false, err
	}
	defer f.Close()
	names, err := f.Readdirnames(1)
	if errors.Is(err, io.EOF) {
		err = nil
	}
	return len(names) > 0, err
}
func (d *Draft) step(ctx context.Context, r *os.Root, j *journal, index int, undo bool) error {
	op := &j.Actions[index]
	name := op.Change.Path
	done := op.Done
	if undo {
		done = op.Reverted
	}
	if done && op.Temp == "" {
		return nil
	}
	parent, err := parentAt(r, name)
	if err != nil {
		return err
	}
	defer parent.Close()
	pid, err := dirID(parent, ".")
	if err != nil || pid != expectedParent(j, name, undo) {
		return fmt.Errorf("parent directory identity changed")
	}
	leaf := path.Base(name)
	source, target := op.Before, op.After
	if undo {
		source, target = op.Published, op.Before
	}
	if done {
		return d.cleanTemp(parent, j, op)
	}
	if op.Temp == "" {
		if err = checkCurrent(ctx, r, op, source); err != nil {
			return err
		}
		temp, err := tempName()
		if err != nil {
			return err
		}
		if target.Hash == "" {
			if op.Change.IsDir {
				yes, err := nonempty(parent, leaf)
				if err != nil {
					return err
				}
				if yes {
					return fmt.Errorf("directory is not empty")
				}
			}
			op.TempID = source.ID
		} else {
			if op.Change.IsDir {
				err = parent.Mkdir(temp, 0700)
			} else {
				var f *os.File
				f, err = parent.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				if err == nil {
					err = d.copyBlob(ctx, target.Hash, f)
					if err == nil {
						err = f.Chmod(target.Mode)
					}
					if err == nil {
						err = f.Sync()
					}
					err = errors.Join(err, f.Close())
				}
			}
			if err != nil {
				parent.Remove(temp)
				return err
			}
			op.TempID, err = fileIdentity(parent, temp)
			if err != nil {
				parent.Remove(temp)
				return err
			}
		}
		op.Temp = temp
		if err = d.save(j); err != nil {
			if target.Hash != "" {
				parent.Remove(temp)
			}
			return err
		}
	}
	stageID, se := fileIdentity(parent, op.Temp)
	if se != nil && !errors.Is(se, fs.ErrNotExist) {
		return se
	}
	liveID, le := fileIdentity(parent, leaf)
	if le != nil && !errors.Is(le, fs.ErrNotExist) {
		return le
	}
	if stageID != "" && stageID != op.TempID {
		return fmt.Errorf("staged file identity changed")
	}
	if target.Hash == "" {
		if liveID != "" {
			if stageID != "" {
				return fmt.Errorf("both deletion target and its journal backup exist")
			}
			if err = checkCurrent(ctx, r, op, source); err != nil {
				return err
			}
			if op.Change.IsDir {
				yes, err := nonempty(parent, leaf)
				if err != nil {
					return err
				}
				if yes {
					return fmt.Errorf("directory is not empty")
				}
			}
			if err = parent.Rename(leaf, op.Temp); err != nil {
				return err
			}
		} else if stageID == "" {
			return fmt.Errorf("cannot verify deletion; both target and journal backup are missing")
		}
	} else {
		if liveID != op.TempID {
			if stageID == "" {
				return fmt.Errorf("staged replacement is missing")
			}
			if err = checkCurrent(ctx, r, op, source); err != nil {
				return err
			}
			if source.Hash == "" && !op.Change.IsDir {
				if err = parent.Link(op.Temp, leaf); err != nil {
					return err
				}
			} else if err = parent.Rename(op.Temp, leaf); err != nil {
				return err
			}
		}
		// A crash between link and unlink leaves both names for the same file.
		if id, e := fileIdentity(parent, op.Temp); e == nil && id == op.TempID {
			if e = parent.Remove(op.Temp); e != nil {
				return e
			}
		}
	}
	live, err := current(ctx, r, op)
	if err != nil {
		return err
	}
	if !sameContent(live, target) || (target.Hash != "" && live.ID != op.TempID) {
		return fmt.Errorf("published bytes or identity do not match the manifest")
	}
	if err = syncRoot(parent); err != nil {
		return err
	}
	if undo {
		op.Restored = live
		op.Reverted = true
	} else {
		op.Published = live
		op.Done = true
	}
	if err = d.save(j); err != nil {
		return err
	}
	return d.cleanTemp(parent, j, op)
}
func syncRoot(r *os.Root) error {
	f, err := r.Open(".")
	if err != nil {
		return err
	}
	defer f.Close()
	return syncDirectory(f)
}
func (d *Draft) cleanTemp(p *os.Root, j *journal, op *operation) error {
	if !strings.HasPrefix(op.Temp, ".noosphere-") {
		return fmt.Errorf("invalid journal temp name")
	}
	id, err := fileIdentity(p, op.Temp)
	if err == nil {
		if id != op.TempID {
			return fmt.Errorf("journal backup was replaced")
		}
		if err = p.Remove(op.Temp); err != nil {
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err = syncRoot(p); err != nil {
		return err
	}
	op.Temp = ""
	op.TempID = ""
	return d.save(j)
}
