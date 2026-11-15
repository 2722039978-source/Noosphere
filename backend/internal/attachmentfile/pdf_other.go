//go:build !windows

package attachmentfile

import "os/exec"

func hidePDFWindow(*exec.Cmd) {}
func boundPDFProcess() error  { return nil }
