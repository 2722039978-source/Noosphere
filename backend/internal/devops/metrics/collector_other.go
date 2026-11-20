//go:build !windows && !linux && !darwin

package metrics

const (
	cpuSource     = "unavailable"
	memorySource  = "unavailable"
	processSource = "unavailable"
	diskSource    = "unavailable"
)

func readCPUTimes() (uint64, uint64, bool) { return 0, 0, false }
func readMemory() (MemoryMetrics, bool)    { return MemoryMetrics{}, false }
func readDisk() (DiskMetrics, bool)        { return DiskMetrics{}, false }
func readProcessCount() (int, bool)        { return 0, false }
