package workspaces

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const agentGrantSchema = `CREATE TABLE IF NOT EXISTS agent_grants(workspace_id TEXT PRIMARY KEY REFERENCES workspaces(id),binding TEXT NOT NULL,identity TEXT NOT NULL)`
const agentSessionSchema = `CREATE TABLE IF NOT EXISTS agent_sessions(run_id TEXT PRIMARY KEY REFERENCES runs(id),input BLOB NOT NULL,binding TEXT NOT NULL,model_hash TEXT NOT NULL,checkpoint BLOB,approval TEXT NOT NULL DEFAULT '',history INTEGER NOT NULL DEFAULT 0)`
const agentControlSchema = `CREATE TABLE IF NOT EXISTS agent_controls(seq INTEGER PRIMARY KEY AUTOINCREMENT,run_id TEXT NOT NULL REFERENCES agent_sessions(run_id),request TEXT NOT NULL,action TEXT NOT NULL,value TEXT NOT NULL,expected TEXT NOT NULL, UNIQUE(run_id,request))`
const agentArtifactSchema = `CREATE TABLE IF NOT EXISTS agent_deliveries(run_id TEXT NOT NULL REFERENCES agent_sessions(run_id),id TEXT NOT NULL,body BLOB NOT NULL,PRIMARY KEY(run_id,id))`
const agentTranscriptSchema = `CREATE TABLE IF NOT EXISTS agent_transcript(event_seq INTEGER PRIMARY KEY REFERENCES events(seq),run_id TEXT NOT NULL REFERENCES agent_sessions(run_id),body BLOB NOT NULL)`

var ErrAgentControlPending = errors.New("agent delivery has pending user control")

type agentGenerationKey struct{}
type modelFingerprintKey struct{}

func WithAgentModelFingerprint(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, modelFingerprintKey{}, value)
}
func AgentModelFingerprint(ctx context.Context) string {
	v, _ := ctx.Value(modelFingerprintKey{}).(string)
	return v
}

type agentAdmission struct {
	input              json.RawMessage
	binding, modelHash string
}
type AgentWait struct{ Status, Reason string }

func (w AgentWait) Error() string { return w.Reason }
func agentWaitState(err error) string {
	var w AgentWait
	if errors.As(err, &w) {
		switch w.Status {
		case "paused", "waiting_approval", "budget_exhausted":
			return w.Status
		}
	}
	return ""
}
func (m *Manager) AgentDataDir() string { return filepath.Join(m.dataDir, "agent-v2") }
func (m *Manager) SetGrantIdentity(f func(string) (string, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.grantIdentity = f
}
func (m *Manager) checkGrantLocked(ws Workspace) error {
	if m.grantIdentity == nil || ws.ProjectPath == "" || ws.ProjectBindingID == "" {
		return fmt.Errorf("%w: 请先授权项目目录", ErrConflict)
	}
	var identity string
	e := m.db.QueryRow("SELECT identity FROM agent_grants WHERE workspace_id=? AND binding=?", ws.ID, ws.ProjectBindingID).Scan(&identity)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	current, err := m.grantIdentity(ws.ProjectPath)
	if e != nil || err != nil || current != identity {
		return fmt.Errorf("%w: 项目授权已失效，请重新授权", ErrConflict)
	}
	return nil
}
func (m *Manager) AgentGrant(id string, set *string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ws, e := m.workspace(id)
	if e != nil {
		return "", e
	}
	if set == nil {
		if err := m.checkGrantLocked(ws); err != nil {
			if errors.Is(err, ErrConflict) {
				return "", nil
			}
			return "", err
		}
		return ws.ProjectBindingID, nil
	}
	if m.publications[id] != "" {
		return "", ErrBusy
	}
	if *set == "" || ws.ProjectBindingID != *set || m.grantIdentity == nil {
		return "", ErrConflict
	}
	identity, err := m.grantIdentity(ws.ProjectPath)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrConflict, err)
	}
	_, err = m.db.Exec("INSERT OR REPLACE INTO agent_grants(workspace_id,binding,identity) VALUES(?,?,?)", id, *set, identity)
	return *set, err
}

type AgentState struct {
	Workspace                    Workspace
	Run                          Run
	Binding, ModelHash, Approval string
	History                      bool
}

