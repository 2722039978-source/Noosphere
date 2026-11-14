package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigurationSchemaReadsLegacyAndRejectsFuture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("app: {name: retained}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil || loaded.SchemaVersion != 1 || loaded.App.Name != "retained" {
		t.Fatalf("legacy configuration: %+v, %v", loaded, err)
	}
	if err := os.WriteFile(path, []byte("config_version: 2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "config_version 2") {
		t.Fatalf("future configuration should require explicit migration support: %v", err)
	}
}

func TestUpgradeBacksUpBeforeOpeningStores(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	cfg := defaults()
	cfg.App.DataDir = data
	cfg.Modules.Nebula.DataDir = filepath.Join(data, "nebula")
	cfg.Gateway.ConfigPath = filepath.Join(root, "models.yaml")
	configPath := filepath.Join(root, "config.yaml")
	files := map[string]string{
		configPath:                                                "app: {name: local}",
		cfg.Gateway.ConfigPath:                                    "providers: []",
		filepath.Join(data, "workspace.sqlite"):                   "db",
		filepath.Join(data, "workspace.sqlite-wal"):               "wal",
		filepath.Join(data, "agent-actions", "change.json"):       "action",
		filepath.Join(cfg.Modules.Nebula.DataDir, "nebula.wal"):   "memory",
		filepath.Join(cfg.Modules.Nebula.DataDir, "vectors.json"): "cache",
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, version := range []string{"0.7.0", "0.7.0", "0.6.0", "0.7.0"} {
		if err := PrepareUpgrade(configPath, cfg, version); err != nil {
			t.Fatal(err)
		}
	}
	snapshots, err := os.ReadDir(filepath.Join(root, "upgrade-backups"))
	if err != nil || len(snapshots) != 3 {
		t.Fatalf("expected legacy, downgrade and re-upgrade snapshots: %v, %v", snapshots, err)
	}
	for _, entry := range snapshots {
		snapshot := filepath.Join(root, "upgrade-backups", entry.Name())
		var manifest upgradeManifest
		readJSON(t, filepath.Join(snapshot, "manifest.json"), &manifest)
		if len(manifest.Files) != len(files)-1 {
			t.Fatalf("manifest files: %+v", manifest.Files)
		}
		for backup, original := range manifest.Files {
			got, err := os.ReadFile(filepath.Join(snapshot, backup))
			if err != nil || string(got) != files[original] {
				t.Fatalf("backup of %s = %q, %v", original, got, err)
			}
		}
	}
}

func TestFailedUpgradeBackupLeavesOriginalStateAndRetries(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	cfg := defaults()
	cfg.App.DataDir = filepath.Join(root, "data")
	cfg.Modules.Nebula.DataDir = filepath.Join(cfg.App.DataDir, "nebula")
	cfg.Gateway.ConfigPath = filepath.Join(root, "models.yaml")
	if err := PrepareUpgrade(configPath, cfg, "0.6.0"); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(cfg.App.DataDir, "workspace.sqlite")
	if err := os.MkdirAll(database, 0700); err != nil {
		t.Fatal(err)
	}
	if err := PrepareUpgrade(configPath, cfg, "0.7.0"); err == nil {
		t.Fatal("startup should stop until a backup can be written")
	}
	var state upgradeState
	readJSON(t, filepath.Join(root, "data-version.json"), &state)
	if state.AppVersion != "0.6.0" {
		t.Fatalf("failed backup advanced version: %+v", state)
	}
	entries, err := os.ReadDir(filepath.Join(root, "upgrade-backups"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("incomplete backup was retained: %v, %v", entries, err)
	}
	if err := os.Remove(database); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(database, []byte("state"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := PrepareUpgrade(configPath, cfg, "0.7.0"); err != nil {
		t.Fatal(err)
	}
	readJSON(t, filepath.Join(root, "data-version.json"), &state)
	if state.AppVersion != "0.7.0" {
		t.Fatalf("retry did not update version: %+v", state)
	}
}

func readJSON(t *testing.T, path string, target any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatal(err)
	}
}
