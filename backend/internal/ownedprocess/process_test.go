package ownedprocess

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	RunSupervisor()
	os.Exit(m.Run())
}

func testCommand(mode, address string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestProcessHelper$", "--", mode, address)
	cmd.Env = append(os.Environ(), "NOOSPHERE_PROCESS_TEST_HELPER=1")
	return cmd
}

func TestProcessHelper(t *testing.T) {
	if os.Getenv("NOOSPHERE_PROCESS_TEST_HELPER") != "1" {
		return
	}
	mode, address := os.Args[len(os.Args)-2], os.Args[len(os.Args)-1]
	switch mode {
	case "listen":
		listener, err := net.Listen("tcp", address)
		if err != nil {
			os.Exit(3)
		}
		fmt.Fprintln(os.Stdout, listener.Addr())
		_, _ = listener.Accept()
	case "child":
		cmd := testCommand("listen", address)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if cmd.Start() != nil {
			os.Exit(4)
		}
		_ = cmd.Wait()
	case "owner":
		cmd := testCommand("child", address)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		child, err := Start(context.Background(), cmd, nil)
		if err != nil {
			os.Exit(5)
		}
		_ = child.Wait()
	case "exit-with-child":
		cmd := testCommand("listen", address)
		output, err := cmd.StdoutPipe()
		if err != nil || cmd.Start() != nil {
			os.Exit(6)
		}
		var bound string
		if _, err := fmt.Fscanln(output, &bound); err != nil {
			os.Exit(7)
		}
		fmt.Fprintln(os.Stdout, bound)
		os.Exit(0)
	}
}

func listenAddress(t *testing.T, reader io.Reader) string {
	t.Helper()
	ch := make(chan string, 1)
	go func() {
		var address string
		_, _ = fmt.Fscanln(reader, &address)
		ch <- address
	}()
	select {
	case address := <-ch:
		if !strings.HasPrefix(address, "127.0.0.1:") {
			t.Fatalf("listener failed to report ownership: %q", address)
		}
		return address
	case <-time.After(10 * time.Second):
		t.Fatal("listener did not report ownership")
		return ""
	}
}

func assertReleased(t *testing.T, address string) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(25 * time.Millisecond) {
		listener, err := net.Listen("tcp", address)
		if err == nil {
			listener.Close()
			return
		}
	}
	t.Fatalf("listener still owned after cleanup: %s", address)
}

func TestCancellationReleasesDescendantListener(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	cmd := testCommand("child", "127.0.0.1:0")
	cmd.Stdout = writer
	child, err := Start(ctx, cmd, nil)
	if err != nil {
		t.Fatal(err)
	}
	address := listenAddress(t, reader)
	cancel()
	if err := child.Wait(); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	assertReleased(t, address)
}

func TestOwnerExitReleasesWorkerListener(t *testing.T) {
	cmd := testCommand("owner", "127.0.0.1:0")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	address := listenAddress(t, stdout)
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	assertReleased(t, address)
}

func TestNormalLeaderExitReleasesDescendants(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	cmd := testCommand("exit-with-child", "127.0.0.1:0")
	cmd.Stdout = writer
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	child, err := Start(ctx, cmd, nil)
	if err != nil {
		t.Fatal(err)
	}
	address := listenAddress(t, reader)
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
	assertReleased(t, address)
}

func TestDeadlineTerminatesWorker(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if err := Run(ctx, testCommand("listen", "127.0.0.1:0"), nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline, got %v", err)
	}
}