func (m *Manager) agentStateLocked(id, workspaceID string) (AgentState, error) {
	var s AgentState
	r, e := m.run(id)
	if e != nil {
		return s, e
	}
	if r.WorkspaceID != workspaceID || r.EngineVersion != 2 {
		return s, ErrNotFound
	}
	s.Run = r
	if s.Workspace, e = m.workspace(workspaceID); e != nil {
		return s, e
	}
	e = m.db.QueryRow("SELECT binding,model_hash,approval,history FROM agent_sessions WHERE run_id=?", id).Scan(&s.Binding, &s.ModelHash, &s.Approval, &s.History)
	return s, e
}
func (m *Manager) AgentState(id, ws string) (AgentState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.agentStateLocked(id, ws)
}
func (m *Manager) validateAgentLocked(s AgentState) error {
	if s.History || s.Binding != s.Workspace.ProjectBindingID || s.Run.Project == nil || s.Run.Project.Path != s.Workspace.ProjectPath {
		return fmt.Errorf("%w: 工作区已变化或此记录来自备份", ErrConflict)
	}
	return m.checkGrantLocked(s.Workspace)
}
func (m *Manager) ValidateAgent(ctx context.Context, id, ws string) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, e := m.agentStateLocked(id, ws)
	if e != nil {
		return e
	}
	if gen, ok := ctx.Value(agentGenerationKey{}).(int64); ok && (gen != s.Run.Generation || s.Run.Status != "running") {
		return ErrConflict
	}
	return m.validateAgentLocked(s)
}

// WithPublicationLease admits exactly the reviewed generation and control cursor.
// Keep unrelated workspaces observable while the per-project host journal commits.
func (m *Manager) WithPublicationLease(ctx context.Context, id, ws string, cursor int64, apply func() error) error {
	m.mu.Lock()
	if m.closed || ctx.Err() != nil {
		m.mu.Unlock()
		return ErrClosed
	}
	state, err := m.agentStateLocked(id, ws)
	if err == nil {
		err = m.validateAgentLocked(state)
	}
	gen, ok := ctx.Value(agentGenerationKey{}).(int64)
	if err == nil && (!ok || state.Run.Generation != gen || state.Run.Status != "running") {
		err = ErrConflict
	}
	if err == nil && m.publications[ws] != "" {
		err = ErrBusy
	}
	if err == nil {
		var latest int64
		err = m.db.QueryRow("SELECT COALESCE(MAX(seq),0) FROM agent_controls WHERE run_id=?", id).Scan(&latest)
		if err == nil && latest != cursor {
			err = fmt.Errorf("%w: 有新的用户指令等待处理", ErrConflict)
		}
	}
	if err != nil {
		m.mu.Unlock()
		return err
	}
	if m.publications == nil {
		m.publications = map[string]string{}
	}
	m.publications[ws] = id
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.publications, ws); m.mu.Unlock() }()
	return apply()
}

