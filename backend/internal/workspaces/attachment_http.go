package workspaces

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/noosphere/backend/internal/attachmentfile"
)

const attachmentHeader = "X-Noosphere-Attachment"
const maxAttachmentChunkBytes = 8 << 20

type uploadKey struct {
	attachmentScope
	id string
}

func (m *Manager) registerAttachmentRoutes(mux *http.ServeMux) {
	var active sync.Map
	slots := make(chan struct{}, 4)
	mux.HandleFunc("POST /api/chat-attachments/stage", func(w http.ResponseWriter, r *http.Request) {
		var input attachmentChunk
		meta, err := url.PathUnescape(r.Header.Get(attachmentHeader))
		if err != nil || len(meta) > 24000 || json.Unmarshal([]byte(meta), &input) != nil || !safeID.MatchString(input.ID) ||
			!safeID.MatchString(input.WorkspaceID) || !safeID.MatchString(input.SessionID) || len(input.ProjectPath) > 16000 {
			respond(w, http.StatusBadRequest, map[string]string{"error": "附件上传信息无效，请重新选择文件"})
			return
		}
		if imageType(input.Name) != "" && input.Size > maxImageBytes {
			respond(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "图片最大为 8 MiB"})
			return
		}
		if input.Size > attachmentfile.MaxBytes {
			respond(w, http.StatusRequestEntityTooLarge, map[string]string{"error": fmt.Sprintf("每份附件最多 %d MB，请缩小文件后重试", attachmentfile.MaxMiB)})
			return
		}
		if input.Size < 0 || input.Offset < 0 || (input.Size != 0 && input.Offset >= input.Size) || (input.Size == 0 && input.Offset != 0) {
			respondError(w, ErrInvalid)
			return
		}
		if err := attachmentfile.ValidateName(input.Name); err != nil {
			respond(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			respond(w, http.StatusTooManyRequests, map[string]string{"error": "附件同时处理数量已满，请稍后重试"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		key := uploadKey{input.attachmentScope, input.ID}
		if _, exists := active.LoadOrStore(key, cancel); exists {
			respond(w, http.StatusConflict, map[string]string{"error": "同一附件正在处理，请稍后重试"})
			return
		}
		defer active.Delete(key)
		r.Body = http.MaxBytesReader(w, r.Body, maxAttachmentChunkBytes)
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			var oversized *http.MaxBytesError
			if errors.As(err, &oversized) {
				respond(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "附件单次传输过大，请重新选择文件"})
			} else {
				respond(w, http.StatusBadRequest, map[string]string{"error": "读取附件失败，请检查文件权限后重试"})
			}
			return
		}
		if input.Size != 0 {
			received, path, err := m.appendAttachmentChunk(ctx, input, raw)
			if err != nil {
				respondError(w, err)
				return
			}
			reply := attachmentChunkResult{Received: received}
			if path != "" {
				defer os.Remove(path)
				ref, err := m.stageAttachmentFile(ctx, input.attachmentUpload, path, input.Size)
				if err != nil {
					respondError(w, err)
					return
				}
				reply.Attachment = &ref
			}
			respond(w, http.StatusOK, reply)
			return
		}
		ref, err := m.stageAttachment(ctx, input.attachmentUpload, raw)
		if err != nil {
			respondError(w, err)
			return
		}
		respond(w, http.StatusOK, ref)
	})
	mux.HandleFunc("POST /api/chat-attachments/discard", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			attachmentScope
			ID string `json:"id"`
		}
		if e := decode(w, r, &input); e != nil || !safeID.MatchString(input.ID) {
			respondError(w, ErrInvalid)
			return
		}
		if err := m.discardAttachment(r.Context(), input.attachmentScope, input.ID); err != nil {
			respondError(w, err)
			return
		}
		if cancel, exists := active.Load(uploadKey{input.attachmentScope, input.ID}); exists {
			cancel.(context.CancelFunc)()
		}
		respond(w, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/chat-attachments/prepare", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			attachmentScope
			AttachmentTurn
		}
		if err := decode(w, r, &input); err != nil {
			respondError(w, err)
			return
		}
		out, err := m.PrepareAttachments(r.Context(), input.WorkspaceID, input.SessionID, input.AttachmentTurn)
		if err != nil {
			respondError(w, err)
			return
		}
		respond(w, http.StatusOK, out)
	})
	mux.HandleFunc("POST /api/chat-attachments/delete-session", func(w http.ResponseWriter, r *http.Request) {
		var input attachmentScope
		if err := decode(w, r, &input); err != nil {
			respondError(w, err)
			return
		}
		// Stop in-flight processing before removing committed records.
		active.Range(func(key, cancel any) bool {
			if key.(uploadKey).attachmentScope == input {
				cancel.(context.CancelFunc)()
			}
			return true
		})
		if err := m.deleteSessionAttachments(r.Context(), input); err != nil {
			respondError(w, err)
			return
		}
		respond(w, http.StatusOK, map[string]bool{"ok": true})
	})
}
