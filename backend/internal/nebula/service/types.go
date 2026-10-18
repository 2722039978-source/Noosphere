package service

import "time"

type MemoryType string

const (
	Working    MemoryType = "working"
	Episodic   MemoryType = "episodic"
	Semantic   MemoryType = "semantic"
	Procedural MemoryType = "procedural"
)

type MemoryRecord struct {
	ID         string            `json:"id"`
	SessionID  string            `json:"session_id"`
	Type       MemoryType        `json:"type"`
	Content    string            `json:"content"`
	Summary    string            `json:"summary,omitempty"`
	Embedding  []float32         `json:"embedding,omitempty"`
	Tags       []string          `json:"tags,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Importance float64           `json:"importance"`
	CreatedAt  time.Time         `json:"created_at"`
}

type PipelineInput struct {
	SessionID string
	Content   string
	Tags      []string
	Metadata  map[string]string
}

type PipelineOutput struct {
	Record     MemoryRecord
	Type       MemoryType
	Importance float64
	Summary    string
	Embedding  []float32
}

type SearchRequest struct {
	SessionID string
	Query     string
	TopK      int
	Type      MemoryType
}

type SearchResult struct {
	Key    string       `json:"key"`
	Record MemoryRecord `json:"record"`
	Score  float64      `json:"score"`
}
