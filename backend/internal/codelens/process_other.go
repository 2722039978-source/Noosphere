//go:build !windows

package codelens

import "os/exec"

func configureWorker(cmd *exec.Cmd) {}