func (m *Manager) AgentCheckpoint(id string) (json.RawMessage, error) {
	var b []byte
	e := m.db.QueryRow("SELECT checkpoint FROM agent_sessions WHERE run_id=?", id).Scan(&b)
	return b, e
}
func (m *Manager) SaveAgentCheckpoint(ctx context.Context, id string, b json.RawMessage, phase, msg string, data any) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if !json.Valid(b) {
		return ErrInvalid
	}
	raw, e := json.Marshal(data)
	if e != nil {
		return e
	}
	publicRaw := raw
	if phase == "agent_delivered" && len(raw) > maxEventBytes {
		if len(raw) > maxResultBytes {
			return ErrInvalid
		}
		publicRaw = json.RawMessage(`{"result_in_run_record":true}`)
	}
	if len(publicRaw) > maxEventBytes {
		return fmt.Errorf("%w: Agent event requires a compact observation", ErrInvalid)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, e := m.run(id)
	if e != nil {
		return e
	}
	gen, _ := ctx.Value(agentGenerationKey{}).(int64)
	if m.closed || gen != r.Generation || r.Status != "running" {
		return ErrConflict
	}
	tx, e := m.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	approval := ""
	if phase == "waiting_approval" {
		var a struct{ Digest string }
		if json.Unmarshal(raw, &a) != nil || a.Digest == "" {
			return ErrInvalid
		}
		approval = a.Digest
	}
	var parsed struct {
		Cursor    int64
		Declined  bool
		History   []json.RawMessage
		Artifacts []struct {
			Path, Hash string
			Metadata   json.RawMessage
		}
	}
	turn := phase == "agent_model_completed" || phase == "tool_completed"
	updateArtifacts := phase == "artifact_changed" || phase == "workspace_refreshed" || phase == "agent_delivered"
	if phase == "tool_completed" {
		var operation struct{ Tool string }
		_ = json.Unmarshal(raw, &operation)
		updateArtifacts = operation.Tool == "present"
	}
	if turn || updateArtifacts {
		if err := json.Unmarshal(b, &parsed); err != nil {
			return ErrInvalid
		}
	}
	if phase == "agent_delivered" {
		var latest int64
		if err := tx.QueryRow("SELECT COALESCE(MAX(seq),0) FROM agent_controls WHERE run_id=?", id).Scan(&latest); err != nil {
			return err
		}
		if latest != parsed.Cursor {
			return ErrAgentControlPending
		}
	}
	if updateArtifacts {
		if _, err := tx.Exec("DELETE FROM agent_deliveries WHERE run_id=?", id); err != nil {
			return err
		}
		if !parsed.Declined {
			for _, item := range parsed.Artifacts {
				var meta struct{ ID, Path, SHA256 string }
				if err := json.Unmarshal(item.Metadata, &meta); err != nil || meta.Path == "" || meta.Path != item.Path || meta.SHA256 != item.Hash || len(item.Hash) != 64 || meta.ID != fmt.Sprintf("%x", sha256.Sum256([]byte(meta.Path))) {
					return ErrInvalid
				}
				if _, err := tx.Exec("INSERT INTO agent_deliveries(run_id,id,body) VALUES(?,?,?)", id, meta.ID, []byte(item.Metadata)); err != nil {
					return err
				}
			}
		}
	}
	if _, e = tx.Exec("UPDATE agent_sessions SET checkpoint=?,approval=? WHERE run_id=?", []byte(b), approval, id); e != nil {
		return e
	}
	r.Phase = phase
	r.UpdatedAt = now()
	if phase == "agent_delivered" {
		r.Status = "completed"
		r.Phase = "completed"
		r.Error = ""
		r.Result = raw
	}
	if e = writeRun(tx, r); e != nil {
		return e
	}
	if e = writeEvent(tx, id, phase, bounded(msg, 2048), publicRaw); e != nil {
		return e
	}
	// Compaction can drop old provider frames from the active checkpoint.
	// Archive each finished frame once, privately, alongside the public event.
	if turn && len(parsed.History) > 0 {
		frame := parsed.History[len(parsed.History)-1]
		if _, err := tx.Exec("INSERT INTO agent_transcript(event_seq,run_id,body) VALUES(last_insert_rowid(),?,?)", id, []byte(frame)); err != nil {
			return err
		}
	}
	if phase == "agent_delivered" {
		if e = writeEvent(tx, id, "completed", "", json.RawMessage("null")); e != nil {
			return e
		}
	}
	return tx.Commit()
}

type AgentControl struct {
	Seq           int64
	Action, Value string
}

