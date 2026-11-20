package metrics

import (
	"os"
	"syscall"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")

const (
	cpuSource     = "windows.GetSystemTimes"
	memorySource  = "windows.GlobalMemoryStatusEx"
	diskSource    = "windows.GetDiskFreeSpaceExW"
	processSource = "windows.CreateToolhelp32Snapshot"
)

func readCPUTimes() (uint64, uint64, bool) {
	var idle, kernel, user syscall.Filetime
	proc := kernel32.NewProc("GetSystemTimes")
	r, _, _ := proc.Call(uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	if r == 0 {
		return 0, 0, false
	}
	val := func(t syscall.Filetime) uint64 { return uint64(t.HighDateTime)<<32 | uint64(t.LowDateTime) }
	return val(idle), val(kernel) + val(user), true
}
func readMemory() (MemoryMetrics, bool) {
	mem := MemoryMetrics{}

	var status struct {
		Length               uint32
		MemoryLoad           uint32
		TotalPhys            uint64
		AvailPhys            uint64
		TotalPageFile        uint64
		AvailPageFile        uint64
		TotalVirtual         uint64
		AvailVirtual         uint64
		AvailExtendedVirtual uint64
	}
	status.Length = uint32(unsafe.Sizeof(status))

	proc := kernel32.NewProc("GlobalMemoryStatusEx")
	r, _, _ := proc.Call(uintptr(unsafe.Pointer(&status)))
	if r == 0 {
		return mem, false
	}
	if status.TotalPhys == 0 || status.AvailPhys > status.TotalPhys {
		return mem, false
	}

	mem.TotalGB = float64(status.TotalPhys) / (1024 * 1024 * 1024)
	used := status.TotalPhys - status.AvailPhys
	mem.UsedGB = float64(used) / (1024 * 1024 * 1024)
	if status.TotalPhys > 0 {
		mem.UsagePercent = float64(used) / float64(status.TotalPhys) * 100
	}
	return mem, true
}

func readDisk() (DiskMetrics, bool) {
	disk := DiskMetrics{}
	wd, _ := os.Getwd()
	if len(wd) >= 2 && wd[1] == ':' {
		path := wd[:2] + "\\"
		var free, total, totalFree uint64

		proc := kernel32.NewProc("GetDiskFreeSpaceExW")
		p, _ := syscall.UTF16PtrFromString(path)
		r, _, _ := proc.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&free)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&totalFree)))
		if r != 0 && total > 0 && free <= total {
			disk.TotalGB = float64(total) / (1024 * 1024 * 1024)
			disk.FreeGB = float64(free) / (1024 * 1024 * 1024)
			disk.UsagePercent = float64(total-free) / float64(total) * 100
			return disk, true
		}
	}
	return disk, false
}

func readProcessCount() (int, bool) {
	var count int

	createSnapshot := kernel32.NewProc("CreateToolhelp32Snapshot")
	procFirst := kernel32.NewProc("Process32FirstW")
	procNext := kernel32.NewProc("Process32NextW")

	const th32csSnapProcess = 2
	handle, _, _ := createSnapshot.Call(th32csSnapProcess, 0)
	if handle == 0 || handle == uintptr(^uintptr(0)) {
		return 0, false
	}
	defer syscall.CloseHandle(syscall.Handle(handle))

	var entry struct {
		Size              uint32
		CntUsage          uint32
		ProcessID         uint32
		DefaultHeapID     uintptr
		ModuleID          uint32
		CntThreads        uint32
		ParentProcessID   uint32
		PriorityClassBase int32
		Flags             uint32
		ExeFile           [260]uint16
	}
	entry.Size = uint32(unsafe.Sizeof(entry))

	r, _, _ := procFirst.Call(handle, uintptr(unsafe.Pointer(&entry)))
	for r != 0 {
		count++
		entry.Size = uint32(unsafe.Sizeof(entry))
		r, _, _ = procNext.Call(handle, uintptr(unsafe.Pointer(&entry)))
	}
	return count, count > 0
}
