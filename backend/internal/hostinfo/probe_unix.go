//go:build linux || darwin

package hostinfo

import (
	"encoding/json"
	"runtime"
)

func decodePlatformInventory(raw []byte) (Inventory, error) {
	var v Inventory
	err := json.Unmarshal(raw, &v)
	return v, err
}

func baseInventory(provider string) Inventory {
	inventory := unavailable("partial", "NOT_REPORTED")
	inventory.CollectorVersion = "hostinfo-" + provider + "-v1"
	inventory.Issues = nil
	inventory.Memory.MissingReason = "installed module capacities are unavailable; OS-usable memory is reported separately"
	inventory.OS = OS{Name: runtime.GOOS, Architecture: runtime.GOARCH, Source: provider}
	inventory.Limitations = []string{
		"Static host-reported inventory. OS-usable RAM is sampled separately.",
		"GPU and system disk hardware identities are unavailable from this collector.",
		"Unified memory is not dedicated GPU memory.",
		"Read-only allowlist excludes serials, UUIDs, host/user/network identifiers.",
	}
	return inventory
}

func portableInventory(v Inventory) ([]byte, error) {
	for _, missing := range []struct {
		condition bool
		component string
	}{
		{v.System.Model == "", "system"},
		{v.Motherboard.Model == "", "motherboard"},
		{v.OS.Version == "", "os"},
		{len(v.CPUs) == 0 || v.CPUs[0].Model == "", "cpu"},
		{true, "memory"},
		{true, "gpu"},
		{true, "system_disk"},
	} {
		if missing.condition {
			v.Issues = append(v.Issues, Issue{Component: missing.component, Code: "NOT_REPORTED", Message: issueMessage("NOT_REPORTED")})
		}
	}
	return json.Marshal(v)
}
