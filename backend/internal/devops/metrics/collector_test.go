package metrics

import "testing"

func TestHostSnapshotProvenanceAndUnknownGPU(t *testing.T) {
	c := NewCollector()
	s, err := c.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	gpu := s.Samples["available_vram_bytes"]
	if gpu.Value != nil || gpu.Quality != "unknown" || gpu.Unit != "bytes" || gpu.Source != "unavailable" || gpu.MissingReason == "" {
		t.Fatalf("invented GPU sample: %+v", gpu)
	}
	cpu := s.Samples["cpu_percent"]
	if cpu.Value != nil || cpu.MissingReason == "" {
		t.Fatalf("first CPU sample is not a measured interval: %+v", cpu)
	}
	ram := s.Samples["ram_used_bytes"]
	if ram.Source != "windows.GlobalMemoryStatusEx" || ram.Unit != "bytes" || ram.Quality != "host_snapshot" || ram.Value == nil {
		t.Fatalf("RAM provenance: %+v", ram)
	}
}
