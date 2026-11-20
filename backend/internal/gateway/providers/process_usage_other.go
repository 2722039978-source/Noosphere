//go:build !windows

package providers

import (
	"context"
	"net"
	"runtime"
)

func observeListenerUsage(context.Context, *net.TCPAddr) func() *ProcessUsage {
	return func() *ProcessUsage {
		return &ProcessUsage{
			Source:        "unavailable",
			MissingReason: "listener process usage sampling is not available on " + runtime.GOOS,
		}
	}
}
