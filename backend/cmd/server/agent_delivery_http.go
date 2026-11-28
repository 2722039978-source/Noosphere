package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/noosphere/backend/internal/agent"
	"github.com/noosphere/backend/internal/agent/artifact"
	"github.com/noosphere/backend/internal/agent/host"
	"github.com/noosphere/backend/internal/agent/renderpack"
	"github.com/noosphere/backend/internal/agent/toolpack"
	"github.com/noosphere/backend/internal/workspaces"
)

func deliveryJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(value)
}
func deliveryError(w http.ResponseWriter, err error) {
	status := http.StatusConflict
	switch {
	case errors.Is(err, workspaces.ErrNotFound):
		status = 404
	case errors.Is(err, workspaces.ErrInvalid):
		status = 400
	case errors.Is(err, workspaces.ErrBusy):
		status = 429
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
func readDeliveryRequest(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(v) != nil || dec.Decode(new(any)) != io.EOF {
		return workspaces.ErrInvalid
	}
	return nil
}
func registerDeliveryRoutes(mux *http.ServeMux, m *workspaces.Manager, systemEnabled bool) {
	native := agent.NewNativeExecutor(systemEnabled)
	toolpack.RegisterRoutes(mux, m.AgentDataDir())
	renderpack.RegisterRoutes(mux, m.AgentDataDir())
	const grant = "/api/workspaces/{workspace}/agent-grant"
	mux.HandleFunc("GET "+grant, func(w http.ResponseWriter, r *http.Request) {
		binding, e := m.AgentGrant(r.PathValue("workspace"), nil)
		if e != nil {
			deliveryError(w, e)
			return
		}
		deliveryJSON(w, map[string]string{"binding_id": binding})
	})
	mux.HandleFunc("POST "+grant, func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Binding string `json:"binding_id"`
		}
		if e := readDeliveryRequest(w, r, &in); e != nil {
			deliveryError(w, e)
			return
		}
		binding, e := m.AgentGrant(r.PathValue("workspace"), &in.Binding)
		if e != nil {
			deliveryError(w, e)
			return
		}
		deliveryJSON(w, map[string]string{"binding_id": binding})
	})
	const base = "/api/workspaces/{workspace}/runs/{run}"
	mux.HandleFunc("POST "+base+"/control", func(w http.ResponseWriter, r *http.Request) {
		var in workspaces.ControlRequest
		if e := readDeliveryRequest(w, r, &in); e != nil {
			deliveryError(w, e)
			return
		}
		id, ws := r.PathValue("run"), r.PathValue("workspace")
		if in.Action == "restore_service" {
			if !native.Services {
				deliveryError(w, workspaces.ErrConflict)
				return
			}
			if _, err := m.RestoreService(r.Context(), id, ws, in.Value, in.Expected, serviceTransition(native)); err != nil {
				deliveryError(w, err)
				return
			}
			run, err := m.Run(id)
			if err != nil {
				deliveryError(w, err)
				return
			}
			deliveryJSON(w, run)
			return
		}
		if in.Action == "undo" {
			s, e := m.AgentState(id, ws)
			if e == nil {
				e = m.ValidateAgent(r.Context(), id, ws)
			}
			if e == nil && (!workspaces.UndoableAgentStatus(s.Run.Status) || s.History) {
				e = workspaces.ErrConflict
			}
			if e == nil {
				e = m.UndoAgent(r.Context(), id, ws, in, func(ctx context.Context) error {
					d, e := host.Open(ctx, m.AgentDataDir(), id, s.Workspace.ProjectPath, s.Binding)
					if e != nil {
						return e
					}
					return d.Undo(ctx, in.Expected)
				})
			}
			if e != nil {
				deliveryError(w, e)
				return
			}
			current, e := m.Run(id)
			if e != nil {
				deliveryError(w, e)
				return
			}
			deliveryJSON(w, current)
			return
		}
		run, e := m.ControlAgent(id, ws, in)
		if e != nil {
			deliveryError(w, e)
			return
		}
		deliveryJSON(w, run)
	})
	mux.HandleFunc("GET "+base+"/delivery", func(w http.ResponseWriter, r *http.Request) {
		offset := 0
		if raw := r.URL.Query().Get("change_offset"); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 0 {
				deliveryError(w, workspaces.ErrInvalid)
				return
			}
			offset = value
		}
		id, ws := r.PathValue("run"), r.PathValue("workspace")
		s, e := m.AgentState(id, ws)
		if e != nil {
			deliveryError(w, e)
			return
		}
		artifacts, e := m.AgentArtifacts(id)
		if e != nil {
			deliveryError(w, e)
			return
		}
		changes := []host.Change{}
		conflicts := []host.FileConflict{}
		status := "draft"
		if host.Existing(m.AgentDataDir(), id) {
			var d *host.Draft
			if s.History || m.ValidateAgent(r.Context(), id, ws) != nil {
				d, e = host.OpenHistory(r.Context(), m.AgentDataDir(), id)
			} else {
				d, e = host.Open(r.Context(), m.AgentDataDir(), id, s.Workspace.ProjectPath, s.Binding)
			}
			if e == nil {
				status, e = d.Status(r.Context())
			}
			if e == nil {
				changes, e = d.Changes(r.Context())
			}
			if e == nil {
				conflicts, e = d.FileConflicts(r.Context())
			}
			if e != nil {
				deliveryError(w, e)
				return
			}
		}
		// The files can be restored before SQLite records the final receipt.
		// A paused run can reissue undo to finalize that same operation.
		undoReceiptPending := status == "undone" && s.Run.Status == "paused"
		canUndo := (status == "published" || status == "publishing" || status == "undoing" || status == "undoing_partial" || undoReceiptPending) && workspaces.UndoableAgentStatus(s.Run.Status) && !s.History && m.ValidateAgent(r.Context(), id, ws) == nil
		effects, err := m.ServiceEffects(id)
		if err != nil {
			deliveryError(w, err)
			return
		}
		canRestore := native.Services && !s.History && len(effects) > 0 && m.ValidateAgent(r.Context(), id, ws) == nil
		digest := host.ManifestDigest(changes)
		total := len(changes)
		offset = min(offset, total)
		end := min(offset+100, total)
		var next *int
		if end < total {
			next = &end
		}
		deliveryJSON(w, map[string]any{"effects": effects, "can_restore_services": canRestore, "conflicts": conflicts, "publication_state": status, "can_undo": canUndo, "undo_receipt_pending": undoReceiptPending && canUndo, "changes": changes[offset:end], "change_offset": offset, "change_total": total, "next_change_offset": next, "digest": digest, "artifacts": artifacts, "published": status == "published" || status == "undoing", "undone": status == "undone", "approval": s.Approval, "history": s.History})
	})
	mux.HandleFunc("GET "+base+"/artifacts/{artifact}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("run")
		if _, e := m.AgentState(id, r.PathValue("workspace")); e != nil {
			deliveryError(w, e)
			return
		}
		items, e := m.AgentArtifacts(id)
		if e != nil {
			deliveryError(w, e)
			return
		}
		for _, raw := range items {
			var a artifact.Item
			if err := json.Unmarshal(raw, &a); err != nil {
				deliveryError(w, err)
				return
			}
			if a.ID != r.PathValue("artifact") {
				continue
			}
			d, e := host.OpenHistory(r.Context(), m.AgentDataDir(), id)
			if e != nil {
				deliveryError(w, e)
				return
			}
			hash, kind := a.SHA256, "application/octet-stream"
			if r.URL.Query().Get("preview") == "1" {
				if !a.Preview {
					deliveryError(w, fmt.Errorf("此版本没有可用预览"))
					return
				}
				kind = a.PreviewMIME
				if a.PreviewHash != "" {
					hash = a.PreviewHash
				}
			}
			f, e := d.OpenBlob(r.Context(), hash)
			if e != nil {
				deliveryError(w, e)
				return
			}
			defer f.Close()
			w.Header().Set("Content-Type", kind)
			w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Cache-Control", "private, no-store")
			http.ServeContent(w, r, a.Name, time.Time{}, f)
			return
		}
		deliveryError(w, workspaces.ErrNotFound)
	})
}
