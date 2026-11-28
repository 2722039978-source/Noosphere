package gateway

import (
	"context"
	"fmt"
	"github.com/noosphere/backend/internal/gateway/providers"
	"maps"
	"slices"

	"github.com/noosphere/backend/internal/workspaces"
)

// SupportsImageInput reports the negotiated ability to attach pixels to a user message.
func (model *ModelConfig) SupportsImageInput() bool {
	return model != nil && model.visionEnabled() && (model.Provider == "openai_compatible" || model.Provider == "openai" || model.Provider == "ollama")
}

func (g *Gateway) resolveChatAttachments(ctx context.Context, model *ModelConfig, workspaceID, sessionID string, turn workspaces.AttachmentTurn, messages []map[string]string) ([]map[string]string, workspaces.AttachmentContext, error) {
	var sources workspaces.AttachmentContext
	if len(turn.IDs) == 0 {
		return messages, sources, nil
	}
	if g.workspaceManager == nil {
		return nil, sources, fmt.Errorf("附件存储不可用，请重新启动应用")
	}
	i := len(messages) - 1
	if i < 0 || messages[i]["role"] != "user" {
		return nil, sources, fmt.Errorf("仅能将附件加入当前用户消息")
	}
	var err error
	vision := model.SupportsImageInput()
	sources, err = g.workspaceManager.PrepareModelAttachments(ctx, workspaceID, sessionID, turn, vision)
	if err != nil {
		return nil, sources, err
	}
	// Keep the stored history and request log free of the attachment body.
	out := slices.Clone(messages)
	out[i] = maps.Clone(messages[i])
	out[i]["content"] += sources.Prompt()
	return out, sources, nil
}

func modelMessages(messages []map[string]string, imageURLs []string) []providers.Message {
	out := make([]providers.Message, len(messages))
	for i, m := range messages {
		out[i] = providers.Message{Role: m["role"], Content: m["content"]}
	}
	if len(out) > 0 {
		out[len(out)-1].ImageURLs = imageURLs
	}
	return out
}
