// Package workspaces owns durable workspace identities and local task execution.
package workspaces

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound = errors.New("not found")
	ErrInvalid  = errors.New("invalid request")
	ErrConflict = errors.New("conflict")
	ErrBusy     = errors.New("task capacity exhausted")
	ErrClosed   = errors.New("workspace manager closed")
)

const maxResultBytes = 2 * 1024 * 1024
const maxEventBytes = 64 * 1024

var safeID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type Workspace struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	ProjectPath    string `json:"project_path"`
	ProjectID      string `json:"project_id"`
	DefaultModelID string `json:"default_model_id"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}
type Run struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspace_id"`
	SessionID   string          `json:"session_id"`
	Kind        string          `json:"kind"`
	Title       string          `json:"title"`
	Status      string          `json:"status"`
	Phase       string          `json:"phase"`
	Error       string          `json:"error"`
	ModelID     string          `json:"model_id"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
	Result      json.RawMessage `json:"result"`
}
type Event struct {
	Seq       int64           `json:"seq"`
	RunID     string          `json:"run_id"`
	Phase     string          `json:"phase"`
	Message   string          `json:"message"`
	Data      json.RawMessage `json:"data"`
	CreatedAt string          `json:"created_at"`
}
type Handler func(context.Context, Workspace, json.RawMessage, func(string, string, any)) (any, error)
type registration struct {
	lane    string
	handler Handler
}
type job struct {
	run     Run
	ws      Workspace
	input   json.RawMessage
	handler Handler
}
type Manager struct {
	mu            sync.Mutex
	db            *sql.DB
	closed        bool
	ctx           context.Context
	cancel        context.CancelFunc
	registrations map[string]registration
	executions    map[string]*Execution
	queues        map[string]chan job
	slots         map[string]chan struct{}
	workers       sync.WaitGroup
}
type Execution struct {
	ID        string
	manager   *Manager
	ctx       context.Context
	cancel    context.CancelFunc
	stopWatch func() bool
	release   func()
	once      sync.Once
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func newID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}
func terminal(s string) bool {
	return s == "completed" || s == "failed" || s == "cancelled" || s == "interrupted"
}
func bounded(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func Open(path string) (*Manager, error) {
	if path == "" {
		return nil, fmt.Errorf("%w: database path required", ErrInvalid)
	}
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return nil, e
	}
	db, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	fail := func(e error) (*Manager, error) { db.Close(); return nil, e }
	for _, q := range []string{
		`PRAGMA busy_timeout=5000`, `PRAGMA journal_mode=WAL`, `PRAGMA foreign_keys=ON`,
		`CREATE TABLE IF NOT EXISTS workspaces (id TEXT PRIMARY KEY, root_key TEXT NOT NULL, body BLOB NOT NULL)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS unique_workspace_root ON workspaces(root_key) WHERE root_key<>''`,
		`CREATE TABLE IF NOT EXISTS runs (id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL REFERENCES workspaces(id), status TEXT NOT NULL, created_at TEXT NOT NULL, body BLOB NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS workspace_runs ON runs(workspace_id,created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS events (seq INTEGER PRIMARY KEY AUTOINCREMENT, run_id TEXT NOT NULL REFERENCES runs(id), phase TEXT NOT NULL, message TEXT NOT NULL, data BLOB NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS run_events ON events(run_id,seq)`,
	} {
		if _, e = db.Exec(q); e != nil {
			return fail(e)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{db: db, ctx: ctx, cancel: cancel, registrations: map[string]registration{}, executions: map[string]*Execution{}, queues: map[string]chan job{}, slots: map[string]chan struct{}{}}
	stamp := now()
	local := Workspace{ID: "local", Name: "本地工作区", CreatedAt: stamp, UpdatedAt: stamp}
	body, _ := json.Marshal(local)
	if _, e = db.Exec(`INSERT OR IGNORE INTO workspaces(id,root_key,body) VALUES('local','',?)`, body); e != nil {
		cancel()
		return fail(e)
	}
	if e = m.recover(); e != nil {
		cancel()
		return fail(e)
	}
	for lane, n := range map[string]int{"interactive": 2, "background": 1} {
		m.queues[lane] = make(chan job, 32)
		m.slots[lane] = make(chan struct{}, n)
	}
	// Finish constructing the maps before workers begin reading them.
	m.slots["chat"] = make(chan struct{}, 2)
	for lane, n := range map[string]int{"interactive": 2, "background": 1} {
		for i := 0; i < n; i++ {
			m.workers.Add(1)
			go m.worker(lane)
		}
	}
	// External chat has reserved capacity; queued diagnostics cannot starve it.
	return m, nil
}

func (m *Manager) recover() error {
	rows, e := m.db.Query(`SELECT body FROM runs WHERE status IN ('queued','running')`)
	if e != nil {
		return e
	}
	var runs []Run
	for rows.Next() {
		var body []byte
		var r Run
		if e = rows.Scan(&body); e == nil {
			e = json.Unmarshal(body, &r)
		}
		if e != nil {
			rows.Close()
			return fmt.Errorf("read unfinished run: %w", e)
		}
		runs = append(runs, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	tx, e := m.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, r := range runs {
		r.Status = "interrupted"
		r.Phase = "interrupted"
		r.Error = "Process stopped before task completion"
		r.UpdatedAt = now()
		if e = writeRun(tx, r); e != nil {
			return e
		}
		if e = writeEvent(tx, r.ID, r.Phase, r.Error, json.RawMessage("null")); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	m.cancel()
	for _, ex := range m.executions {
		ex.cancel()
	}
	// Mark work interrupted before closing; startup recovery covers abrupt termination.
	e := m.recover()
	closeErr := m.db.Close()
	if e != nil {
		return e
	}
	return closeErr
}

func canonicalRoot(path string) (string, string, error) {
	if strings.TrimSpace(path) == "" {
		return "", "", nil
	}
	p, e := filepath.Abs(path)
	if e != nil {
		return "", "", fmt.Errorf("%w: project path", ErrInvalid)
	}
	p, e = filepath.EvalSymlinks(p)
	if e != nil {
		return "", "", fmt.Errorf("%w: project directory does not exist", ErrInvalid)
	}
	info, e := os.Stat(p)
	if e != nil || !info.IsDir() {
		return "", "", fmt.Errorf("%w: project path must be a directory", ErrInvalid)
	}
	p = filepath.Clean(p)
	key := p
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	return p, key, nil
}
func (m *Manager) workspace(id string) (Workspace, error) {
	var w Workspace
	var b []byte
	e := m.db.QueryRow(`SELECT body FROM workspaces WHERE id=?`, id).Scan(&b)
	if errors.Is(e, sql.ErrNoRows) {
		return w, ErrNotFound
	}
	if e != nil {
		return w, e
	}
	if e = json.Unmarshal(b, &w); e != nil {
		return w, fmt.Errorf("decode workspace: %w", e)
	}
	return w, nil
}
func (m *Manager) Workspace(id string) (Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Workspace{}, ErrClosed
	}
	return m.workspace(id)
}
func (m *Manager) Workspaces() ([]Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrClosed
	}
	rows, e := m.db.Query(`SELECT body FROM workspaces ORDER BY CASE WHEN id='local' THEN 0 ELSE 1 END,id`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Workspace{}
	for rows.Next() {
		var b []byte
		var w Workspace
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(b, &w); e != nil {
			return nil, e
		}
		out = append(out, w)
	}
	return out, rows.Err()
}
func (m *Manager) SaveWorkspace(w Workspace) (Workspace, error) {
	w.Name = strings.TrimSpace(w.Name)
	if w.Name == "" || len(w.Name) > 256 || len(w.DefaultModelID) > 256 {
		return Workspace{}, fmt.Errorf("%w: workspace name/model", ErrInvalid)
	}
	path, key, e := canonicalRoot(w.ProjectPath)
	if e != nil {
		return Workspace{}, e
	}
	w.ProjectPath = path
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Workspace{}, ErrClosed
	}
	stamp := now()
	if w.ID == "" {
		w.ID = newID()
		w.CreatedAt = stamp
		w.ProjectID = ""
	} else {
		if !safeID.MatchString(w.ID) {
			return Workspace{}, ErrInvalid
		}
		old, e := m.workspace(w.ID)
		if e != nil {
			return Workspace{}, e
		}
		w.CreatedAt = old.CreatedAt
		w.ProjectID = ""
		if old.ProjectPath == path {
			w.ProjectID = old.ProjectID
		}
	}
	var occupied string
	e = m.db.QueryRow(`SELECT id FROM workspaces WHERE root_key=? AND root_key<>'' AND id<>?`, key, w.ID).Scan(&occupied)
	if e == nil {
		return Workspace{}, fmt.Errorf("%w: project already belongs to a workspace", ErrConflict)
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return Workspace{}, e
	}
	w.UpdatedAt = stamp
	b, e := json.Marshal(w)
	if e != nil {
		return Workspace{}, e
	}
	_, e = m.db.Exec(`INSERT INTO workspaces(id,root_key,body) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET root_key=excluded.root_key,body=excluded.body`, w.ID, key, b)
	return w, e
}
func (m *Manager) BindProject(workspaceID, expectedPath, projectID string) error {
	if projectID == "" || len(projectID) > 256 {
		return ErrInvalid
	}
	path, _, e := canonicalRoot(expectedPath)
	if e != nil {
		return e
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	w, e := m.workspace(workspaceID)
	if e != nil {
		return e
	}
	if w.ProjectPath != path {
		return fmt.Errorf("%w: workspace root changed", ErrConflict)
	}
	w.ProjectID = projectID
	w.UpdatedAt = now()
	b, _ := json.Marshal(w)
	_, e = m.db.Exec(`UPDATE workspaces SET body=? WHERE id=?`, b, w.ID)
	return e
}

type sqlWriter interface {
	Exec(string, ...any) (sql.Result, error)
}

func writeRun(tx sqlWriter, r Run) error {
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	_, e = tx.Exec(`INSERT INTO runs(id,workspace_id,status,created_at,body) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,body=excluded.body`, r.ID, r.WorkspaceID, r.Status, r.CreatedAt, b)
	return e
}
func writeEvent(tx sqlWriter, id, phase, message string, data json.RawMessage) error {
	_, e := tx.Exec(`INSERT INTO events(run_id,phase,message,data,created_at) VALUES(?,?,?,?,?)`, id, bounded(phase, 128), bounded(message, 4096), []byte(data), now())
	return e
}
func (m *Manager) run(id string) (Run, error) {
	var r Run
	var b []byte
	e := m.db.QueryRow(`SELECT body FROM runs WHERE id=?`, id).Scan(&b)
	if errors.Is(e, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if e != nil {
		return r, e
	}
	e = json.Unmarshal(b, &r)
	return r, e
}
func (m *Manager) Run(id string) (Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Run{}, ErrClosed
	}
	return m.run(id)
}
func (m *Manager) Runs(id string) ([]Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrClosed
	}
	if _, e := m.workspace(id); e != nil {
		return nil, e
	}
	// Poll metadata only. Full results remain available through Run(id).
	rows, e := m.db.Query(`SELECT json_remove(CAST(body AS TEXT),'$.result') FROM runs WHERE workspace_id=? ORDER BY created_at DESC LIMIT 100`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		var b []byte
		var r Run
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(b, &r); e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (m *Manager) Events(id string, after int64) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrClosed
	}
	if after < 0 {
		return nil, ErrInvalid
	}
	var exists string
	if e := m.db.QueryRow(`SELECT id FROM runs WHERE id=?`, id).Scan(&exists); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, e
	}
	rows, e := m.db.Query(`SELECT seq,run_id,phase,message,data,created_at FROM events WHERE run_id=? AND seq>? ORDER BY seq LIMIT 200`, id, after)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var ev Event
		if e = rows.Scan(&ev.Seq, &ev.RunID, &ev.Phase, &ev.Message, &ev.Data, &ev.CreatedAt); e != nil {
			return nil, e
		}
		if !json.Valid(ev.Data) {
			return nil, errors.New("corrupt task event data")
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func validateRun(r Run) error {
	if !safeID.MatchString(r.ID) || !safeID.MatchString(r.WorkspaceID) || len(r.SessionID) > 256 || len(r.Title) > 512 || len(r.ModelID) > 256 {
		return ErrInvalid
	}
	if r.Kind != "chat" && r.Kind != "index" && r.Kind != "diagnosis" && r.Kind != "agent" {
		return fmt.Errorf("%w: unsupported run kind", ErrInvalid)
	}
	return nil
}
func (m *Manager) createRun(r Run, status string) (Run, error) {
	if r.ID == "" {
		r.ID = newID()
	}
	if r.WorkspaceID == "" {
		r.WorkspaceID = "local"
	}
	if e := validateRun(r); e != nil {
		return Run{}, e
	}
	if _, e := m.workspace(r.WorkspaceID); e != nil {
		return Run{}, e
	}
	if _, e := m.run(r.ID); e == nil {
		return Run{}, ErrConflict
	} else if !errors.Is(e, ErrNotFound) {
		return Run{}, e
	}
	r.Status = status
	r.Phase = status
	r.Error = ""
	r.Result = json.RawMessage("null")
	r.CreatedAt = now()
	r.UpdatedAt = r.CreatedAt
	tx, e := m.db.Begin()
	if e != nil {
		return Run{}, e
	}
	defer tx.Rollback()
	if e = writeRun(tx, r); e != nil {
		return Run{}, e
	}
	if e = writeEvent(tx, r.ID, status, "", json.RawMessage("null")); e != nil {
		return Run{}, e
	}
	if e = tx.Commit(); e != nil {
		return Run{}, e
	}
	return r, nil
}
func (m *Manager) transition(r Run, phase, message string, data json.RawMessage) error {
	r.UpdatedAt = now()
	tx, e := m.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = writeRun(tx, r); e != nil {
		return e
	}
	if e = writeEvent(tx, r.ID, phase, message, data); e != nil {
		return e
	}
	return tx.Commit()
}

func (m *Manager) Register(kind, lane string, handler Handler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if (kind != "chat" && kind != "index" && kind != "diagnosis" && kind != "agent") || m.queues[lane] == nil || handler == nil {
		panic("invalid workspace worker registration")
	}
	m.registrations[kind] = registration{lane, handler}
}
func (m *Manager) Submit(workspaceID, kind, title string, input json.RawMessage) (Run, error) {
	if len(input) == 0 {
		input = json.RawMessage("null")
	}
	if len(input) > maxResultBytes || !json.Valid(input) {
		return Run{}, ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Run{}, ErrClosed
	}
	reg, ok := m.registrations[kind]
	if !ok {
		return Run{}, fmt.Errorf("%w: unregistered task kind", ErrInvalid)
	}
	q := m.queues[reg.lane]
	if len(q) == cap(q) {
		return Run{}, ErrBusy
	}
	if workspaceID == "" {
		workspaceID = "local"
	}
	ws, e := m.workspace(workspaceID)
	if e != nil {
		return Run{}, e
	}
	spec := Run{WorkspaceID: workspaceID, Kind: kind, Title: title}
	if kind == "agent" {
		var source struct {
			SessionID string `json:"session_id"`
			ModelID   string `json:"model_id"`
		}
		if json.Unmarshal(input, &source) != nil {
			return Run{}, ErrInvalid
		}
		spec.SessionID = source.SessionID
		spec.ModelID = source.ModelID
	}
	r, e := m.createRun(spec, "queued")
	if e != nil {
		return Run{}, e
	}
	q <- job{r, ws, append(json.RawMessage(nil), input...), reg.handler}
	return r, nil
}

type runIDKey struct{}

func RunID(ctx context.Context) string { id, _ := ctx.Value(runIDKey{}).(string); return id }
func (m *Manager) execution(ctx context.Context, id string, release func()) *Execution {
	ctx = context.WithValue(ctx, runIDKey{}, id)
	ctx, cancel := context.WithCancel(ctx)
	ex := &Execution{ID: id, manager: m, ctx: ctx, cancel: cancel, release: release}
	m.executions[id] = ex
	ex.stopWatch = context.AfterFunc(ctx, func() { m.stop(id, ctx.Err()) })
	return ex
}
func (m *Manager) Start(ctx context.Context, spec Run) (context.Context, *Execution, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if e := ctx.Err(); e != nil {
		return nil, nil, e
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, nil, ErrClosed
	}
	// Reject replay before capacity errors so duplicate requests are unambiguous.
	if spec.ID != "" {
		if _, e := m.run(spec.ID); e == nil {
			return nil, nil, ErrConflict
		} else if !errors.Is(e, ErrNotFound) {
			return nil, nil, e
		}
	}
	slots := m.slots["chat"]
	select {
	case slots <- struct{}{}:
	default:
		return nil, nil, ErrBusy
	}
	r, e := m.createRun(spec, "running")
	if e != nil {
		<-slots
		return nil, nil, e
	}
	ex := m.execution(ctx, r.ID, func() { <-slots })
	return ex.ctx, ex, nil
}
func (m *Manager) worker(lane string) {
	defer m.workers.Done()
	q, slots := m.queues[lane], m.slots[lane]
	for {
		var j job
		select {
		case <-m.ctx.Done():
			return
		case j = <-q:
		}
		select {
		case <-m.ctx.Done():
			return
		case slots <- struct{}{}:
		}
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			<-slots
			return
		}
		r, e := m.run(j.run.ID)
		if e != nil || terminal(r.Status) {
			m.mu.Unlock()
			<-slots
			continue
		}
		r.Status = "running"
		r.Phase = "running"
		e = m.transition(r, "running", "", json.RawMessage("null"))
		if e != nil {
			m.mu.Unlock()
			<-slots
			continue
		}
		ex := m.execution(m.ctx, r.ID, func() { <-slots })
		m.mu.Unlock()
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					ex.Finish(nil, errors.New("task handler panicked"))
				}
			}()
			result, err := j.handler(ex.ctx, j.ws, j.input, ex.Emit)
			ex.Finish(result, err)
		}()
	}
}
func (ex *Execution) Emit(phase, message string, data any) {
	b, e := json.Marshal(data)
	if e != nil || len(b) > maxEventBytes {
		b = json.RawMessage(`{"omitted":true}`)
	}
	m := ex.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	r, e := m.run(ex.ID)
	if e != nil || terminal(r.Status) {
		return
	}
	r.Phase = bounded(phase, 128)
	if e = m.transition(r, r.Phase, message, b); e != nil {
		ex.cancel()
	}
}
func (ex *Execution) Finish(result any, taskErr error) error {
	defer ex.once.Do(func() {
		if ex.stopWatch != nil {
			ex.stopWatch()
		}
		ex.cancel()
		ex.release()
	})
	m := ex.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	r, e := m.run(ex.ID)
	if e != nil {
		return e
	}
	if terminal(r.Status) {
		delete(m.executions, ex.ID)
		return nil
	}
	var validation error
	b, e := json.Marshal(result)
	if e != nil || len(b) > maxResultBytes {
		validation = fmt.Errorf("%w: task result is invalid or exceeds 2 MiB", ErrInvalid)
		taskErr = validation
		b = json.RawMessage("null")
	}
	r.Status = "completed"
	r.Error = ""
	r.Result = b
	if errors.Is(ex.ctx.Err(), context.Canceled) || errors.Is(taskErr, context.Canceled) {
		r.Status = "cancelled"
		r.Result = json.RawMessage("null")
	} else if taskErr != nil || ex.ctx.Err() != nil {
		r.Status = "failed"
		if taskErr == nil {
			taskErr = ex.ctx.Err()
		}
		r.Error = bounded(taskErr.Error(), 4096)
	}
	r.Phase = r.Status
	e = m.transition(r, r.Status, r.Error, json.RawMessage("null"))
	if e == nil {
		delete(m.executions, ex.ID)
	}
	if e != nil {
		return e
	}
	return validation
}
func (m *Manager) Cancel(id string) (Run, error) {
	return m.stop(id, context.Canceled)
}
func (m *Manager) stop(id string, reason error) (Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Run{}, ErrClosed
	}
	r, e := m.run(id)
	if e != nil {
		return Run{}, e
	}
	if terminal(r.Status) {
		return r, nil
	}
	r.Status = "cancelled"
	if errors.Is(reason, context.DeadlineExceeded) {
		r.Status = "failed"
		r.Error = reason.Error()
	}
	r.Phase = r.Status
	r.Result = json.RawMessage("null")
	if e = m.transition(r, r.Phase, r.Error, json.RawMessage("null")); e != nil {
		return Run{}, e
	}
	if ex := m.executions[id]; ex != nil {
		ex.cancel()
	}
	return m.run(id)
}
