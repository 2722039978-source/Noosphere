//go:build !windows

package evalrunner

import (
	"fmt"
	"os/exec"
)

type compilerProcess struct{}

// Fail closed until equivalent containment is proved on another platform.
func prepareCompiler(*exec.Cmd) (*compilerProcess, error) {
	return nil, fmt.Errorf("contained Go compilation is supported only on Windows")
}
func (*compilerProcess) contain(*exec.Cmd) error { return fmt.Errorf("unsupported platform") }
func (*compilerProcess) close()                  {}
