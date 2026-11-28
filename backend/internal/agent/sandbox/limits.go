package sandbox

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

type Limits struct {
	ProjectRoot string
	Timeout     time.Duration
	DiskBytes   int64
}

func scopedDiskUsage(ctx context.Context, work string, limit int64) error {
	var size int64
	entries := 0
	return filepath.WalkDir(work, func(path string, d fs.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if e := ctx.Err(); e != nil {
			return e
		}
		entries++
		if entries > 250000 {
			return errors.New("命令工作区文件数量已达上限")
		}
		if d.Type().IsRegular() {
			info, e := d.Info()
			if os.IsNotExist(e) {
				return nil
			}
			if e != nil {
				return e
			}
			size += info.Size()
			if size > limit {
				return errors.New("命令工作区达到本轮磁盘预算")
			}
		}
		return nil
	})
}

// Monitor untrusted command output between the initial and final checks. This bounds
// ordinary jobs without requiring administrator-level filesystem quotas.
func monitorDisk(ctx context.Context, cancel context.CancelCauseFunc, work string, bytes int64) chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(500 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if e := scopedDiskUsage(ctx, work, bytes); e != nil {
					cancel(e)
					return
				}
			}
		}
	}()
	return done
}
