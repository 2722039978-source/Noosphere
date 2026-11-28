//go:build linux || darwin

package host

import (
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
)

const readFlags = unix.O_NONBLOCK | unix.O_NOFOLLOW

func identity(f *os.File) (string, uint64, error) {
	var st unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &st); err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("%d:%d", st.Dev, st.Ino), uint64(st.Nlink), nil
}

func tryLock(f *os.File) (bool, error) {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return false, nil
	}
	return err == nil, err
}

func syncDirectory(f *os.File) error { return f.Sync() }
