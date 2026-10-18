package codelens

import (
	"os/exec"
	"syscall"
)

func configureWorker(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
