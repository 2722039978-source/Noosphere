package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func init() {
	Register("openai", NewOpenAI)
	Register("openai_compatible", NewOpenAI)
	Register("ollama", NewOllama)
	Register("deepseek", func(baseURL, apiKey string) (LLMProvider, error) {
		provider, err := NewOpenAI(baseURL, apiKey)
		if err == nil {
			provider.(*OpenAIProvider).directResponse = true
		}
		return provider, err
	})
}

type OpenAIProvider struct {
	baseURL        string
	apiKey         string
	client         *http.Client
	directResponse bool
}

func NewOpenAI(baseURL, apiKey string) (LLMProvider, error) {
	return &OpenAIProvider{
		baseURL: baseURL,
		apiKey:  apiKey,
		client: &http.Client{
			Timeout: 120 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:       20,
				IdleConnTimeout:    30 * time.Second,
				DisableCompression: false,
			},
		},
	}, nil
}

func (p *OpenAIProvider) Name() string { return "openai" }

func (p *OpenAIProvider) Chat(ctx context.Context, model string, messages []Message, opts ChatOptions) (*ChatResponse, error) {
	msgs := make([]map[string]string, 0)
	if opts.SystemPrompt != "" {
		msgs = append(msgs, map[string]string{"role": "system", "content": opts.SystemPrompt})
	}
	for _, m := range messages {
		msgs = append(msgs, map[string]string{"role": m.Role, "content": m.Content})
	}

	temp := opts.Temperature
	if temp == 0 {
		temp = 0.1
	}
	maxT := opts.MaxTokens
	if maxT == 0 {
		maxT = 4096
	}

	body := map[string]interface{}{
		"model": model, "messages": msgs,
		"max_tokens": maxT, "temperature": temp, "stream": false,
	}
	if p.directResponse {
		body["thinking"] = map[string]string{"type": "disabled"}
	}
	jsonBody, _ := json.Marshal(body)
	url := strings.TrimRight(p.baseURL, "/") + "/chat/completions"

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}

	if resp.StatusCode >= 400 {
		message := p.redact(string(respBody))
		limit := len(message)
		if limit > 300 {
			limit = 300
		}
		return nil, fmt.Errorf("upstream %d: %s", resp.StatusCode, message[:limit])
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	content := ""
	if len(result.Choices) > 0 {
		content = result.Choices[0].Message.Content
	}
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("upstream returned no answer content; check model/output settings")
	}
	return &ChatResponse{
		Content: content, TokensIn: result.Usage.PromptTokens,
		TokensOut: result.Usage.CompletionTokens,
	}, nil
}

func (p *OpenAIProvider) ChatStream(ctx context.Context, model string, messages []Message, opts ChatOptions) (<-chan StreamChunk, error) {
	msgs := make([]map[string]string, 0)
	if opts.SystemPrompt != "" {
		msgs = append(msgs, map[string]string{"role": "system", "content": opts.SystemPrompt})
	}
	for _, m := range messages {
		msgs = append(msgs, map[string]string{"role": m.Role, "content": m.Content})
	}

	temp := opts.Temperature
	if temp == 0 {
		temp = 0.1
	}
	maxT := opts.MaxTokens
	if maxT == 0 {
		maxT = 4096
	}

	body := map[string]interface{}{
		"model": model, "messages": msgs,
		"max_tokens": maxT, "temperature": temp, "stream": true,
	}
	if p.directResponse {
		body["thinking"] = map[string]string{"type": "disabled"}
	}
	jsonBody, _ := json.Marshal(body)
	url := strings.TrimRight(p.baseURL, "/") + "/chat/completions"

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, int64(4096+len(p.apiKey))))
		resp.Body.Close()
		message := p.redact(string(b))
		if len(message) > 4096 {
			message = message[:4096]
		}
		return nil, fmt.Errorf("upstream %d: %s", resp.StatusCode, message)
	}

	ch := make(chan StreamChunk, 64)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 4096), 65536)
		var full strings.Builder
		for scanner.Scan() {
			if ctx.Err() != nil {
				return
			}
			line := scanner.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				sendChunk(ctx, ch, StreamChunk{Done: true, Content: full.String()})
				return
			}
			var chunk struct {
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				sendChunk(ctx, ch, StreamChunk{Error: fmt.Errorf("invalid upstream stream event: %w", err)})
				return
			}
			if chunk.Error != nil {
				sendChunk(ctx, ch, StreamChunk{Error: fmt.Errorf("upstream stream error: %s", p.redact(chunk.Error.Message))})
				return
			}
			if len(chunk.Choices) == 0 {
				continue
			}
			token := chunk.Choices[0].Delta.Content
			if token == "" {
				continue
			}
			full.WriteString(token)
			if !sendChunk(ctx, ch, StreamChunk{Token: token}) {
				return
			}
		}
		sendChunk(ctx, ch, StreamChunk{Error: streamReadError(ctx, scanner.Err()), Content: full.String()})
	}()
	return ch, nil
}

func (p *OpenAIProvider) redact(message string) string {
	if p.apiKey != "" {
		message = strings.ReplaceAll(message, p.apiKey, "[redacted]")
		// A bounded upstream read can end halfway through an echoed credential.
		// Strip a matching suffix before any later replacement/truncation exposes it.
		for size := len(p.apiKey) - 1; size > 0; size-- {
			if strings.HasSuffix(message, p.apiKey[:size]) {
				return message[:len(message)-size] + "[redacted]"
			}
		}
	}
	return message
}
