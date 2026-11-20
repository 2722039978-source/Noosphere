package secure

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

const (
	credTarget = "Noosphere/GatewayKey"
	keySize    = 32
)

type Status string

const (
	NotFound         Status = "not_found"
	Locked           Status = "locked"
	PermissionDenied Status = "permission_denied"
	Unavailable      Status = "unavailable"
	Corrupt          Status = "corrupt"
	alreadyExists    Status = "already_exists"
)

type StoreError struct {
	Provider string
	Status   Status
}

func (e *StoreError) Error() string {
	return fmt.Sprintf("%s credential store: %s", e.Provider, e.Status)
}

// SecretStore holds the single installation master key. Creation is attempted
// only after a successful lookup proves the key is absent.
type SecretStore interface {
	Provider() string
	Load(context.Context) ([]byte, error)
	Create(context.Context, []byte) error
}

func SystemStore() SecretStore { return platformStore{} }

func storeError(store SecretStore, status Status) *StoreError {
	return &StoreError{Provider: store.Provider(), Status: status}
}

func LoadKey(ctx context.Context, store SecretStore) ([]byte, error) {
	key, err := store.Load(ctx)
	if err == nil && len(key) != keySize {
		return nil, storeError(store, Corrupt)
	}
	return key, err
}

var createMu sync.Mutex

func GetOrCreateKey(ctx context.Context, store SecretStore) ([]byte, error) {
	createMu.Lock()
	defer createMu.Unlock()
	key, err := LoadKey(ctx, store)
	if err == nil {
		return key, nil
	}
	var failure *StoreError
	if !errors.As(err, &failure) || failure.Status != NotFound {
		return nil, err
	}
	key, err = GenerateKey()
	if err != nil {
		return nil, err
	}
	err = store.Create(ctx, key)
	clear(key)
	if err != nil && (!errors.As(err, &failure) || failure.Status != alreadyExists) {
		return nil, err
	}
	return LoadKey(ctx, store)
}
