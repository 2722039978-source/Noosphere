package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/noosphere/backend/internal/ownedprocess"
)

func TestMain(m *testing.M) {
	ownedprocess.RunSupervisor()
	os.Exit(m.Run())
}

func runTestHelper(ctx context.Context, dir, mode string) (string, error) {
	cmd := exec.Command(os.Args[0], "-test.run=TestActionCommandHelper", "--", mode)
	cmd.Dir = dir
	cmd.Env = ownedprocess.BaseEnv()
	return executeCommand(ctx, cmd, nil)
}

func TestActionCommandHelper(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "--" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	if mode == "failure" {
		fmt.Print("fixture failed")
		os.Exit(7)
	}
	if mode == "output" {
		if os.Getenv("NOOSPHERE_BACKEND_TOKEN") != "" {
			os.Exit(2)
		}
		fmt.Print(strings.Repeat("x", 40000))
		os.Exit(0)
	}
	if mode == "child" {
		time.Sleep(3 * time.Second)
		os.WriteFile("escaped-child", []byte("unexpected"), 0600)
		os.Exit(0)
	}
	if mode == "hold" {
		cmd := exec.Command(os.Args[0], "-test.run=TestActionCommandHelper", "--", "child")
		if cmd.Start() != nil {
			os.Exit(3)
		}
		os.WriteFile("parent-ready", []byte("ready"), 0600)
		time.Sleep(10 * time.Second)
		os.Exit(0)
	}
}
func TestNativeCommandsBoundOutputAndRemoveLocalCredential(t *testing.T) {
	t.Setenv("NOOSPHERE_BACKEND_TOKEN", "fixture-secret")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := runTestHelper(ctx, t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	if len(output) > 33000 || !strings.Contains(output, "truncated") {
		t.Fatal("output not bounded")
	}
}

func TestNativeCommandsPreserveNonzeroExitCode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := runTestHelper(ctx, t.TempDir(), "failure")
	var status interface{ ExitCode() int }
	if !errors.As(err, &status) || status.ExitCode() != 7 || output != "fixture failed" {
		t.Fatalf("process result lost: output %q, error %v", output, err)
	}
}
func TestNativeCancellationTerminatesTestChildren(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := runTestHelper(ctx, dir, "hold")
		done <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "parent-ready")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper failed to start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancel returned success")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel failed to stop command")
	}
	time.Sleep(3200 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(dir, "escaped-child")); !os.IsNotExist(err) {
		t.Fatal("test child survived cancellation")
	}
}
