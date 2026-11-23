package gateway

import (
	"context"
	"fmt"

	"github.com/noosphere/backend/internal/workspaces"
)

type admittedModelKey struct{}

// SetWorkspaceManager is called once during bootstrap, before serving requests.
func (g *Gateway) SetWorkspaceManager(manager *workspaces.Manager) {
	g.workspaceManager = manager
	if manager != nil {
		manager.SetModelResolver(func(workspaceID, sessionID, modelID string) (string, func(context.Context) context.Context, error) {
			model, err := g.SessionModel(workspaceID, sessionID, modelID)
			if err != nil {
				return "", nil, err
			}
			return model.ID, func(ctx context.Context) context.Context {
				return context.WithValue(ctx, admittedModelKey{}, model)
			}, nil
		})
	}
}

// RunModel keeps all Agent turns on the resolved model configuration from
// admission. Only the model ID is persisted in the task record.
func (g *Gateway) RunModel(ctx context.Context, workspaceID, sessionID, modelID string) (*ModelConfig, error) {
	if model, ok := ctx.Value(admittedModelKey{}).(*ModelConfig); ok {
		if model.ID != modelID {
			return nil, fmt.Errorf("任务的模型绑定已变化，请重新发起")
		}
		return model, nil
	}
	return g.SessionModel(workspaceID, sessionID, modelID)
}

func (g *Gateway) WorkspaceModel(workspaceID, modelID string) (*ModelConfig, error) {
	if workspaceID == "" {
		workspaceID = "local"
	}
	if g.workspaceManager != nil {
		ws, err := g.workspaceManager.Workspace(workspaceID)
		if err != nil {
			return nil, err
		}
		if modelID == "" {
			modelID = ws.DefaultModelID
		}
	} else if workspaceID != "" && workspaceID != "local" {
		return nil, fmt.Errorf("workspace storage unavailable")
	}
	model := g.findModel(modelID)
	if model == nil {
		return nil, fmt.Errorf("model not found or disabled")
	}
	return model, nil
}
