package config

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentRoundBackupWhitelistAndContentBinding(t *testing.T) {
	root := t.TempDir()
	cfg := defaults()
	cfg.App.DataDir = filepath.Join(root, "data")
	cfg.Modules.Nebula.DataDir = filepath.Join(cfg.App.DataDir, "nebula")
	cfg.Gateway.ConfigPath = filepath.Join(root, "models.yaml")
	configPath := filepath.Join(root, "config.yaml")
	put := func(base, name, content string) {
		t.Helper()
		p := filepath.Join(base, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	put(root, "config.yaml", "app:\n  data_dir: ./data\n")
	run := strings.Repeat("a", 32)
	prefix := "data/agent-v2/" + run + "/"
	digest := func(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }
	keep := map[string]string{prefix + "journal.json": "{}", prefix + "blobs/" + digest("artifact"): "artifact", prefix + "command-logs/" + digest("redacted output"): "redacted output"}
	denied := []string{prefix + "work/main.go", prefix + "work-rebase-123/draft.txt", prefix + "dependencies/index", prefix + "blobs/.capture-temp", prefix + "command-logs/not-a-hash", "data/agent-v2/.locks/project", "data/agent-v2/not-a-run/journal.json"}
	for name, content := range keep {
		put(root, name, content)
	}
	for _, name := range denied {
		put(root, name, "private scratch")
	}
	db, err := sql.Open("sqlite", filepath.Join(cfg.App.DataDir, "workspace.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TABLE sample(x TEXT)"); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := BackupData(configPath, cfg, "1.0.2")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := readBackupManifest(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range keep {
		if manifest.SHA256[name] != digest(content) {
			t.Fatal("missing immutable artifact", name)
		}
	}
	for _, name := range denied {
		if _, ok := manifest.Files[name]; ok {
			t.Fatal("scratch in manifest", name)
		}
	}
	target := filepath.Join(root, "restored")
	if err = RestoreData(snapshot, target); err != nil {
		t.Fatal(err)
	}
	for name, content := range keep {
		b, err := os.ReadFile(filepath.Join(target, filepath.FromSlash(name)))
		if err != nil || string(b) != content {
			t.Fatal(name, err)
		}
	}
	if _, err = os.Stat(filepath.Join(target, "data", ".restored-agent-history")); err != nil {
		t.Fatal("missing inert-history marker", err)
	}
	for _, name := range denied {
		if _, err = os.Stat(filepath.Join(target, filepath.FromSlash(name))); !os.IsNotExist(err) {
			t.Fatal("restored private draft", name, err)
		}
	}
	saveManifest := func() {
		b, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		put(snapshot, "manifest.json", string(b))
	}
	manifest.Files[denied[0]] = denied[0]
	manifest.SHA256[denied[0]] = digest("forged")
	put(snapshot, denied[0], "forged")
	saveManifest()
	if err = RestoreData(snapshot, filepath.Join(root, "bad-path")); err == nil || !strings.Contains(err.Error(), "unexpected backup path") {
		t.Fatalf("import accepted draft payload: %v", err)
	}
	delete(manifest.Files, denied[0])
	delete(manifest.SHA256, denied[0])
	bad := prefix + "blobs/" + digest("artifact")
	put(snapshot, bad, "altered")
	manifest.SHA256[bad] = digest("altered")
	saveManifest()
	if err = RestoreData(snapshot, filepath.Join(root, "bad-blob")); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("import trusted renamed artifact: %v", err)
	}
	put(root, bad, "damaged")
	if _, err = BackupData(configPath, cfg, "1.0.2"); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("backup accepted corrupted hash blob: %v", err)
	}
}
