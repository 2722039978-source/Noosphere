package ownedprocess

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func RunSupervisor() {}

func startProcess(cmd *exec.Cmd, directory *os.File) (func() error, func(), error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, nil, err
	}
	stop := func() { _ = windows.CloseHandle(job) }
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		stop()
		return nil, nil, err
	}
	// Join the kill-on-close job before any project or analysis code can run.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_SUSPENDED | windows.CREATE_NO_WINDOW}
	if directory != nil {
		live, liveErr := os.Lstat(cmd.Dir)
		approved, approvedErr := directory.Stat()
		if liveErr != nil || approvedErr != nil || !live.IsDir() || !os.SameFile(live, approved) {
			stop()
			return nil, nil, fmt.Errorf("批准的项目目录已变化")
		}
	}
	if err := cmd.Start(); err != nil {
		stop()
		return nil, nil, err
	}
	if err := resumeInJob(job, uint32(cmd.Process.Pid)); err != nil {
		_ = cmd.Process.Kill()
		stop()
		_ = cmd.Wait()
		return nil, nil, err
	}
	return cmd.Wait, stop, nil
}

func resumeInJob(job windows.Handle, pid uint32) error {
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	if err = windows.AssignProcessToJobObject(job, handle); err != nil {
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
		if entry.OwnerProcessID != pid {
			continue
		}
		thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if err != nil {
			return err
		}
		defer windows.CloseHandle(thread)
		_, err = windows.ResumeThread(thread)
		return err
	}
	return fmt.Errorf("无法启动受管理的子进程")
}
