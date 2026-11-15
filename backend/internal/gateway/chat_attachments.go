package gateway

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/noosphere/backend/internal/workspaces"
)

func (g *Gateway) resolveChatAttachments(ctx context.Context, workspaceID, sessionID string, turn workspaces.AttachmentTurn, messages []map[string]string) ([]map[string]string, workspaces.AttachmentContext, error) {
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
	sources, err = g.workspaceManager.PrepareAttachments(ctx, workspaceID, sessionID, turn)
	if err != nil {
		return nil, sources, err
	}
	// Keep the stored history and request log free of the attachment body.
	out := slices.Clone(messages)
	out[i] = maps.Clone(messages[i])
	out[i]["content"] += sources.Prompt()
	return out, sources, nil
}
