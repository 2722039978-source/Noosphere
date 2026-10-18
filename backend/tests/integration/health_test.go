//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

// Launches real Go/Python processes, never user data/config or a model API.
func TestAuthenticatedCodeIndexAndRetrieval(t *testing.T) {
	root, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatal("cannot find backend root")
		}
		root = parent
	}
	python := os.Getenv("NOOSPHERE_TEST_PYTHON")
	if python == "" {
		t.Skip("set NOOSPHERE_TEST_PYTHON to a Python with CodeLens dependencies")
	}
	temporary := t.TempDir()
	bin := filepath.Join(temporary, "backend.exe")
	build := exec.Command("go", "build", "-o", bin, "./cmd/server")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	port, workerPort := freePort(t), freePort(t)
	cfg := fmt.Sprintf("server:\n  host: 127.0.0.1\n  port: %d\nmodules:\n  nebula:\n    enabled: true\n    data_dir: ./nebula\n    embedding:\n      provider: mock\n  devops:\n    enabled: false\n  codelens:\n    enabled: true\n    python_path: '%s'\n    port: %d\ngateway:\n  config_path: ./models.yaml\nlogging:\n  dir: ./logs\n", port, python, workerPort)
	cfgPath := filepath.Join(temporary, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(temporary, "models.yaml"), []byte("default_model: ''\nproviders: []\n"), 0600)
	cmd := exec.Command(bin, "-config", cfgPath)
	cmd.Dir = temporary
	cmd.Env = append(os.Environ(), "NOOSPHERE_BACKEND_TOKEN=integration-fixture-token", "NOOSPHERE_CODELENS_SCRIPT="+filepath.Join(root, "python", "codelens", "main.py"))
	logFile, err := os.Create(filepath.Join(temporary, "process.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if runtime.GOOS == "windows" {
			exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F").Run()
		} else {
			cmd.Process.Kill()
		}
		cmd.Wait()
		logFile.Close()
		if t.Failed() {
			data, _ := os.ReadFile(logFile.Name())
			t.Log(string(data))
		}
	}()
	client := &http.Client{Timeout: 10 * time.Second}
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	request := func(method, path string, body any, authorized bool) (int, []byte, error) {
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		req, _ := http.NewRequest(method, base+path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		if authorized {
			req.Header.Set("Authorization", "Bearer integration-fixture-token")
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		return resp.StatusCode, data, err
	}
	ready := false
	for i := 0; i < 100; i++ {
		status, _, err := request("GET", "/api/v1/codelens/health", nil, true)
		if err == nil && status == 200 {
			ready = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !ready {
		t.Fatal("CodeLens not ready")
	}
	if status, _, _ := request("GET", "/health", nil, false); status != 401 {
		t.Fatalf("unauthenticated health status=%d", status)
	}
	if status, _, _ := request("GET", "/health", nil, true); status != 200 {
		t.Fatalf("authenticated health status=%d", status)
	}
	project := filepath.Join(temporary, "fixture")
	os.Mkdir(project, 0700)
	os.WriteFile(filepath.Join(project, "payment.py"), []byte("def refund_invoice():\n    return '退款处理 ledger'\n"), 0600)
	status, body, err := request("POST", "/api/v1/codelens/index", map[string]any{"project_path": project}, true)
	if err != nil || status != 200 {
		t.Fatalf("index status=%d err=%v body=%s", status, err, body)
	}
	var indexed struct {
		ProjectID string `json:"project_id"`
		Ready     bool   `json:"retrieval_ready"`
		Chunks    int    `json:"indexed_chunks"`
	}
	json.Unmarshal(body, &indexed)
	if !indexed.Ready || indexed.Chunks == 0 {
		t.Fatalf("index not persisted: %s", body)
	}
	status, body, err = request("POST", "/api/v1/sessions/"+indexed.ProjectID+"/search", map[string]any{"query": "退款处理", "top_k": 3}, true)
	if err != nil || status != 200 || !bytes.Contains(body, []byte("payment.py")) || !bytes.Contains(body, []byte("start_line")) {
		t.Fatalf("retrieval: status=%d err=%v body=%s", status, err, body)
	}
	os.Remove(filepath.Join(project, "payment.py"))
	if status, body, err = request("POST", "/api/v1/codelens/index", map[string]any{"project_path": project}, true); err != nil || status != 200 {
		t.Fatalf("reindex %d %s %v", status, body, err)
	}
	_, body, _ = request("POST", "/api/v1/sessions/"+indexed.ProjectID+"/search", map[string]any{"query": "退款处理", "top_k": 3}, true)
	if bytes.Contains(body, []byte("payment.py")) {
		t.Fatalf("deleted source still retrieved: %s", body)
	}
}
