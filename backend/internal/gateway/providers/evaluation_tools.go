package providers

import (
	"encoding/json"
	"strings"
)

// Evaluation tools retain the identity-bound evaluation transport. The ordinary
// chat tool client has different option and provenance semantics.
func validateEvaluationTools(req EvaluationRequest) error {
	if len(req.Tools) == 0 {
		if len(req.ToolMessages) != 0 || req.ToolChoice != "" {
			return evaluationError("INVALID_REQUEST", "tool messages require tool definitions")
		}
		return nil
	}
	if req.Kind == "warmup" || len(req.Tools) > 4 || len(req.ToolMessages) < 1 || len(req.ToolMessages) > 16 || req.ToolChoice != "required" && req.ToolChoice != "none" {
		return evaluationError("INVALID_REQUEST", "bounded native tool request required")
	}
	names := map[string]bool{}
	for _, tool := range req.Tools {
		if tool.Type != "function" || !evaluationToolName(tool.Function.Name) || names[tool.Function.Name] || len(tool.Function.Description) > 2000 || len(tool.Function.Parameters) > 16000 || !json.Valid(tool.Function.Parameters) {
			return evaluationError("INVALID_REQUEST", "invalid tool definition")
		}
		names[tool.Function.Name] = true
	}
	total := 0
	for _, message := range req.ToolMessages {
		if message.Role != "system" && message.Role != "user" && message.Role != "assistant" && message.Role != "tool" {
			return evaluationError("INVALID_REQUEST", "invalid tool conversation role")
		}
		if len(message.Content) > 16000 || len(message.ToolCalls) > 4 || message.Role == "tool" && (message.ToolCallID == "" || len(message.ToolCallID) > 256) || len(message.ToolCalls) > 0 && message.Role != "assistant" {
			return evaluationError("INVALID_REQUEST", "invalid tool conversation bounds")
		}
		for _, call := range message.ToolCalls {
			if call.ID == "" || len(call.ID) > 256 || call.Type != "function" || !evaluationToolName(call.Function.Name) || len(call.Function.Arguments) > 16000 {
				return evaluationError("INVALID_REQUEST", "invalid prior tool call")
			}
		}
		total += len(message.Content)
	}
	if total > 64000 {
		return evaluationError("INVALID_REQUEST", "tool conversation exceeds limit")
	}
	return nil
}

func evaluationToolName(name string) bool {
	if len(name) < 1 || len(name) > 64 {
		return false
	}
	return strings.IndexFunc(name, func(r rune) bool {
		return r != '_' && r != '-' && !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9')
	}) < 0
}

func parseEvaluationToolCalls(value any) ([]ToolCall, error) {
	if value == nil {
		return nil, nil
	}
	items, ok := value.([]any)
	if !ok || len(items) > 4 {
		return nil, evaluationError("INVALID_OUTPUT", "invalid native tool call list")
	}
	calls := []ToolCall{}
	seen := map[string]bool{}
	for _, item := range items {
		call, ok := item.(map[string]any)
		fn, fnOK := call["function"].(map[string]any)
		args, argsOK := fn["arguments"].(string)
		id, name := str(call, "id"), str(fn, "name")
		if !ok || !fnOK || !argsOK || len(args) > 16000 || id == "" || len(id) > 256 || seen[id] || str(call, "type") != "function" || !evaluationToolName(name) {
			return nil, evaluationError("INVALID_OUTPUT", "invalid native function call protocol")
		}
		seen[id] = true
		calls = append(calls, ToolCall{ID: id, Type: "function", Function: ToolFunction{Name: name, Arguments: args}})
	}
	return calls, nil
}
