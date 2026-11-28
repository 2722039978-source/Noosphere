//go:build linux

package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/noosphere/backend/internal/ownedprocess"
)

func resolveProgram(program, _ string) (string, error) {
	choices := map[string][]string{"go": {"/usr/local/go/bin/go", "/usr/bin/go"}, "git": {"/usr/bin/git"}, "python": {"/usr/bin/python3"}, "python3": {"/usr/bin/python3"}, "node": {"/usr/bin/node"}, "npm": {"/usr/bin/npm"}, "sh": {"/bin/sh"}, "bash": {"/bin/bash"}, "make": {"/usr/bin/make"}}
	for _, p := range choices[program] {
		if st, e := os.Stat(p); e == nil && st.Mode().IsRegular() {
			return p, nil
		}
	}
	return "", fmt.Errorf("程序未登记或未安装：%s", program)
}

func launch(ctx context.Context, work, cwd, program string, args []string, out io.Writer, limits Limits) (int, error) {
	exe, err := resolveProgram(program, limits.ProjectRoot)
	if err != nil {
		return -1, err
	}
	const wrapper = "/usr/bin/bwrap"
	if _, err := os.Stat(wrapper); err != nil {
		return -1, errors.New("缺少 bubblewrap 工作区隔离器")
	}
	opts := []string{"--die-with-parent", "--new-session", "--unshare-all", "--cap-drop", "ALL", "--clearenv", "--ro-bind", "/usr", "/usr"}
	for _, p := range []string{"/bin", "/lib", "/lib64", "/sbin"} {
		if _, err := os.Stat(p); err == nil {
			opts = append(opts, "--ro-bind", p, p)
		}
	}
	opts = append(opts, "--size", "1048576", "--tmpfs", "/usr/local")
	if _, e := os.Stat("/usr/local/go"); e == nil {
		opts = append(opts, "--ro-bind", "/usr/local/go", "/usr/local/go")
	}
	opts = append(opts, "--proc", "/proc", "--dev", "/dev", "--size", strconv.FormatInt(limits.DiskBytes, 10), "--tmpfs", "/tmp", "--dir", "/tmp/home", "--dir", "/tmp/cache", "--bind", work, "/work", "--chdir", filepath.Join("/work", cwd), "--setenv", "HOME", "/tmp/home", "--setenv", "TMPDIR", "/tmp", "--setenv", "GIT_CONFIG_NOSYSTEM", "1", "--setenv", "GIT_TERMINAL_PROMPT", "0", "--setenv", "GOTOOLCHAIN", "local", "--setenv", "GOPROXY", "off", "--setenv", "GOENV", "off", "--setenv", "PATH", "/usr/local/go/bin:/usr/bin:/bin", "--", exe)
	cmd := exec.Command(wrapper, append(opts, args...)...)
	cmd.Env = []string{"LANG=C.UTF-8"}
	cmd.Stdout = out
	cmd.Stderr = out
	err = ownedprocess.Run(ctx, cmd, nil)
	if err != nil {
		var status interface{ ExitCode() int }
		if errors.As(err, &status) {
			return status.ExitCode(), nil
		}
		return -1, err
	}
	return 0, nil
}
