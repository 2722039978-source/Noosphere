package sandbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsToolchains(t *testing.T) {
	for _, c := range []struct {
		program string
		args    []string
		marker  string
	}{{"node", []string{"-e", "console.log('native-node')"}, "native-node"}, {"git", []string{"--version"}, "git version"}, {"python", []string{"-I", "-c", "print('native-python')"}, "native-python"}, {"go", []string{"version"}, "go version"}} {
		t.Run(c.program, func(t *testing.T) {
			path, e := exec.LookPath(c.program + ".exe")
			if e != nil {
				t.Skip("tool is not installed")
			}
			work := filepath.Join(t.TempDir(), "work")
			if c.program == "go" && len(work) < 179 {
				work = filepath.Join(work, strings.Repeat("d", 179-len(work)))
			}
			if e := os.MkdirAll(work, 0700); e != nil {
				t.Fatal(e)
			}
			res, e := Run(context.Background(), work, ".", c.program, c.args)
			if e != nil || res.ExitCode != 0 || !strings.Contains(res.Output, c.marker) {
				resolved, lookupErr := filepath.EvalSymlinks(path)
				t.Fatalf("tool %q (resolved %q, error %v): %+v %v", path, resolved, lookupErr, res, e)
			}
		})
	}
}
