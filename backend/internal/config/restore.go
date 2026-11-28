package config

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

// RestoreData recovers a verified snapshot to a new directory. It preserves
// the original backup and the current installation, including any live data.
func RestoreData(snapshot, destination string) error {
	if !filepath.IsAbs(snapshot) || !filepath.IsAbs(destination) {
		return errors.New("snapshot and restore destination must be absolute paths")
	}
	destination = filepath.Clean(destination)
	if _, err := os.Lstat(destination); !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("restore destination must be a new directory: %s", destination)
	}
	manifest, err := readBackupManifest(snapshot)
	if err != nil {
		return fmt.Errorf("read backup manifest: %w", err)
	}
	if manifest.SchemaVersion != 2 || manifest.SourceOS != runtime.GOOS || manifest.FromVersion == "" ||
		len(manifest.Files) != len(manifest.SHA256) || manifest.Files["config.yaml"] == "" {
		return errors.New("restore requires a checksummed data snapshot from this operating system")
	}
	root, err := os.OpenRoot(snapshot)
	if err != nil {
		return err
	}
	defer root.Close()
	stage, err := os.MkdirTemp(filepath.Dir(destination), ".noosphere-restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for name := range manifest.Files {
		relative := filepath.FromSlash(name)
		if !filepath.IsLocal(relative) || filepath.ToSlash(filepath.Clean(relative)) != name || !isBackupPath(name) {
			return fmt.Errorf("unexpected backup path %q", name)
		}
		target := filepath.Join(stage, relative)
		digest, err := recoverFile(root, relative, target)
		if err != nil {
			return fmt.Errorf("restore file %s: %w", name, err)
		}
		expected := agentContentDigest(name)
		if digest != manifest.SHA256[name] || (expected != "" && expected != digest) {
			return fmt.Errorf("backup SHA-256 mismatch: %s", name)
		}
		if strings.HasPrefix(name, "data/agent-actions/") && strings.HasSuffix(name, ".json") {
			if err := markActionAsHistory(target); err != nil {
				return err
			}
		}
	}
	if err := relocateRestoredConfig(filepath.Join(stage, "config.yaml")); err != nil {
		return err
	}
	if manifest.FromVersion != "legacy" {
		if err := writeUpgradeState(filepath.Join(stage, dataVersionFile), upgradeState{SchemaVersion: 1, AppVersion: manifest.FromVersion}); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(stage, "data", "workspace.sqlite")); err == nil {
		if err = os.WriteFile(filepath.Join(stage, "data", ".restored-agent-history"), []byte("1"), 0600); err != nil {
			return err
		}
	}
	return os.Rename(stage, destination)
}

// Only immutable content and its publication journal travel with restored history.
func agentHistoryPath(relative string, directory bool) bool {
	if relative == "." {
		return directory
	}
	parts := strings.Split(relative, "/")
	digest := func(value string, bytes int) bool {
		if len(value) != 2*bytes || strings.ToLower(value) != value {
			return false
		}
		_, err := hex.DecodeString(value)
		return err == nil
	}
	if !digest(parts[0], 16) {
		return false
	}
	switch len(parts) {
	case 1:
		return directory
	case 2:
		if directory {
			return parts[1] == "blobs" || parts[1] == "command-logs"
		}
		return parts[1] == "journal.json"
	case 3:
		return !directory && (parts[1] == "blobs" || parts[1] == "command-logs") && digest(parts[2], 32)
	}
	return false
}

func agentContentDigest(name string) string {
	suffix, ok := strings.CutPrefix(name, "data/agent-v2/")
	if !ok || !agentHistoryPath(suffix, false) {
		return ""
	}
	parts := strings.Split(suffix, "/")
	if len(parts) == 3 {
		return parts[2]
	}
	return ""
}

func isBackupPath(name string) bool {
	switch name {
	case "config.yaml", "models.yaml", "model-library.v1.json", "data/workspace.sqlite", "data/workspace.sqlite-wal", "data/workspace.sqlite-journal",
		"data/model-engineering.sqlite", "data/model-engineering.sqlite-wal", "data/model-engineering.sqlite-journal":
		return true
	}
	if suffix, ok := strings.CutPrefix(name, "data/agent-v2/"); ok {
		return agentHistoryPath(suffix, false)
	}
	for _, prefix := range []string{"data/nebula/", "data/agent-actions/", "data/model-engineering/artifacts/", "data/codelens-artifacts/", "data/chat-attachment-files/"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func recoverFile(root *os.Root, relative, target string) (string, error) {
	info, err := root.Lstat(relative)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("expected a regular backup file")
	}
	input, err := root.Open(relative)
	if err != nil {
		return "", err
	}
	defer input.Close()
	return writeBackupFile(input, target)
}

func markActionAsHistory(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var record map[string]json.RawMessage
	if err := json.Unmarshal(raw, &record); err != nil || record == nil {
		return fmt.Errorf("invalid action history: %s", filepath.Base(path))
	}
	if len(record["restored_status"]) == 0 {
		record["restored_status"] = record["status"]
	}
	record["status"] = json.RawMessage(`"historical"`)
	record["restored_history"] = json.RawMessage("true")
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(record); err != nil {
		return err
	}
	return os.WriteFile(path, output.Bytes(), 0600)
}

func relocateRestoredConfig(path string) error {
	if _, err := Load(path); err != nil {
		return fmt.Errorf("validate backup configuration: %w", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// Preserve other settings, including fields a later version may use.
	var config map[string]any
	if err := yaml.Unmarshal(raw, &config); err != nil {
		return err
	}
	if config == nil {
		config = make(map[string]any)
	}
	section := func(parent map[string]any, key string) map[string]any {
		if present, ok := parent[key].(map[string]any); ok {
			return present
		}
		child := make(map[string]any)
		parent[key] = child
		return child
	}
	section(config, "app")["data_dir"] = "./data"
	section(section(config, "modules"), "nebula")["data_dir"] = "./data/nebula"
	section(config, "gateway")["config_path"] = "./models.yaml"
	section(config, "logging")["dir"] = "./logs"
	updated, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	return os.WriteFile(path, updated, 0600)
}
