package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/noosphere/backend/internal/nebula/engine/embed"
)

const recordFormat = "noosphere:indexed-record:v1"

type indexedRecord struct {
	Format string `json:"format"`
	Value  []byte `json:"value"`
	Text   string `json:"text"`
}
type cachedVector struct {
	Text   string    `json:"text"`
	Vector []float32 `json:"vector"`
}
type vectorSnapshot struct {
	Model   string                  `json:"model"`
	Entries map[string]cachedVector `json:"entries"`
}

func decodeRecord(raw []byte) ([]byte, string) {
	var envelope indexedRecord
	if json.Unmarshal(raw, &envelope) == nil && envelope.Format == recordFormat {
		return envelope.Value, envelope.Text
	}
	// Upgrade legacy memories without indexing JSON field names/embedding floats.
	var legacy struct {
		Content string   `json:"content"`
		Summary string   `json:"summary"`
		Tags    []string `json:"tags"`
	}
	if json.Unmarshal(raw, &legacy) == nil && (legacy.Content != "" || legacy.Summary != "") {
		return raw, strings.Join([]string{legacy.Content, legacy.Summary, strings.Join(legacy.Tags, " ")}, "\n")
	}
	return raw, string(raw)
}

func embeddingIdentity(emb embed.Embedder) string {
	if named, ok := emb.(interface{ Identity() string }); ok {
		return named.Identity()
	}
	return fmt.Sprintf("%T:%d", emb, emb.Dim())
}

func (e *Engine) vectorCache(model string) map[string]cachedVector {
	raw, err := os.ReadFile(filepath.Join(e.dataDir, "vectors.json"))
	if err != nil {
		return nil
	}
	var snapshot vectorSnapshot
	if json.Unmarshal(raw, &snapshot) != nil || snapshot.Model != model {
		return nil
	}
	dim := 0
	for _, cached := range snapshot.Entries {
		if len(cached.Vector) == 0 || (dim != 0 && len(cached.Vector) != dim) { return nil }
		dim = len(cached.Vector)
	}
	return snapshot.Entries
}

func atomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".vectors-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// Records returns a stable copy of live values in a namespace, including after restart.
func (e *Engine) Records(prefix string) map[string][]byte {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make(map[string][]byte)
	e.lsm.ForEach(func(k, v []byte) bool {
		if strings.HasPrefix(string(k), prefix) {
			value, _ := decodeRecord(v)
			out[string(k)] = append([]byte(nil), value...)
		}
		return true
	})
	return out
}
