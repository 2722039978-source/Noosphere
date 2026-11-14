package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type upgradeState struct {
	SchemaVersion int    `json:"schema_version"`
	AppVersion    string `json:"app_version"`
}

type upgradeManifest struct {
	SchemaVersion int               `json:"schema_version"`
	FromVersion   string            `json:"from_version"`
	ToVersion     string            `json:"to_version"`
	Files         map[string]string `json:"files"`
}

// PrepareUpgrade runs before any store opens. Keep SQLite WAL files alongside
// their databases: after an abrupt exit the WAL may contain committed writes.
// This is a recovery copy; the application continues using the original files.
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
	state, err := readUpgradeState(filepath.Join(filepath.Dir(configPath), "data-version.json"))
	return err == nil && state.AppVersion != version, err
}

func PrepareUpgrade(configPath string, cfg *AppConfig, version string) error {
	configPath, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	root := filepath.Dir(configPath)
	statePath := filepath.Join(root, "data-version.json")
	state, err := readUpgradeState(statePath)
	if err != nil {
		return err
	}
	if state.AppVersion == version {
		return nil
	}

	previous := state.AppVersion
	if previous == "" {
		previous = "legacy"
	}
	if err := backupUpgrade(root, configPath, cfg, previous, version); err != nil {
		return fmt.Errorf("prepare data upgrade: %w", err)
	}
	state.AppVersion = version
	return writeUpgradeState(statePath, state)
}

func backupUpgrade(root, configPath string, cfg *AppConfig, previous, version string) error {
	dataDir := cfg.App.DataDir
	if dataDir == "" {
		dataDir = filepath.Dir(cfg.Modules.Nebula.DataDir)
	}
	dest := filepath.Join(root, "upgrade-backups")
	if err := os.MkdirAll(dest, 0700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(dest, ".pending-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	manifest := upgradeManifest{SchemaVersion: 1, FromVersion: previous, ToVersion: version, Files: make(map[string]string)}
	copyFile := func(source, relative string) error {
		absolute, err := filepath.Abs(source)
		if err != nil {
			return err
		}
		if err = copyBackupFile(absolute, filepath.Join(stage, relative)); errors.Is(err, fs.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
		manifest.Files[filepath.ToSlash(relative)] = absolute
		return nil
	}
	for _, path := range []struct{ source, relative string }{
		{configPath, "config.yaml"},
		{cfg.Gateway.ConfigPath, "models.yaml"},
		{filepath.Join(dataDir, "workspace.sqlite"), "data/workspace.sqlite"},
		{filepath.Join(dataDir, "workspace.sqlite-wal"), "data/workspace.sqlite-wal"},
		{filepath.Join(dataDir, "model-engineering.sqlite"), "data/model-engineering.sqlite"},
		{filepath.Join(dataDir, "model-engineering.sqlite-wal"), "data/model-engineering.sqlite-wal"},
	} {
		if err := copyFile(path.source, path.relative); err != nil {
			return err
		}
	}
	for _, path := range []struct{ source, relative string }{
		{cfg.Modules.Nebula.DataDir, "data/nebula"},
		{filepath.Join(dataDir, "agent-actions"), "data/agent-actions"},
		{filepath.Join(dataDir, "model-engineering", "artifacts"), "data/model-engineering/artifacts"},
		{filepath.Join(dataDir, "codelens-artifacts"), "data/codelens-artifacts"},
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
			return err
		}
	}
	// A fresh installation has no prior user data to restore.
	if previous == "legacy" && len(manifest.Files) <= 1 {
		return nil
	}
	if err := writeUpgradeState(filepath.Join(stage, "manifest.json"), manifest); err != nil {
		return err
	}
	name := time.Now().UTC().Format("20060102T150405.000000000") + "-" + strings.TrimPrefix(filepath.Base(stage), ".pending-")
	return os.Rename(stage, filepath.Join(dest, name))
}

func copyBackupFile(source, target string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("upgrade backup expects regular file: %s", source)
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(output, input)
	if err == nil {
		err = output.Sync()
	}
	return errors.Join(err, output.Close())
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
