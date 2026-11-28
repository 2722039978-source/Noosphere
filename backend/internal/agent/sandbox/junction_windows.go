package sandbox

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

const (
	ioReparseTagMountPoint = 0xA0000003
	fsctlSetReparsePoint   = 0x000900A4
)

// createDirectoryJunction creates an NTFS directory junction directly via
// DeviceIoControl(FSCTL_SET_REPARSE_POINT) without spawning cmd.exe or any shell.
func createDirectoryJunction(linkPath, targetDir string) error {
	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		return err
	}
	absTarget = filepath.Clean(absTarget)
	if err := os.Mkdir(linkPath, 0700); err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(linkPath)
		}
	}()

	linkPtr, err := windows.UTF16PtrFromString(linkPath)
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(
		linkPtr,
		windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)

	subName := `\??\` + absTarget
	subUTF16 := utf16.Encode([]rune(subName))
	printUTF16 := utf16.Encode([]rune(absTarget))

	subByteLen := len(subUTF16) * 2
	printByteLen := len(printUTF16) * 2
	// PathBuffer layout: subUTF16 + NUL + printUTF16 + NUL
	pathBufLen := subByteLen + 2 + printByteLen + 2
	reparseDataLen := 8 + pathBufLen
	if reparseDataLen > 16384-8 {
		return fmt.Errorf("junction target path too long")
	}

	buf := make([]byte, 8+reparseDataLen)
	binary.LittleEndian.PutUint32(buf[0:4], ioReparseTagMountPoint)
	binary.LittleEndian.PutUint16(buf[4:6], uint16(reparseDataLen))
	binary.LittleEndian.PutUint16(buf[6:8], 0) // Reserved
	binary.LittleEndian.PutUint16(buf[8:10], 0)
	binary.LittleEndian.PutUint16(buf[10:12], uint16(subByteLen))
	binary.LittleEndian.PutUint16(buf[12:14], uint16(subByteLen+2))
	binary.LittleEndian.PutUint16(buf[14:16], uint16(printByteLen))

	pos := 16
	for _, u := range subUTF16 {
		binary.LittleEndian.PutUint16(buf[pos:pos+2], u)
		pos += 2
	}
	pos += 2 // NUL terminator
	for _, u := range printUTF16 {
		binary.LittleEndian.PutUint16(buf[pos:pos+2], u)
		pos += 2
	}

	var returned uint32
	if err := windows.DeviceIoControl(
		handle,
		fsctlSetReparsePoint,
		&buf[0],
		uint32(len(buf)),
		nil,
		0,
		&returned,
		nil,
	); err != nil {
		return err
	}
	cleanup = false
	return nil
}
