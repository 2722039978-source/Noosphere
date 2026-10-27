package hostinfo

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDecodeSanitizesSortsAndPreserves64BitVRAM(t *testing.T) {
	raw := []byte(`{"system":{"manufacturer":"OEM","model":"Model X","serial":"PRIVATE-SERIAL"},"cpus":[{"model":"CPU Z","physical_cores":6,"logical_cores":12}],"memory":{"modules":[{"manufacturer":"RAM","part_number":"DIMM B","capacity_bytes":8589934592,"speed_mts":3200},{"manufacturer":"RAM","part_number":"DIMM A","capacity_bytes":8589934592,"speed_mts":3200}]},"gpus":[{"model":"Virtual display","driver_version":"1","adapter_ram":4293918720},{"model":"Intel Arc A580","driver_version":"32.0.101.9150","total_vram_bytes":8589934592,"vram_source":"windows_registry_qword_pnp_driver"}],"system_disks":[{"model":"SSD","capacity_bytes":1000204886016,"serial_number":"PRIVATE-DISK"}],"os":{"name":"Windows","version":"10.0","build":"26100","architecture":"64-bit"}}`)
	v, err := decodeInventory(raw)
	if err != nil {
		t.Fatal(err)
	}
	if v.Memory.InstalledBytes == nil || *v.Memory.InstalledBytes != 16<<30 || v.Memory.Modules[0].PartNumber != "DIMM A" {
		t.Fatal(v.Memory)
	}
	if len(v.GPUs) != 2 || v.GPUs[0].TotalVRAMBytes == nil || *v.GPUs[0].TotalVRAMBytes != 8<<30 || v.GPUs[1].TotalVRAMBytes != nil || v.GPUs[1].VRAMMissingReason == "" {
		t.Fatal(v.GPUs)
	}
	encoded, _ := json.Marshal(v)
	if strings.Contains(string(encoded), "PRIVATE") || strings.Contains(string(encoded), `"adapter_ram"`) || strings.Contains(string(encoded), `"serial"`) || strings.Contains(string(encoded), `"serial_number"`) {
		t.Fatal("private or unreliable field escaped allowlist", string(encoded))
	}
	second, err := decodeInventory(raw)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := json.Marshal(second)
	if string(encoded) != string(again) {
		t.Fatal("static inventory identity changes between reads")
	}
}

func TestMissingComponentsRemainUnknownAndBounded(t *testing.T) {
	v, err := decodeInventory([]byte(`{"system":{"model":"PC"},"memory":{"modules":[]},"gpus":[],"issues":[{"component":"gpu","code":"QUERY_UNAVAILABLE","message":"RAW PRIVATE ERROR"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "partial" || v.Memory.InstalledBytes != nil || v.Memory.MissingReason == "" || len(v.Issues) == 0 {
		t.Fatal(v)
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), "RAW PRIVATE") || strings.Contains(string(raw), `"gpus":null`) {
		t.Fatal(string(raw))
	}
	if _, err = decodeInventory(make([]byte, maxOutputBytes+1)); err == nil {
		t.Fatal("unbounded output accepted")
	}
}

func TestCacheCancellationAndFailures(t *testing.T) {
	var calls atomic.Int32
	c := newCollector(func(ctx context.Context) ([]byte, error) {
		calls.Add(1)
		return []byte(`{"system":{"model":"PC"}}`), nil
	}, 30*time.Second)
	first, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first.System.Model = "mutated caller copy"
	second, err := c.Collect(context.Background())
	if err != nil || second.System.Model != "PC" || calls.Load() != 1 {
		t.Fatal(second, err, calls.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = c.Collect(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled caller read cache", err)
	}
	failure := newCollector(func(context.Context) ([]byte, error) { return nil, errors.New("private path must not escape") }, time.Second)
	v, err := failure.Collect(context.Background())
	if err != nil || v.Status != "unavailable" || len(v.Issues) == 0 {
		t.Fatal(v, err)
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), "private path") {
		t.Fatal(string(raw))
	}
}
