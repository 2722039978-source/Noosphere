package service

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/noosphere/backend/internal/nebula/engine"
	"github.com/noosphere/backend/internal/nebula/engine/retrieval"
)

type Pipeline struct {
	engine    *engine.Engine
	extractor *Extractor
}

func NewPipeline(eng *engine.Engine) *Pipeline {
	return &Pipeline{
		engine:    eng,
		extractor: NewExtractor(eng.Embedder()),
	}
}

func (p *Pipeline) Ingest(input PipelineInput) (*PipelineOutput, error) {
	// When source is "codelens", bypass embedding-based classification:
	// all code content is Semantic memory with elevated importance.
	// The actual code semantics are carried in metadata (file, language, entities).
	var classified ClassifiedMemory
	if input.Metadata != nil && input.Metadata["source"] == "codelens" {
		summary := input.Content
		if len(summary) > 200 {
			summary = summary[:200] + "..."
		}
		if input.Metadata["entity_type"] != "" {
			summary = input.Metadata["entity_type"] + ": " + summary
		}
		classified = ClassifiedMemory{
			Type:       Semantic,
			Importance: 0.85,
			Summary:    summary,
		}
	} else {
		classified = p.extractor.Classify(input.Content, input.Tags)
	}

	output := &PipelineOutput{
		Type:       classified.Type,
		Importance: classified.Importance,
		Summary:    classified.Summary,
	}

	vec, err := p.engine.Embed(input.Content)
	if err != nil {
		log.Printf("[pipeline] embed error: %v", err)
		vec = nil
	}
	output.Embedding = vec

	now := time.Now()
	record := MemoryRecord{
		ID:         fmt.Sprintf("mem-%d", now.UnixNano()),
		SessionID:  input.SessionID,
		Type:       output.Type,
		Content:    input.Content,
		Summary:    output.Summary,
		Embedding:  vec,
		Tags:       input.Tags,
		Metadata:   input.Metadata,
		Importance: output.Importance,
		CreatedAt:  now,
	}
	output.Record = record

	key := fmt.Sprintf("%s:%s", input.SessionID, record.ID)
	val, _ := json.Marshal(record)
	// Index only human-readable text (content + summary + tags). Indexing the
	// raw JSON would pollute BM25 stats with embedding floats and field names.
	indexText := strings.Join([]string{input.Content, output.Summary, strings.Join(input.Tags, " ")}, "\n")
	if err := p.engine.PutText(key, val, indexText); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}

	log.Printf("[pipeline] ingested %s/%s type=%s importance=%.2f",
		input.SessionID, record.ID, output.Type, output.Importance)
	return output, nil
}

func (p *Pipeline) Search(req SearchRequest) ([]SearchResult, error) {
	if req.TopK <= 0 {
		req.TopK = 10
	}

	// Session-scoped search: BM25 filters by prefix during scoring,
	// HNSW results filtered before RRF fusion. Prevents recall degradation
	// when many sessions exist in the engine.
	results, err := p.engine.SearchScoped(req.Query, req.TopK*3, req.SessionID+":")
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	filtered := make([]SearchResult, 0, len(results))
	for _, r := range results {
		val, err := p.engine.Get(r.Key)
		if err != nil {
			continue
		}
		var record MemoryRecord
		if err := json.Unmarshal(val, &record); err != nil {
			continue
		}
		if req.Type != "" && record.Type != req.Type {
			continue
		}
		composite := r.Score * (0.5 + record.Importance*0.5)
		filtered = append(filtered, SearchResult{
			Key:    r.Key,
			Record: record,
			Score:  composite,
		})
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Score > filtered[j].Score
	})

	if len(filtered) > req.TopK {
		filtered = filtered[:req.TopK]
	}

	return filtered, nil
}

func (p *Pipeline) Retrieve(query string, topK int) ([]retrieval.ScoredDoc, error) {
	return p.engine.Search(query, topK)
}
