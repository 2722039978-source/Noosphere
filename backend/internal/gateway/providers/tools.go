package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
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
	// ExtraContent carries provider-specific data that has to be echoed back on
	// the next turn (for example Gemini's thought_signature). It is preserved
	// verbatim so a provider that requires it keeps working.
	ExtraContent map[string]json.RawMessage `json:"extra_content,omitempty"`
}
type ToolMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	// Responses output belongs to the next request on the same transport.
	responsesOutput []json.RawMessage
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
	if strings.EqualFold(opts.ToolTransport, "responses") || slices.ContainsFunc(messages, func(message ToolMessage) bool {
		return len(message.responsesOutput) > 0
	}) {
		return p.chatToolsResponses(ctx, model, messages, tools, opts)
	}
	maxTokens := opts.MaxTokens
	if maxTokens <= 0 || maxTokens > 2048 {
		maxTokens = 2048
	}
	temperature := opts.Temperature
	if temperature == 0 {
		temperature = 0.1
	}
	build := func(o ChatOptions) map[string]interface{} {
		body := map[string]interface{}{
			"model": model, "messages": messages, "tools": tools,
			"tool_choice": "auto", "stream": false,
		}
		p.applyChatOptions(body, model, temperature, maxTokens, o)
		if p.directResponse {
			body["thinking"] = map[string]string{"type": "disabled"}
		}
		return body
	}
	resp, err := p.postChat(ctx, "/chat/completions", build, opts, 256*1024)
	if err != nil {
		var upstream *UpstreamHTTPError
		if errors.As(err, &upstream) {
			// The upstream may only serve function tools over the Responses API.
			if upstream.Status == 400 && prefersResponses(upstream.Detail) {
				return p.chatToolsResponses(ctx, model, messages, tools, opts)
			}
			return nil, fmt.Errorf("structured tool request failed (HTTP %d); check model tool support and configuration: %s", upstream.Status, upstream.Detail)
		}
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
	if err := validateToolMessage(message); err != nil {
		return nil, err
	}
	return &ToolResponse{Message: message, TokensIn: result.Usage.PromptTokens, TokensOut: result.Usage.CompletionTokens}, nil
}

// validateToolMessage applies the same limits to both tool transports so a
// provider cannot return more than the agent loop is prepared to execute.
func validateToolMessage(message ToolMessage) error {
	if len(message.ToolCalls) > 12 {
		return fmt.Errorf("too many tool calls in one response")
	}
	seen := map[string]bool{}
	for _, call := range message.ToolCalls {
		if call.ID == "" || len(call.ID) > 128 || seen[call.ID] || call.Type != "function" || call.Function.Name == "" || len(call.Function.Name) > 64 || len(call.Function.Arguments) > 16000 || !json.Valid([]byte(call.Function.Arguments)) {
			return fmt.Errorf("invalid structured tool call")
		}
		seen[call.ID] = true
	}
	if len(message.ToolCalls) == 0 && strings.TrimSpace(message.Content) == "" {
		return fmt.Errorf("model returned neither answer nor tool calls")
	}
	if len(message.Content) > 32000 {
		return fmt.Errorf("model answer exceeds limit")
	}
	return nil
}
