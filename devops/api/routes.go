// Package api — DevOps Agent REST API
//
// 提供运维 Agent 的 HTTP 接口：系统指标查询、诊断任务、工具调用、日志分析。
// 纯 Go net/http 标准库实现，零框架依赖。
package api

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/devops-agent/agent"
	"github.com/devops-agent/memory"
	"github.com/devops-agent/metrics"
)

// DevOpsHandler DevOps API 处理器
type DevOpsHandler struct {
	devopsAgent *agent.DevOpsAgent
	collector   *metrics.SystemCollector
	faultStore  *memory.FaultStore
}

// NewDevOpsHandler 创建处理器
func NewDevOpsHandler(devopsAgent *agent.DevOpsAgent) *DevOpsHandler {
	return &DevOpsHandler{
		devopsAgent: devopsAgent,
		collector:   metrics.NewSystemCollector(),
		faultStore:  memory.NewFaultStore(devopsAgent.Engine(), "devops-agent"),
	}
}

// RegisterRoutes 注册 DevOps 路由
func (h *DevOpsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/devops/status", h.handleStatus)
	mux.HandleFunc("/api/v1/devops/metrics", h.handleMetrics)
	mux.HandleFunc("/api/v1/devops/diagnose", h.handleDiagnose)
	mux.HandleFunc("/api/v1/devops/tools", h.handleTools)
	mux.HandleFunc("/api/v1/devops/tools/execute", h.handleToolExecute)
	mux.HandleFunc("/api/v1/devops/logs/analyze", h.handleLogAnalyze)
	mux.HandleFunc("/api/v1/devops/logs/search", h.handleLogSearch)
	mux.HandleFunc("/api/v1/devops/faults", h.handleFaults)
	mux.HandleFunc("/api/v1/devops/faults/search", h.handleFaultSearch)
}

// ─── Status ───

func (h *DevOpsHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	snapshot, _ := h.collector.Collect()
	writeJSON(w, http.StatusOK, map[string]any{
		"agent_state": string(h.devopsAgent.State()),
		"tools_count": h.devopsAgent.Registry().Count(),
		"metrics":     snapshot,
		"timestamp":   time.Now(),
	})
}

// ─── Metrics ───

func (h *DevOpsHandler) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 GET")
		return
	}
	snapshot, err := h.collector.Collect()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "采集指标失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

// ─── Diagnose ───

func (h *DevOpsHandler) handleDiagnose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 POST")
		return
	}

	var req struct {
		Query      string   `json:"query"`
		SessionID  string   `json:"session_id"`
		ServerName string   `json:"server_name"`
		Tags       []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "JSON 解析失败: "+err.Error())
		return
	}
	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "query 不能为空")
		return
	}
	if req.SessionID == "" {
		req.SessionID = "devops-agent"
	}

	task := &agent.AgentTask{
		ID:         genID("diag"),
		SessionID:  req.SessionID,
		Query:      req.Query,
		ServerName: req.ServerName,
		Tags:       req.Tags,
		MaxTools:   8,
	}

	resp, err := h.devopsAgent.Diagnose(task)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "诊断失败: "+err.Error())
		return
	}

	// 注入实时系统指标
	snapshot, _ := h.collector.Collect()
	resp.MetricsSnapshot = snapshot

	writeJSON(w, http.StatusOK, resp)
}

// ─── Tools ───

func (h *DevOpsHandler) handleTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 GET")
		return
	}
	category := r.URL.Query().Get("category")
	var tools []agent.ToolDef
	if category != "" {
		tools = h.devopsAgent.Registry().ListByCategory(agent.ToolCategory(category))
	} else {
		tools = h.devopsAgent.Registry().ListDefs()
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": tools, "count": len(tools)})
}

func (h *DevOpsHandler) handleToolExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 POST")
		return
	}
	var req struct {
		Name string         `json:"name"`
		Args map[string]any `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "JSON 解析失败: "+err.Error())
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "工具名称不能为空")
		return
	}

	call := agent.ToolCall{ID: genID("tool"), Name: req.Name, Args: req.Args}
	result, err := h.devopsAgent.ExecuteTool(call)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "工具执行失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ─── Logs ───

func (h *DevOpsHandler) handleLogAnalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 POST")
		return
	}
	var req struct {
		Path  string `json:"path"`
		Level string `json:"level"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "JSON 解析失败: "+err.Error())
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "日志路径不能为空")
		return
	}

	// 调用 parse_error_log 工具
	call := agent.ToolCall{
		ID:   genID("log"),
		Name: "parse_error_log",
		Args: map[string]any{"path": req.Path, "level": req.Level},
	}
	result, err := h.devopsAgent.ExecuteTool(call)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "日志分析失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *DevOpsHandler) handleLogSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 POST")
		return
	}
	var req struct {
		Path      string `json:"path"`
		Keyword   string `json:"keyword"`
		TailLines int    `json:"tail_lines"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "JSON 解析失败: "+err.Error())
		return
	}
	if req.Path == "" || req.Keyword == "" {
		writeError(w, http.StatusBadRequest, "路径和关键词不能为空")
		return
	}

	call := agent.ToolCall{
		ID:   genID("log"),
		Name: "search_logs",
		Args: map[string]any{"path": req.Path, "keyword": req.Keyword, "tail": float64(req.TailLines)},
	}
	result, err := h.devopsAgent.ExecuteTool(call)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "日志搜索失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ─── Faults ───

func (h *DevOpsHandler) handleFaults(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		stats, err := h.faultStore.GetFaultStats()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, stats)
	case http.MethodPost:
		var record agent.FaultRecord
		if err := json.NewDecoder(r.Body).Decode(&record); err != nil {
			writeError(w, http.StatusBadRequest, "JSON 解析失败: "+err.Error())
			return
		}
		if record.ID == "" {
			record.ID = genID("fault")
		}
		if record.OccurredAt.IsZero() {
			record.OccurredAt = time.Now()
		}
		if err := h.faultStore.SaveFault(&record); err != nil {
			writeError(w, http.StatusInternalServerError, "保存失败: "+err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"saved": true, "id": record.ID})
	default:
		writeError(w, http.StatusMethodNotAllowed, "仅支持 GET/POST")
	}
}

func (h *DevOpsHandler) handleFaultSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 POST")
		return
	}
	var req struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "JSON 解析失败: "+err.Error())
		return
	}
	if req.Limit <= 0 {
		req.Limit = 5
	}

	faults, err := h.faultStore.SearchSimilarFaults(req.Query, req.Limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "搜索失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"faults": faults, "count": len(faults)})
}

// ─── Helpers ───

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message, "status": status})
}

func genID(prefix string) string {
	return prefix + "-" + time.Now().Format("20060102-150405")
}

var _ = log.Println
