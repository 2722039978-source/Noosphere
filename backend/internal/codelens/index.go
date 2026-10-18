package codelens

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/noosphere/backend/internal/nebula/engine"
	"github.com/noosphere/backend/internal/nebula/service"
)

type SourceChunk struct {
	ID          string   `json:"id"`
	File        string   `json:"file"`
	Content     string   `json:"content"`
	StartLine   int      `json:"start_line"`
	EndLine     int      `json:"end_line"`
	Language    string   `json:"language"`
	Symbols     []string `json:"symbols"`
	Kind        string   `json:"kind"`
	Symbol      string   `json:"symbol"`
	SourceHash  string   `json:"source_hash"`
	ContentHash string   `json:"content_hash"`
	Part        int      `json:"part"`
	Parts       int      `json:"parts"`
	Callees     []string `json:"callees"`
	Callers     []string `json:"callers"`
	Unresolved  []string `json:"unresolved"`
}

var projectIDPattern = regexp.MustCompile(`^code-[a-f0-9]{24}$`)

func StoreIndex(eng *engine.Engine, project string, chunks []SourceChunk) error {
	_, err := StoreIndexContext(context.Background(), eng, project, chunks)
	return err
}

func StoreIndexContext(ctx context.Context, eng *engine.Engine, project string, chunks []SourceChunk) (engine.DeltaStats, error) {
	if !projectIDPattern.MatchString(project) {
		return engine.DeltaStats{}, fmt.Errorf("invalid project namespace")
	}
	records := make(map[string]engine.TextRecord, len(chunks))
	for _, c := range chunks {
		if c.File == "" || c.StartLine < 1 || c.EndLine < c.StartLine {
			return engine.DeltaStats{}, fmt.Errorf("invalid source reference")
		}
		id := fmt.Sprintf("%x", sha256.Sum256([]byte(c.ID)))
		record := service.MemoryRecord{ID: id, SessionID: project, Type: service.Semantic, Content: c.Content, Summary: c.File,
			Importance: 0.85, CreatedAt: time.Now(), Tags: []string{"codelens", c.Language}, Metadata: map[string]string{
				"source": "codelens", "file": c.File, "start_line": strconv.Itoa(c.StartLine), "end_line": strconv.Itoa(c.EndLine), "language": c.Language}}
		record.Metadata["kind"], record.Metadata["symbol"] = c.Kind, c.Symbol
		record.Metadata["source_hash"], record.Metadata["content_hash"] = c.SourceHash, c.ContentHash
		record.Metadata["part"], record.Metadata["parts"] = strconv.Itoa(c.Part), strconv.Itoa(c.Parts)
		for name, values := range map[string][]string{"callees": c.Callees, "callers": c.Callers, "unresolved": c.Unresolved} {
			encoded, _ := json.Marshal(values)
			record.Metadata[name] = string(encoded)
		}
		if old, err := eng.Get(project + ":" + id); err == nil {
			var previous service.MemoryRecord
			if json.Unmarshal(old, &previous) == nil {
				record.CreatedAt = previous.CreatedAt
			}
		}
		value, err := json.Marshal(record)
		if err != nil {
			return engine.DeltaStats{}, err
		}
		records[project+":"+id] = engine.TextRecord{Value: value, Text: c.File + "\n" + strings.Join(c.Symbols, " ") + "\n" + c.Content}
	}
	return eng.ReplacePrefixContext(ctx, project+":", records)
}

// PersistIndexResponse runs before the proxy exposes a successful index result.
// The UI only announces retrieval readiness after the durable snapshot commits.
func PersistIndexResponse(eng *engine.Engine) func(*http.Response) error {
	return func(resp *http.Response) error {
		if resp.Request.URL.Path != "/api/v1/index" || resp.StatusCode != http.StatusOK {
			return nil
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 48*1024*1024+1))
		if err != nil {
			return err
		}
		if len(raw) > 48*1024*1024 {
			return fmt.Errorf("index response too large")
		}
		var result struct {
			ProjectID string        `json:"project_id"`
			Chunks    []SourceChunk `json:"chunks"`
		}
		if err = json.Unmarshal(raw, &result); err != nil {
			return err
		}
		if eng == nil {
			return fmt.Errorf("memory engine unavailable; code index not persisted")
		}
		if err = resp.Request.Context().Err(); err != nil {
			return err
		}
		stats, storeErr := StoreIndexContext(resp.Request.Context(), eng, result.ProjectID, result.Chunks)
		if storeErr != nil {
			return storeErr
		}
		var summary map[string]any
		json.Unmarshal(raw, &summary)
		delete(summary, "chunks")
		summary["indexed_chunks"] = len(result.Chunks)
		summary["retrieval_ready"] = true
		summary["delta"] = stats
		body, err := json.Marshal(summary)
		if err != nil {
			return err
		}
		resp.Body = io.NopCloser(bytes.NewReader(body))
		resp.ContentLength = int64(len(body))
		resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
		return nil
	}
}
