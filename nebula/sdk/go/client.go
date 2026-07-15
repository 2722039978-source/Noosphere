// Package sdk — Nebula Agent Go SDK
//
// 嵌入式使用（推荐）:
//
//	engine, _ := nebula.Open(nebula.MemoryOptions())
//	session := engine.Session("my-agent")
//	session.RememberEpisodic("用户喜欢 Python", 0.8, []string{"preference"})
//	results, _ := session.Reminisce(&manager.SearchOptions{Query: "编程语言", TopK: 5})
//
// 远程客户端（通过 REST API）:
//
//	client := sdk.NewClient("http://localhost:8730")
//	client.Remember("my-agent", "用户偏好 Python", "episodic", 0.8, nil)
//	results, _ := client.Search("my-agent", "编程语言", sdk.Hybrid, 10)
package sdk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nebula-agent/nebula/engine/manager"
)

// Client Nebula REST API 客户端
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient 创建 API 客户端
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Health 健康检查
func (c *Client) Health() (map[string]interface{}, error) {
	return c.do("GET", "/health", nil)
}

// Stats 获取引擎统计
func (c *Client) Stats() (*StatsResponse, error) {
	var resp StatsResponse
	if err := c.doJSON("GET", "/api/v1/stats", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// StatsResponse 统计响应
type StatsResponse struct {
	SessionCount int            `json:"session_count"`
	EngineStats  *manager.Stats `json:"engine_stats"`
}

// Remember 存储记忆
func (c *Client) Remember(sessionID, content, memType string, importance float64, tags []string) (*MemoryResponse, error) {
	body := map[string]interface{}{
		"content":    content,
		"type":       memType,
		"importance": importance,
		"tags":       tags,
	}
	var resp MemoryResponse
	if err := c.doJSON("POST", fmt.Sprintf("/api/v1/sessions/%s/memories", sessionID), body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// MemoryResponse 存储响应
type MemoryResponse struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
}

// Recall 召回记忆
func (c *Client) Recall(sessionID, memoryID string) (*manager.Memory, error) {
	var mem manager.Memory
	if err := c.doJSON("GET", fmt.Sprintf("/api/v1/sessions/%s/memories/%s", sessionID, memoryID), nil, &mem); err != nil {
		return nil, err
	}
	return &mem, nil
}

// Forget 遗忘记忆
func (c *Client) Forget(sessionID, memoryID string) error {
	_, err := c.do("DELETE", fmt.Sprintf("/api/v1/sessions/%s/memories/%s", sessionID, memoryID), nil)
	return err
}

// Search 检索记忆
func (c *Client) Search(sessionID string, opts *manager.SearchOptions) (*SearchResponse, error) {
	var resp SearchResponse
	if err := c.doJSON("POST", fmt.Sprintf("/api/v1/sessions/%s/search", sessionID), opts, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SearchResponse 检索响应
type SearchResponse struct {
	Results []*manager.SearchResult `json:"results"`
	Count   int                     `json:"count"`
}

// ─── 内部 HTTP 方法 ───

func (c *Client) do(method, path string, body interface{}) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := c.doJSON(method, path, body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) doJSON(method, path string, body interface{}, result interface{}) error {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("api error (%d): %s", resp.StatusCode, string(respData))
	}

	if result != nil {
		if err := json.Unmarshal(respData, result); err != nil {
			return fmt.Errorf("unmarshal response: %w", err)
		}
	}

	return nil
}
