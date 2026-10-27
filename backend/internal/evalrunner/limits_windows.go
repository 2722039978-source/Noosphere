package evalrunner

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// CompilerMemoryLimitBytes bounds aggregate committed memory of the whole
// compiler process tree. Wasm linear memory has a separate limit.
const CompilerMemoryLimitBytes = 2 << 30

type compilerProcess struct{ job windows.Handle }

func prepareCompiler(cmd *exec.Cmd) (*compilerProcess, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_JOB_MEMORY
	info.JobMemoryLimit = CompilerMemoryLimitBytes
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_SUSPENDED}
	cmd.Cancel = func() error { windows.TerminateJobObject(job, 1); return cmd.Process.Kill() }
	return &compilerProcess{job: job}, nil
}
func (p *compilerProcess) contain(cmd *exec.Cmd) error {
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	if err = windows.AssignProcessToJobObject(p.job, handle); err != nil {
		return err
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != uint32(cmd.Process.Pid) {
			continue
		}
		thread, e := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if e != nil {
			return e
		}
		previous, e := windows.ResumeThread(thread)
		windows.CloseHandle(thread)
		if e != nil {
			return e
		}
		if previous != 1 {
			return fmt.Errorf("unexpected compiler thread suspend count %d", previous)
		}
		return nil
	}
	return fmt.Errorf("suspended compiler thread not found: %w", err)
}
func (p *compilerProcess) close() { windows.TerminateJobObject(p.job, 1); windows.CloseHandle(p.job) }
