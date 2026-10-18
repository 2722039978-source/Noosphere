package agent

import (
	"fmt"
	"golang.org/x/sys/windows"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"
)

var actionJobs sync.Map

func prepareActionProcess(cmd *exec.Cmd) (func(), error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	// Join the kill-on-close job before project code can spawn any children.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_SUSPENDED}
	actionJobs.Store(cmd, job)
	cmd.Cancel = func() error { _ = windows.TerminateJobObject(job, 1); return cmd.Process.Kill() }
	return func() { actionJobs.Delete(cmd); windows.CloseHandle(job) }, nil
}
func containActionProcess(cmd *exec.Cmd) error {
	job, _ := actionJobs.Load(cmd)
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	if err = windows.AssignProcessToJobObject(job.(windows.Handle), handle); err != nil {
		return err
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{}
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID == uint32(cmd.Process.Pid) {
			thread, e := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
			if e != nil {
				return e
			}
			_, e = windows.ResumeThread(thread)
			windows.CloseHandle(thread)
			return e
		}
	}
	return fmt.Errorf("无法启动受管理的测试进程")
}
