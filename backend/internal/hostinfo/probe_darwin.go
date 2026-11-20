package hostinfo

import (
	"context"
	"runtime"

	"golang.org/x/sys/unix"
)

func platformProbe(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	v := baseInventory("macos")
	read := func(name string) string {
		value, _ := unix.Sysctl(name)
		return clean(value)
	}
	v.System = Device{Model: read("hw.model"), Source: "macos_sysctl"}
	v.OS = OS{Name: "macOS", Version: read("kern.osproductversion"), Build: read("kern.osversion"), Architecture: runtime.GOARCH, Source: "macos_sysctl"}
	physical, _ := unix.SysctlUint32("hw.physicalcpu")
	v.CPUs = []CPU{{Model: read("machdep.cpu.brand_string"), PhysicalCores: int(physical), LogicalCores: runtime.NumCPU(), Source: "macos_sysctl"}}
	return portableInventory(v)
}
