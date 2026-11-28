package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func checkToolLocation(exe, project string) error {
	info, err := os.Stat(exe)
	if err != nil {
		return err
	}
	for _, name := range []string{"NOOSPHERE_CODELENS_PYTHON", "NOOSPHERE_REA_NODE"} {
		if private, err := os.Stat(os.Getenv(name)); err == nil && os.SameFile(private, info) {
			return errors.New("该工具链是应用内部运行时")
		}
	}
	if project != "" {
		resolved, err := filepath.EvalSymlinks(exe)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(project, resolved)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return errors.New("请使用工作区之外安装的工具链")
		}
	}
	return nil
}

// The primary process can finish before its grandchildren. Poll the Job's
// active-process count before a subsequent tool observes the draft.
func waitForJobExit(job windows.Handle) error {
	var accounting struct {
		TotalUserTime, TotalKernelTime, PeriodUserTime, PeriodKernelTime int64
		PageFaults, TotalProcesses, ActiveProcesses, TerminatedProcesses uint32
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := windows.QueryInformationJobObject(job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&accounting)), uint32(unsafe.Sizeof(accounting)), nil); err != nil {
			return err
		}
		if accounting.ActiveProcesses == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("进程树尚未退出")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
