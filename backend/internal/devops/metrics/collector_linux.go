package metrics

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

const (
	cpuSource     = "linux.proc.stat"
	memorySource  = "linux.proc.meminfo"
	processSource = "linux.proc.pid_directories"
)

func readCPUTimes() (uint64, uint64, bool) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return 0, 0, false
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 9 || fields[0] != "cpu" {
		return 0, 0, false
	}
	var total, idle uint64
	// guest and guest_nice are already counted by user and nice.
	for i, field := range fields[1:9] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return 0, 0, false
		}
		total += value
		if i == 3 || i == 4 {
			idle += value
		}
	}
	return idle, total, total > 0
}

func readMemory() (MemoryMetrics, bool) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return MemoryMetrics{}, false
	}
	defer f.Close()
	var total, available uint64
	haveAvailable := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 3 || fields[2] != "kB" {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total, _ = strconv.ParseUint(fields[1], 10, 64)
		case "MemAvailable:":
			available, err = strconv.ParseUint(fields[1], 10, 64)
			haveAvailable = err == nil
		}
	}
	if scanner.Err() != nil || !haveAvailable || available > total || total > ^uint64(0)/1024 {
		return MemoryMetrics{}, false
	}
	return memoryFromBytes(total*1024, available*1024)
}

func readProcessCount() (int, bool) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, false
	}
	count := 0
	for _, entry := range entries {
		pid, err := strconv.ParseUint(entry.Name(), 10, 32)
		if entry.IsDir() && err == nil && pid > 0 {
			count++
		}
	}
	return count, count > 0
}
