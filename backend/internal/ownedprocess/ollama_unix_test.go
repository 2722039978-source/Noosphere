//go:build linux || darwin

package ownedprocess

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestOllamaFixtureListener(t *testing.T) {
	if os.Getenv("NOOSPHERE_OLLAMA_FIXTURE_BIN") == "" {
		return
	}
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := listener.SetDeadline(time.Now().Add(20 * time.Second)); err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(os.Stdout, listener.Addr())
	if conn, err := listener.Accept(); err == nil {
		conn.Close()
	}
}

func TestOllamaFixtureOwner(t *testing.T) {
	engine := os.Getenv("NOOSPHERE_OLLAMA_FIXTURE_ENGINE")
	if engine == "" {
		return
	}
	child := exec.Command(os.Args[0], ollamaSupervisorFlag, engine)
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	control, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	status, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	logs, err := child.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err := control.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	var pid int
	if _, err := fmt.Fscanln(status, &pid); err != nil || pid <= 0 || pid == child.Process.Pid {
		t.Fatalf("supervisor did not report actual engine PID: %d, %v", pid, err)
	}
	fmt.Fprintln(os.Stdout, listenAddress(t, logs))
	_ = child.Wait()
}

func TestOllamaOwnerDeathReleasesOnlyItsListener(t *testing.T) {
	engine := filepath.Join(t.TempDir(), "ollama")
	const fixture = "#!/bin/sh\n[ \"$1\" = serve ] || exit 9\nexec \"$NOOSPHERE_OLLAMA_FIXTURE_BIN\" -test.run=^TestOllamaFixtureListener$\n"
	if err := os.WriteFile(engine, []byte(fixture), 0700); err != nil {
		t.Fatal(err)
	}
	external, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer external.Close()
	owner := exec.Command(os.Args[0], "-test.run=^TestOllamaFixtureOwner$")
	owner.Env = append(os.Environ(), "NOOSPHERE_OLLAMA_FIXTURE_BIN="+os.Args[0], "NOOSPHERE_OLLAMA_FIXTURE_ENGINE="+engine)
	owner.Stderr = os.Stderr
	stdout, err := owner.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = owner.Process.Kill()
		_ = owner.Wait()
	})
	address := listenAddress(t, stdout)
	if err := owner.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = owner.Wait()
	assertReleased(t, address)
	connected, err := net.DialTimeout("tcp4", external.Addr().String(), time.Second)
	if err != nil {
		t.Fatalf("external listener stopped: %v", err)
	}
	connected.Close()
}
