package host

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const MaxDependencyBytes = 64 << 20

var packageName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._+-]{0,160}$`)

func realSubdir(parent *os.Root, name string) (*os.Root, error) {
	if err := parent.Mkdir(name, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, err
	}
	entry, err := parent.Lstat(name)
	if err != nil || !entry.IsDir() || entry.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("invalid cache directory")
	}
	sub, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	live, err := sub.Stat(".")
	if err != nil || !os.SameFile(live, entry) {
		sub.Close()
		return nil, fmt.Errorf("cache directory changed")
	}
	if _, err = dirID(sub, "."); err != nil {
		sub.Close()
		return nil, err
	}
	return sub, nil
}

// Keep a private, verified copy so a rebase can rebuild the offline cache
// without trusting writable package files or asking for a second download.
func (d *Draft) retainDependency(ctx context.Context, dir *os.Root, tmp, name, digest string) error {
	source, err := dir.Open(tmp)
	if err != nil {
		return err
	}
	defer source.Close()
	blob, err := os.CreateTemp(d.blobs(), ".dependency-")
	if err != nil {
		return err
	}
	defer os.Remove(blob.Name())
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(h, blob), ctxReader{ctx, source})
	if err == nil && hex.EncodeToString(h.Sum(nil)) != digest {
		err = fmt.Errorf("package changed after download")
	}
	if err == nil {
		err = blob.Sync()
	}
	err = errors.Join(err, blob.Close())
	if err != nil {
		return err
	}
	if err = os.Link(blob.Name(), filepath.Join(d.blobs(), digest)); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	if err = syncPath(d.blobs()); err != nil {
		return err
	}
	refs := filepath.Join(d.state, "dependencies")
	if err = os.MkdirAll(refs, 0700); err != nil {
		return err
	}
	marker, err := os.OpenFile(filepath.Join(refs, digest+"_"+name), os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	err = marker.Close()
	if err != nil {
		return err
	}
	return syncPath(refs)
}

// StoreDependency stores verified archive bytes only. The sandbox may read
// this cache, but it is excluded from project publication and undo.
func (d *Draft) StoreDependency(ctx context.Context, name, digest string, r io.Reader) (string, error) {
	if !packageName.MatchString(name) || !filepath.IsLocal(name) || strings.HasSuffix(name, ".") || !digestRE.MatchString(digest) {
		return "", fmt.Errorf("invalid package name or checksum")
	}
	err := d.withDraft(ctx, true, func(root *os.Root) error {
		cache, err := realSubdir(root, ".noosphere-deps")
		if err != nil {
			return err
		}
		defer cache.Close()
		dir, err := realSubdir(cache, digest)
		if err != nil {
			return err
		}
		defer dir.Close()
		tmp, err := tempName()
		if err != nil {
			return err
		}
		f, err := dir.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		defer dir.Remove(tmp)
		hasher := sha256.New()
		n, err := io.Copy(io.MultiWriter(f, hasher), ctxReader{ctx, io.LimitReader(r, MaxDependencyBytes+1)})
		if err == nil && (n < 1 || n > MaxDependencyBytes || hex.EncodeToString(hasher.Sum(nil)) != digest) {
			err = fmt.Errorf("package size or SHA-256 mismatch")
		}
		if err == nil {
			err = f.Sync()
		}
		err = errors.Join(err, f.Close())
		if err != nil {
			return err
		}
		if info, e := dir.Lstat(name); e == nil {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("linked package cache entry")
			}
		} else if !errors.Is(e, fs.ErrNotExist) {
			return e
		}
		if err = d.retainDependency(ctx, dir, tmp, name, digest); err != nil {
			return err
		}
		if err = dir.Rename(tmp, name); err != nil {
			return err
		}
		return syncRoot(dir)
	})
	if err != nil {
		return "", err
	}
	return ".noosphere-deps/" + digest + "/" + name, nil
}
