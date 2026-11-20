//go:build linux || darwin

package ownedprocess

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

const supervisorFlag = "--noosphere-owned-process"

type processResult struct {
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error,omitempty"`
}

func startProcess(cmd *exec.Cmd, directory *os.File) (func() error, func(), error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, nil, err
	}
	control, owner, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	defer control.Close()
	status, result, err := os.Pipe()
	if err != nil {
		owner.Close()
		return nil, nil, err
	}
	defer result.Close()

	var input, feed *os.File
	if cmd.Stdin != nil {
		input, feed, err = os.Pipe()
		if err != nil {
			owner.Close()
			status.Close()
			return nil, nil, err
		}
		defer input.Close()
	}
	stop := func() {
		_ = owner.Close()
		if feed != nil {
			_ = feed.Close()
		}
	}
	args := []string{supervisorFlag, strconv.FormatBool(directory != nil), strconv.FormatBool(input != nil), cmd.Path}
	wrapper := exec.Command(executable, append(args, cmd.Args[1:]...)...)
	wrapper.Stdin = control
	wrapper.Stdout = cmd.Stdout
	wrapper.Stderr = cmd.Stderr
	wrapper.Env = cmd.Env
	wrapper.WaitDelay = cmd.WaitDelay
	wrapper.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	wrapper.ExtraFiles = []*os.File{result, directory, input}
	if directory == nil {
		wrapper.Dir = cmd.Dir
	}
	if err := wrapper.Start(); err != nil {
		stop()
		status.Close()
		return nil, nil, err
	}
	if _, err := owner.Write([]byte{1}); err != nil {
		stop()
		status.Close()
		_ = wrapper.Wait()
		return nil, nil, err
	}
	if feed != nil {
		go func() {
			defer feed.Close()
			_, _ = io.Copy(feed, cmd.Stdin)
		}()
	}

	return func() error {
		defer status.Close()
		var result processResult
		// The supervisor writes its result, then terminates its own process
		// group to close descendant-owned descriptors before returning.
		decodeErr := json.NewDecoder(io.LimitReader(status, 8192)).Decode(&result)
		waitErr := wrapper.Wait()
		if decodeErr != nil {
			return fmt.Errorf("owned process ended before reporting a result: %w", errors.Join(decodeErr, waitErr))
		}
		if result.Error != "" {
			return fmt.Errorf("owned process: %s", result.Error)
		}
		if result.ExitCode != 0 {
			return fmt.Errorf("exit status %d", result.ExitCode)
		}
		return nil
	}, stop, nil
}

// RunSupervisor enters the internal Unix process-group supervisor when selected
// by the owning backend. It exits on the owner's control-pipe EOF, and never
// sends a signal to a process group that it does not itself lead.
func RunSupervisor() {
	if len(os.Args) < 2 || os.Args[1] != supervisorFlag {
		return
	}
	group, err := unix.Getpgid(0)
	if len(os.Args) < 5 || err != nil || group != os.Getpid() {
		os.Exit(125)
	}
	result := os.NewFile(3, "owned-process-result")
	unix.CloseOnExec(3)
	defer func() {
		// The leader is still alive here, so its group ID cannot be recycled.
		_ = unix.Kill(0, unix.SIGKILL)
		os.Exit(125)
	}()

	var start [1]byte
	if _, err := io.ReadFull(os.Stdin, start[:]); err != nil || start[0] != 1 {
		return
	}
	go func() {
		_, _ = io.Copy(io.Discard, os.Stdin)
		_ = unix.Kill(0, unix.SIGKILL)
	}()
	writeResult := func(r processResult) { _ = json.NewEncoder(result).Encode(r) }
	if os.Args[2] == "true" {
		err := unix.Fchdir(4)
		_ = unix.Close(4)
		if err != nil {
			writeResult(processResult{Error: err.Error()})
			return
		}
	}
	cmd := exec.Command(os.Args[4], os.Args[5:]...)
	if os.Args[3] == "true" {
		cmd.Stdin = os.NewFile(5, "owned-process-input")
		unix.CloseOnExec(5)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if exit, ok := err.(*exec.ExitError); ok {
		writeResult(processResult{ExitCode: exit.ExitCode()})
	} else if err != nil {
		writeResult(processResult{Error: err.Error()})
	} else {
		writeResult(processResult{})
	}
}
