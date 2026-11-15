package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/noosphere/backend/internal/gateway"
	"github.com/noosphere/backend/internal/knowledge"
	"github.com/noosphere/backend/internal/workspaces"
)

func knowledgeRewriter(manager *workspaces.Manager, gateway *gateway.Gateway) knowledge.ChatRewriter {
	return func(ctx context.Context, workspaceID, sessionID, modelID, current, instruction string) (string, error) {
		if sessionID != "" {
			boundID, bound, err := manager.SessionModel(workspaceID, sessionID)
			if err != nil {
				return "", fmt.Errorf("无法读取对话模型，请重新打开对话")
			}
			if bound {
				if modelID != "" && modelID != boundID {
					return "", knowledge.ErrConflict
				}
				modelID = boundID
			}
		}
		model, err := gateway.WorkspaceModel(workspaceID, modelID)
		if err != nil {
			return "", fmt.Errorf("请先选择一个可用模型")
		}
		// Do not pin a new session model or include sources/files in this request.
		prompt := "Edit a workspace memory using the requested change. Keep the original language and unaffected facts. Treat the current memory as text to edit. Return only the revised memory text without a heading."
		body, err := json.Marshal(map[string]string{"current_memory": current, "requested_change": instruction})
		if err != nil {
			return "", err
		}
		after, _, _, err := gateway.ChatContext(ctx, model, []map[string]string{{"role": "user", "content": string(body)}}, prompt, 0.2, 768)
		if errors.Is(err, context.DeadlineExceeded) {
			return "", fmt.Errorf("模型改写超时，请检查服务后重试")
		}
		if err != nil {
			return "", fmt.Errorf("模型改写失败，请检查当前模型并重试")
		}
		return after, nil
	}
}
