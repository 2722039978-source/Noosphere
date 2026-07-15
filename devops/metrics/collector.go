// Package metrics — 系统指标采集模块
//
// 支持 Windows (WMI/PowerShell) 和 Linux (procfs/sysfs) 双平台。
// 采集 CPU、内存、磁盘、进程等运维关键指标。
package metrics

import (
	"fmt"
	"math"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/devops-agent/agent"
)

// Collector 指标采集器接口
type Collector interface {
	Collect() (*agent.MetricsSnapshot, error)
	CollectCPU() (*agent.CPUMetrics, error)
	CollectMemory() (*agent.MemMetrics, error)
	CollectDisk() (*agent.DiskMetrics, error)
	CollectProcesses(topN int) ([]agent.ProcInfo, error)
}

// SystemCollector 系统指标采集器（5秒缓存）
type SystemCollector struct {
	mu          sync.RWMutex
	lastCollect time.Time
	cache       *agent.MetricsSnapshot
	cacheTTL    time.Duration
}

// NewSystemCollector 创建采集器
func NewSystemCollector() *SystemCollector {
	return &SystemCollector{cacheTTL: 5 * time.Second}
}

// Collect 采集完整指标快照（带缓存）
func (c *SystemCollector) Collect() (*agent.MetricsSnapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cache != nil && time.Since(c.lastCollect) < c.cacheTTL {
		return c.cache, nil
	}

	cpu, _ := c.collectCPU()
	mem, _ := c.collectMemory()
	disk, _ := c.collectDisk()
	procs, _ := c.CollectProcesses(10)

	c.cache = &agent.MetricsSnapshot{
		Timestamp: time.Now(),
		CPU:       *cpu,
		Memory:    *mem,
		Disk:      *disk,
		Processes: procs,
	}
	c.lastCollect = time.Now()
	return c.cache, nil
}

// CollectCPU 采集 CPU 指标
func (c *SystemCollector) CollectCPU() (*agent.CPUMetrics, error) {
	return c.collectCPU()
}

func (c *SystemCollector) collectCPU() (*agent.CPUMetrics, error) {
	if runtime.GOOS == "windows" {
		return c.collectCPUWindows()
	}
	return c.collectCPULinux()
}

func (c *SystemCollector) collectCPUWindows() (*agent.CPUMetrics, error) {
	psCmd := `
$cpu = (Get-CimInstance Win32_Processor | Measure-Object -Property LoadPercentage -Average).Average
$cores = (Get-CimInstance Win32_ComputerSystem).NumberOfLogicalProcessors
Write-Host "$cpu|$cores"
`
	out, _ := exec.Command("powershell", "-NoProfile", "-Command", psCmd).CombinedOutput()
	parts := strings.Split(strings.TrimSpace(string(out)), "|")
	cpu := &agent.CPUMetrics{Cores: runtime.NumCPU()}
	if len(parts) >= 1 {
		if v, e := strconv.ParseFloat(parts[0], 64); e == nil {
			cpu.UsagePercent = math.Round(v*10) / 10
		}
	}
	if len(parts) >= 2 {
		if v, e := strconv.Atoi(parts[1]); e == nil {
			cpu.Cores = v
		}
	}
	return cpu, nil
}

func (c *SystemCollector) collectCPULinux() (*agent.CPUMetrics, error) {
	out, _ := exec.Command("sh", "-c", "grep 'cpu ' /proc/stat").CombinedOutput()
	fields := strings.Fields(string(out))
	cpu := &agent.CPUMetrics{Cores: runtime.NumCPU()}
	if len(fields) < 5 {
		return cpu, nil
	}
	var total, idle float64
	for i, f := range fields[1:] {
		v, _ := strconv.ParseFloat(f, 64)
		total += v
		if i == 3 {
			idle = v
		}
	}
	if total > 0 {
		cpu.UsagePercent = math.Round((1-idle/total)*1000) / 10
	}
	loadOut, _ := exec.Command("sh", "-c", "cat /proc/loadavg").CombinedOutput()
	loadFields := strings.Fields(string(loadOut))
	if len(loadFields) >= 3 {
		cpu.LoadAvg1, _ = strconv.ParseFloat(loadFields[0], 64)
		cpu.LoadAvg5, _ = strconv.ParseFloat(loadFields[1], 64)
		cpu.LoadAvg15, _ = strconv.ParseFloat(loadFields[2], 64)
	}
	return cpu, nil
}

