package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/noosphere/backend/internal/nebula/engine/textindex"
)

type Embedder interface {
	Embed(text string) ([]float32, error)
	Dim() int
}

type MockEmbedder struct {
	dim int
}

func NewMockEmbedder(dim int) *MockEmbedder {
	if dim <= 0 {
		dim = 128
	}
	return &MockEmbedder{dim: dim}
}

// Embed maps text to a deterministic bag-of-words hash vector: each token
// (via FNV-1a) lights one dimension with a hash-derived sign. Texts sharing
// tokens get higher cosine similarity, so vector search reflects lexical
// overlap instead of pure noise. Same input always yields the same vector.
func (m *MockEmbedder) Embed(text string) ([]float32, error) {
	v := make([]float32, m.dim)
	for _, tok := range tokenize(text) {
		h := fnv.New32a()
		h.Write([]byte(tok))
		hv := h.Sum32()
		idx := int(hv % uint32(m.dim))
		if (hv>>16)&1 == 0 {
			v[idx]++
		} else {
			v[idx]--
		}
	}
	var sq float64
	for _, x := range v {
		sq += float64(x) * float64(x)
	}
	if sq > 0 {
		inv := float32(1 / math.Sqrt(sq))
		for i := range v {
			v[i] *= inv
		}
	}
	return v, nil
}

func tokenize(text string) []string {
	return textindex.Tokens(text)
}

func (m *MockEmbedder) Dim() int { return m.dim }

func (m *MockEmbedder) Identity() string { return fmt.Sprintf("hash-cjk-v2:%d", m.dim) }

type OpenAIEmbedder struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

func NewOpenAIEmbedder(baseURL, apiKey, model string) *OpenAIEmbedder {
	return &OpenAIEmbedder{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (o *OpenAIEmbedder) Embed(text string) ([]float32, error) {
	return o.EmbedContext(context.Background(), text)
}

func (o *OpenAIEmbedder) EmbedContext(ctx context.Context, text string) ([]float32, error) {
	body := map[string]interface{}{
		"model": o.model,
		"input": text,
	}
	jsonBody, _ := json.Marshal(body)
	url := o.baseURL + "/embeddings"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil { return nil, err }
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("embed parse: %w", err)
	}
	if len(result.Data) == 0 {
		return nil, fmt.Errorf("embed: empty response")
	}
	return result.Data[0].Embedding, nil
}

func (o *OpenAIEmbedder) Dim() int { return 1536 }

func (o *OpenAIEmbedder) Identity() string {
	return "openai:" + strings.TrimRight(o.baseURL, "/") + ":" + o.model
}
