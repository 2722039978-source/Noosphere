package hostinfo

import (
	"bufio"
	"context"
	"os"
	"runtime"
	"strings"
)

func trimmedFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil || len(data) > maxOutputBytes {
		return ""
	}
	return clean(string(data))
}

func platformProbe(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	v := baseInventory("linux")
	v.System = Device{Manufacturer: trimmedFile("/sys/class/dmi/id/sys_vendor"), Model: trimmedFile("/sys/class/dmi/id/product_name"), Source: "linux_sysfs_dmi"}
	v.Motherboard = Device{Manufacturer: trimmedFile("/sys/class/dmi/id/board_vendor"), Model: trimmedFile("/sys/class/dmi/id/board_name"), Source: "linux_sysfs_dmi"}
	for _, path := range []string{"/etc/os-release", "/usr/lib/os-release"} {
		if file, err := os.Open(path); err == nil {
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				key, value, ok := strings.Cut(scanner.Text(), "=")
				if !ok {
					continue
				}
				switch key {
				case "NAME":
					v.OS.Name = clean(strings.Trim(value, `"'`))
				case "VERSION_ID":
					v.OS.Version = clean(strings.Trim(value, `"'`))
				}
			}
			file.Close()
			break
		}
	}
	v.OS.Build = trimmedFile("/proc/sys/kernel/osrelease")
	v.OS.Source = "linux_os_release_procfs"
	if file, err := os.Open("/proc/cpuinfo"); err == nil {
		cpu := CPU{LogicalCores: runtime.NumCPU(), Source: "linux_proc_cpuinfo"}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				break
			}
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			switch strings.TrimSpace(key) {
			case "model name":
				cpu.Model = clean(value)
			case "vendor_id":
				cpu.Manufacturer = clean(value)
			}
		}
		file.Close()
		v.CPUs = []CPU{cpu}
	}
	return portableInventory(v)
}
