package api

import (
	"encoding/json"
	"net/http"

	"github.com/noosphere/backend/internal/devops/agent"
)

type Handler struct {
	agent *agent.Agent
}

func NewHandler(a *agent.Agent) *Handler {
	return &Handler{agent: a}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/devops/status", h.handleStatus)
	mux.HandleFunc("/api/v1/devops/metrics", h.handleMetrics)
	mux.HandleFunc("/api/v1/devops/diagnose", h.handleDiagnose)
	mux.HandleFunc("/api/v1/devops/tools", h.handleTools)
	mux.HandleFunc("/api/v1/devops/tools/execute", h.handleToolExecute)
	mux.HandleFunc("/api/v1/devops/loop", h.handleLoop)
}

func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	snap, _ := h.agent.Snapshot()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"agent_state": string(h.agent.State()),
		"tools_count": len(h.agent.ListTools()),
		"metrics":     snap,
	})
}

func (h *Handler) handleMetrics(w http.ResponseWriter, r *http.Request) {
	snap, err := h.agent.Snapshot()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (h *Handler) handleDiagnose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Issue string `json:"issue"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Issue == "" {
		req.Issue = "general system check"
	}
	result := h.agent.Diagnose(req.Issue)
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) handleTools(w http.ResponseWriter, r *http.Request) {
	tools := h.agent.ListTools()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tools": tools,
		"count": len(tools),
	})
}

func (h *Handler) handleToolExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Name string         `json:"name"`
		Args map[string]any `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "tool name required")
		return
	}
	if req.Args == nil {
		req.Args = make(map[string]any)
	}
	result := h.agent.ExecuteToolContext(r.Context(), req.Name, req.Args)
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) handleLoop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Issue       string `json:"issue"`
		MaxSteps    int    `json:"max_steps"`
		AutoApprove bool   `json:"auto_approve"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Issue == "" {
		req.Issue = "general system check"
	}
	if req.MaxSteps <= 0 {
		req.MaxSteps = 5
	}

	var confirmFn agent.RiskConfirmFn
	if req.AutoApprove {
		confirmFn = func(toolName string, level agent.RiskLevel, args map[string]any) bool {
			return true
		}
	}
	result := h.agent.LoopContext(r.Context(), req.Issue, req.MaxSteps, confirmFn)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"issue":      req.Issue,
		"steps":      len(result.Steps),
		"tools_used": result.ToolsUsed,
		"answer":     result.Answer,
		"trace":      result.Steps,
	})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]interface{}{"error": msg, "status": status})
}
