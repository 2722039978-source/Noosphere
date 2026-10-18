package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/noosphere/backend/internal/nebula/engine/vector"
)

type TextRecord struct {
	Value []byte
	Text  string
}

type DeltaStats struct {
	UpdatedRecords int   `json:"updated_records"`
	ReusedRecords  int   `json:"reused_records"`
	DeletedRecords int   `json:"deleted_records"`
	EmbeddedChunks int   `json:"embedded_chunks"`
	GraphRebuilt   bool  `json:"graph_rebuilt"`
	GraphMillis    int64 `json:"graph_ms"`
}

func embedContext(ctx context.Context, emb interface {
	Embed(string) ([]float32, error)
}, text string) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if contextual, ok := emb.(interface {
		EmbedContext(context.Context, string) ([]float32, error)
	}); ok {
		return contextual.EmbedContext(ctx, text)
	}
	return emb.Embed(text)
}

// ReplacePrefix atomically replaces one project's records. Embedding failure
// leaves the previous snapshot available. Unchanged chunks reuse their vectors.
func (e *Engine) ReplacePrefix(prefix string, records map[string]TextRecord) error {
	_, err := e.ReplacePrefixContext(context.Background(), prefix, records)
	return err
}

func (e *Engine) ReplacePrefixContext(ctx context.Context, prefix string, records map[string]TextRecord) (stats DeltaStats, err error) {
	if prefix == "" || !strings.HasSuffix(prefix, ":") {
		return stats, fmt.Errorf("namespace prefix required")
	}
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	if e.closed {
		return stats, fmt.Errorf("engine closed")
	}
	if err := ctx.Err(); err != nil {
		return stats, err
	}
	nodes := e.hnsw.ExportNodes()
	vectors := make(map[string][]float32, len(nodes))
	for _, node := range nodes {
		vectors[node.ID] = node.Vec
	}
	puts := make(map[string][]byte, len(records))
	deletes := []string{}
	for key, record := range records {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		if !strings.HasPrefix(key, prefix) {
			return stats, fmt.Errorf("record outside project namespace")
		}
		if record.Text != "" && (e.indexTexts[key] != record.Text || len(vectors[key]) == 0) {
			v, err := embedContext(ctx, e.embedder, record.Text)
			if err != nil {
				return stats, err
			}
			vectors[key] = v
			stats.EmbeddedChunks++
			stats.GraphRebuilt = true
		}
		if record.Text == "" && len(vectors[key]) > 0 {
			delete(vectors, key)
			stats.GraphRebuilt = true
		}
		previous, getErr := e.Get(key)
		if getErr == nil && bytes.Equal(previous, record.Value) && e.indexTexts[key] == record.Text {
			stats.ReusedRecords++
			continue
		}
		value, err := json.Marshal(indexedRecord{Format: recordFormat, Value: record.Value, Text: record.Text})
		if err != nil {
			return stats, err
		}
		puts[key] = value
	}
	for key := range e.indexTexts {
		if strings.HasPrefix(key, prefix) {
			if _, ok := records[key]; !ok {
				deletes = append(deletes, key)
				delete(vectors, key)
				stats.GraphRebuilt = true
			}
		}
	}
	stats.UpdatedRecords, stats.DeletedRecords = len(puts), len(deletes)
	graph := e.hnsw
	if stats.GraphRebuilt {
		started := time.Now()
		graph = vector.NewHNSW(16, 50)
		keys := make([]string, 0, len(vectors))
		for key := range vectors {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if err := ctx.Err(); err != nil {
				return stats, err
			}
			graph.Insert(key, vectors[key])
		}
		stats.GraphMillis = time.Since(started).Milliseconds()
	}
	if err := ctx.Err(); err != nil {
		return stats, err
	}
	if len(puts) == 0 && len(deletes) == 0 && !stats.GraphRebuilt {
		return stats, nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return stats, err
	}
	if err := e.lsm.Batch(puts, deletes); err != nil {
		return stats, err
	}
	for _, key := range deletes {
		e.removeFromIndex(key)
		delete(e.indexTexts, key)
	}
	for key := range puts {
		record := records[key]
		e.removeFromIndex(key)
		e.addToIndex(key, record.Text)
		e.indexTexts[key] = record.Text
	}
	e.hnsw = graph
	return stats, nil
}
