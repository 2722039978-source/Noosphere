package modelstore

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const MaxArtifactBytes = 8 << 20

func (s *Store) PublishArtifact(ctx context.Context, b []byte, m ArtifactMetadata) (Artifact, error) {
	var a Artifact
	if s.readOnly {
		return a, ErrReadOnly
	}
	if len(b) > MaxArtifactBytes || m.MediaType == "" {
		return a, fmt.Errorf("%w: artifact limit/media type", ErrInvalid)
	}
	switch m.Provenance {
	case "offline_real", "offline_fixture", "simulation_replay", "simulation_injected", "shadow_live", "canary_live":
	default:
		return a, ErrInvalid
	}
	a = Artifact{ID: "art_" + digest(b), SHA256: digest(b), SizeBytes: int64(len(b)), Metadata: m}
	f, e := os.CreateTemp(s.artifactDir, ".publish-*")
	if e != nil {
		return a, e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return a, e
	}
	target := filepath.Join(s.artifactDir, a.SHA256)
	if _, e = os.Stat(target); os.IsNotExist(e) {
		if e = os.Rename(tmp, target); e != nil {
			if _, readErr := readVerified(target, a.SHA256, a.SizeBytes); readErr != nil {
				return a, e
			}
		}
	} else if e != nil {
		return a, e
	}
	if _, e = readVerified(target, a.SHA256, a.SizeBytes); e != nil {
		return a, e
	}
	// File.Sync is mandatory. Directory fsync is supported on Unix, while Windows
	// refuses directory Sync; rename+file flush is the portable durability bound.
	if dir, err := os.Open(s.artifactDir); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	body, _, e := immutableBody(a)
	if e != nil {
		return a, e
	}
	e = s.write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, "INSERT INTO artifacts(id,sha256,relative_name,size_bytes,media_type,provenance,retention_until,body_json) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(sha256) DO NOTHING", a.ID, a.SHA256, a.SHA256, a.SizeBytes, m.MediaType, m.Provenance, null(m.RetentionUntil), string(body))
		if e != nil {
			return e
		}
		stored, err := s.verifyArtifact(ctx, tx, a.ID)
		if err != nil {
			return err
		}
		a = stored
		return nil
	})
	return a, e
}
func readVerified(path, hash string, size int64) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, fmt.Errorf("%w: unavailable artifact", ErrArtifactMissing)
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() != size || size > MaxArtifactBytes {
		return nil, fmt.Errorf("%w: size/type mismatch", ErrArtifactMissing)
	}
	b, e := io.ReadAll(io.LimitReader(f, MaxArtifactBytes+1))
	if e != nil || int64(len(b)) != size || digest(b) != hash {
		return nil, fmt.Errorf("%w: content hash mismatch", ErrArtifactMissing)
	}
	return b, nil
}
func (s *Store) verifyArtifact(ctx context.Context, q queryer, id string) (Artifact, error) {
	var a Artifact
	var name string
	var deleted sql.NullString
	err := q.QueryRowContext(ctx, "SELECT id,sha256,relative_name,size_bytes,media_type,provenance,coalesce(retention_until,''),deleted_at FROM artifacts WHERE id=?", id).Scan(&a.ID, &a.SHA256, &name, &a.SizeBytes, &a.Metadata.MediaType, &a.Metadata.Provenance, &a.Metadata.RetentionUntil, &deleted)
	if err != nil || deleted.Valid || !hashPattern.MatchString(a.SHA256) || name != a.SHA256 {
		return a, ErrArtifactMissing
	}
	_, err = readVerified(filepath.Join(s.artifactDir, a.SHA256), a.SHA256, a.SizeBytes)
	return a, err
}
func (s *Store) ReadArtifact(ctx context.Context, id string) ([]byte, error) {
	a, e := s.verifyArtifact(ctx, s.db, id)
	if e != nil {
		return nil, e
	}
	return readVerified(filepath.Join(s.artifactDir, a.SHA256), a.SHA256, a.SizeBytes)
}
