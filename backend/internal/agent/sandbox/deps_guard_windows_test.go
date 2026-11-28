package sandbox

import "testing"

func createDirLinkForTest(t *testing.T, linkPath, targetDir string) {
	t.Helper()
	if err := createDirectoryJunction(linkPath, targetDir); err != nil {
		t.Fatal(err)
	}
}
