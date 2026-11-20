//go:build linux || darwin

package metrics

import (
	"os"

	"golang.org/x/sys/unix"
)

const diskSource = "unix.statfs"

func memoryFromBytes(total, available uint64) (MemoryMetrics, bool) {
	if total == 0 || available > total {
		return MemoryMetrics{}, false
	}
	used := total - available
	const gib = 1 << 30
	return MemoryMetrics{
		TotalGB:      float64(total) / gib,
		UsedGB:       float64(used) / gib,
		UsagePercent: float64(used) / float64(total) * 100,
	}, true
}

func readDisk() (DiskMetrics, bool) {
	path, err := os.Getwd()
	if err != nil {
		return DiskMetrics{}, false
	}
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil || stat.Bsize <= 0 || stat.Blocks == 0 || stat.Bavail > stat.Blocks {
		return DiskMetrics{}, false
	}
	const gib = 1 << 30
	return DiskMetrics{
		TotalGB:      float64(stat.Blocks) * float64(stat.Bsize) / gib,
		FreeGB:       float64(stat.Bavail) * float64(stat.Bsize) / gib,
		UsagePercent: float64(stat.Blocks-stat.Bavail) / float64(stat.Blocks) * 100,
	}, true
}
