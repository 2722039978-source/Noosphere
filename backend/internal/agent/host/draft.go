// Package host owns the private working tree, content store and recoverable
// publication journal for each desktop agent turn.
package host

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Draft struct {
	base, state, root, binding, rootID string
	history                            bool
}
type journal struct {
	Schema          int                     `json:"schema"`
	Work            string                  `json:"work,omitempty"`
	Conflicts       map[string]FileConflict `json:"conflicts,omitempty"`
	Root            string                  `json:"root"`
	RootID          string                  `json:"root_id"`
	Binding         string                  `json:"binding"`
	Status          string                  `json:"status"`
	Base            tree                    `json:"base"`
	Digest          string                  `json:"digest,omitempty"`
	PartialPrepared bool                    `json:"partial_prepared,omitempty"`
	Actions         []operation             `json:"actions,omitempty"`
}
type operation struct {
	Change    Change  `json:"change"`
	Before    version `json:"before"`
	After     version `json:"after"`
	Published version `json:"published"`
	Restored  version `json:"restored"`
	Temp      string  `json:"temp,omitempty"`
	TempID    string  `json:"temp_id,omitempty"`
	Done      bool    `json:"done"`
	Reverted  bool    `json:"reverted"`
	Abandoned bool    `json:"abandoned,omitempty"`
}

