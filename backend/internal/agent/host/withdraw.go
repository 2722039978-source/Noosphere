package host

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
)

// Recover only effects proven to have landed. An unapplied item whose live
// target changed is left alone; it must never be applied merely to undo it.
func (d *Draft) preparePartial(ctx context.Context, r *os.Root, j *journal) error {
	for i := range j.Actions {
		op := &j.Actions[i]
		if op.Temp == "" {
			continue
		}
		p, err := parentAt(r, op.Change.Path)
		if err != nil {
			return err
		}
		if op.Done || op.Abandoned {
			err = d.cleanTemp(p, j, op)
			p.Close()
			if err != nil {
				return err
			}
			continue
		}
		stageID, se := fileIdentity(p, op.Temp)
		liveID, le := fileIdentity(p, path.Base(op.Change.Path))
		if se != nil && !errors.Is(se, fs.ErrNotExist) {
			p.Close()
			return se
		}
		if le != nil && !errors.Is(le, fs.ErrNotExist) {
			p.Close()
			return le
		}
		if stageID != "" && stageID != op.TempID {
			p.Close()
			return fmt.Errorf("withdrawal conflict: staged identity changed (%s)", op.Change.Path)
		}
		landed := false
		if op.After.Hash == "" {
			landed = stageID == op.TempID && liveID == ""
			if !landed && (stageID != "" || liveID != op.Before.ID) {
				p.Close()
				return fmt.Errorf("cannot verify pending deletion: %s", op.Change.Path)
			}
		} else {
			landed = liveID == op.TempID
			if !landed && stageID == "" {
				p.Close()
				return fmt.Errorf("cannot identify pending replacement: %s", op.Change.Path)
			}
		}
		if landed {
			if op.After.Hash != "" && stageID == liveID {
				// A create that landed immediately before unlinking its staging name.
				if err = p.Remove(op.Temp); err != nil {
					p.Close()
					return err
				}
			}
			var live version
			live, err = current(ctx, r, op)
			if err == nil && (!sameContent(live, op.After) || (live.Hash != "" && live.ID != op.TempID)) {
				err = fmt.Errorf("published version changed before withdrawal: %s", op.Change.Path)
			}
			if err == nil {
				op.Done = true
				op.Published = live
				err = d.save(j)
			}
			if err == nil {
				err = d.cleanTemp(p, j, op)
			}
			p.Close()
			if err != nil {
				return err
			}
		} else {
			op.Abandoned = true
			err = d.save(j)
			if err == nil {
				err = d.cleanTemp(p, j, op)
			}
			p.Close()
			if err != nil {
				return err
			}
		}
	}
	return nil
}
