//go:build !windows

package agent

import "os/exec"

func prepareActionProcess(cmd *exec.Cmd) (func(), error) { return func() {}, nil }
func containActionProcess(cmd *exec.Cmd) error           { return nil }
