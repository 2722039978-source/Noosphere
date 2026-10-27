package modellab

import (
	"encoding/json"
	"testing"

	"github.com/noosphere/backend/internal/hostinfo"
	"github.com/noosphere/backend/internal/modelstore"
)

func TestHardwareEnvironmentStrictAndLegacyComparison(t *testing.T) {
	legacy, err := freezeEnvironment(modelstore.EnvironmentSnapshot{OS: "windows", Arch: "amd64", CollectorVersion: "modellab-devops-host-v1", ToolchainVersion: "go1.26.5", Cores: modelstore.Int(12), RAMBytes: modelstore.Int(16000), VRAM: unknownMetric("bytes", "not collected")})
	if err != nil {
		t.Fatal(err)
	}
	original, _ := json.Marshal(legacy)
	modern := legacy
	modern.CPU = "CPU model"
	modern.GPU = "GPU model"
	modern.Driver = "driver1"
	modern.Hardware = &hostinfo.Inventory{SchemaVersion: 1, CollectorVersion: "hostinfo-windows-cim-v1", Platform: "windows", System: hostinfo.Device{Model: "PC"}}
	modern, err = freezeEnvironment(modern)
	if err != nil {
		t.Fatal(err)
	}
	if !environmentEqual(modern, legacy) {
		t.Fatal("optional new evidence rejected unchanged legacy known facts")
	}
	oldMessage := "physical GPU/driver/VRAM collection unavailable; daemon residency is recorded separately"
	oldUnknown := legacy
	oldUnknown.VRAM.MissingReason = oldMessage
	oldUnknown, _ = freezeEnvironment(oldUnknown)
	newUnknown := modern
	newUnknown.VRAM.MissingReason = "physical available GPU memory collection unavailable; total adapter VRAM is recorded separately in hardware inventory"
	newUnknown, _ = freezeEnvironment(newUnknown)
	if !environmentEqual(newUnknown, oldUnknown) {
		t.Fatal("corrected unknown-free-VRAM explanation rejected legacy identity")
	}
	known := newUnknown
	known.VRAM.Value = modelstore.Float(0)
	known, _ = freezeEnvironment(known)
	if environmentEqual(known, oldUnknown) {
		t.Fatal("legacy explanation projection ignored a VRAM value change")
	}
	if environmentEqual(legacy, modern) {
		t.Fatal("hardware-aware history accepted loss of evidence")
	}
	changed := modern
	copy := *modern.Hardware
	copy.System.Model = "changedPC"
	changed.Hardware = &copy
	changed, _ = freezeEnvironment(changed)
	if environmentEqual(changed, modern) {
		t.Fatal("changed hardware accepted for modern snapshot")
	}
	changed = modern
	changed.ToolchainVersion = "changedGo"
	changed, _ = freezeEnvironment(changed)
	if environmentEqual(changed, legacy) {
		t.Fatal("legacy known toolchain drift ignored")
	}
	known = legacy
	known.CPU = "previousKnownCPU"
	known, _ = freezeEnvironment(known)
	if environmentEqual(modern, known) {
		t.Fatal("legacy known CPU drift ignored")
	}
	after, _ := json.Marshal(legacy)
	if string(after) != string(original) {
		t.Fatal("legacy immutable body modified")
	}
}
