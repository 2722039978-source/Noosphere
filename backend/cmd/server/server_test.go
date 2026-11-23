package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/noosphere/backend/internal/config"
	"github.com/noosphere/backend/internal/gateway"
	"github.com/noosphere/backend/internal/workspaces"
)

func TestQueuedAgentUsesModelConfigurationAtSubmission(t *testing.T) {
	type request struct {
		Path  string
		Model string `json:"model"`
	}
	calls := make(chan request, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var call request
		if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
			t.Error(err)
		}
		call.Path = r.URL.Path
		calls <- call
		io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"Fixture result"}}]}`)
	}))
	defer upstream.Close()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "models.yaml")
	if err := os.WriteFile(configPath, []byte(fmt.Sprintf("default_model: pinned\nproviders:\n  - id: pinned\n    type: openai_compatible\n    enabled: true\n    model: before-edit\n    base_url: %s/before/v1\n", upstream.URL)), 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := workspaces.Open(filepath.Join(dir, "workspace.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	gw := gateway.New(configPath)
	gw.SetWorkspaceManager(manager)
	registerAgentWorker(manager, gw, nil, false)

	// Occupy both interactive workers so model edits occur after admission
	// and before the Agent reads its first request parameters.
	occupied, release := make(chan struct{}, 2), make(chan struct{})
	manager.Register("diagnosis", "interactive", func(ctx context.Context, _ workspaces.Workspace, _ json.RawMessage, _ func(string, string, any)) (any, error) {
		occupied <- struct{}{}
		select {
		case <-release:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	deadline, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i := 0; i < 2; i++ {
		if _, err := manager.Submit("local", "diagnosis", "占用工作线程", nil); err != nil {
			t.Fatal(err)
		}
		select {
		case <-occupied:
		case <-deadline.Done():
			t.Fatal("interactive worker did not start")
		}
	}
	run, err := manager.Submit("local", "agent", "模型配置快照", json.RawMessage(`{"goal":"Read the project summary","session_id":"queued-session","model_id":"pinned"}`))
	if err != nil || run.ModelID != "pinned" {
		t.Fatalf("submit Agent returned %+v, %v", run, err)
	}
	body := fmt.Sprintf(`{"id":"pinned","provider":"openai_compatible","model_name":"after-edit","base_url":%q}`, upstream.URL+"/after/v1")
	saved := httptest.NewRecorder()
	gw.HandleAddModel(saved, httptest.NewRequest(http.MethodPost, "/api/gateway/models", strings.NewReader(body)))
	if saved.Code != http.StatusCreated {
		t.Fatalf("edit model returned %d: %s", saved.Code, saved.Body.String())
	}
	close(release)
	select {
	case got := <-calls:
		if want := (request{Path: "/before/v1/chat/completions", Model: "before-edit"}); got != want {
			t.Fatalf("Agent used %+v; wanted admitted model %+v", got, want)
		}
	case <-deadline.Done():
		current, _ := manager.Run(run.ID)
		t.Fatalf("Agent never contacted the provider: %+v", current)
	}
}

func TestManagedServerUsesOwnedLoopbackAndStopsWhenParentCloses(t *testing.T) {
	t.Setenv("NOOSPHERE_BACKEND_TOKEN", "private-test-credential")
	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	// A stale standalone configuration must have no effect on desktop mode.
	cfg.Server.Host, cfg.Server.Port = "192.0.2.1", 8080
	parent, control := io.Pipe()
	defer parent.Close()
	defer control.Close()
	ready, output := io.Pipe()
	defer ready.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "owned")
	})
	stopped := make(chan error, 1)
	go func() {
		stopped <- serve(&App{Config: cfg, Mux: mux}, true, parent, output)
		output.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	report := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(ready).ReadString('\n')
		report <- strings.TrimSpace(line)
	}()
	var line string
	select {
	case line = <-report:
	case <-ctx.Done():
		t.Fatal("managed listener did not report ready")
	}
	port, err := strconv.Atoi(strings.TrimPrefix(line, managedReadyPrefix))
	if err != nil || port <= 0 || port == 8080 {
		t.Fatalf("invalid managed listener report: %q", line)
	}

	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	for _, tc := range []struct {
		authorization string
		want          int
	}{
		{"", http.StatusUnauthorized},
		{"Bearer private-test-credential", http.StatusOK},
	} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/health", port), nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", tc.authorization)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Fatalf("HTTP status %d, want %d", resp.StatusCode, tc.want)
		}
	}
	control.Close()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("managed server did not exit after closing the parent pipe")
	}
}
