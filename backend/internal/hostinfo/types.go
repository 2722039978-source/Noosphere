// Package hostinfo reads an allowlisted, static hardware inventory. It has no
// model, task, credential, filesystem exploration or device-control capability.
package hostinfo

type Device struct {
	Manufacturer  string `json:"manufacturer"`
	Model         string `json:"model"`
	Source        string `json:"source"`
	MissingReason string `json:"missing_reason,omitempty"`
}
type OS struct {
	Name          string `json:"name"`
	Version       string `json:"version"`
	Build         string `json:"build"`
	Architecture  string `json:"architecture"`
	Source        string `json:"source"`
	MissingReason string `json:"missing_reason,omitempty"`
}
type CPU struct {
	Manufacturer  string `json:"manufacturer"`
	Model         string `json:"model"`
	PhysicalCores int    `json:"physical_cores"`
	LogicalCores  int    `json:"logical_cores"`
	MaxClockMHz   int    `json:"max_clock_mhz"`
	Source        string `json:"source"`
}
type MemoryModule struct {
	Manufacturer  string  `json:"manufacturer"`
	PartNumber    string  `json:"part_number"`
	CapacityBytes *uint64 `json:"capacity_bytes"`
	SpeedMTS      *uint64 `json:"speed_mts"`
	Source        string  `json:"source"`
}
type Memory struct {
	InstalledBytes *uint64        `json:"installed_bytes"`
	Modules        []MemoryModule `json:"modules"`
	Source         string         `json:"source"`
	MissingReason  string         `json:"missing_reason,omitempty"`
}
type GPU struct {
	Model             string  `json:"model"`
	Manufacturer      string  `json:"manufacturer"`
	DriverVersion     string  `json:"driver_version"`
	TotalVRAMBytes    *uint64 `json:"total_vram_bytes"`
	Source            string  `json:"source"`
	VRAMSource        string  `json:"vram_source"`
	VRAMMissingReason string  `json:"vram_missing_reason,omitempty"`
}
type Disk struct {
	Model         string  `json:"model"`
	CapacityBytes *uint64 `json:"capacity_bytes"`
	InterfaceType string  `json:"interface_type"`
	Source        string  `json:"source"`
}
type Issue struct {
	Component string `json:"component"`
	Code      string `json:"code"`
	Message   string `json:"message"`
}

// Inventory deliberately excludes timestamps, free memory, usage, serials,
// network identifiers and device instance paths. It can be frozen in a manifest.
type Inventory struct {
	SchemaVersion    int      `json:"schema_version"`
	CollectorVersion string   `json:"collector_version"`
	Platform         string   `json:"platform"`
	Status           string   `json:"status"`
	System           Device   `json:"system"`
	Motherboard      Device   `json:"motherboard"`
	OS               OS       `json:"os"`
	CPUs             []CPU    `json:"cpus"`
	Memory           Memory   `json:"memory"`
	GPUs             []GPU    `json:"gpus"`
	SystemDisks      []Disk   `json:"system_disks"`
	Issues           []Issue  `json:"issues"`
	Limitations      []string `json:"limitations"`
}
