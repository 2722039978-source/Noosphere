package attachmentfile

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// The worker owns this job until it exits. Attach before examining any PDF bytes.
func boundPDFProcess() error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY
	info.ProcessMemoryLimit = 192 << 20
	_, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, windows.CurrentProcess())
	}
	if err != nil {
		_ = windows.CloseHandle(job)
	}
	return err
}
