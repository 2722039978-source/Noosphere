package config

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/noosphere/backend/internal/attachmentfile"
)

type upgradeState struct {
	SchemaVersion int    `json:"schema_version"`
	AppVersion    string `json:"app_version"`
}

type upgradeManifest struct {
	SchemaVersion int               `json:"schema_version"`
	FromVersion   string            `json:"from_version"`
	ToVersion     string            `json:"to_version"`
	SourceOS      string            `json:"source_os,omitempty"`
	Files         map[string]string `json:"files"`
	SHA256        map[string]string `json:"sha256,omitempty"`
}

const (
	dataVersionFile         = "data-version.json"
	pendingUpgradeFile      = "data-upgrade-pending.json"
	upgradeBackupsDirectory = "upgrade-backups"
)

type pendingUpgrade struct {
	FromVersion string `json:"from_version"`
	ToVersion   string `json:"to_version"`
	Snapshot    string `json:"snapshot,omitempty"`
}

func readUpgradeState(statePath string) (upgradeState, error) {
	state := upgradeState{SchemaVersion: 1}
	raw, err := os.ReadFile(statePath)
	switch {
	case err == nil:
		if err = json.Unmarshal(raw, &state); err != nil || state.SchemaVersion != 1 || state.AppVersion == "" {
			return upgradeState{}, fmt.Errorf("invalid data version file %s", statePath)
		}
	case !errors.Is(err, fs.ErrNotExist):
		return upgradeState{}, fmt.Errorf("read data version: %w", err)
	}
	return state, nil
}

// UpgradeNeeded tells the desktop supervisor when startup may spend time
// copying user data before opening the HTTP listener.
func UpgradeNeeded(configPath, version string) (bool, error) {
	state, err := readUpgradeState(filepath.Join(filepath.Dir(configPath), dataVersionFile))
	return err == nil && state.AppVersion != version, err
}

// PrepareUpgrade runs under the data directory lock before any store opens.
// Keep a single pre-migration snapshot through interrupted startup attempts.
func PrepareUpgrade(configPath string, cfg *AppConfig, version string) error {
	configPath, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	root := filepath.Dir(configPath)
	state, err := readUpgradeState(filepath.Join(root, dataVersionFile))
	if err != nil {
		return err
	}
	previous := state.AppVersion
	if previous == "" {
		previous = "legacy"
	}
	pendingPath := filepath.Join(root, pendingUpgradeFile)
	var pending pendingUpgrade
	data, err := os.ReadFile(pendingPath)
	switch {
	case err == nil:
		if err = json.Unmarshal(data, &pending); err != nil || pending.FromVersion == "" || pending.ToVersion == "" {
			return fmt.Errorf("invalid pending data upgrade record: %s", pendingPath)
		}
		if state.AppVersion == pending.ToVersion {
			// Startup committed successfully before the journal was removed.
			if err := os.Remove(pendingPath); err != nil {
				return fmt.Errorf("clear completed data upgrade record: %w", err)
			}
		} else {
			if pending.FromVersion != previous || pending.ToVersion != version {
				return fmt.Errorf("data upgrade from %s to %s is unfinished; restart that version or recover its snapshot", pending.FromVersion, pending.ToVersion)
			}
			if pending.Snapshot != "" {
				snapshot := filepath.Join(root, upgradeBackupsDirectory, pending.Snapshot)
				if filepath.Base(pending.Snapshot) != pending.Snapshot || !filepath.IsLocal(pending.Snapshot) || pending.Snapshot == "." {
					return fmt.Errorf("invalid pending upgrade snapshot name")
				}
				manifest, err := readBackupManifest(snapshot)
				if err != nil || manifest.FromVersion != previous || manifest.ToVersion != version {
					return fmt.Errorf("pre-upgrade snapshot is missing or unreadable: %s", snapshot)
				}
			}
			return nil
		}
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}
	if state.AppVersion == version {
		return nil
	}
	snapshot, err := backupUpgrade(root, configPath, cfg, previous, version, false)
	if err != nil {
		return fmt.Errorf("prepare data upgrade: %w", err)
	}
	pending = pendingUpgrade{FromVersion: previous, ToVersion: version}
	if snapshot != "" {
		pending.Snapshot = filepath.Base(snapshot)
	}
	return writeUpgradeState(pendingPath, pending)
}

func CompleteUpgrade(configPath string, version string) error {
	root := filepath.Dir(configPath)
	statePath := filepath.Join(root, dataVersionFile)
	state, err := readUpgradeState(statePath)
	if err != nil || state.AppVersion == version {
		return err
	}
	state.AppVersion = version
	if err = writeUpgradeState(statePath, state); err != nil {
		return err
	}
	// A leftover journal is harmless after the state file commit.
	_ = os.Remove(filepath.Join(root, pendingUpgradeFile))
	return nil
}

func (cfg *AppConfig) WorkspaceDataDir() string {
	if cfg.App.DataDir != "" {
		return cfg.App.DataDir
	}
	return filepath.Dir(cfg.Modules.Nebula.DataDir)
}