// CollectMemory 采集内存指标
func (c *SystemCollector) CollectMemory() (*agent.MemMetrics, error) {
	return c.collectMemory()
}

func (c *SystemCollector) collectMemory() (*agent.MemMetrics, error) {
	if runtime.GOOS == "windows" {
		return c.collectMemoryWindows()
	}
	return c.collectMemoryLinux()
}

func (c *SystemCollector) collectMemoryWindows() (*agent.MemMetrics, error) {
	psCmd := `
$os = Get-CimInstance Win32_OperatingSystem
$total = [math]::Round($os.TotalVisibleMemorySize/1MB, 1)
$free  = [math]::Round($os.FreePhysicalMemory/1MB, 1)
$used  = [math]::Round($total - $free, 1)
$pct   = if($total -gt 0){[math]::Round(($used/$total)*100, 1)}else{0}
Write-Host "$total|$used|$free|$pct"
`
	out, _ := exec.Command("powershell", "-NoProfile", "-Command", psCmd).CombinedOutput()
	parts := strings.Split(strings.TrimSpace(string(out)), "|")
	mem := &agent.MemMetrics{}
	if len(parts) >= 1 {
		mem.TotalGB, _ = strconv.ParseFloat(parts[0], 64)
	}
	if len(parts) >= 2 {
		mem.UsedGB, _ = strconv.ParseFloat(parts[1], 64)
	}
	if len(parts) >= 3 {
		mem.AvailableGB, _ = strconv.ParseFloat(parts[2], 64)
	}
	if len(parts) >= 4 {
		mem.UsagePercent, _ = strconv.ParseFloat(parts[3], 64)
	}
	return mem, nil
}

func (c *SystemCollector) collectMemoryLinux() (*agent.MemMetrics, error) {
	out, _ := exec.Command("sh", "-c", "cat /proc/meminfo | head -5").CombinedOutput()
	mem := &agent.MemMetrics{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		val, _ := strconv.ParseFloat(fields[1], 64)
		gb := math.Round(val/1024/1024*10) / 10
		switch fields[0] {
		case "MemTotal:":
			mem.TotalGB = gb
		case "MemAvailable:":
			mem.AvailableGB = gb
		}
	}
	if mem.TotalGB > 0 {
		mem.UsedGB = math.Round((mem.TotalGB-mem.AvailableGB)*10) / 10
		mem.UsagePercent = math.Round(mem.UsedGB/mem.TotalGB*1000) / 10
	}
	return mem, nil
}

// CollectDisk 采集磁盘指标
func (c *SystemCollector) CollectDisk() (*agent.DiskMetrics, error) {
	return c.collectDisk()
}

func (c *SystemCollector) collectDisk() (*agent.DiskMetrics, error) {
	if runtime.GOOS == "windows" {
		return c.collectDiskWindows()
	}
	return c.collectDiskLinux()
}

