package config

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
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
		configPath:                                                                    "app: {name: local}",
		cfg.Gateway.ConfigPath:                                                        "providers: []",
		filepath.Join(data, "workspace.sqlite"):                                       "db",
		filepath.Join(data, "workspace.sqlite-wal"):                                   "wal",
		filepath.Join(data, "agent-actions", "change.json"):                           "action",
		filepath.Join(data, "chat-attachment-files", "original.bin"):                  "attachment original",
		filepath.Join(data, "chat-attachment-files", ".uploads", "upload-incomplete"): "unfinished",
		filepath.Join(cfg.Modules.Nebula.DataDir, "nebula.wal"):                       "memory",
		filepath.Join(cfg.Modules.Nebula.DataDir, "vectors.json"):                     "cache",
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
		if err := CompleteUpgrade(configPath, version); err != nil {
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
		if len(manifest.Files) != len(files)-2 ||
			manifest.Files["data/chat-attachment-files/original.bin"] == "" ||
			manifest.Files["data/chat-attachment-files/.uploads/upload-incomplete"] != "" {
			t.Fatalf("manifest files: %+v", manifest.Files)
		}
		for backup, original := range manifest.Files {
			got, err := os.ReadFile(filepath.Join(snapshot, backup))
			if err != nil || string(got) != files[original] {
				t.Fatalf("backup of %s = %q, %v", original, got, err)
			}
			if want := fmt.Sprintf("%x", sha256.Sum256(got)); manifest.SHA256[backup] != want {
				t.Fatalf("backup of %s did not record SHA-256 %s", original, want)
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
	if err := CompleteUpgrade(configPath, "0.6.0"); err != nil {
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
	if err := CompleteUpgrade(configPath, "0.7.0"); err != nil {
		t.Fatal(err)
	}
	readJSON(t, filepath.Join(root, "data-version.json"), &state)
	if state.AppVersion != "0.7.0" {
		t.Fatalf("retry did not update version: %+v", state)
	}
}

func TestInterruptedUpgradeRetainsPreMigrationSnapshot(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	cfg := defaults()
	cfg.App.DataDir = filepath.Join(root, "data")
	cfg.Modules.Nebula.DataDir = filepath.Join(cfg.App.DataDir, "nebula")
	cfg.Gateway.ConfigPath = filepath.Join(root, "models.yaml")
	statePath := filepath.Join(root, "data-version.json")
	if err := writeUpgradeState(statePath, upgradeState{SchemaVersion: 1, AppVersion: "0.9.0"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.Gateway.ConfigPath, []byte("pre-migration data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := PrepareUpgrade(configPath, cfg, "1.0.0"); err != nil {
		t.Fatal(err)
	}
	var state upgradeState
	readJSON(t, statePath, &state)
	if state.AppVersion != "0.9.0" {
		t.Fatalf("prepared upgrade advanced version before stores opened: %+v", state)
	}
	if err := os.WriteFile(cfg.Gateway.ConfigPath, []byte("partially migrated data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := PrepareUpgrade(configPath, cfg, "1.0.1"); err == nil {
		t.Fatal("a different version should not proceed from a possibly partial migration")
	}
	if err := PrepareUpgrade(configPath, cfg, "1.0.0"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "upgrade-backups"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("retry should retain one pre-migration snapshot: %v, %v", entries, err)
	}
	original, err := os.ReadFile(filepath.Join(root, "upgrade-backups", entries[0].Name(), "models.yaml"))
	if err != nil || string(original) != "pre-migration data" {
		t.Fatalf("snapshot no longer represents the old version: %q, %v", original, err)
	}
	if err := CompleteUpgrade(configPath, "1.0.0"); err != nil {
		t.Fatal(err)
	}
	readJSON(t, statePath, &state)
	if state.AppVersion != "1.0.0" {
		t.Fatalf("completed upgrade left an old version: %+v", state)
	}
}

func TestWorkspaceLockExcludesSecondOwnerUntilClose(t *testing.T) {
	dir := t.TempDir()
	first, err := LockWorkspaceData(dir)
	if err != nil {
		t.Fatal(err)
	}
	if lock, err := LockWorkspaceData(dir); err == nil {
		lock.Close()
		first.Close()
		t.Fatal("second owner obtained live workspace lock")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	retry, err := LockWorkspaceData(dir)
	if err != nil {
		t.Fatal(err)
	}
	retry.Close()
}

func TestOfflineBackupRestoresCommittedWALAndAttachment(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	cfg := defaults()
	cfg.App.DataDir = filepath.Join(root, "data")
	cfg.Modules.Nebula.DataDir = filepath.Join(cfg.App.DataDir, "nebula")
	cfg.Gateway.ConfigPath = filepath.Join(root, "models.yaml")
	attachment := filepath.Join(cfg.App.DataDir, "chat-attachment-files", "original.bin")
	action := filepath.Join(cfg.App.DataDir, "agent-actions", "saved.json")
	for path, content := range map[string]string{
		configPath:             "app:\n  data_dir: " + filepath.ToSlash(cfg.App.DataDir) + "\nfeature_flags: {preserve: true}\n",
		cfg.Gateway.ConfigPath: "providers: [{id: fixture, encrypted_api_key: 'enc:v1:retained'}]",
		attachment:             "retained attachment content",
		action:                 `{"id":"saved","status":"applied","output":"recorded result"}`,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	database := filepath.Join(cfg.App.DataDir, "workspace.sqlite")
	writer := exec.Command(os.Args[0], "-test.run=^TestWriteSQLiteWALFixture$")
	writer.Env = append(os.Environ(), "NOOSPHERE_TEST_WAL_DATABASE="+database)
	if output, err := writer.CombinedOutput(); err != nil {
		t.Fatalf("create abrupt exit database fixture: %v\n%s", err, output)
	}
	if info, err := os.Stat(database + "-wal"); err != nil || info.Size() == 0 {
		t.Fatalf("database fixture lacks committed WAL pages: %v", err)
	}
	if err := CompleteUpgrade(configPath, "0.9.0"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := BackupData(configPath, cfg, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "recovery 空间")
	if err := RestoreData(snapshot, destination); err != nil {
		t.Fatal(err)
	}
	if err := RestoreData(snapshot, destination); err == nil {
		t.Fatal("restore reused an existing data directory")
	}
	restoredConfigPath := filepath.Join(destination, "config.yaml")
	restoredConfig, err := Load(restoredConfigPath)
	if err != nil || restoredConfig.WorkspaceDataDir() != "./data" {
		t.Fatalf("restored config must reference restored stores: %+v, %v", restoredConfig, err)
	}
	if data, err := os.ReadFile(restoredConfigPath); err != nil || !strings.Contains(string(data), "preserve: true") {
		t.Fatalf("restore lost unrelated config data: %s, %v", data, err)
	}
	var state upgradeState
	readJSON(t, filepath.Join(destination, dataVersionFile), &state)
	if state.AppVersion != "0.9.0" {
		t.Fatalf("restore lost prior data version: %+v", state)
	}
	db, err := sql.Open("sqlite", filepath.Join(destination, "data", "workspace.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var attachmentName string
	if err := db.QueryRow("SELECT original_file FROM retained").Scan(&attachmentName); err != nil || attachmentName != "original.bin" {
		t.Fatalf("restore lost committed WAL data: %q, %v", attachmentName, err)
	}
	if data, err := os.ReadFile(filepath.Join(destination, "data", "chat-attachment-files", attachmentName)); err != nil || string(data) != "retained attachment content" {
		t.Fatalf("restore lost attachment original: %q, %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(destination, "models.yaml")); err != nil || !strings.Contains(string(data), "enc:v1:retained") {
		t.Fatalf("restore lost stored credential: %q, %v", data, err)
	}
	var history map[string]any
	readJSON(t, filepath.Join(destination, "data", "agent-actions", "saved.json"), &history)
	if history["status"] != "historical" || history["restored_status"] != "applied" || history["restored_history"] != true || history["output"] != "recorded result" {
		t.Fatalf("restore did not preserve inert action history: %+v", history)
	}
	if err := os.WriteFile(filepath.Join(snapshot, "data", "chat-attachment-files", "original.bin"), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(root, "corrupt")
	if err := RestoreData(snapshot, bad); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("restore accepted an altered backup: %v", err)
	}
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Fatalf("restore published incomplete data: %v", err)
	}
}

func TestWriteSQLiteWALFixture(t *testing.T) {
	path := os.Getenv("NOOSPHERE_TEST_WAL_DATABASE")
	if path == "" {
		t.Skip("helper invoked by offline backup test")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; CREATE TABLE retained (original_file TEXT); INSERT INTO retained VALUES ('original.bin')`); err != nil {
		t.Fatal(err)
	}
	// Leave the committed WAL on disk as an interrupted backend would.
	os.Exit(0)
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
