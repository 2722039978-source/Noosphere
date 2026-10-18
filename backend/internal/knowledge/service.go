// Package knowledge owns curated workspace facts, independently of chat archives.
package knowledge

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/noosphere/backend/internal/nebula/engine"
	"github.com/noosphere/backend/internal/nebula/engine/embed"
	"github.com/noosphere/backend/internal/nebula/service"
	"github.com/noosphere/backend/internal/workspaces"
)

var ErrConflict = errors.New("记录已被更新，请刷新后重试")

type Source struct {
	Kind        string `json:"kind"`
	File        string `json:"file,omitempty"`
	StartLine   int    `json:"start_line,omitempty"`
	EndLine     int    `json:"end_line,omitempty"`
	SourceHash  string `json:"source_hash,omitempty"`
	ProjectPath string `json:"project_path,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
	MessageID   string `json:"message_id,omitempty"`
}
type Memory struct {
	ID                 string `json:"id"`
	WorkspaceID        string `json:"workspace_id"`
	Content            string `json:"content"`
	Category           string `json:"category"`
	Status             string `json:"status"`
	Revision           int    `json:"revision"`
	Source             Source `json:"source"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
	ValidUntil         string `json:"valid_until,omitempty"`
	Supersedes         string `json:"supersedes,omitempty"`
	SupersedesRevision int    `json:"supersedes_revision,omitempty"`
	SupersededBy       string `json:"superseded_by,omitempty"`
	InactiveReason     string `json:"inactive_reason,omitempty"`
}
type Citation struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Excerpt     string `json:"excerpt"`
	File        string `json:"file,omitempty"`
	StartLine   int    `json:"start_line,omitempty"`
	EndLine     int    `json:"end_line,omitempty"`
	SourceHash  string `json:"source_hash,omitempty"`
	Symbol      string `json:"symbol,omitempty"`
	MemoryID    string `json:"memory_id,omitempty"`
	Revision    int    `json:"revision,omitempty"`
	WorkspaceID string `json:"workspace_id"`
	ProjectID   string `json:"project_id,omitempty"`
}
type ContextResult struct {
	WorkspaceID string     `json:"workspace_id"`
	ProjectID   string     `json:"project_id"`
	Mode        string     `json:"mode"`
	Context     string     `json:"context"`
	Citations   []Citation `json:"citations"`
	Warnings    []string   `json:"warnings"`
}
type Service struct {
	mu      sync.Mutex
	manager *workspaces.Manager
	eng     *engine.Engine
}

func New(m *workspaces.Manager, e *engine.Engine) *Service { return &Service{manager: m, eng: e} }
func prefix(ws string) string                              { return "knowledge-" + ws + ":" }
func (s *Service) records(ws string) map[string]Memory {
	result := map[string]Memory{}
	for _, raw := range s.eng.Records(prefix(ws)) {
		var m Memory
		if json.Unmarshal(raw, &m) == nil && m.WorkspaceID == ws {
			result[m.ID] = m
		}
	}
	return result
}

