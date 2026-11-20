package metrics

import (
	"fmt"
	"runtime"
	"sync"
	"time"
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
	mu            sync.Mutex
	lastIdle      uint64
	lastTotal     uint64
	haveCPUSample bool
}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Snapshot() (*SystemSnapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := &SystemSnapshot{
		CPU:        CPUMetrics{Cores: runtime.NumCPU()},
		GoRoutines: runtime.NumGoroutine(),
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	}

	s.Samples = map[string]Sample{
		"cpu_percent":          {Unit: "percent", Source: cpuSource, Quality: "unknown", MissingReason: "two successful CPU samples with positive elapsed counters required"},
		"ram_used_bytes":       {Unit: "bytes", Source: memorySource, Quality: "unknown", MissingReason: "physical memory sample unavailable"},
		"ram_total_bytes":      {Unit: "bytes", Source: memorySource, Quality: "unknown", MissingReason: "physical memory sample unavailable"},
		"disk_used_percent":    {Unit: "percent", Source: diskSource, Quality: "unknown", MissingReason: "filesystem usage unavailable"},
		"process_count":        {Unit: "count", Source: processSource, Quality: "unknown", MissingReason: "host process count unavailable"},
		"available_vram_bytes": {Unit: "bytes", Source: "unavailable", Quality: "unknown", MissingReason: "physical GPU free-memory collector unavailable"},
	}
	set := func(name string, value float64) {
		v := s.Samples[name]
		v.Value = &value
		v.Quality = "host_snapshot"
		v.MissingReason = ""
		s.Samples[name] = v
	}
	if idle, total, ok := readCPUTimes(); ok {
		if c.haveCPUSample && total > c.lastTotal && idle >= c.lastIdle && idle-c.lastIdle <= total-c.lastTotal {
			s.CPU.UsagePercent = float64((total-c.lastTotal)-(idle-c.lastIdle)) / float64(total-c.lastTotal) * 100
			set("cpu_percent", s.CPU.UsagePercent)
		}
		c.lastIdle, c.lastTotal, c.haveCPUSample = idle, total, true
	} else {
		v := s.Samples["cpu_percent"]
		v.MissingReason = "host CPU counters unavailable on this platform or at sampling time"
		s.Samples["cpu_percent"] = v
	}
	memory, ok := readMemory()
	if ok {
		s.Memory = memory
		set("ram_used_bytes", s.Memory.UsedGB*(1024*1024*1024))
		set("ram_total_bytes", s.Memory.TotalGB*(1024*1024*1024))
	}
	if disk, ok := readDisk(); ok {
		s.Disk = disk
		set("disk_used_percent", s.Disk.UsagePercent)
	}
	if count, ok := readProcessCount(); ok {
		s.Processes = count
		set("process_count", float64(count))
	}

	return s, nil
}

func (s *SystemSnapshot) DisplaySample(name, format string) string {
	sample := s.Samples[name]
	if sample.Value == nil {
		return "unavailable"
	}
	return fmt.Sprintf(format, *sample.Value)
}
