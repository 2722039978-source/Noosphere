//go:build !windows && !linux && !darwin

package attachmentfile

import "fmt"

func boundPDFProcess() error { return fmt.Errorf("PDF worker limits are unavailable on this platform") }
