package host

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	maxFileSize        = 200 << 20
	maxTreeBytes int64 = 2 << 30
	maxEntries         = 50000
)

var errSecret = errors.New("file contains credential-like content")
var secretText = regexp.MustCompile(`(?i)-----BEGIN (?:[A-Z0-9 ]+ )?PRIVATE KEY-----|(?:sk-|gh[pousr]_|github_pat_)[a-zA-Z0-9_-]{14,}|Bearer[ \t]+[a-z0-9._/-]{12,}|["']?(?:api[_-]?key|access[_-]?token|password|client[_-]?secret)["']?[ \t]*[:=][ \t]*["'][a-z0-9+_./=-]{8,}["']`)

// Internal files, host credentials, installed dependencies and build caches are never copied into the runner.
func eligible(name string) bool {
	if name == "." || !fs.ValidPath(name) || len(name) > 1024 || strings.ContainsAny(name, "\\:\x00") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		p := strings.ToLower(part)
		if !filepath.IsLocal(part) || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return false
		}
		if strings.HasPrefix(p, ".") && p != ".github" && p != ".gitignore" && p != ".gitattributes" && p != ".editorconfig" {
			return false
		}
		switch p {
		case "node_modules", "vendor", "__pycache__", "target", "venv", "models.yaml", "models.yml", "credentials", "credentials.json", "secrets.json", "id_rsa", "id_ed25519", "id_dsa", "id_ecdsa", "kubeconfig":
			return false
		}
		if strings.Contains(p, "credential") || strings.Contains(p, "secret") {
			return false
		}
		switch path.Ext(p) {
		case ".pem", ".p12", ".pfx", ".key", ".kdbx", ".gguf", ".safetensors":
			return false
		}
	}
	return true
}

// ValidatePath checks the same path eligibility rules as draft operations.
func (d *Draft) ValidatePath(name string) error {
	if !eligible(name) {
		return errors.New("路径超出工作区范围或属于受保护文件")
	}
	return nil
}

func checkedRoot(dir string) (*os.Root, error) {
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("project path must be absolute")
	}
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil || filepath.Clean(dir) != canonical {
		return nil, fmt.Errorf("project path moved or contains a link")
	}
	info, err := os.Lstat(canonical)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("project is not a real directory")
	}
	root, err := os.OpenRoot(canonical)
	if err != nil {
		return nil, err
	}
	live, err := root.Stat(".")
	if err != nil || !os.SameFile(info, live) {
		root.Close()
		return nil, fmt.Errorf("project changed while opening")
	}
	return root, nil
}

func RootIdentity(dir string) (string, error) {
	r, err := checkedRoot(dir)
	if err != nil {
		return "", err
	}
	defer r.Close()
	return dirID(r, ".")
}

func dirID(r *os.Root, name string) (string, error) {
	f, err := r.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("invalid directory %q", name)
	}
	id, _, err := identity(f)
	return id, err
}

// Pin an inspected directory and compare each component before descending.
func parentAt(r *os.Root, name string) (*os.Root, error) {
	if !eligible(name) {
		return nil, fmt.Errorf("ineligible workspace path %q", name)
	}
	cur, err := r.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	parts := strings.Split(name, "/")
	for _, part := range parts[:len(parts)-1] {
		entry, err := cur.Lstat(part)
		if err != nil {
			cur.Close()
			return nil, err
		}
		if !entry.IsDir() || entry.Mode()&os.ModeSymlink != 0 {
			cur.Close()
			return nil, fmt.Errorf("linked or invalid parent of %q", name)
		}
		n, err := cur.OpenRoot(part)
		if err == nil {
			var actual os.FileInfo
			actual, err = n.Stat(".")
			if err == nil && !os.SameFile(actual, entry) {
				err = fmt.Errorf("directory changed for %q", name)
			}
			if err == nil {
				_, err = dirID(n, ".")
			}
		}
		cur.Close()
		if err != nil {
			if n != nil {
				n.Close()
			}
			return nil, err
		}
		cur = n
	}
	return cur, nil
}

type version struct {
	Hash   string      `json:"hash"`
	ID     string      `json:"id"`
	Parent string      `json:"parent"`
	Size   int64       `json:"size"`
	Mode   os.FileMode `json:"mode"`
}

func sameContent(a, b version) bool { return a.Hash == b.Hash && a.Mode == b.Mode && a.Size == b.Size }
func sameVersion(a, b version) bool { return sameContent(a, b) && a.ID == b.ID && a.Parent == b.Parent }

type checkedStream struct {
	ctx  context.Context
	out  io.Writer
	tail []byte
}

