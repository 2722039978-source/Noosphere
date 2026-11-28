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
			if _, e := exec.LookPath(c.program + ".exe"); e != nil {
				t.Skip("tool is not installed")
			}
			work := filepath.Join(t.TempDir(), "work")
			if e := os.Mkdir(work, 0700); e != nil {
				t.Fatal(e)
			}
			res, e := Run(context.Background(), work, ".", c.program, c.args)
			if e != nil || res.ExitCode != 0 || !strings.Contains(res.Output, c.marker) {
				t.Fatalf("%+v %v", res, e)
			}
		})
	}
}
