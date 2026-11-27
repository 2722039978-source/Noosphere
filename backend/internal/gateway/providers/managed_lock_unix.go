//go:build linux || darwin

package providers

import (
	"golang.org/x/sys/unix"
	"os"
)

func lockManagedRequest(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_SH|unix.LOCK_NB) }

// The retained supervisor leads the engine process group. Its ID remains
// reserved until desktop cleanup, even after the engine child has been reaped.
func managedGroup(pid uint32) uint32 {
	group, err := unix.Getpgid(int(pid))
	if err != nil || group <= 0 {
		return 0
	}
	return uint32(group)
}
