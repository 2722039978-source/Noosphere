package secure

import (
	"context"
	"errors"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

type platformStore struct{}

func (platformStore) Provider() string { return "windows_credential_manager" }

type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
)

var (
	advapi32  = windows.NewLazySystemDLL("advapi32.dll")
	credRead  = advapi32.NewProc("CredReadW")
	credWrite = advapi32.NewProc("CredWriteW")
	credFree  = advapi32.NewProc("CredFree")
)

func (s platformStore) failure(err error) *StoreError {
	switch {
	case errors.Is(err, windows.ERROR_NOT_FOUND):
		return storeError(s, NotFound)
	case errors.Is(err, windows.ERROR_ACCESS_DENIED):
		return storeError(s, PermissionDenied)
	case errors.Is(err, windows.ERROR_ACCOUNT_LOCKED_OUT):
		return storeError(s, Locked)
	case errors.Is(err, windows.ERROR_INVALID_DATA):
		return storeError(s, Corrupt)
	default:
		return storeError(s, Unavailable)
	}
}

func (s platformStore) Load(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	target, _ := windows.UTF16PtrFromString(credTarget)
	var value *credential
	ok, _, err := credRead.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&value)))
	if ok == 0 {
		return nil, s.failure(err)
	}
	if value == nil {
		return nil, storeError(s, Corrupt)
	}
	defer credFree.Call(uintptr(unsafe.Pointer(value)))
	if value.CredentialBlobSize != keySize || value.CredentialBlob == nil {
		return nil, storeError(s, Corrupt)
	}
	return append([]byte(nil), unsafe.Slice(value.CredentialBlob, keySize)...), nil
}

func (s platformStore) Create(ctx context.Context, key []byte) error {
	if len(key) != keySize {
		return storeError(s, Corrupt)
	}
	// CredWrite is an upsert. Serialize the recheck and write across
	// Noosphere backends sharing this login session.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	name, _ := windows.UTF16PtrFromString(`Local\Noosphere-GatewayKey-Create`)
	mutex, err := windows.CreateMutex(nil, false, name)
	if mutex == 0 {
		return s.failure(err)
	}
	defer windows.CloseHandle(mutex)
	result, err := windows.WaitForSingleObject(mutex, 2000)
	if err != nil || (result != windows.WAIT_OBJECT_0 && result != windows.WAIT_ABANDONED) {
		return storeError(s, Unavailable)
	}
	defer windows.ReleaseMutex(mutex)
	existing, err := LoadKey(ctx, s)
	if err == nil {
		clear(existing)
		return storeError(s, alreadyExists)
	}
	var failure *StoreError
	if !errors.As(err, &failure) || failure.Status != NotFound {
		return err
	}
	target, _ := windows.UTF16PtrFromString(credTarget)
	value := credential{
		Type: credTypeGeneric, TargetName: target, CredentialBlobSize: keySize,
		CredentialBlob: &key[0], Persist: credPersistLocalMachine,
	}
	ok, _, err := credWrite.Call(uintptr(unsafe.Pointer(&value)), 0)
	runtime.KeepAlive(key)
	runtime.KeepAlive(target)
	if ok == 0 {
		return s.failure(err)
	}
	return nil
}
