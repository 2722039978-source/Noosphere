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

// ---- Claude (Anthropic) Provider ----

func init() {
	Register("claude", NewClaude)
	Register("anthropic", NewClaude)
}

type ClaudeProvider struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewClaude(baseURL, apiKey string) (LLMProvider, error) {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	return &ClaudeProvider{
		baseURL: baseURL,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: 120 * time.Second},
	}, nil
}

func (p *ClaudeProvider) Name() string { return "claude" }

func (p *ClaudeProvider) Chat(ctx context.Context, model string, messages []Message, opts ChatOptions) (*ChatResponse, error) {
	// Build Anthropic Messages API format
	systemPrompt := opts.SystemPrompt
	anthropicMsgs := make([]map[string]interface{}, 0)
	for _, m := range messages {
		anthropicMsgs = append(anthropicMsgs, map[string]interface{}{
			"role":    m.Role,
			"content": m.Content,
		})
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
		"model":       model,
		"messages":    anthropicMsgs,
		"max_tokens":  maxT,
		"temperature": temp,
	}
	if systemPrompt != "" {
		body["system"] = systemPrompt
	}

	jsonBody, _ := json.Marshal(body)
	url := strings.TrimRight(p.baseURL, "/") + "/messages"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

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
		limit := len(respBody)
		if limit > 300 {
			limit = 300
		}
		return nil, fmt.Errorf("upstream %d: %s", resp.StatusCode, string(respBody[:limit]))
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	content := ""
	if len(result.Content) > 0 {
		content = result.Content[0].Text
	}
	return &ChatResponse{
		Content:   content,
		TokensIn:  result.Usage.InputTokens,
		TokensOut: result.Usage.OutputTokens,
	}, nil
}

func (p *ClaudeProvider) ChatStream(ctx context.Context, model string, messages []Message, opts ChatOptions) (<-chan StreamChunk, error) {
	// Build streaming request with stream: true
	systemPrompt := opts.SystemPrompt
	anthropicMsgs := make([]map[string]interface{}, 0)
	for _, m := range messages {
		anthropicMsgs = append(anthropicMsgs, map[string]interface{}{
			"role":    m.Role,
			"content": m.Content,
		})
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
		"model":       model,
		"messages":    anthropicMsgs,
		"max_tokens":  maxT,
		"temperature": temp,
		"stream":      true,
	}
	if systemPrompt != "" {
		body["system"] = systemPrompt
	}

	jsonBody, _ := json.Marshal(body)
	url := strings.TrimRight(p.baseURL, "/") + "/messages"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return nil, fmt.Errorf("upstream %d: %s", resp.StatusCode, string(b))
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
			// Anthropic SSE format: {"type":"content_block_delta","delta":{"type":"text_delta","text":"..."}}
			var event struct {
				Type  string `json:"type"`
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
				Delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				sendChunk(ctx, ch, StreamChunk{Error: fmt.Errorf("invalid upstream stream event: %w", err)})
				return
			}
			if event.Type == "message_stop" {
				sendChunk(ctx, ch, StreamChunk{Done: true, Content: full.String()})
				return
			}
			if event.Type == "error" {
				sendChunk(ctx, ch, StreamChunk{Error: fmt.Errorf("upstream stream error: %s", event.Error.Message)})
				return
			}
			if event.Type != "content_block_delta" {
				continue
			}
			token := event.Delta.Text
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

// ---- Gemini (Google) Provider ----

func init() {
	Register("gemini", NewGemini)
	Register("google", NewGemini)
}

type GeminiProvider struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewGemini(baseURL, apiKey string) (LLMProvider, error) {
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com/v1beta"
	}
	return &GeminiProvider{
		baseURL: baseURL,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: 120 * time.Second},
	}, nil
}

func (p *GeminiProvider) Name() string { return "gemini" }

func (p *GeminiProvider) Chat(ctx context.Context, model string, messages []Message, opts ChatOptions) (*ChatResponse, error) {
	// Build Gemini generateContent format
	contents := make([]map[string]interface{}, 0)
	if opts.SystemPrompt != "" {
		contents = append(contents, map[string]interface{}{
			"role":  "user",
			"parts": []map[string]string{{"text": opts.SystemPrompt}},
		})
	}
	for _, m := range messages {
		role := m.Role
		if role == "assistant" {
			role = "model"
		}
		contents = append(contents, map[string]interface{}{
			"role":  role,
			"parts": []map[string]string{{"text": m.Content}},
		})
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
		"contents": contents,
		"generationConfig": map[string]interface{}{
			"temperature":     temp,
			"maxOutputTokens": maxT,
		},
	}

	jsonBody, _ := json.Marshal(body)
	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s",
		strings.TrimRight(p.baseURL, "/"), model, p.apiKey)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

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
		limit := len(respBody)
		if limit > 300 {
			limit = 300
		}
		return nil, fmt.Errorf("upstream %d: %s", resp.StatusCode, string(respBody[:limit]))
	}

	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	content := ""
	tokensIn := 0
	tokensOut := 0
	if len(result.Candidates) > 0 {
		for _, part := range result.Candidates[0].Content.Parts {
			content += part.Text
		}
	}
	tokensIn = result.UsageMetadata.PromptTokenCount
	tokensOut = result.UsageMetadata.CandidatesTokenCount

	return &ChatResponse{
		Content:   content,
		TokensIn:  tokensIn,
		TokensOut: tokensOut,
	}, nil
}

func (p *GeminiProvider) ChatStream(ctx context.Context, model string, messages []Message, opts ChatOptions) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk, 1)
	go func() {
		defer close(ch)
		// Gemini streaming not yet implemented - fall back to non-streaming
		resp, err := p.Chat(ctx, model, messages, opts)
		if err != nil {
			sendChunk(ctx, ch, StreamChunk{Error: err})
			return
		}
		// The fallback's single terminal frame carries the full answer.
		sendChunk(ctx, ch, StreamChunk{Content: resp.Content, Done: true})
	}()
	return ch, nil
}
