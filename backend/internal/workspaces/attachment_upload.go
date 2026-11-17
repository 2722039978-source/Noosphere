package workspaces

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/noosphere/backend/internal/attachmentfile"
)

const uploadLifetime = 30 * time.Minute
const maxPendingUploads = 4

type attachmentChunk struct {
	attachmentUpload
	Size   int `json:"size"`
	Offset int `json:"offset"`
}

type attachmentChunkResult struct {
	Received   int            `json:"received"`
	Attachment *AttachmentRef `json:"attachment,omitempty"`
}

type pendingAttachment struct {
	path     string
	input    attachmentUpload
	size     int
	received int
	updated  time.Time
}

func attachmentFilename(id string) string {
	return fmt.Sprintf("%x.bin", sha256.Sum256([]byte(id)))
}

func (m *Manager) attachmentPath(id string) string {
	return filepath.Join(m.attachmentDir, attachmentFilename(id))
}

// appendAttachmentChunk owns a private temporary file until the last byte.
// Closing each write lets discard, retry, and shutdown remove it on Windows.
func (m *Manager) appendAttachmentChunk(ctx context.Context, chunk attachmentChunk, raw []byte) (int, string, error) {
	if len(raw) == 0 || len(raw) > chunk.Size-chunk.Offset {
		return 0, "", fmt.Errorf("%w: 附件分段大小无效，请重新选择文件", ErrInvalid)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, "", err
	}
	current, err := m.attachmentWorkspace(chunk.attachmentScope)
	if err != nil {
		return 0, "", err
	}
	if current.ProjectPath != chunk.ProjectPath {
		return 0, "", fmt.Errorf("%w: 项目目录已变化，请重新选择附件", ErrConflict)
	}
	key := uploadKey{chunk.attachmentScope, chunk.ID}
	upload := m.attachmentUploads[key]
	if chunk.Offset == 0 {
		if err := m.checkAttachmentUpload(ctx, current, chunk.attachmentUpload); err != nil {
			return 0, "", err
		}
		for id, pending := range m.attachmentUploads {
			if id == key || time.Since(pending.updated) > uploadLifetime {
				m.removePendingAttachment(id)
			}
		}
		if len(m.attachmentUploads) >= maxPendingUploads {
			return 0, "", fmt.Errorf("%w: 附件同时处理数量已满，请稍后重试", ErrBusy)
		}
		dir := filepath.Join(m.attachmentDir, attachmentfile.UploadDirectory)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return 0, "", err
		}
		file, err := os.CreateTemp(dir, "upload-*")
		if err != nil {
			return 0, "", err
		}
		upload = pendingAttachment{path: file.Name(), input: chunk.attachmentUpload, size: chunk.Size}
		m.attachmentUploads[key] = upload
		if err := file.Close(); err != nil {
			m.removePendingAttachment(key)
			return 0, "", err
		}
	}
	if upload.path == "" || upload.received != chunk.Offset || upload.size != chunk.Size || upload.input != chunk.attachmentUpload {
		return 0, "", fmt.Errorf("%w: 附件上传已中断，请重试", ErrConflict)
	}
	file, err := os.OpenFile(upload.path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		m.removePendingAttachment(key)
		return 0, "", err
	}
	_, err = file.Write(raw)
	if err == nil && upload.received+len(raw) == upload.size {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		m.removePendingAttachment(key)
		return 0, "", err
	}
	upload.received += len(raw)
	upload.updated = time.Now()
	if upload.received == upload.size {
		delete(m.attachmentUploads, key)
		return upload.received, upload.path, nil
	}
	m.attachmentUploads[key] = upload
	return upload.received, "", nil
}

func (m *Manager) removePendingAttachment(key uploadKey) {
	if upload, ok := m.attachmentUploads[key]; ok {
		_ = os.Remove(upload.path)
		delete(m.attachmentUploads, key)
	}
}

type attachmentTextIndex struct {
	Encoding string `json:"encoding"`
}

func (m *Manager) stageAttachmentFile(ctx context.Context, input attachmentUpload, path string, size int) (AttachmentRef, error) {
	ref := AttachmentRef{ID: input.ID, Name: input.Name, Size: size}
	file, err := os.Open(path)
	if err != nil {
		return ref, err
	}
	hash := sha256.New()
	count, err := io.Copy(hash, file)
	closeErr := file.Close()
	if err != nil {
		return ref, err
	}
	if closeErr != nil {
		return ref, closeErr
	}
	if count != int64(size) {
		return ref, fmt.Errorf("%w: 附件大小校验失败，请重试", ErrInvalid)
	}
	ref.SourceHash = fmt.Sprintf("%x", hash.Sum(nil))
	ws, exists, err := m.attachmentDraft(ctx, input, ref)
	if err != nil || exists {
		return ref, err
	}

	pdf, err := attachmentfile.PDFFormat(input.Name)
	if err != nil {
		return ref, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	var evidence []byte
	if pdf {
		var sections []attachmentfile.Section
		sections, err = attachmentfile.ExtractPDFFile(ctx, path)
		if err == nil {
			evidence, err = encodeAttachmentSections(sections)
		}
	} else {
		var encoding string
		encoding, err = attachmentfile.DetectTextEncoding(ctx, path)
		if err == nil {
			evidence, err = json.Marshal(attachmentTextIndex{Encoding: encoding})
		}
	}
	if err != nil {
		return ref, attachmentExtractionError(ctx, err)
	}
	return ref, m.saveAttachment(ctx, input, ws, ref, nil, evidence, path)
}

// A crash between renaming a file and committing its metadata can leave an
// orphan. The database remains authoritative for completed originals.
func (m *Manager) recoverAttachmentStorage() error {
	entries, err := os.ReadDir(m.attachmentDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	known := make(map[string]bool)
	rows, err := m.db.Query("SELECT id FROM chat_attachments WHERE original IS NULL AND cancelled=0")
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		known[attachmentFilename(id)] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, entry := range entries {
		if entry.Type().IsRegular() && len(entry.Name()) == 68 && strings.HasSuffix(entry.Name(), ".bin") && !known[entry.Name()] {
			if _, err := hex.DecodeString(entry.Name()[:64]); err == nil {
				_ = os.Remove(filepath.Join(m.attachmentDir, entry.Name()))
			}
		}
	}
	dir := filepath.Join(m.attachmentDir, attachmentfile.UploadDirectory)
	if uploads, err := os.ReadDir(dir); err == nil {
		for _, upload := range uploads {
			if upload.Type().IsRegular() && strings.HasPrefix(upload.Name(), "upload-") {
				_ = os.Remove(filepath.Join(dir, upload.Name()))
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (m *Manager) removeAttachmentFile(id string) error {
	err := os.Remove(m.attachmentPath(id))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (m *Manager) attachmentFileExists(id string) bool {
	info, err := os.Lstat(m.attachmentPath(id))
	return err == nil && info.Mode().IsRegular()
}
