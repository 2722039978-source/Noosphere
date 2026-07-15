// Package retrieval — Embedding 嵌入向量提供者接口与实现
package retrieval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Embedder 嵌入向量提供者接口
// 支持多种后端实现：HTTP API（Ollama/OpenAI）、本地 ONNX、Mock
type Embedder interface {
	// Embed 将文本转为向量
	Embed(texts []string) ([][]float32, error)

	// Dimension 返回向量维度
	Dimension() int
}

// ─── HTTP Embedder (Ollama / OpenAI 兼容) ───

// HTTPEmbedder 通过 HTTP API 获取 Embedding
// 支持 OpenAI 兼容接口和 Ollama 接口
type HTTPEmbedder struct {
	apiURL     string
	model      string
	dimension  int
	client     *http.Client
	mu         sync.Mutex
}

// HTTPEmbedderConfig HTTP Embedder 配置
type HTTPEmbedderConfig struct {
	APIURL    string
	Model     string
	Dimension int
}

// NewHTTPEmbedder 创建 HTTP Embedder
func NewHTTPEmbedder(cfg HTTPEmbedderConfig) *HTTPEmbedder {
	if cfg.Dimension == 0 {
		cfg.Dimension = 1536 // OpenAI text-embedding-ada-002
	}
	return &HTTPEmbedder{
		apiURL:    cfg.APIURL,
		model:     cfg.Model,
		dimension: cfg.Dimension,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ollamaReq Ollama embedding 请求
type ollamaReq struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// ollamaResp Ollama embedding 响应
type ollamaResp struct {
	Embedding []float64 `json:"embedding"`
}

// openAIReq OpenAI embedding 请求
type openAIReq struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

// openAIResp OpenAI embedding 响应
type openAIResp struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

func (e *HTTPEmbedder) Embed(texts []string) ([][]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	results := make([][]float32, len(texts))

	for i, text := range texts {
		vec, err := e.embedOne(text)
		if err != nil {
			return nil, fmt.Errorf("embed text[%d]: %w", i, err)
		}
		results[i] = vec
	}

	return results, nil
}

func (e *HTTPEmbedder) embedOne(text string) ([]float32, error) {
	// 尝试 OpenAI 格式
	openaiBody := openAIReq{
		Input: []string{text},
		Model: e.model,
	}

	bodyBytes, _ := json.Marshal(openaiBody)
	resp, err := e.client.Post(e.apiURL+"/v1/embeddings", "application/json", bytes.NewReader(bodyBytes))
	if err == nil && resp.StatusCode == 200 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var oaiResp openAIResp
		if err := json.Unmarshal(body, &oaiResp); err == nil && len(oaiResp.Data) > 0 {
			vec := make([]float32, len(oaiResp.Data[0].Embedding))
			for j, v := range oaiResp.Data[0].Embedding {
				vec[j] = float32(v)
			}
			return vec, nil
		}
	}

	// 回退到 Ollama 格式
	ollamaBody := ollamaReq{
		Model:  e.model,
		Prompt: text,
	}
	bodyBytes, _ = json.Marshal(ollamaBody)
	resp, err = e.client.Post(e.apiURL, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("http embed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var olResp ollamaResp
	if err := json.Unmarshal(body, &olResp); err != nil {
		return nil, fmt.Errorf("ollama embed decode: %w (body=%s)", err, string(body[:min(len(body), 200)]))
	}

	vec := make([]float32, len(olResp.Embedding))
	for i, v := range olResp.Embedding {
		vec[i] = float32(v)
	}
	return vec, nil
}

func (e *HTTPEmbedder) Dimension() int {
	return e.dimension
}

// ─── Mock Embedder（测试和开发用）───

// MockEmbedder 返回简单的哈希向量（用于测试）
type MockEmbedder struct {
	dimension int
}

// NewMockEmbedder 创建 Mock Embedder
func NewMockEmbedder(dimension int) *MockEmbedder {
	if dimension < 1 {
		dimension = 128
	}
	return &MockEmbedder{dimension: dimension}
}

func (e *MockEmbedder) Embed(texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, text := range texts {
		vec := make([]float32, e.dimension)
		// 简单 hash → 向量（文本相似 → 向量近似）
		h := fnvHash(text)
		for j := 0; j < e.dimension; j++ {
			// 用 hash 的不同的窗口生成各维度
			vec[j] = float32(uint32(h>>uint(j%32))) / float32(0xFFFFFFFF)
		}
		results[i] = vec
	}
	return results, nil
}

func (e *MockEmbedder) Dimension() int {
	return e.dimension
}

// ─── 辅助 ───

func fnvHash(s string) uint64 {
	h := uint64(14695981039346656037)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
