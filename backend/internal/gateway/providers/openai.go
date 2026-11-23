package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func init() {
	Register("openai", NewOpenAI)
	Register("openai_compatible", NewLocalCompatible)
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
	openAIEndpoint bool
}

func NewOpenAI(baseURL, apiKey string) (LLMProvider, error) {
	endpoint, _ := url.Parse(baseURL)
	return &OpenAIProvider{
		baseURL:        baseURL,
		apiKey:         apiKey,
		openAIEndpoint: endpoint != nil && strings.EqualFold(endpoint.Hostname(), "api.openai.com"),
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

// isReasoningModel reports models that use the newer OpenAI parameter shape:
// no explicit temperature and max_completion_tokens instead of max_tokens.
func isReasoningModel(model string) bool {
	for _, prefix := range []string{"o1", "o3", "o4", "o5", "gpt-5", "gpt-6"} {
		if strings.HasPrefix(model, prefix) {
			return true
		}
	}
	return false
}

// applyChatOptions writes the output budget and sampling fields. An explicit
// ChatOptions value wins; otherwise the endpoint and model name decide, which
// keeps official OpenAI reasoning models working without breaking older
// third-party endpoints that only accept max_tokens.
func (p *OpenAIProvider) applyChatOptions(body map[string]interface{}, model string, temperature float64, maxTokens int, opts ChatOptions) {
	field := strings.TrimSpace(opts.MaxTokensField)
	omitTemperature := opts.OmitTemperature
	if field == "" {
		if p.openAIEndpoint && isReasoningModel(model) {
			field = "max_completion_tokens"
			omitTemperature = true
		} else {
			field = "max_tokens"
		}
	}
	body[field] = maxTokens
	if omitTemperature {
		return
	}
	body["temperature"] = temperature
}

// maxAdjustmentAttempts bounds how many times one request may be rebuilt after
// the upstream names a rejected parameter. Each retry only reacts to a newly
// reported field, so a content-level rejection is never retried.
const maxAdjustmentAttempts = 2

// maxNetworkRetries bounds transport-level retries for one request. The request
// body is rebuilt each time, so the caller never observes a half-sent payload.
const maxNetworkRetries = 1

func retryableTransportError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, needle := range []string{"unexpected eof", "connection reset", "broken pipe", "server closed idle connection"} {
		if strings.Contains(message, needle) {
			return true
		}
	}
	return false
}

// adjustChatOptions turns one rejected request into the parameter set the
// upstream says it wants. It only reacts to provider-reported parameter names,
// so a content-level 400 is never retried.
func adjustChatOptions(detail string, opts ChatOptions) (ChatOptions, bool) {
	lower := strings.ToLower(detail)
	adjusted := opts
	changed := false
	if !strings.EqualFold(opts.MaxTokensField, "max_completion_tokens") &&
		(strings.Contains(lower, "max_completion_tokens") ||
			(strings.Contains(lower, "max_tokens") && strings.Contains(lower, "unsupported"))) {
		adjusted.MaxTokensField = "max_completion_tokens"
		changed = true
	}
	if !opts.OmitTemperature &&
		strings.Contains(lower, "temperature") &&
		(strings.Contains(lower, "unsupported") || strings.Contains(lower, "does not support") || strings.Contains(lower, "only the default")) {
		adjusted.OmitTemperature = true
		changed = true
	}
	return adjusted, changed
}

type acceptedChatResponse struct {
	*http.Response
	temperatureOmitted bool
}

// postChat applies bounded parameter adjustments when an upstream reports a
// rejected field. The caller owns the response body. A nonpositive maxBody
// disables the request body size check.
func (p *OpenAIProvider) postChat(ctx context.Context, path string, build func(ChatOptions) map[string]interface{}, opts ChatOptions, maxBody int) (*acceptedChatResponse, error) {
	attempts := 0
	networkRetries := 0
	for {
		body := build(opts)
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
		if maxBody > 0 && len(encoded) > maxBody {
			return nil, fmt.Errorf("model request exceeds %d KiB", maxBody/1024)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.baseURL, "/")+path, bytes.NewReader(encoded))
		if err != nil {
			return nil, fmt.Errorf("request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if p.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+p.apiKey)
		}
		resp, err := sendChatRequest(p.client, req)
		if err != nil {
			// A dropped connection is common on public relays and the request is
			// a read-only completion, so one retry is safe.
			if networkRetries < maxNetworkRetries && ctx.Err() == nil && retryableTransportError(err) {
				networkRetries++
				continue
			}
			return nil, fmt.Errorf("fetch: %w", err)
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			_, temperatureSent := body["temperature"]
			return &acceptedChatResponse{Response: resp, temperatureOmitted: !temperatureSent}, nil
		}
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		resp.Body.Close()
		if attempts < maxAdjustmentAttempts {
			if adjusted, ok := adjustChatOptions(string(detail), opts); ok {
				opts = adjusted
				attempts++
				continue
			}
		}
		return nil, &UpstreamHTTPError{Status: resp.StatusCode, Detail: truncateDetail(p.redact(string(detail)))}
	}
}

func truncateDetail(message string) string {
	if len(message) > 1000 {
		return message[:1000]
	}
	return message
}

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

	build := func(o ChatOptions) map[string]interface{} {
		body := map[string]interface{}{
			"model": model, "messages": msgs,
			"stream": false,
		}
		p.applyChatOptions(body, model, temp, maxT, o)
		if p.directResponse {
			body["thinking"] = map[string]string{"type": "disabled"}
		}
		return body
	}
	resp, err := p.postChat(ctx, "/chat/completions", build, opts, 0)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}

	var result struct {
		Model   string `json:"model"`
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
		Content: content, Model: result.Model, TokensIn: result.Usage.PromptTokens,
		TokensOut: result.Usage.CompletionTokens, TemperatureOmitted: resp.temperatureOmitted,
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

	build := func(o ChatOptions) map[string]interface{} {
		body := map[string]interface{}{
			"model": model, "messages": msgs,
			"stream": true,
		}
		p.applyChatOptions(body, model, temp, maxT, o)
		if p.directResponse {
			body["thinking"] = map[string]string{"type": "disabled"}
		}
		return body
	}
	resp, err := p.postChat(ctx, "/chat/completions", build, opts, 0)
	if err != nil {
		return nil, err
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
				sendChunk(ctx, ch, StreamChunk{Done: true, Content: full.String(), TemperatureOmitted: resp.temperatureOmitted})
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
	return RedactCredential(message, p.apiKey)
}
