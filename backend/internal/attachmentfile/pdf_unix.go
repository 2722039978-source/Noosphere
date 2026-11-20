//go:build linux || darwin

package attachmentfile

import "golang.org/x/sys/unix"

func boundPDFProcess() error {
	// A request deadline bounds wall time. This kernel limit independently
	// bounds CPU consumption. GOMEMLIMIT is a Go GC target, not a hard cap.
	return unix.Setrlimit(unix.RLIMIT_CPU, &unix.Rlimit{Cur: 45, Max: 45})
}
