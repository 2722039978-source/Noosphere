//go:build !windows

package config

import (
	"os"

	"golang.org/x/sys/unix"
)

func lockDataFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}