// Source paths are always project-relative, resolved through symlinks, and bounded.
func sourceBytes(root, file string) ([]byte, error) {
	if root == "" || file == "" || filepath.IsAbs(file) || strings.Contains(file, ":") || strings.Contains(file, "\\") {
		return nil, fmt.Errorf("来源必须是项目内的相对路径")
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(file)))
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return nil, fmt.Errorf("来源超出项目目录")
	}
	f, err := os.Open(resolved)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1024*1024 {
		return nil, fmt.Errorf("来源文件超出 1 MiB 或不是普通文件")
	}
	raw, err := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	if len(raw) > 1024*1024 {
		return nil, fmt.Errorf("来源过大")
	}
	return raw, err
}
func hash(raw []byte) string { return fmt.Sprintf("%x", sha256.Sum256(raw)) }
func inactive(m Memory, ws workspaces.Workspace) string {
	if m.Status != "confirmed" {
		return m.Status
	}
	if m.Source.ProjectPath != ws.ProjectPath {
		return "所属项目目录已变化"
	}
	if m.ValidUntil != "" {
		expires, err := time.Parse(time.RFC3339, m.ValidUntil)
		if err != nil || !expires.After(time.Now()) {
			return "已过期"
		}
	}
	if m.Source.Kind == "file" {
		raw, err := sourceBytes(ws.ProjectPath, m.Source.File)
		if err != nil || m.Source.ProjectPath != ws.ProjectPath || m.Source.SourceHash == "" || hash(raw) != m.Source.SourceHash {
			return "来源已变化或不可用"
		}
	}
	return ""
}
func (s *Service) List(wsID string) ([]Memory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.eng == nil {
		return nil, fmt.Errorf("记忆引擎不可用")
	}
	ws, err := s.manager.Workspace(wsID)
	if err != nil {
		return nil, err
	}
	items := []Memory{}
	for _, m := range s.records(wsID) {
		m.InactiveReason = inactive(m, ws)
		items = append(items, m)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt > items[j].UpdatedAt })
	return items, nil
}
func (s *Service) Save(ctx context.Context, wsID string, input Memory) (Memory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fail := func(err error) (Memory, error) { return Memory{}, err }
	if s.eng == nil {
		return fail(fmt.Errorf("记忆引擎不可用"))
	}
	ws, err := s.manager.Workspace(wsID)
	if err != nil {
		return fail(err)
	}
	items := s.records(wsID)
	old, exists := items[input.ID]
	if input.ID != "" && (!exists || input.Revision != old.Revision) {
		return fail(ErrConflict)
	}
	if exists && old.Status == "superseded" {
		return fail(fmt.Errorf("已替代记录只读，请新建记录"))
	}
	input.Content = strings.TrimSpace(input.Content)
	if input.Content == "" || len(input.Content) > 16000 {
		return fail(fmt.Errorf("内容需要 1–16000 字节"))
	}
	if !strings.Contains("|convention|decision|preference|solution|", "|"+input.Category+"|") || input.Category == "" {
		return fail(fmt.Errorf("无效记忆分类"))
	}
	if input.Status == "" {
		input.Status = "proposed"
	}
	if input.Status != "proposed" && input.Status != "confirmed" && input.Status != "rejected" {
		return fail(fmt.Errorf("无效记忆状态"))
	}
	if input.ValidUntil != "" {
		if _, err := time.Parse(time.RFC3339, input.ValidUntil); err != nil {
			return fail(fmt.Errorf("无效到期时间"))
		}
	}
	if input.Source.Kind == "" {
		input.Source.Kind = "note"
	}
	deactivating := input.Status != "confirmed"
	if input.ValidUntil != "" {
		until, _ := time.Parse(time.RFC3339, input.ValidUntil)
		deactivating = deactivating || !until.After(time.Now())
	}
	if exists && old.Source.ProjectPath != ws.ProjectPath && !deactivating {
		return fail(fmt.Errorf("所属项目已变化，请在当前项目创建新记录"))
	}
	// Deactivation preserves the original snapshot even when its file no longer exists.
	if !(exists && deactivating && input.Source == old.Source) {
		switch input.Source.Kind {
		case "file":
			raw, err := sourceBytes(ws.ProjectPath, input.Source.File)
			if err != nil {
				return fail(err)
			}
			current := hash(raw)
			if input.Source.SourceHash != "" && (input.Source.SourceHash != current || input.Source.ProjectPath != ws.ProjectPath) {
				return fail(fmt.Errorf("来源已变化，请检查新版本并创建新记录"))
			}
			if input.Source.StartLine < 1 || input.Source.EndLine < input.Source.StartLine || input.Source.EndLine > strings.Count(string(raw), "\n")+1 {
				return fail(fmt.Errorf("来源行号无效"))
			}
			input.Source.SourceHash = current
			input.Source.ProjectPath = ws.ProjectPath
		case "chat":
			if input.Source.SessionID == "" || input.Source.MessageID == "" || len(input.Source.SessionID) > 128 || len(input.Source.MessageID) > 128 {
				return fail(fmt.Errorf("会话来源不完整"))
			}
		case "note":
			input.Source = Source{Kind: "note"}
		default:
			return fail(fmt.Errorf("无效来源类型"))
		}
	}
	input.Source.ProjectPath = ws.ProjectPath
	if exists {
		input.Source.ProjectPath = old.Source.ProjectPath
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if !exists {
		if len(items) >= 500 {
			return fail(fmt.Errorf("每个工作区最多 500 条整理记忆"))
		}
		id := make([]byte, 16)
		if _, err := rand.Read(id); err != nil {
			return fail(err)
		}
		input.ID = hex.EncodeToString(id)
		input.CreatedAt = now
	} else {
		input.CreatedAt = old.CreatedAt
		input.SupersedesRevision = old.SupersedesRevision
		if input.Supersedes != old.Supersedes {
			return fail(fmt.Errorf("替代关系不可修改"))
		}
	}
	if input.Supersedes != "" && !exists {
		previous, ok := items[input.Supersedes]
		if !ok || input.SupersedesRevision != previous.Revision {
			return fail(ErrConflict)
		}
		if !ok || previous.Status == "superseded" || input.Status != "confirmed" {
			return fail(fmt.Errorf("替代目标不存在或新记录未确认"))
		}
		previous.Status = "superseded"
		previous.SupersededBy = input.ID
		previous.Revision++
		previous.UpdatedAt = now
		items[previous.ID] = previous
	}
	input.WorkspaceID = wsID
	input.Revision = old.Revision + 1
	input.UpdatedAt = now
	input.SupersededBy = ""
	input.InactiveReason = ""
	items[input.ID] = input
	records := map[string]engine.TextRecord{}
	for id, m := range items {
		raw, _ := json.Marshal(m)
		text := ""
		if m.Status == "confirmed" {
			text = m.Content
		}
		records[prefix(wsID)+id] = engine.TextRecord{Value: raw, Text: text}
	}
	latest, err := s.manager.Workspace(wsID)
	if err != nil || latest.ProjectPath != ws.ProjectPath {
		return fail(ErrConflict)
	}
	_, err = s.eng.ReplacePrefixContext(ctx, prefix(wsID), records)
	if err != nil {
		return fail(err)
	}
	return input, nil
}

func (s *Service) Context(ctx context.Context, wsID, query string, topK int) (ContextResult, error) {
	result := ContextResult{WorkspaceID: wsID, Mode: "lexical", Citations: []Citation{}, Warnings: []string{}}
	if s.eng == nil {
		return result, fmt.Errorf("记忆引擎不可用")
	}
	ws, err := s.manager.Workspace(wsID)
	if err != nil {
		return result, err
	}
	result.ProjectID = ws.ProjectID
	if _, ok := s.eng.Embedder().(*embed.MockEmbedder); !ok {
		result.Mode = "semantic"
	}
	if len(query) > 16000 {
		return result, fmt.Errorf("检索问题过长")
	}
	if strings.TrimSpace(query) == "" {
		return result, nil
	}
	if topK < 1 || topK > 8 {
		topK = 8
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	budget := 16000
	add := func(c Citation) {
		if len(result.Citations) >= topK || budget < 200 {
			return
		}
		runes := []rune(c.Excerpt)
		limit := min(4000, budget)
		if len(runes) > limit {
			c.Excerpt = string(runes[:limit]) + "\n[片段已截断]"
		}
		budget -= len([]rune(c.Excerpt))
		c.ID = fmt.Sprintf("R%d", len(result.Citations)+1)
		c.WorkspaceID = wsID
		result.Citations = append(result.Citations, c)
	}
	warn := func(message string) {
		for _, w := range result.Warnings {
			if w == message {
				return
			}
		}
		if len(result.Warnings) < 8 {
			result.Warnings = append(result.Warnings, message)
		}
	}
	// Curated knowledge gets a small reserved share; inactive facts never become context.
	hits, err := s.eng.SearchScopedContext(ctx, query, 60, prefix(wsID))
	if err != nil {
		return result, err
	}
	s.mu.Lock()
	for _, hit := range hits {
		raw, err := s.eng.Get(hit.Key)
		if err != nil {
			continue
		}
		var m Memory
		if json.Unmarshal(raw, &m) != nil || m.WorkspaceID != wsID {
			continue
		}
		reason := inactive(m, ws)
		if reason != "" {
			if m.Status == "confirmed" {
				warn("部分长期记忆已过期或来源变化，已排除。")
			}
			continue
		}
		add(Citation{Kind: "memory", Title: m.Category, Excerpt: m.Content, MemoryID: m.ID, Revision: m.Revision, File: m.Source.File, StartLine: m.Source.StartLine, EndLine: m.Source.EndLine, SourceHash: m.Source.SourceHash})
		if len(result.Citations) >= min(3, topK) {
			break
		}
	}
	s.mu.Unlock()
	if ws.ProjectID != "" {
		hits, err = s.eng.SearchScopedContext(ctx, query, 60, ws.ProjectID+":")
		if err != nil {
			return result, err
		}
		versions := map[string]string{}
		for _, hit := range hits {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			raw, err := s.eng.Get(hit.Key)
			if err != nil {
				continue
			}
			var m service.MemoryRecord
			if json.Unmarshal(raw, &m) != nil || m.SessionID != ws.ProjectID || m.Metadata["source"] != "codelens" {
				continue
			}
			file := m.Metadata["file"]
			version, ok := versions[file]
			if !ok {
				source, e := sourceBytes(ws.ProjectPath, file)
				if e == nil {
					version = hash(source)
				}
				versions[file] = version
			}
			if version == "" || version != m.Metadata["source_hash"] {
				warn("检索到的部分代码已变化、删除或缺少版本，已排除；请在代码工作台重新索引。")
				continue
			}
			start, _ := strconv.Atoi(m.Metadata["start_line"])
			end, _ := strconv.Atoi(m.Metadata["end_line"])
			excerpt := m.Content
			for _, relation := range []string{"callees", "callers", "unresolved"} {
				if v := m.Metadata[relation]; v != "" && v != "null" && v != "[]" {
					excerpt += "\n[" + relation + "] " + v
				}
			}
			add(Citation{Kind: "code", Title: file, File: file, StartLine: start, EndLine: end, SourceHash: version, Symbol: m.Metadata["symbol"], Excerpt: excerpt, ProjectID: ws.ProjectID})
			if len(result.Citations) >= topK {
				break
			}
		}
	}
	latest, err := s.manager.Workspace(wsID)
	if err != nil || latest.ProjectPath != ws.ProjectPath || latest.ProjectID != ws.ProjectID {
		return ContextResult{}, ErrConflict
	}
	for _, c := range result.Citations {
		result.Context += fmt.Sprintf("[%s] %s %s L%d–%d (version %s; memory revision %d)\n%s\n\n", c.ID, c.Kind, c.Title, c.StartLine, c.EndLine, c.SourceHash, c.Revision, c.Excerpt)
	}
	return result, nil
}

func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	respond := func(w http.ResponseWriter, value any, err error) {
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			status := 400
			if errors.Is(err, ErrConflict) {
				status = 409
			}
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(value)
	}
	decode := func(w http.ResponseWriter, r *http.Request, v any) error {
		r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(v); err != nil {
			return err
		}
		if decoder.Decode(new(any)) != io.EOF {
			return fmt.Errorf("仅接受一个 JSON 对象")
		}
		return nil
	}
	mux.HandleFunc("GET /api/workspaces/{id}/knowledge", func(w http.ResponseWriter, r *http.Request) {
		items, err := s.List(r.PathValue("id"))
		respond(w, map[string]any{"memories": items}, err)
	})
	mux.HandleFunc("POST /api/workspaces/{id}/knowledge", func(w http.ResponseWriter, r *http.Request) {
		var input Memory
		if err := decode(w, r, &input); err != nil {
			respond(w, nil, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		item, err := s.Save(ctx, r.PathValue("id"), input)
		respond(w, item, err)
	})
	mux.HandleFunc("POST /api/workspaces/{id}/context", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Query string `json:"query"`
			TopK  int    `json:"top_k"`
		}
		if err := decode(w, r, &input); err != nil {
			respond(w, nil, err)
			return
		}
		result, err := s.Context(r.Context(), r.PathValue("id"), input.Query, input.TopK)
		respond(w, result, err)
	})
}
