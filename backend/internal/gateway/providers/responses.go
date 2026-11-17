package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// prefersResponses reports whether an upstream rejection means the model only
// accepts function tools over the Responses API.
func prefersResponses(detail string) bool {
	lower := strings.ToLower(detail)
	return strings.Contains(lower, "/v1/responses") || strings.Contains(lower, "reasoning_effort")
}

// chatToolsResponses runs one structured tool turn over /v1/responses. Some
// providers reject function tools on /chat/completions and only serve them
// here, so this path mirrors ChatTools without the chat-completions envelope.
func (p *OpenAIProvider) chatToolsResponses(ctx context.Context, model string, messages []ToolMessage, tools []ToolDefinition, opts ChatOptions) (*ToolResponse, error) {
	build := func(o ChatOptions) map[string]interface{} {
		maxTokens := o.MaxTokens
		if maxTokens <= 0 || maxTokens > 2048 {
			maxTokens = 2048
		}
		body := map[string]interface{}{
			"model":             model,
			"input":             responsesInput(messages),
			"tools":             responsesTools(tools),
			"tool_choice":       "auto",
			"max_output_tokens": maxTokens,
			"stream":            false,
		}
		if p.openAIEndpoint {
			body["store"] = false
		}
		if !o.OmitTemperature && o.Temperature != 0 {
			body["temperature"] = o.Temperature
		}
		if p.directResponse {
			body["thinking"] = map[string]string{"type": "disabled"}
		}
		return body
	}
	resp, err := p.postChat(ctx, "/responses", build, opts, 256*1024)
	if err != nil {
		var upstream *UpstreamHTTPError
		if errors.As(err, &upstream) {
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
		Output []json.RawMessage `json:"output"`
		Usage  struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("invalid structured model response")
	}
	message := ToolMessage{Role: "assistant", responsesOutput: result.Output}
	var answer strings.Builder
	for _, rawItem := range result.Output {
		var item struct {
			Type      string `json:"type"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
			Content   []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal(rawItem, &item); err != nil {
			return nil, fmt.Errorf("invalid structured model output item")
		}
		switch item.Type {
		case "function_call":
			message.ToolCalls = append(message.ToolCalls, ToolCall{
				ID: item.CallID, Type: "function",
				Function: ToolFunction{Name: item.Name, Arguments: item.Arguments},
			})
		case "message":
			for _, part := range item.Content {
				if part.Type == "output_text" {
					answer.WriteString(part.Text)
				}
			}
		}
	}
	message.Content = strings.TrimSpace(answer.String())
	if err := validateToolMessage(message); err != nil {
		return nil, err
	}
	return &ToolResponse{Message: message, TokensIn: result.Usage.InputTokens, TokensOut: result.Usage.OutputTokens}, nil
}

// responsesInput maps the chat-shaped turn history onto Responses API items.
func responsesInput(messages []ToolMessage) []any {
	items := make([]any, 0, len(messages))
	for _, message := range messages {
		switch {
		case len(message.responsesOutput) > 0:
			// Preserve all provider items in their original order, including
			// opaque reasoning state and the original function call IDs.
			for _, item := range message.responsesOutput {
				items = append(items, item)
			}
		case message.Role == "tool":
			items = append(items, map[string]interface{}{
				"type": "function_call_output", "call_id": message.ToolCallID, "output": message.Content,
			})
		case len(message.ToolCalls) > 0:
			if strings.TrimSpace(message.Content) != "" {
				items = append(items, map[string]interface{}{"role": "assistant", "content": message.Content})
			}
			for _, call := range message.ToolCalls {
				items = append(items, map[string]interface{}{
					"type": "function_call", "call_id": call.ID, "name": call.Function.Name, "arguments": call.Function.Arguments,
				})
			}
		default:
			role := message.Role
			if role == "" {
				role = "user"
			}
			items = append(items, map[string]interface{}{"role": role, "content": message.Content})
		}
	}
	return items
}

// responsesTools flattens function definitions into the Responses API shape.
func responsesTools(tools []ToolDefinition) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(tools))
	for _, tool := range tools {
		out = append(out, map[string]interface{}{
			"type":        "function",
			"name":        tool.Function.Name,
			"description": tool.Function.Description,
			"parameters":  json.RawMessage(tool.Function.Parameters),
		})
	}
	return out
}
