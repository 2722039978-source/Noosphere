package host

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"strings"

	"github.com/noosphere/backend/internal/ownedprocess"
)

// PrepareGit imports the authorized baseline into a private database. The real repository's hooks,
// remotes, credential helpers, alternates and uncommitted index never enter the runner.
func (d *Draft) PrepareGit(ctx context.Context) error {
	if d.history {
		return errors.New("历史记录只读")
	}
	unlock, e := d.lock(ctx)
	if e != nil {
		return e
	}
	defer unlock()
	target := filepath.Join(d.WorkingDir(), ".git")
	if st, err := os.Lstat(target); err == nil {
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return errors.New("副本 Git 元数据目录已更换")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	j, e := d.load()
	if e != nil {
		return e
	}
	if j.Status != "draft" {
		return errors.New("工作副本已冻结")
	}
	exe, e := exec.LookPath("git")
	if e != nil {
		return e
	}
	exe, e = filepath.EvalSymlinks(exe)
	if e != nil || !filepath.IsAbs(exe) || isInside(d.root, exe) || isInside(d.base, exe) {
		return errors.New("需要项目之外的可信 Git 可执行文件")
	}
	tmp, e := os.MkdirTemp(d.state, ".baseline-git-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(tmp)
	gitDir := filepath.Join(tmp, "repo")
	run := func(input io.Reader, args ...string) error {
		cmd := exec.Command(exe, append([]string{"-c", "gc.auto=0", "--git-dir=" + gitDir, "--work-tree=" + tmp}, args...)...)
		cmd.Dir = tmp
		cmd.Env = []string{"HOME=" + tmp, "USERPROFILE=" + tmp, "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "PATH=" + filepath.Dir(exe)}
		if sys := os.Getenv("SystemRoot"); sys != "" {
			cmd.Env = append(cmd.Env, "SystemRoot="+sys, "WINDIR="+sys)
		}
		cmd.Stdin = input
		if e := ownedprocess.Run(ctx, cmd, nil); e != nil {
			return fmt.Errorf("初始化私有 Git 数据：%w", e)
		}
		return nil
	}
	if e = run(nil, "init", "--quiet", "--template=", "--initial-branch=workspace"); e != nil {
		return e
	}
	reader, writer := io.Pipe()
	wrote := make(chan error, 1)
	go func() {
		e := d.importBaseline(ctx, writer, j)
		writer.CloseWithError(e)
		wrote <- e
	}()
	e = run(reader, "fast-import", "--quiet", "--done")
	reader.CloseWithError(e)
	if err := <-wrote; err != nil && e == nil {
		e = err
	}
	if e != nil {
		return e
	}
	if e = run(nil, "read-tree", "HEAD"); e != nil {
		return e
	}
	if e = run(nil, "config", "user.name", "Noosphere Agent"); e != nil {
		return e
	}
	if e = run(nil, "config", "user.email", "agent@localhost"); e != nil {
		return e
	}
	if e = run(nil, "config", "core.bare", "false"); e != nil {
		return e
	}
	if e = run(nil, "config", "core.worktree", ".."); e != nil {
		return e
	}
	return os.Rename(gitDir, target)
}
func (d *Draft) importBaseline(ctx context.Context, dest io.Writer, j *journal) error {
	out := bufio.NewWriter(dest)
	paths := make([]string, 0, len(j.Base.Files))
	for p := range j.Base.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	fmt.Fprint(out, "commit refs/heads/workspace\ncommitter Noosphere Agent <agent@localhost> 1 +0000\ndata 18\nWorkspace baseline\n")
	for _, p := range paths {
		if e := ctx.Err(); e != nil {
			return e
		}
		v := j.Base.Files[p]
		mode := "100644"
		if v.Mode&0111 != 0 {
			mode = "100755"
		}
		name := gitQuotedPath(p)
		fmt.Fprintf(out, "M %s inline %s\ndata %d\n", mode, name, v.Size)
		f, e := d.OpenBlob(ctx, v.Hash)
		if e != nil {
			return e
		}
		n, e := io.Copy(out, f)
		f.Close()
		if e != nil {
			return e
		}
		if n != v.Size {
			return errors.New("基线文件长度已变化")
		}
		fmt.Fprint(out, "\n")
	}
	fmt.Fprint(out, "\ndone\n")
	return out.Flush()
}
func gitQuotedPath(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, c := range []byte(s) {
		if c < 32 || c >= 127 || c == '"' || c == '\\' {
			fmt.Fprintf(&b, "\\%03o", c)
		} else {
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}
