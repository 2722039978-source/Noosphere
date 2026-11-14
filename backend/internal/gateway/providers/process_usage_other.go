//go:build !windows

package providers

import (
	"context"
	"net"
)

func observeListenerUsage(context.Context, *net.TCPAddr) func() *ProcessUsage {
	return func() *ProcessUsage { return nil }
}