func BackupData(configPath string, cfg *AppConfig, version string) (string, error) {
	root := filepath.Dir(configPath)
	if _, err := os.Stat(filepath.Join(root, pendingUpgradeFile)); err == nil {
		return "", fmt.Errorf("a data upgrade is unfinished; use the previous snapshot in %s", filepath.Join(root, upgradeBackupsDirectory))
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	state, err := readUpgradeState(filepath.Join(root, dataVersionFile))
	if err != nil {
		return "", err
	}
	if state.AppVersion == "" {
		state.AppVersion = "legacy"
	}
	return backupUpgrade(root, configPath, cfg, state.AppVersion, version, true)
}

func backupUpgrade(root, configPath string, cfg *AppConfig, previous, version string, manual bool) (string, error) {
	dataDir := cfg.WorkspaceDataDir()
	dest := filepath.Join(root, upgradeBackupsDirectory)
	if err := os.MkdirAll(dest, 0700); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(dest, ".pending-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	manifest := upgradeManifest{SchemaVersion: 2, FromVersion: previous, ToVersion: version, SourceOS: runtime.GOOS, Files: make(map[string]string), SHA256: make(map[string]string)}
	copyFile := func(source, relative string) error {
		absolute, err := filepath.Abs(source)
		if err != nil {
			return err
		}
		sum, err := copyBackupFile(absolute, filepath.Join(stage, relative))
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
		key := filepath.ToSlash(relative)
		manifest.Files[key] = absolute
		manifest.SHA256[key] = sum
		return nil
	}
	for _, path := range []struct{ source, relative string }{
		{configPath, "config.yaml"},
		{cfg.Gateway.ConfigPath, "models.yaml"},
		{filepath.Join(dataDir, "workspace.sqlite"), "data/workspace.sqlite"},
		{filepath.Join(dataDir, "workspace.sqlite-wal"), "data/workspace.sqlite-wal"},
		{filepath.Join(dataDir, "workspace.sqlite-journal"), "data/workspace.sqlite-journal"},
		{filepath.Join(dataDir, "model-engineering.sqlite"), "data/model-engineering.sqlite"},
		{filepath.Join(dataDir, "model-engineering.sqlite-wal"), "data/model-engineering.sqlite-wal"},
		{filepath.Join(dataDir, "model-engineering.sqlite-journal"), "data/model-engineering.sqlite-journal"},
	} {
		if err := copyFile(path.source, path.relative); err != nil {
			return "", err
		}
	}
	for _, path := range []struct{ source, relative string }{
		{cfg.Modules.Nebula.DataDir, "data/nebula"},
		{filepath.Join(dataDir, "agent-actions"), "data/agent-actions"},
		{filepath.Join(dataDir, "model-engineering", "artifacts"), "data/model-engineering/artifacts"},
		{filepath.Join(dataDir, "codelens-artifacts"), "data/codelens-artifacts"},
		{filepath.Join(dataDir, attachmentfile.StorageDirectory), "data/" + attachmentfile.StorageDirectory},
	} {
		if path.source == "" {
			continue
		}
		err := filepath.WalkDir(path.source, func(file string, item fs.DirEntry, visitErr error) error {
			if visitErr != nil {
				if file == path.source && errors.Is(visitErr, fs.ErrNotExist) {
					return nil
				}
				return visitErr
			}
			relative, err := filepath.Rel(path.source, file)
			if err != nil {
				return err
			}
			if item.IsDir() {
				if (path.relative == "data/codelens-artifacts" && relative == "work") ||
					(path.relative == "data/"+attachmentfile.StorageDirectory && relative == attachmentfile.UploadDirectory) ||
					(path.relative == "data/nebula" && relative != ".") {
					return fs.SkipDir
				}
				return nil
			}
			if path.relative == "data/nebula" {
				sstable, _ := filepath.Match("sst-*.sst", relative)
				if !sstable && relative != "nebula.wal" {
					return nil
				}
			}
			return copyFile(file, filepath.Join(path.relative, relative))
		})
		if err != nil {
			return "", err
		}
	}
	// A fresh installation has no prior user data to restore.
	if !manual && previous == "legacy" && len(manifest.Files) <= 1 {
		return "", nil
	}
	if err := writeUpgradeState(filepath.Join(stage, "manifest.json"), manifest); err != nil {
		return "", err
	}
	name := time.Now().UTC().Format("20060102T150405.000000000") + "-" + strings.TrimPrefix(filepath.Base(stage), ".pending-")
	snapshot := filepath.Join(dest, name)
	if err := os.Rename(stage, snapshot); err != nil {
		return "", err
	}
	return snapshot, nil
}

func readBackupManifest(snapshot string) (upgradeManifest, error) {
	raw, err := os.ReadFile(filepath.Join(snapshot, "manifest.json"))
	if err != nil {
		return upgradeManifest{}, err
	}
	var manifest upgradeManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return upgradeManifest{}, err
	}
	return manifest, nil
}

func copyBackupFile(source, target string) (string, error) {
	info, err := os.Lstat(source)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("upgrade backup expects regular file: %s", source)
	}
	input, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer input.Close()
	return writeBackupFile(input, target)
}

func writeBackupFile(input io.Reader, target string) (string, error) {
	var err error
	if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return "", err
	}
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	digest := sha256.New()
	_, err = io.Copy(io.MultiWriter(output, digest), input)
	if err == nil {
		err = output.Sync()
	}
	if err = errors.Join(err, output.Close()); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", digest.Sum(nil)), nil
}

func writeUpgradeState(path string, value any) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".upgrade-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(value)
	if err == nil {
		err = file.Sync()
	}
	if err = errors.Join(err, file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
