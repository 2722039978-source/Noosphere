package workspaces

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

const serviceEffectsSchema = `CREATE TABLE IF NOT EXISTS agent_service_effects(id TEXT PRIMARY KEY,run_id TEXT NOT NULL REFERENCES agent_sessions(run_id),name TEXT NOT NULL COLLATE NOCASE,before_state TEXT NOT NULL,after_state TEXT NOT NULL,status TEXT NOT NULL,digest TEXT NOT NULL)`

type ServiceEffect struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Before string `json:"before"`
	After  string `json:"after"`
	Status string `json:"status"`
	Digest string `json:"digest"`
}

func (m *Manager) ServiceEffects(id string) ([]ServiceEffect, error) {
	rows, err := m.db.Query("SELECT id,name,before_state,after_state,status,digest FROM agent_service_effects WHERE run_id=? ORDER BY rowid", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ServiceEffect{}
	for rows.Next() {
		var item ServiceEffect
		if err = rows.Scan(&item.ID, &item.Name, &item.Before, &item.After, &item.Status, &item.Digest); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (m *Manager) effectLocked(id, ws string, ctx context.Context, worker bool) error {
	if m.closed {
		return ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	state, err := m.agentStateLocked(id, ws)
	if err != nil {
		return err
	}
	if worker {
		gen, ok := ctx.Value(agentGenerationKey{}).(int64)
		if !ok || state.Run.Generation != gen || state.Run.Status != "running" {
			return ErrConflict
		}
	}
	if err = m.validateAgentLocked(state); err != nil {
		return err
	}
	if m.publications[ws] != "" {
		return ErrBusy
	}
	return nil
}

type ServiceTransition func(context.Context, string, string, string) error

// Serialize changes to an individual service and pin the workspace binding
// while its native operation runs. A durable intent precedes the OS effect.
func (m *Manager) ChangeService(ctx context.Context, id, ws, name, before, after string, apply ServiceTransition) (ServiceEffect, error) {
	item := ServiceEffect{ID: newID(), Name: name, Before: before, After: after, Status: "applying"}
	item.Digest = fmt.Sprintf("%x", sha256.Sum256([]byte(id+"\x00"+item.ID+"\x00"+name+"\x00"+before+"\x00"+after)))
	if !((before == "running" && after == "stopped") || (before == "stopped" && after == "running")) {
		return item, ErrInvalid
	}
	m.mu.Lock()
	if err := m.effectLocked(id, ws, ctx, true); err != nil {
		m.mu.Unlock()
		return item, err
	}
	var busy int
	err := m.db.QueryRow("SELECT count(*) FROM agent_service_effects WHERE name=? AND status IN ('applying','restoring')", name).Scan(&busy)
	if err == nil && busy > 0 {
		err = ErrBusy
	}
	if err == nil {
		_, err = m.db.Exec("INSERT INTO agent_service_effects(id,run_id,name,before_state,after_state,status,digest) VALUES(?,?,?,?,?,?,?)", item.ID, id, name, before, after, item.Status, item.Digest)
	}
	if err != nil {
		m.mu.Unlock()
		return item, err
	}
	return m.runServiceLocked(ctx, ws, id, item, false, apply)
}

// RestoreService is an explicit user action authenticated against the original
// run and effect. The native adapter must check the live state again.
func (m *Manager) RestoreService(ctx context.Context, id, ws, effect, digest string, apply ServiceTransition) (ServiceEffect, error) {
	m.mu.Lock()
	if err := m.effectLocked(id, ws, ctx, false); err != nil {
		m.mu.Unlock()
		return ServiceEffect{}, err
	}
	var item ServiceEffect
	err := m.db.QueryRow("SELECT id,name,before_state,after_state,status,digest FROM agent_service_effects WHERE run_id=? AND id=?", id, effect).Scan(&item.ID, &item.Name, &item.Before, &item.After, &item.Status, &item.Digest)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	if err == nil && item.Digest != digest {
		err = ErrConflict
	}
	if err == nil && item.Status == "restored" {
		m.mu.Unlock()
		return item, nil
	}
	if err == nil && item.Status != "applied" {
		err = ErrConflict
	}
	if err == nil {
		var newer int
		// A later action on the same service invalidates this receipt, even if the
		// service happens to have come back to the same state.
		err = m.db.QueryRow("SELECT count(*) FROM agent_service_effects WHERE name=? AND rowid>(SELECT rowid FROM agent_service_effects WHERE id=?) AND status<>'restored'", item.Name, effect).Scan(&newer)
		if err == nil && newer > 0 {
			err = ErrConflict
		}
	}
	if err == nil {
		_, err = m.db.Exec("UPDATE agent_service_effects SET status='restoring' WHERE id=?", effect)
	}
	if err != nil {
		m.mu.Unlock()
		return item, err
	}
	return m.runServiceLocked(ctx, ws, id, item, true, apply)
}

// Caller holds Manager.mu. Release it while querying and changing the host;
// Close waits for the operation before closing the database.
func (m *Manager) runServiceLocked(ctx context.Context, ws, id string, item ServiceEffect, restore bool, apply ServiceTransition) (ServiceEffect, error) {
	if m.publications == nil {
		m.publications = map[string]string{}
	}
	m.publications[ws] = id
	m.operations.Add(1)
	m.mu.Unlock()
	defer m.operations.Done()
	from, to := item.Before, item.After
	item.Status = "applied"
	if restore {
		from, to = to, from
		item.Status = "restored"
	}
	err := apply(ctx, item.Name, from, to)
	if err != nil {
		item.Status = "unknown"
		if errors.Is(err, ErrConflict) {
			if restore {
				item.Status = "applied"
			} else {
				item.Status = "obsolete"
			}
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.publications, ws)
	tx, e := m.db.Begin()
	if e != nil {
		return item, errors.Join(err, e)
	}
	defer tx.Rollback()
	if _, e = tx.Exec("UPDATE agent_service_effects SET status=? WHERE id=?", item.Status, item.ID); e == nil {
		b, _ := json.Marshal(item)
		e = writeEvent(tx, id, "service_effect", "系统服务 "+item.Name+" · "+item.Status, b)
	}
	if e == nil {
		e = tx.Commit()
	}
	return item, errors.Join(err, e)
}
