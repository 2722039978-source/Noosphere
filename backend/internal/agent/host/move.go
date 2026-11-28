package host

import (
	"context"
	"fmt"
	"os"
)

// Move renames a verified file in the private draft. Draft tools hold the same
// project lock and native commands must have exited before the next tool runs.
func (d *Draft) Move(ctx context.Context, oldPath, newPath, expectedHash string) error {
	if oldPath == newPath || !eligible(oldPath) || !eligible(newPath) || !digestRE.MatchString(expectedHash) {
		return fmt.Errorf("invalid move")
	}
	return d.withDraft(ctx, true, func(r *os.Root) error {
		src, err := inspect(ctx, r, oldPath, nil)
		if err != nil {
			return err
		}
		if src.Hash != expectedHash {
			return fmt.Errorf("source revision changed: %s", oldPath)
		}
		dest, err := inspect(ctx, r, newPath, nil)
		if err != nil {
			return err
		}
		if dest.Hash != "" {
			return fmt.Errorf("destination already exists: %s", newPath)
		}
		if err = makeParents(r, newPath); err != nil {
			return err
		}
		p, err := parentAt(r, newPath)
		if err != nil {
			return err
		}
		p.Close()
		return r.Rename(oldPath, newPath)
	})
}
