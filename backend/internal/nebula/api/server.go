package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/noosphere/backend/internal/nebula/engine"
	"github.com/noosphere/backend/internal/nebula/service"
)

type Server struct {
	eng      *engine.Engine
	pipeline *service.Pipeline
	mu       sync.RWMutex
	sessions map[string]*Session
}

type Session struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	memories  []Memory
}

type Memory struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	Content   string            `json:"content"`
	Tags      []string          `json:"tags,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt string            `json:"created_at"`
}

func NewServer(eng *engine.Engine) *Server {
	s := &Server{
		eng:      eng,
		pipeline: service.NewPipeline(eng),
		sessions: make(map[string]*Session),
	}
	s.restoreSessions()
	return s
}

func (s *Server) restoreSessions() {
	s.mu.Lock()
	defer s.mu.Unlock()
	restored := make(map[string]*Session)
	for key, value := range s.eng.Records("") {
		if strings.HasPrefix(key, "__session_meta:") {
			var sess Session
			if json.Unmarshal(value, &sess) == nil && sess.ID != "" {
				restored[sess.ID] = &sess
			}
		}
	}
	for _, value := range s.eng.Records("") {
		var record service.MemoryRecord
		if json.Unmarshal(value, &record) != nil || record.SessionID == "" || record.ID == "" {
			continue
		}
		sess := restored[record.SessionID]
		if sess == nil {
			sess = &Session{ID: record.SessionID, Name: record.SessionID, CreatedAt: record.CreatedAt}
			restored[sess.ID] = sess
		}
		sess.memories = append(sess.memories, Memory{ID: record.ID, Content: record.Content, Type: string(record.Type), Metadata: record.Metadata, Tags: record.Tags, CreatedAt: record.CreatedAt.Format(time.RFC3339)})
	}
	s.sessions = restored
}

func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/stats", s.handleStats)
	mux.HandleFunc("/api/v1/sessions", s.handleSessions)
	mux.HandleFunc("/api/v1/sessions/", s.handleSessionByID)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	stats := s.eng.Stats()
	s.mu.RLock()
	sc := len(s.sessions)
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"engine":    stats,
		"sessions":  sc,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listSessions(w, r)
	case http.MethodPost:
		s.createSession(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"session_id"`
		Name      string `json:"name"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.SessionID == "" {
		req.SessionID = fmt.Sprintf("session-%d", time.Now().UnixNano())
	}
	if req.Name == "" {
		req.Name = req.SessionID
	}
	if strings.ContainsAny(req.SessionID, ":/\\") || len(req.SessionID) > 128 {
		writeError(w, 400, "invalid session id")
		return
	}
	s.mu.Lock()
	s.sessions[req.SessionID] = &Session{
		ID: req.SessionID, Name: req.Name,
		CreatedAt: time.Now(), memories: make([]Memory, 0),
	}
	value, _ := json.Marshal(s.sessions[req.SessionID])
	if err := s.eng.PutText("__session_meta:"+req.SessionID, value, ""); err != nil {
		s.mu.Unlock()
		writeError(w, 500, "session persistence failed")
		return
	}
	s.mu.Unlock()
	log.Printf("[nebula] session created: %s", req.SessionID)
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"session_id": req.SessionID, "name": req.Name, "created": true,
	})
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	s.restoreSessions()
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]map[string]interface{}, 0, len(s.sessions))
	for _, sess := range s.sessions {
		list = append(list, map[string]interface{}{
			"id": sess.ID, "name": sess.Name,
			"created_at":   sess.CreatedAt.Format(time.RFC3339),
			"memory_count": len(sess.memories),
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"sessions": list, "count": len(list),
	})
}

func (s *Server) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "missing session id")
		return
	}
	sessionID := parts[0]
	if strings.ContainsAny(sessionID, ":\\") || len(sessionID) > 128 {
		writeError(w, 400, "invalid session id")
		return
	}
	if len(parts) == 1 {
		if r.Method == http.MethodDelete {
			s.deleteSession(w, r, sessionID)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	switch parts[1] {
	case "memories":
		s.handleMemories(w, r, sessionID, parts)
	case "search":
		s.handleSearch(w, r, sessionID)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.eng.ReplacePrefix(id+":", nil); err != nil {
		writeError(w, 500, "session delete failed")
		return
	}
	if err := s.eng.Delete("__session_meta:" + id); err != nil {
		writeError(w, 500, "session metadata delete failed")
		return
	}
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]interface{}{"deleted": true})
}

func (s *Server) getOrCreateSession(sessionID string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[sessionID]
	if !ok {
		sess = &Session{ID: sessionID, Name: sessionID, CreatedAt: time.Now(), memories: make([]Memory, 0)}
		s.sessions[sessionID] = sess
	}
	return sess
}

func (s *Server) handleMemories(w http.ResponseWriter, r *http.Request, sessionID string, parts []string) {
	sess := s.getOrCreateSession(sessionID)
	if len(parts) == 2 {
		if r.Method == http.MethodPost {
			s.storeMemory(w, r, sess)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	memoryID := parts[2]
	switch r.Method {
	case http.MethodGet:
		s.getMemory(w, r, sess, memoryID)
	case http.MethodDelete:
		s.deleteMemory(w, r, sess, memoryID)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) storeMemory(w http.ResponseWriter, r *http.Request, sess *Session) {
	var req struct {
		Content    string            `json:"content"`
		Type       string            `json:"type"`
		Importance float64           `json:"importance"`
		Tags       []string          `json:"tags"`
		Metadata   map[string]string `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	// Use pipeline for enriched processing
	output, err := s.pipeline.Ingest(service.PipelineInput{
		SessionID: sess.ID,
		Content:   req.Content,
		Tags:      req.Tags,
		Metadata:  req.Metadata,
	})
	if err != nil {
		log.Printf("[nebula] pipeline error: %v", err)
		writeError(w, http.StatusInternalServerError, "pipeline processing failed")
		return
	}

	// Keep in-memory session for backward compat
	mem := Memory{
		ID:        output.Record.ID,
		Type:      string(output.Record.Type),
		Content:   output.Record.Content,
		Tags:      output.Record.Tags,
		Metadata:  output.Record.Metadata,
		CreatedAt: output.Record.CreatedAt.Format(time.RFC3339),
	}
	s.mu.Lock()
	sess.memories = append(sess.memories, mem)
	s.mu.Unlock()

	log.Printf("[nebula] memory stored via pipeline: %s/%s type=%s importance=%.2f",
		sess.ID, mem.ID, output.Type, output.Importance)
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id": mem.ID, "type": output.Type, "importance": output.Importance,
		"summary": output.Summary, "created_at": mem.CreatedAt,
	})
}

