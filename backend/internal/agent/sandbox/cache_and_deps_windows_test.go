package sandbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWindowsToolchainCacheReuseAndReadOnlyProtection(t *testing.T) {
	if _, err := exec.LookPath("python.exe"); err != nil {
		t.Skip("python is not installed")
	}
	cacheDir := filepath.Join(t.TempDir(), "cache")
	work := filepath.Join(t.TempDir(), "work")
	if err := os.Mkdir(work, 0700); err != nil {
		t.Fatal(err)
	}
	limits := Limits{CacheDir: cacheDir}
	probe := `import os, sys; p = os.path.join(os.path.dirname(sys.executable), 'tamper.txt'); wrote = False
try:
    open(p, 'w').write('x'); wrote = True
except OSError:
    pass
print(f'ok|wrote={wrote}')`

	// First run populates the versioned read-only cache.
	res1, err := Run(context.Background(), work, ".", "python", []string{"-I", "-c", probe}, limits)
	if err != nil || res1.ExitCode != 0 || !strings.Contains(res1.Output, "ok|wrote=False") {
		t.Fatalf("first python run failed or allowed toolchain tamper: %+v %v", res1, err)
	}

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	foundReady := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "python-") {
			if _, e := os.Stat(filepath.Join(cacheDir, entry.Name(), ".ready")); e == nil {
				foundReady = true
			}
		}
	}
	if !foundReady {
		t.Fatalf("expected reusable python cache with .ready marker in %s", cacheDir)
	}

	// Second run reuses the cache directly without recopying the toolchain.
	start := time.Now()
	res2, err := Run(context.Background(), work, ".", "python", []string{"-I", "-c", probe}, limits)
	elapsed := time.Since(start)
	if err != nil || res2.ExitCode != 0 || !strings.Contains(res2.Output, "ok|wrote=False") {
		t.Fatalf("second cached python run failed: %+v %v", res2, err)
	}
	t.Logf("cached python execution completed in %s", elapsed)
}

func TestWindowsReadOnlyNodeModulesCacheAndJunctionIsolation(t *testing.T) {
	if _, err := exec.LookPath("python.exe"); err != nil {
		t.Skip("python is not installed")
	}
	cacheDir := filepath.Join(t.TempDir(), "cache")
	projectRoot := filepath.Join(t.TempDir(), "project")
	depDir := filepath.Join(projectRoot, "node_modules", "demo-helper")
	if err := os.MkdirAll(depDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(depDir, "index.js"), []byte(`module.exports = "from-readonly-dep";`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "package.json"), []byte(`{"name":"app"}`), 0600); err != nil {
		t.Fatal(err)
	}

	work := filepath.Join(t.TempDir(), "work")
	if err := os.Mkdir(work, 0700); err != nil {
		t.Fatal(err)
	}

	roSID, err := readonlyCapability()
	if err != nil || roSID == nil {
		t.Skip("named LPAC capability unavailable")
	}
	_, cleanup := mountReadOnlyNodeModules(context.Background(), projectRoot, work, ".", cacheDir, roSID)

	// Inside LPAC, verify the mounted node_modules is readable AND strictly read-only.
	script := `from pathlib import Path
txt = Path('node_modules/demo-helper/index.js').read_text(encoding='utf-8')
wrote = False
try:
    Path('node_modules/demo-helper/pwn.txt').write_text('bad', encoding='utf-8')
    wrote = True
except OSError:
    pass
print(f'{txt}|wrote={wrote}')`
	res, runErr := Run(context.Background(), work, ".", "python", []string{"-I", "-c", script}, Limits{CacheDir: cacheDir})
	cleanup()
	if runErr != nil || res.ExitCode != 0 || !strings.Contains(res.Output, "from-readonly-dep") || !strings.Contains(res.Output, "wrote=False") {
		t.Fatalf("read-only node_modules verification failed: %+v %v", res, runErr)
	}
	if _, err := os.Lstat(filepath.Join(work, "node_modules")); !os.IsNotExist(err) {
		t.Fatalf("expected temporary node_modules junction to be removed, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(depDir, "pwn.txt")); !os.IsNotExist(err) {
		t.Fatal("host project node_modules was mutated")
	}
}
