package sandbox

import (
	"bytes"
	"os/exec"
	"strconv"
	"sync"
)

var bubblewrapSizedTmpfs = sync.OnceValue(func() bool {
	cmd := exec.Command("/usr/bin/bwrap", "--help")
	cmd.Env = []string{"LC_ALL=C"}
	out, err := cmd.Output()
	return err == nil && bytes.Contains(out, []byte("--size "))
})

// BubblewrapScratch preserves kernel tmpfs limits when supported. The caller
// monitors the backing directory when the system bubblewrap is older.
func BubblewrapScratch(backing, target string, limit int64) []string {
	if bubblewrapSizedTmpfs() {
		return []string{"--size", strconv.FormatInt(limit, 10), "--tmpfs", target}
	}
	return []string{"--bind", backing, target}
}
