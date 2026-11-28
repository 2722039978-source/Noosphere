package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/noosphere/backend/internal/agent"
	agentcore "github.com/noosphere/backend/internal/agent/core"
	"github.com/noosphere/backend/internal/agent/host"
	"github.com/noosphere/backend/internal/gateway/providers"
	"github.com/noosphere/backend/internal/workspaces"
)

func effectDefinition(name, description string, props map[string]any, required ...string) providers.ToolDefinition {
	schema, _ := json.Marshal(map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false})
	return providers.ToolDefinition{Type: "function", Function: providers.FunctionDefinition{Name: name, Description: description, Parameters: schema}}
}
func effectArgs(raw json.RawMessage, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if dec.Decode(new(any)) != io.EOF {
		return fmt.Errorf("invalid trailing arguments")
	}
	return nil
}

type dependencyArgs struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

var artifactPath = regexp.MustCompile(`^/[A-Za-z0-9@!+._/-]+$`)
var shaPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func dependencyPlan(raw json.RawMessage) (dependencyArgs, string, error) {
	var a dependencyArgs
	if err := effectArgs(raw, &a); err != nil {
		return a, "", err
	}
	u, err := url.Parse(a.URL)
	if err != nil || len(a.URL) > 500 || u.Scheme != "https" || u.Host != u.Hostname() || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || !artifactPath.MatchString(u.Path) || !fs.ValidPath(strings.TrimPrefix(u.Path, "/")) || !shaPattern.MatchString(a.SHA256) {
		return a, "", fmt.Errorf("请提供受支持的公开包文件 HTTPS 地址和 SHA-256")
	}
	name := path.Base(u.Path)
	allowed := false
	switch u.Host {
	case "files.pythonhosted.org":
		allowed = strings.HasPrefix(u.Path, "/packages/") && (strings.HasSuffix(name, ".whl") || strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".zip"))
	case "registry.npmjs.org":
		allowed = strings.Contains(u.Path, "/-/") && strings.HasSuffix(name, ".tgz")
	case "static.crates.io":
		allowed = strings.HasPrefix(u.Path, "/crates/") && strings.HasSuffix(name, ".crate")
	case "proxy.golang.org":
		allowed = strings.Contains(u.Path, "/@v/") && strings.HasSuffix(name, ".zip")
	}
	if !allowed {
		return a, "", fmt.Errorf("仅接受 PyPI、npm、crates.io 或 Go 模块代理的公开包归档")
	}
	return a, fmt.Sprintf("从 %s 下载依赖至本轮临时缓存（最多 64 MiB）\nSHA-256 %s", a.URL, a.SHA256), nil
}

var reservedNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"),
}

func publicAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.Zone() != "" {
		return false
	}
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	for _, block := range reservedNetworks {
		if block.Contains(ip) {
			return false
		}
	}
	return true
}
func registryClient(name string) *http.Client {
	t := &http.Transport{Proxy: nil, DisableCompression: true, DisableKeepAlives: true, ResponseHeaderTimeout: 12 * time.Second, TLSHandshakeTimeout: 10 * time.Second, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	t.DialContext = func(ctx context.Context, _, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || host != name || port != "443" {
			return nil, fmt.Errorf("unexpected registry origin")
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("registry has no public address")
		}
		for _, ip := range ips {
			if !publicAddress(ip) {
				return nil, fmt.Errorf("registry resolves to a restricted address")
			}
		}
		dial := net.Dialer{Timeout: 5 * time.Second}
		var last error
		for _, ip := range ips {
			conn, err := dial.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		return nil, last
	}
	return &http.Client{Transport: t, Timeout: 60 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return errors.New("registry redirects disabled") }}
}

type serviceArgs struct {
	Name   string `json:"name"`
	Target string `json:"target"`
}

var serviceID = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

func managedService(name string) bool {
	if !serviceID.MatchString(name) {
		return false
	}
	list := []string{"Ollama", "Noosphere", "postgresql", "MySQL", "Redis", "nginx"}
	for _, n := range strings.Split(os.Getenv("NOOSPHERE_AGENT_MANAGED_SERVICES"), ",") {
		if s := strings.TrimSpace(n); s != "" {
			list = append(list, s)
		}
	}
	for _, n := range list {
		if strings.EqualFold(n, name) {
			return true
		}
	}
	return false
}

type nativeService interface {
	ServiceState(context.Context, string) (string, error)
	Execute(context.Context, agent.Action) (string, error)
}

func inspectService(ctx context.Context, n nativeService, raw json.RawMessage) (serviceArgs, string, string, error) {
	var a serviceArgs
	if err := effectArgs(raw, &a); err != nil {
		return a, "", "", err
	}
	if !managedService(a.Name) || (a.Target != "running" && a.Target != "stopped") {
		return a, "", "", fmt.Errorf("服务须属于宿主允许管理的清单，目标须为 running/stopped")
	}
	before, err := n.ServiceState(ctx, a.Name)
	if err != nil {
		return a, "", "", err
	}
	if (before != "running" && before != "stopped") || before == a.Target {
		return a, "", "", fmt.Errorf("服务已在目标状态或尚未稳定")
	}
	return a, before, fmt.Sprintf("此电脑上的 Windows 服务 %s 将从 %s 变为 %s。此操作立即生效，可在任务详情单独恢复。", a.Name, before, a.Target), nil
}
func serviceTransition(n nativeService) workspaces.ServiceTransition {
	return func(ctx context.Context, name, before, after string) error {
		if !managedService(name) {
			return workspaces.ErrConflict
		}
		observed, err := n.ServiceState(ctx, name)
		if err != nil {
			return err
		}
		if observed != before {
			return fmt.Errorf("%w: 服务当前状态已变化", workspaces.ErrConflict)
		}
		_, err = n.Execute(ctx, agent.Action{Kind: "service", Service: name, Previous: before, Target: after})
		return err
	}
}

func deliveryProtectedTools(m *workspaces.Manager, ws workspaces.Workspace, id string, d *host.Draft, systemEnabled bool) []agentcore.ProtectedTool {
	stringField := map[string]string{"type": "string"}
	fetch := agentcore.ProtectedTool{Definition: effectDefinition("fetch_dependency", "Fetch a public package archive by exact URL and SHA-256 into the offline build cache after user confirmation.", map[string]any{"url": stringField, "sha256": stringField}, "url", "sha256")}
	fetch.Prepare = func(_ context.Context, raw json.RawMessage) (string, error) {
		_, scope, err := dependencyPlan(raw)
		return scope, err
	}
	fetch.Execute = func(ctx context.Context, raw json.RawMessage, approved string) (any, error) {
		a, scope, err := dependencyPlan(raw)
		if err != nil {
			return nil, err
		}
		if approved != scope {
			return nil, workspaces.ErrConflict
		}
		if err = m.ValidateAgent(ctx, id, ws.ID); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
		if err != nil {
			return nil, err
		}
		resp, err := registryClient(req.URL.Host).Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK || resp.ContentLength > host.MaxDependencyBytes {
			return nil, fmt.Errorf("registry rejected the request or exceeds 64 MiB (%d)", resp.StatusCode)
		}
		name, err := d.StoreDependency(ctx, path.Base(req.URL.Path), a.SHA256, resp.Body)
		if err != nil {
			return nil, err
		}
		return map[string]string{"path": name, "sha256": a.SHA256}, nil
	}
	out := []agentcore.ProtectedTool{fetch}
	if !systemEnabled || runtime.GOOS != "windows" {
		return out
	}
	n := agent.NewNativeExecutor(true)
	service := agentcore.ProtectedTool{Definition: effectDefinition("change_service", "After confirmation, start or stop an explicitly managed local development service.", map[string]any{"name": stringField, "target": map[string]any{"type": "string", "enum": []string{"running", "stopped"}}}, "name", "target")}
	service.Prepare = func(ctx context.Context, raw json.RawMessage) (string, error) {
		_, _, scope, err := inspectService(ctx, n, raw)
		return scope, err
	}
	service.Execute = func(ctx context.Context, raw json.RawMessage, approved string) (any, error) {
		a, prior, scope, err := inspectService(ctx, n, raw)
		if err != nil {
			return nil, err
		}
		if scope != approved {
			return nil, workspaces.ErrConflict
		}
		return m.ChangeService(ctx, id, ws.ID, a.Name, prior, a.Target, serviceTransition(n))
	}
	return append(out, service)
}
