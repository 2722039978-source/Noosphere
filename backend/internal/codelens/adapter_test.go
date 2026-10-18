package codelens

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Reuse the test executable as a deterministic worker; no Python install or model API needed.
func TestWorkerHelper(t *testing.T) {
	if os.Getenv("NOOSPHERE_TEST_WORKER") != "1" {
		return
	}
	if os.Args[len(os.Args)-1] == "exit" {
		os.Exit(17)
	}
	port := os.Getenv("NOOSPHERE_CODELENS_PORT")
	http.HandleFunc("/api/v1/health", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "ok") })
	if err := http.ListenAndServe("127.0.0.1:"+port, nil); err != nil {
		os.Exit(18)
	}
	os.Exit(0)
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func helper(t *testing.T, mode string, port int) *Adapter {
	t.Helper()
	t.Setenv("NOOSPHERE_TEST_WORKER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return NewAdapter([]string{executable, "-test.run=^TestWorkerHelper$", "--"}, mode, port)
}

func TestExitedWorkerFailsPromptlyAndIsReaped(t *testing.T) {
	a := helper(t, "exit", freePort(t))
	started := time.Now()
	err := a.Start()
	if err == nil || !strings.Contains(err.Error(), "exited before ready") {
		t.Fatalf("unexpected result: %v", err)
	}
	if time.Since(started) > 5*time.Second {
		t.Fatal("early exit should not wait for health timeout")
	}
	if a.cmd.ProcessState == nil {
		t.Fatal("child not reaped")
	}
	a.Stop() // repeated shutdown is safe
}

func TestWorkerUsesConfiguredPortAndStopReleasesIt(t *testing.T) {
	port := freePort(t)
	a := helper(t, "serve", port)
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Stop)
	if !a.Healthy() {
		t.Fatal("worker not healthy")
	}
	a.Stop()
	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatalf("worker port still owned after Stop: %v", err)
	}
	l.Close()
}

func TestOccupiedPortDoesNotSpawnOrAdoptAnotherWorker(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	a := helper(t, "serve", l.Addr().(*net.TCPAddr).Port)
	if err := a.Start(); err == nil || !strings.Contains(err.Error(), "occupied") {
		t.Fatalf("unexpected result: %v", err)
	}
	if a.cmd != nil {
		t.Fatal("must not start a process on an occupied port")
	}
}
