package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/noosphere/backend/internal/errors"
	"github.com/noosphere/backend/internal/workspaces"
)

// Explicit selection never falls back to a different provider. Implicit selection
// uses the enabled default, then the first enabled model.
func (g *Gateway) findModel(modelID string) *ModelConfig {
	g.mu.RLock()
	defer g.mu.RUnlock()
	for _, m := range g.config.Models {
		if m.Enabled && (m.ID == modelID || (modelID == "" && m.ID == g.config.DefaultModel)) {
			c := m
			return &c
		}
	}
	if modelID != "" {
		return nil
	}
	for _, m := range g.config.Models {
		if m.Enabled {
			c := m
			return &c
		}
	}
	return nil
}

// findModelByID returns a copy of the model with the exact ID. No fallback.
func (g *Gateway) findModelByID(modelID string) *ModelConfig {
	g.mu.RLock()
	defer g.mu.RUnlock()
	for _, m := range g.config.Models {
		if m.ID == modelID {
			c := m
			return &c
		}
	}
	return nil
}

// HandleChat processes POST /api/gateway/chat — sends messages to an LLM and
// returns the response with token usage and latency.
func (g *Gateway) HandleChat(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	var body struct {
		Messages     []map[string]string `json:"messages"`
		ModelID      string              `json:"model_id"`
		WorkspaceID  string              `json:"workspace_id"`
		SessionID    string              `json:"session_id"`
		Project      string              `json:"project"`
		SystemPrompt string              `json:"system_prompt"`
		Temperature  float64             `json:"temperature"`
		MaxTokens    int                 `json:"max_tokens"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errors.WriteHTTP(w, r, errors.InvalidInput(errors.CatGateway, "invalid request body"))
		return
	}

	model, modelErr := g.SessionModel(body.WorkspaceID, body.SessionID, body.ModelID)
	if modelErr != nil {
		errors.WriteHTTP(w, r, errors.New(errors.ErrInvalidInput, errors.CatGateway, "model not found or disabled"))
		return
	}
	ctx := r.Context()
	var execution *workspaces.Execution
	if body.SessionID != "" && g.workspaceManager != nil {
		var err error
		ctx, execution, err = g.workspaceManager.Start(ctx, workspaces.Run{WorkspaceID: body.WorkspaceID, SessionID: body.SessionID, Kind: "chat", Title: "AI 对话", ModelID: model.ID})
		if err != nil {
			selectionFailure(w, err)
			return
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	start := time.Now()
	content, tokensIn, tokensOut, callErr := g.callLLMContext(ctx, model, body.Messages, body.SystemPrompt, body.Temperature, body.MaxTokens)
	var completionErr error
	if execution != nil {
		completionErr = execution.Finish(map[string]any{"content": content, "model_id": model.ID}, callErr)
		if completionErr == nil {
			committed, err := g.workspaceManager.Run(execution.ID)
			if err != nil {
				completionErr = err
			} else if committed.Status != "completed" {
				completionErr = fmt.Errorf("%w: 对话任务未完成或已取消", workspaces.ErrConflict)
			}
		}
	}
	if completionErr != nil {
		callErr = completionErr
	}
	latency := time.Since(start).Seconds() * 1000

	logEntry := CallLog{
		ID:              fmt.Sprintf("log-%d", time.Now().UnixNano()),
		ModelID:         model.ID,
		Provider:        model.Provider,
		Project:         body.Project,
		Status:          streamLogStatus(callErr),
		RequestPreview:  g.preview(body.Messages),
		ResponsePreview: previewText(content),
		TokensIn:        tokensIn,
		TokensOut:       tokensOut,
		LatencyMs:       latency,
		Timestamp:       time.Now().Format(time.RFC3339),
	}
	if callErr != nil {
		logEntry.Error = callErr.Error()
	}
	g.appendLog(logEntry)

	if completionErr != nil {
		selectionFailure(w, completionErr)
		return
	}
	if callErr != nil {
		json.NewEncoder(w).Encode(map[string]string{"error": callErr.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":          logEntry.ID,
		"model":       model.ModelName,
		"provider":    model.Provider,
		"content":     content,
		"tokens_used": tokensIn + tokensOut,
		"latency_ms":  latency,
		"project":     body.Project,
		"timestamp":   logEntry.Timestamp,
	})
}

// HandleTestModel processes POST /api/gateway/models/{id}/test — runs a
// connectivity test against the specified model's provider.
func (g *Gateway) HandleTestModel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	modelID := r.PathValue("id")
	if modelID == "" {
		errors.WriteHTTP(w, r, errors.InvalidInput(errors.CatGateway, "missing model id"))
		return
	}

	model := g.findModelByID(modelID)
	if model == nil {
		errors.WriteHTTP(w, r, errors.NotFound(errors.CatGateway, "model not found"))
		return
	}

	json.NewEncoder(w).Encode(g.testModelContext(r.Context(), model))
}
