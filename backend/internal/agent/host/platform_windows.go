package host

import (
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"syscall"
)

const readFlags = 0

func identity(f *os.File) (string, uint64, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info); err != nil {
		return "", 0, err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return "", 0, fmt.Errorf("linked file or directory")
	}
	return fmt.Sprintf("%d:%d:%d", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow), uint64(info.NumberOfLinks), nil
}

func tryLock(f *os.File) (bool, error) {
	err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
	if errors.Is(err, syscall.Errno(windows.ERROR_LOCK_VIOLATION)) {
		return false, nil
	}
	return err == nil, err
}

func syncDirectory(_ *os.File) error { return nil }