func (m *Manager) AgentControls(id string, after int64) ([]AgentControl, error) {
	rows, e := m.db.Query("SELECT seq,action,value FROM agent_controls WHERE run_id=? AND seq>? ORDER BY seq LIMIT 200", id, after)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []AgentControl
	for rows.Next() {
		var c AgentControl
		if e = rows.Scan(&c.Seq, &c.Action, &c.Value); e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

type ControlRequest struct {
	Action    string `json:"action"`
	Value     string `json:"value"`
	RequestID string `json:"request_id"`
	Expected  string `json:"expected"`
}

func (m *Manager) ControlAgent(id, ws string, in ControlRequest) (Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Run{}, ErrClosed
	}
	s, e := m.agentStateLocked(id, ws)
	if e != nil {
		return Run{}, e
	}
	r := s.Run
	if !safeID.MatchString(in.RequestID) || len(in.Value) > 32000 || len(in.Expected) > 128 {
		return Run{}, ErrInvalid
	}
	var action, value, expected string
	e = m.db.QueryRow("SELECT action,value,expected FROM agent_controls WHERE run_id=? AND request=?", id, in.RequestID).Scan(&action, &value, &expected)
	if e == nil {
		if action == in.Action && (value == in.Value || ((action == "approve" || action == "reject") && value == in.Expected)) && expected == in.Expected {
			return r, nil
		}
		return Run{}, ErrConflict
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return Run{}, e
	}
	if e = m.validateAgentLocked(s); e != nil {
		return Run{}, e
	}
	if m.publications[ws] == id || terminal(r.Status) || r.Phase == "publishing" || r.Phase == "agent_delivered" {
		return Run{}, ErrConflict
	}
	if r.Status == "paused" && (in.Action == "mode" || in.Action == "steer") {
		var digest string
		if err := m.db.QueryRow("SELECT COALESCE(json_extract(checkpoint, '$.PublishingDigest'),'') FROM agent_sessions WHERE run_id=?", id).Scan(&digest); err != nil {
			return Run{}, err
		}
		if digest != "" {
			return Run{}, fmt.Errorf("%w: 请先恢复提交或撤回已提交的文件", ErrConflict)
		}
	}
	wake := false
	switch in.Action {
	case "steer":
		if strings.TrimSpace(in.Value) == "" {
			return Run{}, ErrInvalid
		}
		wake = r.Status == "waiting_approval"
	case "mode":
		if in.Value != "auto" && in.Value != "review" {
			return Run{}, ErrInvalid
		}
		wake = r.Status == "waiting_approval"
	case "pause":
		if r.Status != "queued" && r.Status != "running" {
			return Run{}, ErrConflict
		}
		r.Status = "paused"
	case "resume":
		if r.Status != "paused" {
			return Run{}, ErrConflict
		}
		wake = true
	case "approve", "reject":
		if r.Status != "waiting_approval" || in.Expected == "" || in.Expected != s.Approval {
			return Run{}, ErrConflict
		}
		in.Value = in.Expected
		wake = true
	case "extend":
		if r.Status != "budget_exhausted" {
			return Run{}, ErrConflict
		}
		wake = true
	default:
		return Run{}, ErrInvalid
	}
	var j job
	var q chan job
	if wake {
		if m.executions[id] != nil {
			return Run{}, ErrBusy
		}
		reg := m.registrations["agent"]
		q = m.queues[reg.lane]
		if reg.handler == nil || len(q) == cap(q) {
			return Run{}, ErrBusy
		}
		var input []byte
		if e = m.db.QueryRow("SELECT input FROM agent_sessions WHERE run_id=?", id).Scan(&input); e != nil {
			return Run{}, e
		}
		r.Generation++
		r.Status = "queued"
		r.Error = ""
		j = job{run: r, ws: s.Workspace, input: input, handler: reg.handler}
	}
	tx, e := m.db.Begin()
	if e != nil {
		return Run{}, e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("INSERT INTO agent_controls(run_id,request,action,value,expected) VALUES(?,?,?,?,?)", id, in.RequestID, in.Action, in.Value, in.Expected); e != nil {
		return Run{}, e
	}
	r.Phase = r.Status
	r.UpdatedAt = now()
	if e = writeRun(tx, r); e != nil {
		return Run{}, e
	}
	note, _ := json.Marshal(map[string]string{"action": in.Action, "value": in.Value, "request_id": in.RequestID})
	if e = writeEvent(tx, id, "control", in.Action, note); e != nil {
		return Run{}, e
	}
	if e = tx.Commit(); e != nil {
		return Run{}, e
	}
	if in.Action == "pause" {
		if ex := m.executions[id]; ex != nil {
			ex.cancel()
		}
	}
	if wake {
		q <- j
	}
	return r, nil
}
func (m *Manager) AgentArtifacts(id string) ([]json.RawMessage, error) {
	rows, e := m.db.Query("SELECT body FROM agent_deliveries WHERE run_id=? ORDER BY id", id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var b []byte
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (m *Manager) clearRestoredGrants() error {
	marker := filepath.Join(m.dataDir, ".restored-agent-history")
	if _, e := os.Stat(marker); errors.Is(e, os.ErrNotExist) {
		return nil
	} else if e != nil {
		return e
	}
	tx, e := m.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("DELETE FROM agent_grants"); e != nil {
		return e
	}
	if _, e = tx.Exec("UPDATE agent_sessions SET history=1,approval=''"); e != nil {
		return e
	}
	// Preserve the original state in the event stream. A restored in-flight
	// task is inert history even after the workspace is independently granted again.
	rows, e := tx.Query("SELECT r.body FROM runs r JOIN agent_sessions a ON r.id=a.run_id WHERE a.history=1 AND r.status NOT IN ('completed','failed','cancelled','interrupted')")
	if e != nil {
		return e
	}
	var unfinished []Run
	for rows.Next() {
		var b []byte
		var r Run
		if e = rows.Scan(&b); e == nil {
			e = json.Unmarshal(b, &r)
		}
		if e != nil {
			rows.Close()
			return e
		}
		unfinished = append(unfinished, r)
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		return e
	}
	if e = rows.Close(); e != nil {
		return e
	}
	for _, r := range unfinished {
		prior, _ := json.Marshal(map[string]string{"previous_status": r.Status, "previous_phase": r.Phase})
		r.Status = "interrupted"
		r.Phase = "restored_history"
		r.UpdatedAt = now()
		r.Error = "此任务为已恢复的历史记录"
		if e = writeRun(tx, r); e != nil {
			return e
		}
		if e = writeEvent(tx, r.ID, "restored_history", r.Error, prior); e != nil {
			return e
		}
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	return os.Remove(marker)
}

func UndoableAgentStatus(status string) bool {
	switch status {
	case "completed", "paused", "cancelled", "failed", "interrupted":
		return true
	}
	return false
}

// UndoAgent serializes binding changes and duplicate client requests with the filesystem transaction.
// The host journal is independently durable, so a crash before the SQLite receipt can be reconciled.
func (m *Manager) UndoAgent(ctx context.Context, id, ws string, in ControlRequest, apply func(context.Context) error) error {
	if !safeID.MatchString(in.RequestID) || len(in.Expected) != 64 {
		return ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	state, e := m.agentStateLocked(id, ws)
	if e != nil {
		return e
	}
	if e = m.validateAgentLocked(state); e != nil {
		return e
	}
	var action, expected string
	e = m.db.QueryRow("SELECT action,expected FROM agent_controls WHERE run_id=? AND request=?", id, in.RequestID).Scan(&action, &expected)
	if e == nil {
		if action == "undo" && expected == in.Expected {
			return nil
		}
		return ErrConflict
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if !UndoableAgentStatus(state.Run.Status) || m.executions[id] != nil || m.publications[ws] != "" || m.closed {
		return ErrConflict
	}
	if e = apply(ctx); e != nil {
		return e
	}
	tx, e := m.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("INSERT INTO agent_controls(run_id,request,action,value,expected) VALUES(?,?,'undo','',?)", id, in.RequestID, in.Expected); e != nil {
		return e
	}
	if state.Run.Status != "completed" {
		r := state.Run
		r.Status = "cancelled"
		r.Phase = "undone"
		r.UpdatedAt = now()
		r.Error = ""
		r.Result = json.RawMessage("null")
		if e = writeRun(tx, r); e != nil {
			return e
		}
	}
	if e = writeEvent(tx, id, "undone", "已撤销本轮工作区改动", json.RawMessage("null")); e != nil {
		return e
	}
	return tx.Commit()
}

func validateAgentBudget(input json.RawMessage) error {
	var spec struct {
		Budget map[string]int `json:"budget"`
	}
	if json.Unmarshal(input, &spec) != nil {
		return ErrInvalid
	}
	max := map[string]int{"model_calls": 1000, "tool_calls": 5000, "tokens": 10000000, "active_seconds": 86400, "command_seconds": 3600, "disk_mib": 2048}
	for name, value := range spec.Budget {
		n, ok := max[name]
		if !ok || value < 1 || value > n {
			return fmt.Errorf("%w: Agent 预算 %s 必须在 1 到 %d 之间", ErrInvalid, name, n)
		}
	}
	return nil
}

func agentSubmissionID(ws, key string) string {
	sum := sha256.Sum256([]byte(ws + "\x00" + key))
	return fmt.Sprintf("%x", sum[:16])
}
func sameSubmission(a, b json.RawMessage) bool {
	var left, right any
	if json.Unmarshal(a, &left) != nil || json.Unmarshal(b, &right) != nil {
		return false
	}
	l, _ := json.Marshal(left)
	r, _ := json.Marshal(right)
	return bytes.Equal(l, r)
}
