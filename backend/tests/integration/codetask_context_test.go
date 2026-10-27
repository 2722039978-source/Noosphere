//go:build integration

package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/noosphere/backend/internal/codelens"
	"github.com/noosphere/backend/internal/modellab"
)

// TestCodeTaskRealPythonContext is intentionally opt-in via the integration
// build tag and fails (never skips) if an explicit working interpreter is absent.
// It launches exactly one owned worker, never adopts an existing service.
func TestCodeTaskRealPythonContext(t *testing.T) {
	python := os.Getenv("NOOSPHERE_TEST_CODELENS_PYTHON")
	if python == "" || !filepath.IsAbs(python) {
		t.Fatal("set NOOSPHERE_TEST_CODELENS_PYTHON to an existing absolute Python executable")
	}
	if info, err := os.Stat(python); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("Python executable unavailable: %v", err)
	}
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("source location unavailable")
	}
	script := filepath.Clean(filepath.Join(filepath.Dir(filename), "../../python/codelens/main.py"))
	port := freePort(t)
	adapter := codelens.NewAdapter([]string{python}, script, port)
	if err := adapter.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			adapter.Stop()
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	// Establish the real Python listener requires worker credentials. No token is
	// exposed to this package; successful typed calls use Adapter authentication.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/api/v1/health", port), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("unauthenticated worker HTTP %d", resp.StatusCode)
	}
	dataset, err := modellab.LoadDataset(modellab.DatasetID)
	if err != nil {
		t.Fatal(err)
	}
	builder, err := modellab.NewContextBuilder(adapter, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer builder.Close()
	hash := func(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
	count := 0
	for _, ref := range dataset.Cases() {
		t.Run(ref.ID(), func(t *testing.T) {
			e, err := dataset.Evidence(ref)
			if err != nil {
				t.Fatal(err)
			}
			frozen, err := builder.BuildContext(ctx, dataset, ref)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(frozen.Public)
			if frozen.ContextHash != hash(raw) || frozen.PromptHash != hash([]byte(frozen.Prompt)) || frozen.AnalysisResponseHash != hash(frozen.AnalysisResponse) || frozen.HealthResponseHash != hash(frozen.HealthResponse) {
				t.Fatal("frozen hashes do not match actual evidence bytes")
			}
			if frozen.Public.CodeLensVersion != "0.6.0" || frozen.Public.Parser != "tree-sitter" || frozen.Public.Language != "go" || frozen.Public.Symbol.Name != e.Function || frozen.Public.HostFixtureHash != hash([]byte(e.Faulty)) || frozen.CaseHash != e.CaseHash || frozen.DatasetHash != dataset.Revision().ContentHash {
				t.Fatalf("unexpected actual protocol evidence: %+v", frozen.Public)
			}
			if strings.Contains(frozen.Prompt, e.Reference) || strings.Contains(frozen.Prompt, "assertions") || filepath.IsAbs(frozen.Public.File) {
				t.Fatal("prompt leaked hidden data or machine path")
			}
			if frozen.Public.Symbol.StartByte <= 0 || frozen.Public.Symbol.EndByte > len(e.Faulty) || frozen.Public.Symbol.Line < 3 {
				t.Fatal("bad Go function range")
			}
			count++
			t.Logf("real Python version=%s parser=%s host_fixture_sha256=%s context_sha256=%s", frozen.Public.CodeLensVersion, frozen.Public.Parser, frozen.Public.HostFixtureHash, frozen.ContextHash)
		})
	}
	if count != 12 {
		t.Fatalf("analyzed %d/12 real fixtures", count)
	}
	adapter.Stop()
	stopped = true
	// Real Uvicorn/Windows teardown is asynchronous after the launched process
	// is reaped. Diagnose the immediate condition, then require both connection
	// refusal and exclusive port reuse within a bounded cleanup deadline.
	shutdownStart := time.Now()
	address := fmt.Sprintf("127.0.0.1:%d", port)
	conn, dialErr := net.DialTimeout("tcp", address, 100*time.Millisecond)
	if dialErr == nil {
		conn.Close()
		t.Log("immediate post-Stop TCP connection accepted")
	}
	if runtime.GOOS == "windows" {
		raw, netstatErr := exec.Command("netstat.exe", "-ano", "-p", "tcp").CombinedOutput()
		if netstatErr != nil {
			t.Fatalf("shutdown listener diagnostic: %v", netstatErr)
		}
		for _, line := range strings.Split(string(raw), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 5 || fields[1] != address {
				continue
			}
			t.Logf("immediate owned-port socket: %s", strings.TrimSpace(line))
			if fields[3] == "LISTENING" {
				pid := fields[4]
				if strings.Trim(pid, "0123456789") != "" {
					t.Fatal("invalid socket PID")
				}
				query := fmt.Sprintf("Get-CimInstance Win32_Process -Filter 'ProcessId = %s OR ParentProcessId = %s' | Select-Object ProcessId,ParentProcessId,Name,CommandLine | ConvertTo-Json -Compress", pid, pid)
				tree, treeErr := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", query).CombinedOutput()
				if treeErr != nil {
					t.Fatalf("owned-port process diagnostic: %v", treeErr)
				}
				t.Logf("owned-port process/tree after Stop: %s", strings.TrimSpace(string(tree)))
			}
		}
	}
	deadline := shutdownStart.Add(3 * time.Second)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	var bindErr error
	for {
		conn, dialErr = net.DialTimeout("tcp", address, 100*time.Millisecond)
		if dialErr == nil {
			conn.Close()
		} else if timeout, ok := dialErr.(net.Error); !ok || !timeout.Timeout() {
			listener, err := net.Listen("tcp", address)
			bindErr = err
			if err == nil {
				listener.Close()
				t.Logf("owned worker connection refused and port reusable after %s", time.Since(shutdownStart))
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("owned worker failed bounded teardown: connection error=%v bind error=%v", dialErr, bindErr)
		}
		<-ticker.C
	}
}
