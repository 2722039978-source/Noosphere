// Package rest — Nebula REST API Server
//
// 纯 Go 标准库实现，零外部 HTTP 框架依赖。
// 支持 CORS、JSON 请求/响应、健康检查。
//
// 端点:
//
//	GET  /health                     — 健康检查
//	GET  /api/v1/stats                — 引擎统计
//	POST /api/v1/sessions             — 创建会话
//	GET  /api/v1/sessions             — 列出会话
//	DELETE /api/v1/sessions/{id}      — 删除会话
//	POST /api/v1/sessions/{id}/memories     — 存储记忆
//	GET  /api/v1/sessions/{id}/memories/{mid} — 获取记忆
//	DELETE /api/v1/sessions/{id}/memories/{mid} — 删除记忆
//	POST /api/v1/sessions/{id}/search  — 检索记忆
//	GET  /api/v1/sessions/{id}/stats   — 会话统计
package rest

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/nebula-agent/nebula/engine/manager"
	"github.com/nebula-agent/nebula/service"
)

// Server REST API Server
type Server struct {
	svc      *service.Service
	addr     string
	mux      *http.ServeMux
	apiKey   string // DeepSeek API key
}

// NewServer 创建 REST Server
func NewServer(svc *service.Service, addr string) *Server {
	return NewServerWithLLM(svc, addr, "")
}

// NewServerWithLLM 创建带 LLM 集成 的 REST Server
func NewServerWithLLM(svc *service.Service, addr, apiKey string) *Server {
	s := &Server{
		svc:    svc,
		addr:   addr,
		mux:    http.NewServeMux(),
		apiKey: apiKey,
	}
	s.registerRoutes()
	return s
}

// Start 启动 HTTP 服务
func (s *Server) Start() error {
	log.Printf("[Nebula REST API] starting on %s", s.addr)
	handler := corsMiddleware(loggingMiddleware(s.mux))
	return http.ListenAndServe(s.addr, handler)
}

// ─── 路由注册 ───

func (s *Server) registerRoutes() {
	// 健康检查
	s.mux.HandleFunc("/health", s.handleHealth)

	// 所有 /api/ 请求统一走 API 路由器，避免 ServeMux 优先级问题
	s.mux.HandleFunc("/api/", s.handleAPI)

	// 静态文件（前端）— 其他所有路径
	s.mux.Handle("/", http.FileServer(http.Dir("./web")))
}

// handleAPI 手动分发 API 路由（彻底避免 Go ServeMux 匹配歧义）
func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	switch {
	case path == "/api/v1/stats":
		s.handleStats(w, r)
	case path == "/api/v1/sessions":
		s.handleSessions(w, r)
	case strings.HasPrefix(path, "/api/v1/sessions/"):
		s.handleSessionByID(w, r)
	case path == "/api/v1/chat":
		s.handleChat(w, r)
	default:
		writeError(w, http.StatusNotFound, "API endpoint not found: "+path)
	}
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	eng := s.svc.Engine()
	memAPI := manager.NewMemoryAPI(eng, eng, eng, eng)
	handler := NewChatHandler(memAPI, s.apiKey)
	handler.ServeHTTP(w, r)
}

// ─── Handlers ───

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"version": "0.1.0",
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	stats := s.svc.Stats()
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.createSession(w, r)
	case http.MethodGet:
		s.listSessions(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	// 解析路径: /api/v1/sessions/{id}/...
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/")
	parts := strings.SplitN(path, "/", 3)

	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "missing session id")
		return
	}

	sessionID := parts[0]

	if len(parts) == 1 {
		// /api/v1/sessions/{id}
		if r.Method == http.MethodDelete {
			s.deleteSession(w, r, sessionID)
		} else {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	// /api/v1/sessions/{id}/memories or /api/v1/sessions/{id}/search or /api/v1/sessions/{id}/stats
	switch parts[1] {
	case "memories":
		s.handleMemories(w, r, sessionID, parts)
	case "search":
		s.handleSearch(w, r, sessionID)
	case "stats":
		s.handleSessionStats(w, r, sessionID)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

// ─── Session CRUD ───

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.SessionID = fmt.Sprintf("session-%d", time.Now().UnixNano())
	}
	if req.SessionID == "" {
		req.SessionID = fmt.Sprintf("session-%d", time.Now().UnixNano())
	}

	sess := s.svc.GetSession(req.SessionID)
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"session_id": sess.ID(),
		"created":    true,
	})
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	sessions := s.svc.ListSessions()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"sessions": sessions,
	})
}

