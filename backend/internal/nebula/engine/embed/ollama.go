package embed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func init() {
	// Register Ollama embedder constructors are done via NewOllamaEmbedder directly
}

type OllamaEmbedder struct {
	baseURL string
	model   string
	client  *http.Client
}

func NewOllamaEmbedder(baseURL, model string) *OllamaEmbedder {
	if baseURL == "" {
		baseURL = "http://localhost:11434/api"
	}
	if model == "" {
		model = "nomic-embed-text"
	}
	return &OllamaEmbedder{
		baseURL: baseURL,
		model:   model,
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

func (o *OllamaEmbedder) Embed(text string) ([]float32, error) {
	body := map[string]interface{}{
		"model":  o.model,
		"prompt": text,
	}
	jsonBody, _ := json.Marshal(body)
	url := o.baseURL + "/embeddings"

	resp, err := o.client.Post(url, "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ollama parse: %w", err)
	}

	if len(result.Embedding) == 0 {
		return nil, fmt.Errorf("ollama: empty embedding")
	}

	return result.Embedding, nil
}

func (o *OllamaEmbedder) Dim() int {
	return 768 // nomic-embed-text default dimension
}
