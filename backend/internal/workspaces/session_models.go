package workspaces

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// SetModelResolver is installed during bootstrap, before serving requests.
func (m *Manager) SetModelResolver(resolve func(workspaceID, sessionID, modelID string) (string, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.modelResolver = resolve
}

func validSessionModel(workspaceID, sessionID string) bool {
	return safeID.MatchString(workspaceID) && sessionID != "" && len(sessionID) <= 256 && !strings.ContainsRune(sessionID, '\x00')
}

func (m *Manager) sessionModel(workspaceID, sessionID string) (string, bool, error) {
	var id string
	err := m.db.QueryRow(`SELECT model_id FROM session_models WHERE workspace_id=? AND session_id=?`, workspaceID, sessionID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return id, err == nil, err
}

func (m *Manager) SessionModel(workspaceID, sessionID string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return "", false, ErrClosed
	}
	if !validSessionModel(workspaceID, sessionID) {
		return "", false, ErrInvalid
	}
	if _, err := m.workspace(workspaceID); err != nil {
		return "", false, err
	}
	return m.sessionModel(workspaceID, sessionID)
}

// Pin only once on first inference. A simultaneous validated switch wins over
// a stale first request; createRun checks the binding again at admission.
func (m *Manager) PinSessionModel(workspaceID, sessionID, modelID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return "", ErrClosed
	}
	if !validSessionModel(workspaceID, sessionID) || modelID == "" || len(modelID) > 256 {
		return "", ErrInvalid
	}
	if _, err := m.workspace(workspaceID); err != nil {
		return "", err
	}
	_, err := m.db.Exec(`INSERT OR IGNORE INTO session_models(workspace_id,session_id,model_id) VALUES(?,?,?)`, workspaceID, sessionID, modelID)
	if err != nil {
		return "", err
	}
	id, _, err := m.sessionModel(workspaceID, sessionID)
	return id, err
}

func (m *Manager) modelTasksIdle(workspaceID, sessionID string) error {
	var count int
	err := m.db.QueryRow(`SELECT count(*) FROM runs WHERE status IN ('queued','running') AND json_extract(CAST(body AS TEXT),'$.kind') IN ('chat','agent','diagnosis') AND (?='' OR workspace_id=?) AND (?='' OR json_extract(CAST(body AS TEXT),'$.session_id')=?)`, workspaceID, workspaceID, sessionID, sessionID).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("%w: 会话有正在执行或排队的任务，请等待完成或停止后再切换模型", ErrConflict)
	}
	return nil
}

// WithIdleModelTasks holds the same lock used for task admission until commit.
// The callback must not call Manager methods.
func (m *Manager) WithIdleModelTasks(workspaceID, sessionID string, commit func() error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	if err := m.modelTasksIdle(workspaceID, sessionID); err != nil {
		return err
	}
	return commit()
}

func (m *Manager) SelectSessionModel(workspaceID, sessionID, modelID string) error {
	if !validSessionModel(workspaceID, sessionID) || modelID == "" || len(modelID) > 256 {
		return ErrInvalid
	}
	return m.WithIdleModelTasks(workspaceID, sessionID, func() error {
		if _, err := m.workspace(workspaceID); err != nil {
			return err
		}
		_, err := m.db.Exec(`INSERT INTO session_models(workspace_id,session_id,model_id) VALUES(?,?,?) ON CONFLICT(workspace_id,session_id) DO UPDATE SET model_id=excluded.model_id`, workspaceID, sessionID, modelID)
		return err
	})
}

func (m *Manager) SelectWorkspaceModel(workspaceID, modelID string) error {
	return m.WithIdleModelTasks(workspaceID, "", func() error {
		ws, err := m.workspace(workspaceID)
		if err != nil {
			return err
		}
		ws.DefaultModelID = modelID
		ws.UpdatedAt = now()
		body, err := json.Marshal(ws)
		if err != nil {
			return err
		}
		_, err = m.db.Exec(`UPDATE workspaces SET body=? WHERE id=?`, body, workspaceID)
		return err
	})
}
