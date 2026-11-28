package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/noosphere/backend/internal/agent"
	"github.com/noosphere/backend/internal/workspaces"
	"net/netip"
	"strings"
	"testing"
)

func TestDependencyScopeAndNetworkBoundary(t *testing.T) {
	hash := strings.Repeat("1", 64)
	for _, tt := range []struct {
		url     string
		allowed bool
	}{
		{"https://registry.npmjs.org/lodash/-/lodash-4.17.21.tgz", true},
		{"https://files.pythonhosted.org/packages/ab/example-1-py3-none-any.whl", true},
		{"https://static.crates.io/crates/serde/serde-1.0.0.crate", true},
		{"https://proxy.golang.org/golang.org/x/sys/@v/v0.48.0.zip", true},
		{"https://registry.npmjs.org:123/lodash/-/x.tgz", false},
		{"https://registry.npmjs.org/lodash/-/x.tgz?auth=token", false},
		{"http://registry.npmjs.org/lodash/-/x.tgz", false},
		{"https://user:pass@registry.npmjs.org/lodash/-/x.tgz", false},
		{"https://registry.npmjs.org.example.com/lodash/-/x.tgz", false},
		{"https://registry.npmjs.org/lodash/-/%2E%2E/x.tgz", false},
		{"https://registry.npmjs.org/lodash/-/../../x.tgz", false},
	} {
		raw, _ := json.Marshal(dependencyArgs{URL: tt.url, SHA256: hash})
		_, scope, err := dependencyPlan(raw)
		if (err == nil) != tt.allowed {
			t.Fatal(tt, err)
		}
		if tt.allowed && (!strings.Contains(scope, hash) || !strings.Contains(scope, tt.url)) {
			t.Fatal(scope)
		}
	}
	for _, s := range []string{"127.0.0.1", "169.254.169.254", "10.2.3.4", "100.64.0.1", "224.0.0.1", "::1", "::ffff:192.168.0.2", "fd00::1", "64:ff9b::127.0.0.1", "2001:db8::1"} {
		if publicAddress(netip.MustParseAddr(s)) {
			t.Fatal(s)
		}
	}
	if !publicAddress(netip.MustParseAddr("1.1.1.1")) || !publicAddress(netip.MustParseAddr("2606:4700:4700::1111")) {
		t.Fatal("public address refused")
	}
	client := registryClient("registry.npmjs.org")
	if client.CheckRedirect(nil, nil) == nil {
		t.Fatal("followed registry redirect")
	}
}

type mockService struct {
	state string
	calls int
}

func (m *mockService) ServiceState(context.Context, string) (string, error) { return m.state, nil }
func (m *mockService) Execute(_ context.Context, a agent.Action) (string, error) {
	m.calls++
	m.state = a.Target
	return "", nil
}

func TestServiceScopeBindsPriorStateAndExplicitTarget(t *testing.T) {
	t.Setenv("NOOSPHERE_AGENT_MANAGED_SERVICES", "NoosphereFixture")
	mock := &mockService{state: "running"}
	raw := json.RawMessage(`{"name":"NoosphereFixture","target":"stopped"}`)
	a, before, scope, err := inspectService(t.Context(), mock, raw)
	if err != nil || !strings.Contains(scope, "running") || !strings.Contains(scope, "stopped") {
		t.Fatalf("%s %v", scope, err)
	}
	mock.state = "pending"
	if err = serviceTransition(mock)(t.Context(), a.Name, before, a.Target); !errors.Is(err, workspaces.ErrConflict) || mock.calls != 0 {
		t.Fatal(mock, err)
	}
	mock.state = "running"
	if err = serviceTransition(mock)(t.Context(), a.Name, before, a.Target); err != nil || mock.state != "stopped" {
		t.Fatal(mock, err)
	}
	if _, _, _, err := inspectService(t.Context(), mock, json.RawMessage(`{"name":"UnlistedService","target":"stopped"}`)); err == nil {
		t.Fatal("unmanaged service accepted")
	}
}
