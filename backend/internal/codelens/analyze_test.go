package codelens

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"strings"
	"testing"
	"time"
)

func TestAnalyzeClientAuthenticationBoundsAndFailures(t *testing.T) {
	for _, mode := range []string{"ok", "auth", "bad-json", "trailing-json", "oversize", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer private-test-token" {
					t.Error("worker auth missing")
				}
				switch mode {
				case "auth":
					w.WriteHeader(401)
				case "bad-json":
					fmt.Fprint(w, "{")
				case "trailing-json":
					fmt.Fprint(w, `{} {}`)
				case "oversize":
					fmt.Fprint(w, strings.Repeat("x", (256<<10)+1))
				case "cancelled":
					<-r.Context().Done()
				case "ok":
					if r.URL.Path == "/api/v1/health" {
						fmt.Fprint(w, `{"status":"ok","service":"codelens","version":"observed-v1","parser":"tree-sitter","languages":["go"]}`)
					} else {
						fmt.Fprint(w, `{"file":"fixture.go","language":"go","lines":3,"size_bytes":40,"entities":[],"imports":[],"calls":[]}`)
					}
				}
			}))
			defer server.Close()
			adapter := &Adapter{port: server.Listener.Addr().(*net.TCPAddr).Port, token: "private-test-token", done: make(chan struct{}), proxy: &httputil.ReverseProxy{}, ModifyResponse: func(*http.Response) error { t.Fatal("index response hook invoked"); return nil }}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			analysis, raw, err := adapter.AnalyzeFile(ctx, "owned-root", "fixture.go")
			if mode != "ok" {
				if err == nil {
					t.Fatal("protocol failure accepted")
				}
				return
			}
			if err != nil || analysis.Language != "go" || len(raw) == 0 {
				t.Fatalf("%+v %v", analysis, err)
			}
			health, raw, err := adapter.HealthEvidence(ctx)
			if err != nil || health.Version != "observed-v1" || len(raw) == 0 {
				t.Fatalf("%+v %v", health, err)
			}
		})
	}
	if _, _, err := (&Adapter{}).AnalyzeFile(context.Background(), "root", "file"); err == nil {
		t.Fatal("unavailable adapter accepted")
	}
}
