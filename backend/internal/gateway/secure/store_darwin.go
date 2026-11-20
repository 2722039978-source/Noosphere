package secure

import (
	"context"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

type platformStore struct{}

func (platformStore) Provider() string { return "macos_keychain" }

type keychainAPI struct {
	copyDefault func(*uintptr) int32
	status      func(uintptr, *uint32) int32
	interaction func(bool) int32
	find        func(uintptr, uint32, string, uint32, string, *uint32, **byte, uintptr) int32
	add         func(uintptr, uint32, string, uint32, string, uint32, *byte, uintptr) int32
	free        func(uintptr, *byte) int32
	release     func(uintptr)
}

var appleKeychain = sync.OnceValue(func() *keychainAPI {
	security, err := purego.Dlopen("/System/Library/Frameworks/Security.framework/Security", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil
	}
	foundation, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		purego.Dlclose(security)
		return nil
	}
	api := new(keychainAPI)
	for _, symbol := range []struct {
		lib  uintptr
		name string
		dest any
	}{
		{security, "SecKeychainCopyDefault", &api.copyDefault},
		{security, "SecKeychainGetStatus", &api.status},
		{security, "SecKeychainSetUserInteractionAllowed", &api.interaction},
		{security, "SecKeychainFindGenericPassword", &api.find},
		{security, "SecKeychainAddGenericPassword", &api.add},
		{security, "SecKeychainItemFreeContent", &api.free},
		{foundation, "CFRelease", &api.release},
	} {
		address, err := purego.Dlsym(symbol.lib, symbol.name)
		if err != nil {
			purego.Dlclose(foundation)
			purego.Dlclose(security)
			return nil
		}
		purego.RegisterFunc(symbol.dest, address)
	}
	// The background server never opens a modal unlock or ACL prompt.
	if api.interaction(false) != 0 {
		purego.Dlclose(foundation)
		purego.Dlclose(security)
		return nil
	}
	return api
})

func (s platformStore) failure(code int32) *StoreError {
	switch code {
	case -25300: // errSecItemNotFound
		return storeError(s, NotFound)
	case -25299: // errSecDuplicateItem
		return storeError(s, alreadyExists)
	case -61, -128, -25292, -25293, -25308, -25315, -25317, -34018, -34020:
		return storeError(s, PermissionDenied)
	case -25295, -25304, -25316, -26275:
		return storeError(s, Corrupt)
	default:
		return storeError(s, Unavailable)
	}
}

func (s platformStore) defaultKeychain(ctx context.Context, write bool) (*keychainAPI, uintptr, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	api := appleKeychain()
	if api == nil {
		return nil, 0, storeError(s, Unavailable)
	}
	var keychain uintptr
	if code := api.copyDefault(&keychain); code != 0 {
		return nil, 0, s.failure(code)
	}
	if keychain == 0 {
		return nil, 0, storeError(s, Unavailable)
	}
	var status uint32
	code := api.status(keychain, &status)
	var err error
	const unlocked, readable, writable = 1, 2, 4
	switch {
	case code != 0:
		err = s.failure(code)
	case status&unlocked == 0:
		err = storeError(s, Locked)
	case status&readable == 0 || (write && status&writable == 0):
		err = storeError(s, PermissionDenied)
	}
	if err != nil {
		api.release(keychain)
		return nil, 0, err
	}
	return api, keychain, nil
}

const keychainAccount = "gateway-master-key"

func (s platformStore) keychainFailure(api *keychainAPI, keychain uintptr, code int32) error {
	if code == -25308 || code == -25315 {
		var status uint32
		if api.status(keychain, &status) == 0 && status&1 == 0 {
			return storeError(s, Locked)
		}
	}
	return s.failure(code)
}

func (s platformStore) Load(ctx context.Context) ([]byte, error) {
	api, keychain, err := s.defaultKeychain(ctx, false)
	if err != nil {
		return nil, err
	}
	defer api.release(keychain)
	var size uint32
	var data *byte
	code := api.find(keychain, uint32(len(credTarget)), credTarget, uint32(len(keychainAccount)), keychainAccount, &size, &data, 0)
	if code != 0 {
		return nil, s.keychainFailure(api, keychain, code)
	}
	if data != nil {
		defer api.free(0, data)
	}
	if size != keySize || data == nil {
		return nil, storeError(s, Corrupt)
	}
	return append([]byte(nil), unsafe.Slice(data, keySize)...), nil
}

func (s platformStore) Create(ctx context.Context, key []byte) error {
	if len(key) != keySize {
		return storeError(s, Corrupt)
	}
	api, keychain, err := s.defaultKeychain(ctx, true)
	if err != nil {
		return err
	}
	defer api.release(keychain)
	code := api.add(keychain, uint32(len(credTarget)), credTarget, uint32(len(keychainAccount)), keychainAccount, keySize, &key[0], 0)
	if code != 0 {
		return s.keychainFailure(api, keychain, code)
	}
	return nil
}
