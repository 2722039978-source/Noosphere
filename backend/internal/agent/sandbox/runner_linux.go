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
	"strings"

	"github.com/noosphere/backend/internal/ownedprocess"
)

func resolveProgram(program, _ string) (string, error) {
	choices := map[string][]string{
		"go":      {"/usr/local/go/bin/go", "/usr/bin/go"},
		"git":     {"/usr/bin/git"},
		"python":  {"/usr/bin/python3"},
		"python3": {"/usr/bin/python3"},
		"pytest":  {"/usr/bin/pytest", "/usr/bin/pytest-3"},
		"node":    {"/usr/bin/node", "/usr/local/bin/node"},
		"npm":     {"/usr/bin/npm", "/usr/local/bin/npm"},
		"npx":     {"/usr/bin/npx", "/usr/local/bin/npx"},
		"pnpm":    {"/usr/bin/pnpm", "/usr/local/bin/pnpm"},
		"yarn":    {"/usr/bin/yarn", "/usr/local/bin/yarn"},
		"cargo":   {"/usr/bin/cargo", "/usr/local/bin/cargo"},
		"rustc":   {"/usr/bin/rustc", "/usr/local/bin/rustc"},
		"sh":      {"/bin/sh"},
		"bash":    {"/bin/bash"},
		"make":    {"/usr/bin/make"},
	}
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
	// Bind private writable scratch instead of requiring bubblewrap's newer --size
	// option. Ubuntu 22.04 ships a version without that option.
	runtimeDir, err := os.MkdirTemp(filepath.Dir(work), ".command-")
	if err != nil {
		return -1, err
	}
	defer os.RemoveAll(runtimeDir)
	scratch := filepath.Join(runtimeDir, "tmp")
	localMask := filepath.Join(runtimeDir, "local")
	for _, dir := range []string{scratch, localMask} {
		if err := os.Mkdir(dir, 0700); err != nil {
			return -1, err
		}
	}

	opts := []string{"--die-with-parent", "--new-session", "--unshare-all", "--cap-drop", "ALL", "--clearenv", "--ro-bind", "/usr", "/usr"}
	for _, p := range []string{"/bin", "/lib", "/lib64", "/sbin"} {
		if _, err := os.Stat(p); err == nil {
			opts = append(opts, "--ro-bind", p, p)
		}
	}
	opts = append(opts, "--ro-bind", localMask, "/usr/local")
	if _, e := os.Stat("/usr/local/go"); e == nil {
		if err := os.Mkdir(filepath.Join(localMask, "go"), 0700); err != nil {
			return -1, err
		}
		opts = append(opts, "--ro-bind", "/usr/local/go", "/usr/local/go")
	}
	pathEnv := "/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin"
	opts = append(opts, "--proc", "/proc", "--dev", "/dev")
	opts = append(opts, BubblewrapScratch(scratch, "/tmp", limits.DiskBytes)...)
	opts = append(opts, "--dir", "/tmp/home", "--dir", "/tmp/cache", "--bind", work, "/work")
	if limits.ProjectRoot != "" && (program == "node" || program == "npm" || program == "npx" || program == "pnpm" || program == "yarn") {
		candidates := []string{"."}
		if cwd != "" && cwd != "." {
			candidates = append([]string{filepath.ToSlash(cwd)}, candidates...)
		}
		for _, relDir := range candidates {
			relNM := "node_modules"
			if relDir != "." {
				relNM = relDir + "/node_modules"
			}
			src, ok := safeContainedRealDir(limits.ProjectRoot, relNM)
			if !ok {
				continue
			}
			if _, ok := safeContainedChildPath(work, relDir, "node_modules"); !ok {
				continue
			}
			guestPath := "/work/" + relNM
			opts = append(opts, "--ro-bind", src, guestPath)
			pathEnv = guestPath + "/.bin:" + pathEnv
		}
	}
	if program == "go" {
		if modCache, err := exec.CommandContext(ctx, exe, "env", "GOMODCACHE").Output(); err == nil {
			mc := strings.TrimSpace(string(modCache))
			if resolved, ok := canonicalRealDir(mc); ok {
				opts = append(opts, "--ro-bind", resolved, "/tmp/gomodcache", "--setenv", "GOMODCACHE", "/tmp/gomodcache", "--setenv", "GOCACHE", "/tmp/cache/go-build")
			}
		}
	}
	opts = append(opts, "--chdir", filepath.Join("/work", cwd), "--setenv", "HOME", "/tmp/home", "--setenv", "TMPDIR", "/tmp", "--setenv", "GIT_CONFIG_NOSYSTEM", "1", "--setenv", "GIT_TERMINAL_PROMPT", "0", "--setenv", "GOTOOLCHAIN", "local", "--setenv", "GOPROXY", "off", "--setenv", "GOENV", "off", "--setenv", "PATH", pathEnv, "--", exe)
	cmd := exec.Command(wrapper, append(opts, args...)...)
	cmd.Env = []string{"LANG=C.UTF-8"}
	cmd.Stdout = out
	cmd.Stderr = out
	err = WithDirectoryBudget(ctx, scratch, limits.DiskBytes, func(ctx context.Context) error {
		return ownedprocess.Run(ctx, cmd, nil)
	})
	if err != nil {
		var status interface{ ExitCode() int }
		if errors.As(err, &status) {
			return status.ExitCode(), nil
		}
		return -1, err
	}
	return 0, nil
}