func (s *Server) getMemory(w http.ResponseWriter, r *http.Request, sess *Session, memoryID string) {
	if raw, err := s.eng.Get(sess.ID + ":" + memoryID); err == nil {
		var record service.MemoryRecord
		if json.Unmarshal(raw, &record) == nil {
			writeJSON(w, 200, record)
			return
		}
	}
	writeError(w, http.StatusNotFound, "memory not found")
}

func (s *Server) deleteMemory(w http.ResponseWriter, r *http.Request, sess *Session, memoryID string) {
	s.mu.Lock()
	filtered := make([]Memory, 0)
	for _, m := range sess.memories {
		if m.ID != memoryID {
			filtered = append(filtered, m)
		}
	}
	sess.memories = filtered
	s.mu.Unlock()
	key := fmt.Sprintf("%s:%s", sess.ID, memoryID)
	s.eng.Delete(key)
	writeJSON(w, http.StatusOK, map[string]interface{}{"deleted": true})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request, sessionID string) {
	// Use pipeline for enriched search
	_ = r
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Query string `json:"query"`
		TopK  int    `json:"top_k"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}
	if req.TopK <= 0 {
		req.TopK = 10
	}
	if req.TopK > 100 {
		req.TopK = 100
	}
	results, err := s.pipeline.Search(service.SearchRequest{
		SessionID: sessionID,
		Query:     req.Query,
		TopK:      req.TopK,
	})
	if err != nil {
		log.Printf("[nebula] search error: session=%s query=%q err=%v", sessionID, req.Query, err)
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}
	log.Printf("[nebula] search ok: session=%s query=%q -> %d hits", sessionID, req.Query, len(results))
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"results": results, "count": len(results), "query": req.Query,
	})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]interface{}{
		"error": message, "status": status,
	})
}