func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.svc.DeleteSession(id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"deleted": true,
	})
}

// ─── Memory CRUD ───

func (s *Server) handleMemories(w http.ResponseWriter, r *http.Request, sessionID string, parts []string) {
	if len(parts) == 2 {
		// /api/v1/sessions/{id}/memories
		switch r.Method {
		case http.MethodPost:
			s.storeMemory(w, r)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	// /api/v1/sessions/{id}/memories/{memory_id}
	memoryID := parts[2]
	switch r.Method {
	case http.MethodGet:
		s.getMemory(w, r, sessionID, memoryID)
	case http.MethodDelete:
		s.deleteMemory(w, r, sessionID, memoryID)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) storeMemory(w http.ResponseWriter, r *http.Request) {
	sessionID := extractSessionID(r.URL.Path)

	var req struct {
		Content    string            `json:"content"`
		Type       string            `json:"type"`
		Importance float64           `json:"importance"`
		Tags       []string          `json:"tags"`
		Metadata   map[string]string `json:"metadata"`
		TTL        int64             `json:"ttl_seconds"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}

	sess := s.svc.GetSession(sessionID)
	mtype := manager.MemoryTypeFromString(req.Type)

	var mem *manager.Memory
	switch mtype {
	case manager.EpisodicMemory:
		mem = manager.NewEpisodicMemory(sessionID, req.Content, req.Importance, req.Tags)
	case manager.SemanticMemory:
		mem = manager.NewSemanticMemory(sessionID, req.Content, nil)
	case manager.WorkingMemory:
		ttl := time.Duration(req.TTL) * time.Second
		if ttl <= 0 {
			ttl = 5 * time.Minute
		}
		mem = manager.NewWorkingMemory(sessionID, "", req.Content, ttl)
	default:
		mem = manager.NewEpisodicMemory(sessionID, req.Content, req.Importance, req.Tags)
	}

	if req.Metadata != nil {
		mem.Metadata = req.Metadata
	}

	if err := sess.Remember(mem); err != nil {
		writeError(w, http.StatusInternalServerError, "store failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":         mem.ID,
		"type":       mem.Type.String(),
		"created_at": mem.CreatedAt,
	})
}

func (s *Server) getMemory(w http.ResponseWriter, r *http.Request, sessionID, memoryID string) {
	sess := s.svc.GetSession(sessionID)
	mem, err := sess.Recall(memoryID)
	if err != nil {
		writeError(w, http.StatusNotFound, "memory not found")
		return
	}
	writeJSON(w, http.StatusOK, mem)
}

func (s *Server) deleteMemory(w http.ResponseWriter, r *http.Request, sessionID, memoryID string) {
	sess := s.svc.GetSession(sessionID)
	if err := sess.Forget(memoryID); err != nil {
		writeError(w, http.StatusNotFound, "memory not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"deleted": true,
	})
}

// ─── Search ───

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Query       string   `json:"query"`
		TopK        int      `json:"top_k"`
		Strategy    string   `json:"strategy"`
		MemoryTypes []string `json:"memory_types"`
		Threshold   float64  `json:"threshold"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}

	if req.TopK <= 0 {
		req.TopK = 10
	}

	opts := &manager.SearchOptions{
		Query:     req.Query,
		TopK:      req.TopK,
		Threshold: req.Threshold,
	}

	switch req.Strategy {
	case "vector":
		opts.Strategy = manager.VectorSearch
	case "keyword":
		opts.Strategy = manager.KeywordSearch
	case "temporal":
		opts.Strategy = manager.TemporalSearch
	default:
		opts.Strategy = manager.HybridSearch
	}

	for _, mt := range req.MemoryTypes {
		opts.MemoryTypes = append(opts.MemoryTypes, manager.MemoryTypeFromString(mt))
	}

	sess := s.svc.GetSession(sessionID)
	results, err := sess.Reminisce(opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"results": results,
		"count":   len(results),
	})
}

// ─── Session Stats ───

func (s *Server) handleSessionStats(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	sess := s.svc.GetSession(sessionID)
	stats := sess.Stats()
	writeJSON(w, http.StatusOK, stats)
}

// ─── 中间件 ───

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("[%s] %s %s (%v)", r.Method, r.URL.Path, r.RemoteAddr, time.Since(start))
	})
}

// ─── 辅助 ───

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]interface{}{
		"error":   message,
		"status":  status,
	})
}

func extractSessionID(path string) string {
	path = strings.TrimPrefix(path, "/api/v1/sessions/")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}
