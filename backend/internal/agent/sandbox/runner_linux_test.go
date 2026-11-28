package sandbox

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/noosphere/backend/internal/ownedprocess"
)

func TestMain(m *testing.M) { ownedprocess.RunSupervisor(); os.Exit(m.Run()) }
func TestLinuxIsolatedCommand(t *testing.T) {
	if _, e := os.Stat("/usr/bin/bwrap"); e != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("bubblewrap required in Linux CI", e)
		}
		t.Skip("bubblewrap is not installed")
	}
	tmp := t.TempDir()
	work := filepath.Join(tmp, "work")
	if e := os.Mkdir(work, 0700); e != nil {
		t.Fatal(e)
	}
	secret := filepath.Join(tmp, "secret")
	if e := os.WriteFile(secret, []byte("private-test-value"), 0600); e != nil {
		t.Fatal(e)
	}
	svc, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer svc.Close()
	t.Setenv("NOOSPHERE_BACKEND_TOKEN", "credential-fixture")
	script := "import os,sys,socket; assert not os.path.exists(sys.argv[1]); assert 'NOOSPHERE_BACKEND_TOKEN' not in os.environ; assert socket.socket().connect_ex(('127.0.0.1',int(sys.argv[2])))!=0; open('result','w').write('ok');print('isolated')"
	got, e := Run(t.Context(), work, ".", "python", []string{"-c", script, secret, strconv.Itoa(svc.Addr().(*net.TCPAddr).Port)})
	if e != nil || got.ExitCode != 0 || !strings.Contains(got.Output, "isolated") {
		t.Fatalf("%+v %v", got, e)
	}
	b, e := os.ReadFile(filepath.Join(work, "result"))
	if string(b) != "ok" || e != nil {
		t.Fatal(string(b), e)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	_, e = Run(ctx, work, ".", "sh", []string{"-c", "(sleep 1; echo stray > orphan) & wait"})
	if e == nil {
		t.Fatal("expected cancellation")
	}
	time.Sleep(time.Second)
	if _, e = os.Stat(filepath.Join(work, "orphan")); !os.IsNotExist(e) {
		t.Fatal("child outlived cancellation")
	}
}

func TestLinuxPrivateScratchBudget(t *testing.T) {
	if _, err := os.Stat("/usr/bin/bwrap"); err != nil {
		t.Skip("bubblewrap is not installed")
	}
	result, err := Run(t.Context(), t.TempDir(), ".", "python", []string{"-c",
		"from pathlib import Path; Path('/tmp/scratch.bin').write_bytes(bytes(2 * 1024 * 1024))",
	}, Limits{DiskBytes: 1 << 20})
	switch {
	case err != nil && strings.Contains(err.Error(), "磁盘预算"):
		return
	case result.ExitCode != 0 && strings.Contains(result.Output, "No space left on device"):
		return
	default:
		t.Fatalf("scratch escaped budget: %+v, %v", result, err)
	}
}
