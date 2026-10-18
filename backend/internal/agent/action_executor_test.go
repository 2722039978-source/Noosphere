package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestActionCommandHelper(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "--" {
		return
	}
	mode := os.Args[len(os.Args)-1]
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
	output, err := executeCommand(ctx, t.TempDir(), os.Args[0], "-test.run=TestActionCommandHelper", "--", "output")
	if err != nil {
		t.Fatal(err)
	}
	if len(output) > 33000 || !strings.Contains(output, "truncated") {
		t.Fatal("output not bounded")
	}
}
func TestNativeCancellationTerminatesTestChildren(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := executeCommand(ctx, dir, os.Args[0], "-test.run=TestActionCommandHelper", "--", "hold")
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
