package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// LockWorkspaceData keeps the backend and offline backup from reading or
// writing store files at the same time. The OS releases the lock on exit.
// Leave the lock file in place so two processes cannot lock different files.
func LockWorkspaceData(dir string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, ".noosphere-data.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := lockDataFile(lock); err != nil {
		lock.Close()
		return nil, fmt.Errorf("cannot lock project data directory %s; close other Noosphere instances or check file access: %w", dir, err)
	}
	return lock, nil
}
