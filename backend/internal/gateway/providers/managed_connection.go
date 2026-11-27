package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const managedAddress = "127.0.0.1:18789"

func (p *OpenAIProvider) SetLibraryDirectory(directory string) { p.libraryDirectory = directory }

// Pin the newly connected socket before the HTTP transport can send headers or user data.
// Read the current lease on each dial so a queued request cannot follow a model switch.
func (p *OpenAIProvider) completionClient(model string, needsVision bool) *http.Client {
	endpoint, err := url.Parse(p.baseURL)
	if err != nil || !strings.HasPrefix(model, "gguf-") || !managedEndpoint(endpoint) {
		return p.client
	}
	transport := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if p.libraryDirectory == "" {
			return nil, fmt.Errorf("无法验证本地模型进程，请从本地模型中心启动")
		}
		lock, err := holdManagedRequest(p.libraryDirectory)
		if err != nil {
			return nil, fmt.Errorf("模型正在切换或已停止，请等待就绪后重试: %w", err)
		}
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp4", managedAddress)
		if err != nil {
			lock.Close()
			return nil, err
		}
		clientPort := conn.LocalAddr().(*net.TCPAddr).Port
		serverPort := conn.RemoteAddr().(*net.TCPAddr).Port
		if err := verifyModelListener(ctx, p.libraryDirectory, model, serverPort, clientPort, needsVision); err != nil {
			conn.Close()
			lock.Close()
			return nil, err
		}
		return &managedConn{Conn: conn, lock: lock}, nil
	}}
	return &http.Client{Transport: transport, Timeout: p.client.Timeout}
}

func managedEndpoint(endpoint *url.URL) bool {
	if endpoint.Port() != "18789" {
		return false
	}
	host := strings.TrimSuffix(endpoint.Hostname(), ".")
	return strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()
}

type managedConn struct {
	net.Conn
	lock *os.File
}

func (c *managedConn) Close() error {
	err := c.Conn.Close()
	_ = c.lock.Close()
	return err
}

func holdManagedRequest(dir string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, "local-engines", "custom-requests.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = lockManagedRequest(f); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func verifyModelListener(ctx context.Context, dir, model string, serverPort, clientPort int, needsVision bool) error {
	denied := fmt.Errorf("此 GGUF 的受管进程已退出或已切换，请从本地模型中心重新启动")
	f, err := os.Open(filepath.Join(dir, "local-engines", "custom-listener.json"))
	if err != nil {
		return denied
	}
	defer f.Close()
	var owner struct {
		Desktop int    `json:"desktop_pid"`
		PID     uint32 `json:"pid"`
		Group   uint32 `json:"group_pid"`
		Model   string `json:"model"`
		Vision  bool   `json:"vision"`
	}
	if json.NewDecoder(io.LimitReader(f, 4096)).Decode(&owner) != nil || owner.Desktop != os.Getppid() || owner.PID == 0 || owner.Group == 0 || owner.Model != model {
		return denied
	}
	if needsVision && !owner.Vision {
		return fmt.Errorf("当前运行的模型未确认支持图像。请检查视觉投影器并重新加载，或移除图像附件")
	}
	// The kernel may complete connect before the server's accept installs a
	// descriptor. Pin that exact established peer, not a subsequent listener.
	probe, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	for {
		if managedPeerMatches(probe, owner.PID, serverPort, clientPort) && managedGroup(owner.PID) == owner.Group {
			return nil
		}
		select {
		case <-probe.Done():
			return denied
		case <-time.After(20 * time.Millisecond):
		}
	}
}
