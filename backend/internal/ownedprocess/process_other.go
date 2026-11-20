//go:build !windows && !linux && !darwin

package ownedprocess

import (
	"fmt"
	"os"
	"os/exec"
)

func RunSupervisor() {}

func startProcess(*exec.Cmd, *os.File) (func() error, func(), error) {
	return nil, nil, fmt.Errorf("this platform has no owned process implementation")
}
