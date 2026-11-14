package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/noosphere/backend/internal/config"
)

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
