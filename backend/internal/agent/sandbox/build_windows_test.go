package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWindowsNPMRunsWorkspaceBuild(t *testing.T) {
	if _, err := exec.LookPath("npm.cmd"); err != nil {
		t.Skip("npm is not installed")
	}
	work := t.TempDir()
	manifest := []byte(`{"name":"sandbox-fixture","scripts":{"build":"node build.cjs"}}`)
	if err := os.WriteFile(filepath.Join(work, "package.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "build.cjs"), []byte(`require('node:fs').writeFileSync('result.txt','offline build complete');`), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := Run(t.Context(), work, ".", "npm", []string{"run", "build"})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("Build failed: %+v %v", result, err)
	}
	got, err := os.ReadFile(filepath.Join(work, "result.txt"))
	if err != nil || string(got) != "offline build complete" {
		t.Fatalf("%q %v", got, err)
	}
}
