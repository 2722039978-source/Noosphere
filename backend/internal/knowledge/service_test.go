package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/noosphere/backend/internal/codelens"
	"github.com/noosphere/backend/internal/nebula/engine"
	"github.com/noosphere/backend/internal/workspaces"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) (*Service, workspaces.Workspace) {
	t.Helper()
	dir := t.TempDir()
	m, err := workspaces.Open(filepath.Join(dir, "ws.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	e, err := engine.Open(engine.DefaultOptions(filepath.Join(dir, "nebula")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	ws, err := m.SaveWorkspace(workspaces.Workspace{Name: "Project", ProjectPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	return New(m, e), ws
}

func TestCodeContextRejectsChangedDeletedAndUnversionedSources(t *testing.T) {
	s, ws := fixture(t)
	ctx := context.Background()
	project := "code-012345678901234567890123"
	path := filepath.Join(ws.ProjectPath, "a.py")
	raw := []byte("def transaction():\n    return 1\n")
	os.WriteFile(path, raw, 0600)
	if err := codelens.StoreIndex(s.eng, project, []codelens.SourceChunk{{ID: "a.py::transaction", File: "a.py", Content: string(raw), StartLine: 1, EndLine: 2, Symbol: "transaction", SourceHash: hash(raw)}}); err != nil {
		t.Fatal(err)
	}
	if err := s.manager.BindProject(ws.ID, ws.ProjectPath, project); err != nil {
		t.Fatal(err)
	}
	result, err := s.Context(ctx, ws.ID, "transaction", 8)
	if err != nil || len(result.Citations) != 1 || result.Citations[0].SourceHash != hash(raw) {
		t.Fatalf("missing evidence %+v %v", result, err)
	}
	os.WriteFile(path, []byte("changed"), 0600)
	result, err = s.Context(ctx, ws.ID, "transaction", 8)
	if err != nil || len(result.Citations) != 0 || len(result.Warnings) == 0 {
		t.Fatal("changed source cited")
	}
	os.Remove(path)
	result, _ = s.Context(ctx, ws.ID, "transaction", 8)
	if len(result.Citations) != 0 {
		t.Fatal("deleted source cited")
	}
	os.WriteFile(path, raw, 0600)
	if err := codelens.StoreIndex(s.eng, project, []codelens.SourceChunk{{ID: "a", File: "a.py", Content: string(raw), StartLine: 1, EndLine: 2}}); err != nil {
		t.Fatal(err)
	}
	result, _ = s.Context(ctx, ws.ID, "transaction", 8)
	if len(result.Citations) != 0 {
		t.Fatal("unversioned source cited")
	}
}

func TestKnowledgeHTTPValidation(t *testing.T) {
	s, ws := fixture(t)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	path := "/api/workspaces/" + ws.ID + "/knowledge"
	if w := request("POST", path, `{"content":"hello"} {}`); w.Code != 400 {
		t.Fatal("multiple JSON allowed")
	}
	w := request("POST", path, `{"content":"persist transaction rule","category":"decision","status":"confirmed","source":{"kind":"note"}}`)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var m Memory
	json.Unmarshal(w.Body.Bytes(), &m)
	body, _ := json.Marshal(m)
	m.Revision--
	stale, _ := json.Marshal(m)
	if w := request("POST", path, string(stale)); w.Code != 409 {
		t.Fatalf("expected conflict: %d", w.Code)
	}
	if w := request("POST", "/api/workspaces/local/knowledge", string(body)); w.Code != 409 {
		t.Fatal("foreign mutation allowed")
	}
}

func TestLifecycleIsolationAndSupersession(t *testing.T) {
	s, ws := fixture(t)
	ctx := context.Background()
	a, err := s.Save(ctx, ws.ID, Memory{Content: "Use SQLite transactions", Category: "decision", Source: Source{Kind: "note"}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Context(ctx, ws.ID, "SQLite", 8)
	if err != nil || len(result.Citations) != 0 {
		t.Fatalf("unconfirmed leaked: %+v %v", result, err)
	}
	a.Status = "confirmed"
	a, err = s.Save(ctx, ws.ID, a)
	if err != nil {
		t.Fatal(err)
	}
	old := a
	a.Content = "Use SQLite WAL transactions"
	a, err = s.Save(ctx, ws.ID, a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Save(ctx, ws.ID, old); err == nil {
		t.Fatal("stale revision accepted")
	}
	if _, err = s.Save(ctx, "local", a); err == nil {
		t.Fatal("foreign id accepted")
	}
	result, err = s.Context(ctx, ws.ID, "SQLite", 8)
	if err != nil || len(result.Citations) != 1 {
		t.Fatalf("confirmed missing: %+v %v", result, err)
	}
	b, err := s.Save(ctx, ws.ID, Memory{Content: "Use PostgreSQL transactions", Category: "decision", Status: "confirmed", Supersedes: a.ID, SupersedesRevision: a.Revision, Source: Source{Kind: "note"}})
	if err != nil {
		t.Fatal(err)
	}
	list, _ := s.List(ws.ID)
	for _, m := range list {
		if m.ID == a.ID && (m.Status != "superseded" || m.SupersededBy != b.ID) {
			t.Fatal("old still active")
		}
	}
	result, _ = s.Context(ctx, ws.ID, "transactions", 8)
	if len(result.Citations) != 1 || result.Citations[0].MemoryID != b.ID {
		t.Fatalf("superseded leaked: %+v", result)
	}
	b.ValidUntil = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	_, err = s.Save(ctx, ws.ID, b)
	if err != nil {
		t.Fatal(err)
	}
	result, _ = s.Context(ctx, ws.ID, "transactions", 8)
	if len(result.Citations) != 0 {
		t.Fatal("expired leaked")
	}
}

func TestFileVersionAndPathEscape(t *testing.T) {
	s, ws := fixture(t)
	ctx := context.Background()
	file := filepath.Join(ws.ProjectPath, "rule.txt")
	os.WriteFile(file, []byte("SQLite"), 0600)
	a, err := s.Save(ctx, ws.ID, Memory{Content: "SQLite rule", Category: "convention", Status: "confirmed", Source: Source{Kind: "file", File: "rule.txt", StartLine: 1, EndLine: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if a.Source.SourceHash == "" {
		t.Fatal("missing version")
	}
	os.WriteFile(file, []byte("PostgreSQL"), 0600)
	result, err := s.Context(ctx, ws.ID, "SQLite", 8)
	if err != nil || len(result.Citations) != 0 || len(result.Warnings) == 0 {
		t.Fatalf("stale leaked: %+v %v", result, err)
	}
	if _, err = s.Save(ctx, ws.ID, Memory{Content: "Escape", Category: "decision", Source: Source{Kind: "file", File: "../escape.txt"}}); err == nil {
		t.Fatal("escape accepted")
	}
	if _, err = s.Save(ctx, ws.ID, a); err == nil {
		t.Fatal("silently refreshed source hash")
	}
}

func TestSupersedingUsesTargetRevision(t *testing.T) {
	s, ws := fixture(t)
	ctx := context.Background()
	original, err := s.Save(ctx, ws.ID, Memory{Content: "SQLite decision", Category: "decision", Status: "confirmed", Source: Source{Kind: "note"}})
	if err != nil {
		t.Fatal(err)
	}
	stale := original.Revision
	original.Content = "SQLite newer decision"
	original, err = s.Save(ctx, ws.ID, original)
	if err != nil {
		t.Fatal(err)
	}
	// Decode through JSON so this regression compiles before the field exists.
	raw := fmt.Sprintf(`{"content":"PostgreSQL decision","category":"decision","status":"confirmed","source":{"kind":"note"},"supersedes":%q,"supersedes_revision":%d}`, original.ID, stale)
	var replacement Memory
	json.Unmarshal([]byte(raw), &replacement)
	if _, err = s.Save(ctx, ws.ID, replacement); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale replacement accepted: %v", err)
	}
}

func TestInvalidFileCanBeRejectedWithoutRefreshingProvenance(t *testing.T) {
	s, ws := fixture(t)
	ctx := context.Background()
	file := filepath.Join(ws.ProjectPath, "a.txt")
	os.WriteFile(file, []byte("SQLite"), 0600)
	item, err := s.Save(ctx, ws.ID, Memory{Content: "SQLite rule", Category: "decision", Status: "confirmed", Source: Source{Kind: "file", File: "a.txt", StartLine: 1, EndLine: 1}})
	if err != nil {
		t.Fatal(err)
	}
	version := item.Source.SourceHash
	os.Remove(file)
	item.Status = "rejected"
	saved, err := s.Save(ctx, ws.ID, item)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Source.SourceHash != version {
		t.Fatal("historical source changed")
	}
}

func TestWorkspaceRootChangeInvalidatesNoteAndChatMemory(t *testing.T) {
	s, ws := fixture(t)
	ctx := context.Background()
	for _, source := range []Source{{Kind: "note"}, {Kind: "chat", SessionID: "session", MessageID: "message"}} {
		if _, err := s.Save(ctx, ws.ID, Memory{Content: "SQLite project convention", Category: "convention", Status: "confirmed", Source: source}); err != nil {
			t.Fatal(err)
		}
	}
	ws.ProjectPath = t.TempDir()
	if _, err := s.manager.SaveWorkspace(ws); err != nil {
		t.Fatal(err)
	}
	result, err := s.Context(ctx, ws.ID, "SQLite", 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Citations) != 0 || len(result.Warnings) == 0 {
		t.Fatalf("old project memory leaked %+v", result)
	}
}
