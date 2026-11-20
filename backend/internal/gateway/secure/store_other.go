//go:build !windows && !linux && !darwin

package secure

import "context"

type platformStore struct{}

func (platformStore) Provider() string { return "unsupported" }

func (s platformStore) Load(context.Context) ([]byte, error) {
	return nil, storeError(s, Unavailable)
}

func (s platformStore) Create(context.Context, []byte) error {
	return storeError(s, Unavailable)
}
