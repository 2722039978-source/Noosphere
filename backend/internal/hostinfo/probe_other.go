//go:build !windows

package hostinfo

import "context"

func platformProbe(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, errUnsupported
}
