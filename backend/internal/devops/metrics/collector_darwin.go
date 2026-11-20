package metrics

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	cpuSource     = "unavailable"
	memorySource  = "macos.hw.memsize_vm_stat_free_inactive"
	processSource = "macos.kern.proc.all"
)

func readCPUTimes() (uint64, uint64, bool) { return 0, 0, false }

func readMemory() (MemoryMetrics, bool) {
	total, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return MemoryMetrics{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/vm_stat")
	cmd.Env = []string{"LANG=C", "LC_ALL=C"}
	out, err := cmd.Output()
	if err != nil {
		return MemoryMetrics{}, false
	}
	var free, inactive uint64
	foundFree, foundInactive := false, false
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		pages, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(value), "."), 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "Pages free":
			free, foundFree = pages, true
		case "Pages inactive":
			inactive, foundInactive = pages, true
		}
	}
	// Estimate reclaimable physical memory. Never treat it as GPU memory.
	pageSize := uint64(os.Getpagesize())
	if !foundFree || !foundInactive || free > total/pageSize || inactive > total/pageSize-free {
		return MemoryMetrics{}, false
	}
	return memoryFromBytes(total, (free+inactive)*pageSize)
}

func readProcessCount() (int, bool) {
	entries, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	return len(entries), err == nil && len(entries) > 0
}