func (c *SystemCollector) collectDiskWindows() (*agent.DiskMetrics, error) {
	psCmd := `
Get-CimInstance Win32_LogicalDisk -Filter "DriveType=3" | ForEach-Object {
	$total = [math]::Round($_.Size/1GB, 1)
	$free  = [math]::Round($_.FreeSpace/1GB, 1)
	$used  = [math]::Round($total - $free, 1)
	$pct   = if($total -gt 0){[math]::Round(($used/$total)*100, 1)}else{0}
	Write-Host "$($_.DeviceID)|$total|$used|$free|$pct"
}
`
	out, _ := exec.Command("powershell", "-NoProfile", "-Command", psCmd).CombinedOutput()
	disk := &agent.DiskMetrics{}
	var grandTotal, grandUsed float64
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 2 {
			continue
		}
		total, _ := strconv.ParseFloat(parts[1], 64)
		used, _ := strconv.ParseFloat(parts[2], 64)
		grandTotal += total
		grandUsed += used
		if len(parts) >= 5 {
			usage, _ := strconv.ParseFloat(parts[4], 64)
			disk.Partitions = append(disk.Partitions, agent.DiskPartition{
				MountPoint: parts[0], TotalGB: total, UsedGB: used, UsagePercent: usage,
			})
		}
	}
	disk.TotalGB = math.Round(grandTotal*10) / 10
	disk.UsedGB = math.Round(grandUsed*10) / 10
	if disk.TotalGB > 0 {
		disk.UsagePercent = math.Round(grandUsed/grandTotal*1000) / 10
	}
	return disk, nil
}

func (c *SystemCollector) collectDiskLinux() (*agent.DiskMetrics, error) {
	out, _ := exec.Command("df", "-BG", "--total").CombinedOutput()
	disk := &agent.DiskMetrics{}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "total") {
			fields := strings.Fields(line)
			if len(fields) >= 4 {
				disk.TotalGB, _ = strconv.ParseFloat(strings.TrimSuffix(fields[1], "G"), 64)
				disk.UsedGB, _ = strconv.ParseFloat(strings.TrimSuffix(fields[2], "G"), 64)
				if disk.TotalGB > 0 {
					disk.UsagePercent = math.Round(disk.UsedGB/disk.TotalGB*1000) / 10
				}
			}
		}
	}
	return disk, nil
}

// CollectProcesses 采集进程信息
func (c *SystemCollector) CollectProcesses(topN int) ([]agent.ProcInfo, error) {
	if topN <= 0 {
		topN = 10
	}
	if runtime.GOOS == "windows" {
		return c.collectProcessesWindows(topN)
	}
	return c.collectProcessesLinux(topN)
}

func (c *SystemCollector) collectProcessesWindows(topN int) ([]agent.ProcInfo, error) {
	psCmd := fmt.Sprintf(`
Get-Process | Sort-Object CPU -Descending | Select-Object -First %d | ForEach-Object {
	$mem = [math]::Round($_.WorkingSet64/1MB, 1)
	$uptime = ((Get-Date) - $_.StartTime).ToString()
	Write-Host ("{0}|{1}|{2}|{3}|{4}|{5}" -f $_.Id, $_.ProcessName, $_.CPU, $mem, $uptime, $_.Responding)
}
`, topN)
	out, _ := exec.Command("powershell", "-NoProfile", "-Command", psCmd).CombinedOutput()
	var procs []agent.ProcInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Split(strings.TrimSpace(line), "|")
		if len(parts) < 4 {
			continue
		}
		pid, _ := strconv.Atoi(parts[0])
		cpu, _ := strconv.ParseFloat(parts[2], 64)
		mem, _ := strconv.ParseFloat(parts[3], 64)
		status := "running"
		if len(parts) >= 6 && parts[5] == "False" {
			status = "not responding"
		}
		procs = append(procs, agent.ProcInfo{PID: pid, Name: parts[1], CPU: cpu, Memory: mem, Uptime: parts[4], Status: status})
	}
	return procs, nil
}

func (c *SystemCollector) collectProcessesLinux(topN int) ([]agent.ProcInfo, error) {
	out, _ := exec.Command("ps", "aux", "--sort=-%cpu").CombinedOutput()
	var procs []agent.ProcInfo
	for i, line := range strings.Split(string(out), "\n") {
		if i == 0 || line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 11 {
			continue
		}
		pid, _ := strconv.Atoi(fields[1])
		cpu, _ := strconv.ParseFloat(fields[2], 64)
		mem, _ := strconv.ParseFloat(fields[3], 64)
		procs = append(procs, agent.ProcInfo{PID: pid, Name: fields[10], CPU: cpu, Memory: mem, Status: "running"})
		if len(procs) >= topN {
			break
		}
	}
	return procs, nil
}

