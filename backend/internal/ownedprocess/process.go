// Package ownedprocess supervises processes launched by the backend. It does
// not isolate project code from the current user's operating-system permissions.
package ownedprocess

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

type Process struct {
	stop func()
	once sync.Once
	done chan struct{}
	err  error
}

// Start takes an unstarted command created with exec.Command. The caller may
// supply an open working directory on Unix to pin it across the launch.
func Start(ctx context.Context, cmd *exec.Cmd, directory *os.File) (*Process, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cmd.Err != nil {
		return nil, cmd.Err
	}
	if !filepath.IsAbs(cmd.Path) || cmd.Env == nil {
		return nil, fmt.Errorf("owned commands require an absolute executable and an explicit environment")
	}
	cmd.WaitDelay = 2 * time.Second
	wait, stop, err := startProcess(cmd, directory)
	if err != nil {
		return nil, err
	}
	p := &Process{stop: stop, done: make(chan struct{})}
	unregister := context.AfterFunc(ctx, p.Stop)
	go func() {
		err := wait()
		unregister()
		p.Stop()
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		p.err = err
		close(p.done)
	}()
	return p, nil
}

func Run(ctx context.Context, cmd *exec.Cmd, directory *os.File) error {
	p, err := Start(ctx, cmd, directory)
	if err != nil {
		return err
	}
	return p.Wait()
}

func (p *Process) Stop() { p.once.Do(p.stop) }
func (p *Process) Wait() error {
	<-p.done
	return p.err
}

// BaseEnv includes only OS/user location and locale settings. In particular it
// excludes credentials, PATH, interpreter injection and network proxy options.
func BaseEnv() []string {
	env := []string{}
	for _, key := range []string{"SystemRoot", "WINDIR", "SystemDrive", "OS", "COMSPEC", "USERPROFILE", "HOMEDRIVE", "HOMEPATH", "LOCALAPPDATA", "APPDATA", "HOME", "TMPDIR", "TEMP", "TMP", "LANG", "LC_ALL", "LC_CTYPE", "TZ"} {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return env
}
