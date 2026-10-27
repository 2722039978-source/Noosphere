package gateway

import (
	"fmt"
	"github.com/noosphere/backend/internal/workspaces"
)

// SetWorkspaceManager is called once during bootstrap, before serving requests.
func (g *Gateway) SetWorkspaceManager(manager *workspaces.Manager) {
	g.workspaceManager = manager
	if manager != nil {
		manager.SetModelResolver(func(workspaceID, sessionID, modelID string) (string, error) {
			model, err := g.SessionModel(workspaceID, sessionID, modelID)
			if err != nil {
				return "", err
			}
			return model.ID, nil
		})
	}
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
