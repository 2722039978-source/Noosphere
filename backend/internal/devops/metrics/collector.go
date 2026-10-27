package metrics

import (
	"os"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

type SystemSnapshot struct {
	// Samples retain explicit units and host-only attribution. Legacy display
	// fields stay compatible; evaluation callers should consume these samples.
	Samples    map[string]Sample `json:"samples,omitempty"`
	CPU        CPUMetrics        `json:"cpu"`
	Memory     MemoryMetrics     `json:"memory"`
	Disk       DiskMetrics       `json:"disk"`
	Processes  int               `json:"process_count"`
	GoRoutines int               `json:"goroutines"`
	Timestamp  string            `json:"timestamp"`
}

type Sample struct {
	Value         *float64 `json:"value"`
	Unit          string   `json:"unit"`
	Source        string   `json:"source"`
	Quality       string   `json:"quality"`
	MissingReason string   `json:"missing_reason,omitempty"`
}

type CPUMetrics struct {
	UsagePercent float64 `json:"usage_percent"`
	Cores        int     `json:"cores"`
}

type MemoryMetrics struct {
	TotalGB      float64 `json:"total_gb"`
	UsedGB       float64 `json:"used_gb"`
	UsagePercent float64 `json:"usage_percent"`
}

type DiskMetrics struct {
	TotalGB      float64 `json:"total_gb"`
	FreeGB       float64 `json:"free_gb"`
	UsagePercent float64 `json:"usage_percent"`
}

type Collector struct {
	mu          sync.Mutex
	cpuKnown    bool
	memoryKnown bool
	lastIdle    uint64
	lastKernel  uint64
	lastUser    uint64
	lastTime    time.Time
}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Snapshot() (*SystemSnapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cpuKnown = false
	c.memoryKnown = false
	s := &SystemSnapshot{
		GoRoutines: runtime.NumGoroutine(),
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	}

	s.CPU = c.collectCPU()
	s.Memory = c.collectMemory()
	s.Disk = c.collectDisk()
	s.Processes = c.countProcesses()
	s.Samples = map[string]Sample{
		"cpu_percent":          {Unit: "percent", Source: "windows.GetSystemTimes", Quality: "unknown", MissingReason: "two successful CPU samples with positive elapsed counters required"},
		"ram_used_bytes":       {Unit: "bytes", Source: "windows.GlobalMemoryStatusEx", Quality: "unknown", MissingReason: "physical memory sample unavailable"},
		"ram_total_bytes":      {Unit: "bytes", Source: "windows.GlobalMemoryStatusEx", Quality: "unknown", MissingReason: "physical memory sample unavailable"},
		"available_vram_bytes": {Unit: "bytes", Source: "unavailable", Quality: "unknown", MissingReason: "physical GPU free-memory collector unavailable"},
	}
	set := func(name string, value float64) {
		v := s.Samples[name]
		v.Value = &value
		v.Quality = "host_snapshot"
		v.MissingReason = ""
		s.Samples[name] = v
	}
	if c.cpuKnown {
		set("cpu_percent", s.CPU.UsagePercent)
	}
	if c.memoryKnown {
		set("ram_used_bytes", s.Memory.UsedGB*(1024*1024*1024))
		set("ram_total_bytes", s.Memory.TotalGB*(1024*1024*1024))
	}

	return s, nil
}

func (c *Collector) collectCPU() CPUMetrics {
	cpu := CPUMetrics{Cores: runtime.NumCPU()}

	var idle, kernel, user syscall.Filetime
	k32 := syscall.NewLazyDLL("kernel32.dll")
	proc := k32.NewProc("GetSystemTimes")
	r, _, _ := proc.Call(uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	if r == 0 {
		return cpu
	}

	idleVal := uint64(idle.HighDateTime)<<32 | uint64(idle.LowDateTime)
	kernelVal := uint64(kernel.HighDateTime)<<32 | uint64(kernel.LowDateTime)
	userVal := uint64(user.HighDateTime)<<32 | uint64(user.LowDateTime)

	total := kernelVal + userVal
	now := time.Now()

	if c.lastTime.IsZero() {
		c.lastIdle = idleVal
		c.lastKernel = kernelVal
		c.lastUser = userVal
		c.lastTime = now
		return cpu
	}

	deltaIdle := idleVal - c.lastIdle
	deltaTotal := (kernelVal + userVal) - (c.lastKernel + c.lastUser)

	if deltaTotal > 0 && deltaIdle <= deltaTotal {
		c.cpuKnown = true
		cpu.UsagePercent = float64(deltaTotal-deltaIdle) / float64(deltaTotal) * 100
		if cpu.UsagePercent > 100 {
			cpu.UsagePercent = 100
		}
		if cpu.UsagePercent < 0 {
			cpu.UsagePercent = 0
		}
	}

	c.lastIdle = idleVal
	c.lastKernel = kernelVal
	c.lastUser = userVal
	c.lastTime = now

	_ = total
	return cpu
}

func (c *Collector) collectMemory() MemoryMetrics {
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

	k32 := syscall.NewLazyDLL("kernel32.dll")
	proc := k32.NewProc("GlobalMemoryStatusEx")
	r, _, _ := proc.Call(uintptr(unsafe.Pointer(&status)))
	if r == 0 {
		return mem
	}
	c.memoryKnown = status.TotalPhys > 0 && status.AvailPhys <= status.TotalPhys
	if !c.memoryKnown {
		return mem
	}

	mem.TotalGB = float64(status.TotalPhys) / (1024 * 1024 * 1024)
	used := status.TotalPhys - status.AvailPhys
	mem.UsedGB = float64(used) / (1024 * 1024 * 1024)
	if status.TotalPhys > 0 {
		mem.UsagePercent = float64(used) / float64(status.TotalPhys) * 100
	}
	return mem
}

func (c *Collector) collectDisk() DiskMetrics {
	disk := DiskMetrics{}
	wd, _ := os.Getwd()
	if len(wd) >= 2 && wd[1] == ':' {
		path := wd[:2] + "\\"
		var free, total, totalFree uint64
		k32 := syscall.NewLazyDLL("kernel32.dll")
		proc := k32.NewProc("GetDiskFreeSpaceExW")
		p, _ := syscall.UTF16PtrFromString(path)
		r, _, _ := proc.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&free)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&totalFree)))
		if r != 0 {
			disk.TotalGB = float64(total) / (1024 * 1024 * 1024)
			disk.FreeGB = float64(free) / (1024 * 1024 * 1024)
			if total > 0 {
				disk.UsagePercent = float64(total-free) / float64(total) * 100
			}
		}
	}
	return disk
}

func (c *Collector) countProcesses() int {
	var count int
	k32 := syscall.NewLazyDLL("kernel32.dll")
	createSnapshot := k32.NewProc("CreateToolhelp32Snapshot")
	procFirst := k32.NewProc("Process32FirstW")
	procNext := k32.NewProc("Process32NextW")

	const th32csSnapProcess = 2
	handle, _, _ := createSnapshot.Call(th32csSnapProcess, 0)
	if handle == 0 || handle == uintptr(^uintptr(0)) {
		return 0
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
	return count
}
