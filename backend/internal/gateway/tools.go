package gateway

import (
	"context"
	"fmt"
	"github.com/noosphere/backend/internal/gateway/providers"
	"time"
)

func (g *Gateway) ToolCapability(model *ModelConfig) error {
	if model == nil {
		return fmt.Errorf("model unavailable")
	}
	switch model.Provider {
	case "deepseek", "openai", "openai_compatible", "ollama":
	default:
		return fmt.Errorf("模型提供方 %s 尚未接入结构化工具协议，请选择 DeepSeek 或 OpenAI 兼容模型", model.Provider)
	}
	provider, err := g.getOrCreateProvider(model)
	if err != nil {
		return err
	}
	if _, ok := provider.(providers.ToolProvider); !ok {
		return fmt.Errorf("model provider does not support structured tools")
	}
	return nil
}

// Each invocation counts as exactly one model request. Do not hide retry costs.
func (g *Gateway) ChatTools(ctx context.Context, model *ModelConfig, workspaceID string, messages []providers.ToolMessage, tools []providers.ToolDefinition) (*providers.ToolResponse, error) {
	if err := g.ToolCapability(model); err != nil {
		return nil, err
	}
	if err := g.acquireCall(ctx); err != nil {
		return nil, err
	}
	defer func() { <-g.sem }()
	provider, err := g.getOrCreateProvider(model)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	result, err := provider.(providers.ToolProvider).ChatTools(ctx, model.ModelName, messages, tools, providers.ChatOptions{MaxTokens: 2048})
	entry := CallLog{ID: fmt.Sprintf("tool-%d", time.Now().UnixNano()), ModelID: model.ID, Provider: model.Provider, Project: workspaceID, RequestPreview: "Agent structured tool turn", Status: streamLogStatus(err), LatencyMs: float64(time.Since(start).Milliseconds()), Timestamp: time.Now().UTC().Format(time.RFC3339)}
	if result != nil {
		entry.TokensIn = result.TokensIn
		entry.TokensOut = result.TokensOut
	}
	if err != nil {
		entry.Error = err.Error()
	}
	g.appendLog(entry)
	return result, err
}
