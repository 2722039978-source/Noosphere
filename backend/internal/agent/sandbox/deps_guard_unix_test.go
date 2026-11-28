//go:build !windows

package sandbox

import (
	"os"
	"testing"
)

func createDirLinkForTest(t *testing.T, linkPath, targetDir string) {
	t.Helper()
	if err := os.Symlink(targetDir, linkPath); err != nil {
		t.Fatal(err)
	}
}