var safeRun = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$`)
var digestRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

func Existing(stateDir, runID string) bool {
	if !safeRun.MatchString(runID) {
		return false
	}
	i, err := os.Lstat(filepath.Join(stateDir, runID, "journal.json"))
	return err == nil && i.Mode().IsRegular()
}

// OpenHistory resolves only a journal inside trusted application state. It
// deliberately avoids opening the original project, which may have moved.
func OpenHistory(ctx context.Context, stateDir, runID string) (*Draft, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(stateDir) || !Existing(stateDir, runID) {
		return nil, fs.ErrNotExist
	}
	dir := filepath.Join(stateDir, runID)
	b, err := os.ReadFile(filepath.Join(dir, "journal.json"))
	if err != nil {
		return nil, err
	}
	var j journal
	if err = json.Unmarshal(b, &j); err != nil {
		return nil, err
	}
	if j.Schema != 1 || j.RootID == "" || j.Binding == "" {
		return nil, fmt.Errorf("invalid draft journal")
	}
	return &Draft{base: stateDir, state: dir, root: j.Root, binding: j.Binding, rootID: j.RootID, history: true}, nil
}
func (d *Draft) WorkingDir() string {
	if d.history {
		return ""
	}
	dir := "work"
	raw, err := os.ReadFile(filepath.Join(d.state, "journal.json"))
	if err == nil {
		var pointer struct{ Work string }
		if json.Unmarshal(raw, &pointer) != nil {
			return ""
		}
		if pointer.Work != "" {
			if !workVersion.MatchString(pointer.Work) {
				return ""
			}
			dir = pointer.Work
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return ""
	}
	return filepath.Join(d.state, dir)
}
func (d *Draft) blobs() string { return filepath.Join(d.state, "blobs") }
func sum(s string) string      { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func isInside(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func Open(ctx context.Context, stateDir, runID, root, binding string) (*Draft, error) {
	if !safeRun.MatchString(runID) || !filepath.IsAbs(stateDir) || binding == "" {
		return nil, fmt.Errorf("invalid draft location or binding")
	}
	root = filepath.Clean(root)
	rid, err := RootIdentity(root)
	if err != nil {
		return nil, err
	}
	if isInside(root, stateDir) || isInside(stateDir, root) {
		return nil, fmt.Errorf("draft storage and project overlap")
	}
	if err = os.MkdirAll(stateDir, 0700); err != nil {
		return nil, err
	}
	stateDir, err = filepath.EvalSymlinks(stateDir)
	if err != nil {
		return nil, err
	}
	if isInside(root, stateDir) || isInside(stateDir, root) {
		return nil, fmt.Errorf("draft storage and project overlap")
	}
	d := &Draft{base: stateDir, state: filepath.Join(stateDir, runID), root: root, binding: binding, rootID: rid}
	release, err := d.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	if Existing(stateDir, runID) {
		_, err = d.load()
		return d, err
	}
	if _, err = os.Lstat(d.state); !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("draft directory already exists or unavailable")
	}
	tmp, err := os.MkdirTemp(stateDir, ".initializing-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	staged := *d
	staged.state = tmp
	if err = os.Mkdir(staged.blobs(), 0700); err != nil {
		return nil, err
	}
	if err = os.Mkdir(staged.WorkingDir(), 0700); err != nil {
		return nil, err
	}
	rr, err := checkedRoot(root)
	if err != nil {
		return nil, err
	}
	defer rr.Close()
	base, err := staged.scan(ctx, rr, true, true)
	if err != nil {
		return nil, err
	}
	if err = staged.materialize(ctx, staged.WorkingDir(), base); err != nil {
		return nil, err
	}
	if err = staged.save(&journal{Schema: 1, Root: root, RootID: rid, Binding: binding, Status: "draft", Base: base}); err != nil {
		return nil, err
	}
	if err = os.Rename(tmp, d.state); err != nil {
		return nil, err
	}
	if err = syncPath(stateDir); err != nil {
		return nil, err
	}
	return d, nil
}

// All processes using the same application state take the same project lock.
// Closing the file releases its OS lock even if the owner crashes.
func (d *Draft) lock(ctx context.Context) (func(), error) {
	dir := filepath.Join(d.base, ".locks")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, sum(d.rootID)), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	for {
		if err = ctx.Err(); err != nil {
			f.Close()
			return nil, err
		}
		ok, err := tryLock(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		if ok {
			return func() { f.Close() }, nil
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}
func (d *Draft) load() (*journal, error) {
	b, err := os.ReadFile(filepath.Join(d.state, "journal.json"))
	if err != nil {
		return nil, err
	}
	var j journal
	if err = json.Unmarshal(b, &j); err != nil {
		return nil, err
	}
	if j.Schema != 1 || j.Root != d.root || j.RootID != d.rootID || j.Binding != d.binding {
		return nil, fmt.Errorf("draft project binding changed")
	}
	return &j, nil
}
func syncPath(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return syncDirectory(f)
}
func (d *Draft) save(j *journal) error {
	f, err := os.CreateTemp(d.state, ".journal-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = json.NewEncoder(f).Encode(j)
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), filepath.Join(d.state, "journal.json")); err != nil {
		return err
	}
	return syncPath(d.state)
}

func (d *Draft) Status(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	j, err := d.load()
	if err != nil {
		return "", err
	}
	return j.Status, nil
}

// OpenBlob accepts only immutable content hashes. Callers must first resolve a
// workspace-scoped artifact ID; this method does not authorize an HTTP request.
func (d *Draft) OpenBlob(ctx context.Context, hash string) (*os.File, error) {
	if !digestRE.MatchString(hash) {
		return nil, fmt.Errorf("invalid blob hash")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return os.Open(filepath.Join(d.blobs(), hash))
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (r ctxReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}
func (d *Draft) copyBlob(ctx context.Context, hash string, dest io.Writer) error {
	f, err := d.OpenBlob(ctx, hash)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(h, dest), ctxReader{ctx, io.LimitReader(f, maxFileSize+1)})
	if err == nil && (n > maxFileSize || hex.EncodeToString(h.Sum(nil)) != hash) {
		err = fmt.Errorf("snapshot hash mismatch")
	}
	return err
}
func (d *Draft) withDraft(ctx context.Context, write bool, fn func(*os.Root) error) error {
	if d.history {
		return fmt.Errorf("historical draft is read-only")
	}
	unlock, err := d.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	j, err := d.load()
	if err != nil {
		return err
	}
	if write && j.Status != "draft" {
		return fmt.Errorf("draft is frozen (%s)", j.Status)
	}
	r, err := checkedRoot(d.WorkingDir())
	if err != nil {
		return err
	}
	defer r.Close()
	return fn(r)
}
func (d *Draft) List(ctx context.Context) ([]string, error) {
	var names []string
	err := d.withDraft(ctx, false, func(r *os.Root) error {
		// Listing returns names only. Reads independently verify contents and
		// hashes, so enumerating a directory never reads every project file.
		return fs.WalkDir(r.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err = ctx.Err(); err != nil {
				return err
			}
			if name == "." {
				return nil
			}
			if !eligible(name) {
				if entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				dir, err := parentAt(r, name+"/probe")
				if err != nil {
					return err
				}
				return dir.Close()
			}
			if !entry.Type().IsRegular() {
				return fmt.Errorf("linked or special draft path: %s", name)
			}
			if len(names) >= maxEntries {
				return fmt.Errorf("too many draft entries")
			}
			names = append(names, name)
			return nil
		})
	})
	return names, err
}
func (d *Draft) Stat(ctx context.Context, name string) (int64, string, error) {
	var v version
	err := d.withDraft(ctx, false, func(r *os.Root) error {
		var err error
		v, err = inspect(ctx, r, name, nil)
		if err == nil && v.Hash == "" {
			return fs.ErrNotExist
		}
		return err
	})
	return v.Size, v.Hash, err
}
func (d *Draft) Archive(ctx context.Context, name string) (string, error) {
	var v version
	err := d.withDraft(ctx, false, func(r *os.Root) error {
		var err error
		v, err = d.capture(ctx, r, name, true)
		if err == nil && v.Hash == "" {
			return fs.ErrNotExist
		}
		return err
	})
	return v.Hash, err
}

type byteRange struct {
	offset int64
	limit  int
	data   []byte
}

func (b *byteRange) Write(p []byte) (int, error) {
	n := len(p)
	skip := min(b.offset, int64(n))
	b.offset -= skip
	p = p[skip:]
	b.data = append(b.data, p[:min(b.limit-len(b.data), len(p))]...)
	return n, nil
}
func (d *Draft) Read(ctx context.Context, name string, offset int64, limit int) ([]byte, error) {
	if offset < 0 || limit < 0 || limit > 8<<20 {
		return nil, fmt.Errorf("invalid file range (maximum 8 MiB)")
	}
	slice := &byteRange{offset: offset, limit: limit}
	err := d.withDraft(ctx, false, func(r *os.Root) error {
		v, err := inspect(ctx, r, name, slice)
		if err == nil && v.Hash == "" {
			return fs.ErrNotExist
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return slice.data, nil
}
func (d *Draft) Write(ctx context.Context, name string, content []byte) error {
	return d.write(ctx, name, nil, content)
}
func (d *Draft) Replace(ctx context.Context, name, expectedHash string, content []byte) error {
	return d.write(ctx, name, &expectedHash, content)
}
func (d *Draft) write(ctx context.Context, name string, expected *string, content []byte) error {
	if !eligible(name) || len(content) > maxFileSize {
		return fmt.Errorf("invalid write target or size")
	}
	if secretText.Match(content) {
		return errSecret
	}
	return d.withDraft(ctx, true, func(r *os.Root) error {
		old, err := inspect(ctx, r, name, nil)
		if err != nil {
			return err
		}
		if expected != nil && old.Hash != *expected {
			return fmt.Errorf("draft file changed: %s", name)
		}
		if err = makeParents(r, name); err != nil {
			return err
		}
		p, err := parentAt(r, name)
		if err != nil {
			return err
		}
		defer p.Close()
		temp, err := tempName()
		if err != nil {
			return err
		}
		f, err := p.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		defer p.Remove(temp)
		_, err = f.Write(content)
		if err == nil && old.Hash != "" {
			err = f.Chmod(old.Mode)
		}
		if err == nil {
			err = f.Sync()
		}
		err = errors.Join(err, f.Close())
		if err != nil {
			return err
		}
		if old.Hash == "" {
			if err = p.Link(temp, path.Base(name)); err != nil {
				return err
			}
			return p.Remove(temp)
		}
		return p.Rename(temp, path.Base(name))
	})
}
func makeParents(r *os.Root, name string) error {
	parts := strings.Split(name, "/")
	for i := 1; i < len(parts); i++ {
		dir := strings.Join(parts[:i], "/")
		p, err := parentAt(r, dir)
		if err != nil {
			return err
		}
		err = p.Mkdir(path.Base(dir), 0700)
		p.Close()
		if err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
	}
	return nil
}
func (d *Draft) Delete(ctx context.Context, name string) error {
	return d.withDraft(ctx, true, func(r *os.Root) error {
		v, err := inspect(ctx, r, name, nil)
		if err != nil {
			return err
		}
		if v.Hash == "" {
			return fs.ErrNotExist
		}
		p, err := parentAt(r, name)
		if err != nil {
			return err
		}
		defer p.Close()
		return p.Remove(path.Base(name))
	})
}
