// Package aiGateway — AI Gateway Go SDK
//
// Unified client for all LLM calls through the AI Gateway.
// Use this SDK in Nebula, DevOps, or any Go service that needs LLM access.
//
// Usage:
//
//	import gw "github.com/noosphere/sdk/aiGateway/go"
//
//	client := gw.NewClient("http://localhost:8800")
//	resp, err := client.Chat(gw.ChatRequest{
//	    Project: "nebula",
//	    Messages: []gw.Message{{Role: "user", Content: "Hello"}},
//	})
package aiGateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ─── Types ───

// Message is a single chat message
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest is the request to Gateway /api/v1/gateway/chat
type ChatRequest struct {
	ModelID      string    `json:"model_id,omitempty"`
	Provider     string    `json:"provider,omitempty"`
	Messages     []Message `json:"messages"`
	SystemPrompt string    `json:"system_prompt,omitempty"`
	Temperature  float64   `json:"temperature,omitempty"`
	MaxTokens    int       `json:"max_tokens,omitempty"`
	Stream       bool      `json:"stream,omitempty"`
	Project      string    `json:"project,omitempty"`
	SessionID    string    `json:"session_id,omitempty"`
	Tags         []string  `json:"tags,omitempty"`
}

// ChatResponse is the unified chat response
type ChatResponse struct {
	ID         string    `json:"id"`
	Model      string    `json:"model"`
	Provider   string    `json:"provider"`
	Content    string    `json:"content"`
	TokensUsed int       `json:"tokens_used"`
	LatencyMs  float64   `json:"latency_ms"`
	Timestamp  time.Time `json:"timestamp"`
	Project    string    `json:"project,omitempty"`
}

// EmbeddingRequest for embedding calls
type EmbeddingRequest struct {
	ModelID string   `json:"model_id,omitempty"`
	Input   []string `json:"input"`
	Project string   `json:"project,omitempty"`
}

// EmbeddingResponse for embedding results
type EmbeddingResponse struct {
	Model      string      `json:"model"`
	Embeddings [][]float64 `json:"embeddings"`
	TokensUsed int         `json:"tokens_used"`
	LatencyMs  float64     `json:"latency_ms"`
}

// VisionRequest for vision/image understanding
type VisionRequest struct {
	ModelID   string   `json:"model_id,omitempty"`
	Prompt    string   `json:"prompt"`
	ImageURLs []string `json:"image_urls,omitempty"`
	ImageB64  []string `json:"image_base64,omitempty"`
	Project   string   `json:"project,omitempty"`
}

// ModelInfo describes a configured model
type ModelInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	ModelName string `json:"model_name"`
	Enabled   bool   `json:"enabled"`
	IsDefault bool   `json:"is_default"`
	Status    string `json:"status"`
}

// ─── Client ───

// Client is the AI Gateway SDK client
type Client struct {
	baseURL    string
	httpClient *http.Client
	project    string
}

// NewClient creates a new Gateway client
func NewClient(gatewayURL string) *Client {
	return &Client{
		baseURL: gatewayURL,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// NewClientWithProject creates a client with a default project name
func NewClientWithProject(gatewayURL, project string) *Client {
	c := NewClient(gatewayURL)
	c.project = project
	return c
}

// ─── API Methods ───

// Chat sends a chat completion request through the Gateway
func (c *Client) Chat(req ChatRequest) (*ChatResponse, error) {
	if req.Project == "" {
		req.Project = c.project
	}
	body, _ := json.Marshal(req)
	resp, err := c.httpClient.Post(c.baseURL+"/api/v1/gateway/chat", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gateway chat: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gateway error %d: %s", resp.StatusCode, string(b))
	}

	var result ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// StreamChat sends a streaming chat request. Returns a channel of text chunks.
func (c *Client) StreamChat(req ChatRequest) (<-chan string, <-chan error) {
	chunks := make(chan string, 100)
	errs := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errs)

		if req.Project == "" {
			req.Project = c.project
		}
		req.Stream = true
		body, _ := json.Marshal(req)

		resp, err := c.httpClient.Post(c.baseURL+"/api/v1/gateway/chat/stream", "application/json", bytes.NewReader(body))
		if err != nil {
			errs <- err
			return
		}
		defer resp.Body.Close()

		// Parse SSE stream
		buf := make([]byte, 4096)
		remainder := ""
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				remainder += string(buf[:n])
				for {
					idx := -1
					for i := 0; i < len(remainder)-1; i++ {
						if remainder[i] == '\n' && remainder[i+1] == '\n' {
							idx = i
							break
						}
					}
					if idx < 0 {
						break
					}
					line := remainder[:idx]
					remainder = remainder[idx+2:]
					if len(line) > 6 && line[:6] == "data: " {
						data := line[6:]
						if data == "[DONE]" {
							return
						}
						chunks <- data
					}
				}
			}
			if err != nil {
				if err != io.EOF {
					errs <- err
				}
				return
			}
		}
	}()

	return chunks, errs
}

// Vision sends a vision/image understanding request
func (c *Client) Vision(req VisionRequest) (*ChatResponse, error) {
	if req.Project == "" {
		req.Project = c.project
	}
	body, _ := json.Marshal(req)
	resp, err := c.httpClient.Post(c.baseURL+"/api/v1/gateway/vision", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gateway vision: %w", err)
	}
	defer resp.Body.Close()

	var result ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Embedding sends an embedding request through the Gateway
func (c *Client) Embedding(req EmbeddingRequest) (*EmbeddingResponse, error) {
	if req.Project == "" {
		req.Project = c.project
	}
	body, _ := json.Marshal(req)
	resp, err := c.httpClient.Post(c.baseURL+"/api/v1/gateway/embedding", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gateway embedding: %w", err)
	}
	defer resp.Body.Close()

	var result EmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ─── Management Methods ───

// ListModels returns all configured models
func (c *Client) ListModels() ([]ModelInfo, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/api/v1/gateway/models")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Models []ModelInfo `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Models, nil
}

// TestModel tests connectivity for a specific model
func (c *Client) TestModel(modelID string) (map[string]any, error) {
	req, _ := http.NewRequest("POST", c.baseURL+"/api/v1/gateway/models/"+modelID+"/test", nil)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	return result, nil
}

// GetStats returns usage statistics
func (c *Client) GetStats() (map[string]any, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/api/v1/gateway/stats")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	return result, nil
}

// Health checks if the Gateway is running
func (c *Client) Health() bool {
	resp, err := c.httpClient.Get(c.baseURL + "/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}