func (s *checkedStream) Write(buf []byte) (int, error) {
	if err := s.ctx.Err(); err != nil {
		return 0, err
	}
	joined := append(s.tail, buf...)
	if secretText.Match(joined) {
		return 0, errSecret
	}
	s.tail = bytes.Clone(joined[max(0, len(joined)-512):])
	return s.out.Write(buf)
}

// inspect reads only a single-linked, regular file via its checked parent.
// A zero version is absence. Errors may never be interpreted as absence.
func inspect(ctx context.Context, r *os.Root, name string, out io.Writer) (version, error) {
	var v version
	parent, err := parentAt(r, name)
	if errors.Is(err, fs.ErrNotExist) {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	defer parent.Close()
	leaf := path.Base(name)
	info, err := parent.Lstat(leaf)
	if errors.Is(err, fs.ErrNotExist) {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxFileSize {
		return v, fmt.Errorf("unsupported file or over %d MiB: %s", maxFileSize>>20, name)
	}
	f, err := parent.OpenFile(leaf, os.O_RDONLY|readFlags, 0)
	if err != nil {
		return v, err
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil || !os.SameFile(info, actual) {
		return v, fmt.Errorf("file changed: %s", name)
	}
	id, links, err := identity(f)
	if err != nil {
		return v, err
	}
	if links != 1 {
		return v, fmt.Errorf("linked file %s", name)
	}
	hasher := sha256.New()
	if out == nil {
		out = io.Discard
	}
	n, err := io.Copy(&checkedStream{ctx: ctx, out: io.MultiWriter(hasher, out)}, io.LimitReader(f, maxFileSize+1))
	if err != nil {
		return v, err
	}
	live, err := parent.Lstat(leaf)
	if err != nil || !os.SameFile(info, live) || info.ModTime() != live.ModTime() || live.Size() != n || n != info.Size() || live.Mode() != info.Mode() {
		return v, fmt.Errorf("file changed during read: %s", name)
	}
	pid, err := dirID(parent, ".")
	if err != nil {
		return v, err
	}
	return version{Hash: hex.EncodeToString(hasher.Sum(nil)), ID: id, Parent: pid, Size: n, Mode: info.Mode().Perm()}, nil
}

type tree struct {
	Files map[string]version `json:"files"`
	Dirs  map[string]string  `json:"dirs"`
}

// Nonproject-controlled temp files live outside the writable draft and are renamed only after verification.
func (d *Draft) capture(ctx context.Context, r *os.Root, name string, save bool) (version, error) {
	if !save {
		return inspect(ctx, r, name, nil)
	}
	f, err := os.CreateTemp(d.blobs(), ".capture-")
	if err != nil {
		return version{}, err
	}
	defer os.Remove(f.Name())
	v, err := inspect(ctx, r, name, f)
	if err == nil && v.Hash != "" {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil || v.Hash == "" {
		return v, err
	}
	dest := filepath.Join(d.blobs(), v.Hash)
	if _, err := os.Stat(dest); errors.Is(err, fs.ErrNotExist) {
		if err = os.Rename(f.Name(), dest); err != nil {
			return version{}, err
		}
	} else if err != nil {
		return version{}, err
	}
	return v, nil
}

func (d *Draft) scan(ctx context.Context, r *os.Root, baseline, save bool, skip ...map[string]bool) (tree, error) {
	result := tree{Files: map[string]version{}, Dirs: map[string]string{}}
	var total int64
	var filters map[string]bool
	if baseline {
		filters = baselineFilters(ctx, r)
	}
	err := fs.WalkDir(r.FS(), ".", func(name string, e fs.DirEntry, err error) error {
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
			if e.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if len(skip) > 0 && skip[0][name] && !e.IsDir() {
			return nil
		}
		if len(result.Dirs)+len(result.Files) >= maxEntries {
			return fmt.Errorf("workspace exceeds %d entries", maxEntries)
		}
		if e.IsDir() {
			dir, err := parentAt(r, name+"/probe")
			if err != nil {
				return err
			}
			defer dir.Close()
			id, err := dirID(dir, ".")
			if err != nil {
				return err
			}
			result.Dirs[name] = id
			if filters[name] {
				return fs.SkipDir
			}
			return nil
		}
		if e.Type()&os.ModeSymlink != 0 || !e.Type().IsRegular() {
			if baseline {
				return nil
			}
			return fmt.Errorf("linked or special draft path %q", name)
		}
		v, err := d.capture(ctx, r, name, save)
		if errors.Is(err, errSecret) && baseline {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if v.Hash == "" {
			return fmt.Errorf("file vanished: %s", name)
		}
		total += v.Size
		if total > maxTreeBytes {
			return fmt.Errorf("workspace exceeds %d MiB", maxTreeBytes>>20)
		}
		result.Files[name] = v
		return nil
	})
	return result, err
}
