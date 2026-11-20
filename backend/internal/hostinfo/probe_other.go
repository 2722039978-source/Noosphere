//go:build !windows && !linux && !darwin

package hostinfo

import "context"

func decodePlatformInventory([]byte) (Inventory, error) { return Inventory{}, errUnsupported }

func platformProbe(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, errUnsupported
}
