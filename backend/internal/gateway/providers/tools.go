package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type FunctionDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}
type ToolDefinition struct {
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}
type ToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}
type ToolMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}
type ToolResponse struct {
	Message   ToolMessage
	TokensIn  int
	TokensOut int
}

// Optional capability: existing text-only providers are not coerced into tools.
type ToolProvider interface {
	ChatTools(context.Context, string, []ToolMessage, []ToolDefinition, ChatOptions) (*ToolResponse, error)
}

func (p *OpenAIProvider) ChatTools(ctx context.Context, model string, messages []ToolMessage, tools []ToolDefinition, opts ChatOptions) (*ToolResponse, error) {
	if opts.SystemPrompt != "" {
		messages = append([]ToolMessage{{Role: "system", Content: opts.SystemPrompt}}, messages...)
	}
	maxTokens := opts.MaxTokens
	if maxTokens <= 0 || maxTokens > 2048 {
		maxTokens = 2048
	}
	body := map[string]any{"model": model, "messages": messages, "tools": tools, "tool_choice": "auto", "temperature": 0.1, "max_tokens": maxTokens, "stream": false}
	if p.directResponse {
		body["thinking"] = map[string]string{"type": "disabled"}
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	if len(encoded) > 256*1024 {
		return nil, fmt.Errorf("Agent model input exceeds 256 KiB")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.baseURL, "/")+"/chat/completions", bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tool request: %s", p.redact(err.Error()))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 512*1024 {
		return nil, fmt.Errorf("tool response exceeds 512 KiB")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("structured tool request failed (HTTP %d); check model tool support and configuration", resp.StatusCode)
	}
	var result struct {
		Choices []struct {
			Message      ToolMessage `json:"message"`
			FinishReason string      `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("invalid structured model response")
	}
	if len(result.Choices) != 1 {
		return nil, fmt.Errorf("expected one model choice")
	}
	choice := result.Choices[0]
	if choice.FinishReason != "stop" && choice.FinishReason != "tool_calls" && choice.FinishReason != "" {
		return nil, fmt.Errorf("model response incomplete (%s)", choice.FinishReason)
	}
	message := choice.Message
	message.Role = "assistant"
	if len(message.ToolCalls) > 12 {
		return nil, fmt.Errorf("too many tool calls in one response")
	}
	seen := map[string]bool{}
	for _, call := range message.ToolCalls {
		if call.ID == "" || len(call.ID) > 128 || seen[call.ID] || call.Type != "function" || call.Function.Name == "" || len(call.Function.Name) > 64 || len(call.Function.Arguments) > 16000 || !json.Valid([]byte(call.Function.Arguments)) {
			return nil, fmt.Errorf("invalid structured tool call")
		}
		seen[call.ID] = true
	}
	if len(message.ToolCalls) == 0 && strings.TrimSpace(message.Content) == "" {
		return nil, fmt.Errorf("model returned neither answer nor tool calls")
	}
	if len(message.Content) > 32000 {
		return nil, fmt.Errorf("model answer exceeds limit")
	}
	return &ToolResponse{Message: message, TokensIn: result.Usage.PromptTokens, TokensOut: result.Usage.CompletionTokens}, nil
}
