package hostinfo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode"
)

const maxOutputBytes = 64 << 10
const cacheTTL = 30 * time.Second

var errUnsupported = errors.New("platform inventory unsupported")
var defaultCollector = newCollector(platformProbe, cacheTTL)

type Collector struct {
	gate  chan struct{}
	probe func(context.Context) ([]byte, error)
	ttl   time.Duration
	value *Inventory
	until time.Time
}

func newCollector(probe func(context.Context) ([]byte, error), ttl time.Duration) *Collector {
	return &Collector{gate: make(chan struct{}, 1), probe: probe, ttl: ttl}
}
func Collect(ctx context.Context) (Inventory, error) { return defaultCollector.Collect(ctx) }

// Collect has one bounded probe at a time. A cancelled waiter does not cancel
// another caller's probe; failed probes expose no raw stderr or private paths.
func (c *Collector) Collect(ctx context.Context) (Inventory, error) {
	if err := ctx.Err(); err != nil {
		return Inventory{}, err
	}
	select {
	case c.gate <- struct{}{}:
	case <-ctx.Done():
		return Inventory{}, ctx.Err()
	}
	defer func() { <-c.gate }()
	if err := ctx.Err(); err != nil {
		return Inventory{}, err
	}
	if c.value != nil && time.Now().Before(c.until) {
		return clone(*c.value), nil
	}
	child, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	raw, err := c.probe(child)
	if ctx.Err() != nil {
		return Inventory{}, ctx.Err()
	}
	var inventory Inventory
	if err == nil {
		inventory, err = decodeInventory(raw)
	}
	if err != nil {
		inventory = unavailable("unavailable", "QUERY_UNAVAILABLE")
		if errors.Is(err, errUnsupported) {
			inventory = unavailable("unsupported", "PLATFORM_UNSUPPORTED")
		}
		if errors.Is(err, context.DeadlineExceeded) || child.Err() != nil {
			inventory = unavailable("unavailable", "QUERY_TIMEOUT")
		}
	}
	c.value = &inventory
	c.until = time.Now().Add(c.ttl)
	return clone(inventory), nil
}
func clone(in Inventory) Inventory {
	raw, _ := json.Marshal(in)
	var out Inventory
	_ = json.Unmarshal(raw, &out)
	return out
}
func clean(s string) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s))
	r := []rune(s)
	if len(r) > 256 {
		r = r[:256]
	}
	return string(r)
}
func positive(v *uint64) *uint64 {
	if v == nil || *v == 0 {
		return nil
	}
	return v
}
func decodeInventory(raw []byte) (Inventory, error) {
	var v Inventory
	if len(raw) > maxOutputBytes {
		return v, fmt.Errorf("inventory response too large")
	}
	// Unknown protocol properties are discarded, never copied into evidence.
	if err := json.Unmarshal(bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf}), &v); err != nil {
		return v, err
	}
	v.SchemaVersion = 1
	v.CollectorVersion = "hostinfo-windows-cim-v1"
	v.Platform = runtime.GOOS
	v.Status = "complete"
	v.Limitations = []string{"Static host-reported inventory, not a performance benchmark or per-model attribution.", "Installed DIMM capacity differs from OS-usable physical RAM.", "GPU total VRAM is not free VRAM, a driver memory budget, or a model peak measurement.", "SMBIOS configured memory speed is reported, not independently measured; virtual/OEM model labels may be generic.", "Read-only allowlist excludes serials, UUIDs, host/user/network identifiers; inventory may be cached for up to 30 seconds."}
	issues := []Issue{}
	issue := func(component, code string) {
		for _, i := range issues {
			if i.Component == component && i.Code == code {
				return
			}
		}
		issues = append(issues, Issue{Component: component, Code: code, Message: issueMessage(code)})
	}
	for _, i := range v.Issues {
		switch i.Component {
		case "system", "motherboard", "os", "cpu", "memory", "gpu", "system_disk":
			issue(i.Component, "QUERY_UNAVAILABLE")
		}
	}
	device := func(d *Device, component string) {
		d.Manufacturer = clean(d.Manufacturer)
		d.Model = clean(d.Model)
		d.Source = "windows_cim"
		d.MissingReason = ""
		if d.Model == "" {
			d.MissingReason = "model not reported by firmware/CIM"
			issue(component, "NOT_REPORTED")
		} else if d.Model == "System Product Name" || d.Model == "System Model" || d.Model == "Default string" || strings.EqualFold(d.Model, "To Be Filled By O.E.M.") {
			d.MissingReason = "firmware reports a generic model label; exact assembled-PC model unavailable"
			issue(component, "GENERIC_MODEL")
		}
	}
	device(&v.System, "system")
	device(&v.Motherboard, "motherboard")
	v.OS.Name = clean(v.OS.Name)
	v.OS.Version = clean(v.OS.Version)
	v.OS.Build = clean(v.OS.Build)
	v.OS.Architecture = clean(v.OS.Architecture)
	v.OS.Source = "windows_cim"
	v.OS.MissingReason = ""
	if v.OS.Name == "" {
		v.OS.MissingReason = "OS details not reported"
		issue("os", "NOT_REPORTED")
	}
	if v.CPUs == nil {
		v.CPUs = []CPU{}
	}
	if len(v.CPUs) > 64 {
		return Inventory{}, fmt.Errorf("too many CPU records")
	}
	for i := range v.CPUs {
		c := &v.CPUs[i]
		c.Model = clean(c.Model)
		c.Manufacturer = clean(c.Manufacturer)
		c.Source = "windows_cim"
		if c.PhysicalCores < 0 {
			c.PhysicalCores = 0
		}
		if c.LogicalCores < 0 {
			c.LogicalCores = 0
		}
		if c.MaxClockMHz < 0 {
			c.MaxClockMHz = 0
		}
		if c.Model == "" {
			issue("cpu", "NOT_REPORTED")
		}
	}
	if len(v.CPUs) == 0 {
		issue("cpu", "NOT_REPORTED")
	}
	if v.Memory.Modules == nil {
		v.Memory.Modules = []MemoryModule{}
	}
	if len(v.Memory.Modules) > 256 {
		return Inventory{}, fmt.Errorf("too many DIMM records")
	}
	v.Memory.Source = "windows_cim_smbios"
	v.Memory.MissingReason = ""
	v.Memory.InstalledBytes = nil
	var total uint64
	allKnown := len(v.Memory.Modules) > 0
	for i := range v.Memory.Modules {
		m := &v.Memory.Modules[i]
		m.Manufacturer = clean(m.Manufacturer)
		m.PartNumber = clean(m.PartNumber)
		m.Source = "windows_cim_smbios"
		m.CapacityBytes = positive(m.CapacityBytes)
		m.SpeedMTS = positive(m.SpeedMTS)
		if m.PartNumber == "" {
			issue("memory", "PART_NUMBER_UNAVAILABLE")
		}
		if m.CapacityBytes == nil || *m.CapacityBytes > 1<<48 || total > (1<<48)-*m.CapacityBytes {
			allKnown = false
		} else {
			total += *m.CapacityBytes
		}
	}
	if allKnown {
		v.Memory.InstalledBytes = &total
	} else {
		v.Memory.MissingReason = "complete installed DIMM capacities unavailable"
		issue("memory", "NOT_REPORTED")
	}
	if v.GPUs == nil {
		v.GPUs = []GPU{}
	}
	if len(v.GPUs) > 64 {
		return Inventory{}, fmt.Errorf("too many GPU records")
	}
	for i := range v.GPUs {
		g := &v.GPUs[i]
		g.Model = clean(g.Model)
		g.Manufacturer = clean(g.Manufacturer)
		g.DriverVersion = clean(g.DriverVersion)
		g.Source = "windows_cim"
		g.TotalVRAMBytes = positive(g.TotalVRAMBytes)
		g.VRAMMissingReason = ""
		if g.VRAMSource != "windows_registry_qword_pnp_driver" || g.TotalVRAMBytes == nil || *g.TotalVRAMBytes > 1<<48 {
			g.TotalVRAMBytes = nil
			g.VRAMSource = "unavailable"
			g.VRAMMissingReason = "64-bit adapter memory matched by device driver key unavailable; 32-bit AdapterRAM is not trusted"
			issue("gpu", "VRAM_UNAVAILABLE")
		}
	}
	if len(v.GPUs) == 0 {
		issue("gpu", "NOT_REPORTED")
	}
	if v.SystemDisks == nil {
		v.SystemDisks = []Disk{}
	}
	if len(v.SystemDisks) > 64 {
		return Inventory{}, fmt.Errorf("too many system disk records")
	}
	for i := range v.SystemDisks {
		d := &v.SystemDisks[i]
		d.Model = clean(d.Model)
		d.InterfaceType = clean(d.InterfaceType)
		d.CapacityBytes = positive(d.CapacityBytes)
		d.Source = "windows_cim_system_drive_association"
	}
	if len(v.SystemDisks) == 0 {
		issue("system_disk", "NOT_REPORTED")
	}
	sort.Slice(v.CPUs, func(i, j int) bool {
		a, _ := json.Marshal(v.CPUs[i])
		b, _ := json.Marshal(v.CPUs[j])
		return bytes.Compare(a, b) < 0
	})
	sort.Slice(v.Memory.Modules, func(i, j int) bool {
		a, _ := json.Marshal(v.Memory.Modules[i])
		b, _ := json.Marshal(v.Memory.Modules[j])
		return bytes.Compare(a, b) < 0
	})
	sort.Slice(v.GPUs, func(i, j int) bool {
		a, _ := json.Marshal(v.GPUs[i])
		b, _ := json.Marshal(v.GPUs[j])
		return bytes.Compare(a, b) < 0
	})
	sort.Slice(v.SystemDisks, func(i, j int) bool {
		a, _ := json.Marshal(v.SystemDisks[i])
		b, _ := json.Marshal(v.SystemDisks[j])
		return bytes.Compare(a, b) < 0
	})
	sort.Slice(issues, func(i, j int) bool { return issues[i].Component+issues[i].Code < issues[j].Component+issues[j].Code })
	v.Issues = issues
	if len(issues) > 0 {
		v.Status = "partial"
	}
	return v, nil
}
func issueMessage(code string) string {
	switch code {
	case "QUERY_TIMEOUT":
		return "hardware query exceeded its bounded deadline"
	case "PLATFORM_UNSUPPORTED":
		return "detailed hardware inventory is not implemented on this platform"
	case "VRAM_UNAVAILABLE":
		return "reliable total GPU memory unavailable"
	case "NOT_REPORTED":
		return "hardware field was not reported"
	case "GENERIC_MODEL":
		return "firmware reported only a generic model label"
	case "PART_NUMBER_UNAVAILABLE":
		return "one or more DIMM part numbers were not reported by SMBIOS"
	default:
		return "hardware component query unavailable"
	}
}
func unavailable(status, code string) Inventory {
	v, _ := decodeInventory([]byte(`{}`))
	v.Status = status
	v.Issues = []Issue{{Component: "hardware", Code: code, Message: issueMessage(code)}}
	if status == "unsupported" {
		v.CollectorVersion = "hostinfo-portable-v1"
		v.System.Source = "unavailable"
		v.Motherboard.Source = "unavailable"
		v.Memory.Source = "unavailable"
		v.OS = OS{Name: runtime.GOOS, Architecture: runtime.GOARCH, Source: "go_runtime", MissingReason: "detailed platform inventory unsupported"}
	}
	return v
}
