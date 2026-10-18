package service

import (
	"math"
	"strings"
	"sync"
)

// embedder is a minimal interface for generating text embeddings.
type embedder interface {
	Embed(text string) ([]float32, error)
}

// anchorTexts maps each MemoryType to a representative description
// used for embedding-based classification.
var anchorTexts = map[MemoryType]string{
	Working:    "current task in progress, working on now, active project, short-term goal, todo item, immediate action",
	Episodic:   "event that happened, past experience, meeting notes, conversation record, personal story, memory of an event",
	Semantic:   "factual knowledge, concept definition, general information, learned fact, encyclopedia entry, objective truth",
	Procedural: "step by step instructions, how to guide, procedure, tutorial, recipe, method, workflow explanation",
}

type ClassifiedMemory struct {
	Type       MemoryType
	Importance float64
	Summary    string
}
type Extractor struct {
	emb     embedder
	anchors map[MemoryType][]float32
	mu      sync.Mutex
	initd   bool
}

func NewExtractor(emb embedder) *Extractor {
	return &Extractor{
		emb:     emb,
		anchors: make(map[MemoryType][]float32),
	}
}

// ensureAnchors lazily computes anchor embeddings once on first use.
func (e *Extractor) ensureAnchors() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.initd {
		return
	}
	e.initd = true
	if e.emb == nil {
		return
	}
	for memType, text := range anchorTexts {
		vec, err := e.emb.Embed(text)
		if err != nil {
			continue
		}
		e.anchors[memType] = vec
	}
}

// Classify determines the memory type using embedding similarity.
// Falls back to keyword matching if the embedder is unavailable or fails.

// Classify determines the memory type using embedding similarity.
// Note: content from CodeLens (metadata.source == "codelens") bypasses this
// method entirely — pipeline.go assigns Semantic type + 0.85 importance
// directly, preserving code semantics without running embedding classification.
func (e *Extractor) Classify(content string, tags []string) ClassifiedMemory {
	// Tags override any embedding-based classification
	tagSet := make(map[string]bool)
	for _, t := range tags {
		tagSet[strings.ToLower(t)] = true
	}
	if tagSet["procedural"] || tagSet["howto"] {
		return ClassifiedMemory{Type: Procedural, Importance: 0.7, Summary: extractFirstSentence(content)}
	}
	if tagSet["semantic"] || tagSet["knowledge"] {
		return ClassifiedMemory{Type: Semantic, Importance: 0.8, Summary: extractFirstSentence(content)}
	}
	if tagSet["working"] || tagSet["task"] {
		return ClassifiedMemory{Type: Working, Importance: 0.9, Summary: extractFirstSentence(content)}
	}
	if tagSet["episodic"] {
		return ClassifiedMemory{Type: Episodic, Importance: 0.6, Summary: extractFirstSentence(content)}
	}

	// Try embedding-based classification
	e.ensureAnchors()
	if e.emb != nil && len(e.anchors) > 0 {
		contentVec, err := e.emb.Embed(content)
		if err == nil && len(contentVec) > 0 {
			bestType := Episodic
			bestSim := -1.0
			for memType, anchorVec := range e.anchors {
				sim := cosineSimilarity(contentVec, anchorVec)
				if sim > bestSim {
					bestSim = sim
					bestType = memType
				}
			}
			importance := importanceForType(bestType)
			if bestSim > 0.3 {
				importance = clamp(importance+bestSim*0.3, 0.0, 1.0)
			}
			return ClassifiedMemory{
				Type:       bestType,
				Importance: importance,
				Summary:    extractFirstSentence(content),
			}
		}
	}

	// Fallback: keyword-based classification
	return fallbackClassify(content)
}

// fallbackClassify uses keyword matching when embeddings are unavailable.
func fallbackClassify(content string) ClassifiedMemory {
	lower := strings.ToLower(content)

	result := ClassifiedMemory{
		Type:       Episodic,
		Importance: 0.5,
		Summary:    truncate(content, 200),
	}

	if strings.Contains(lower, "step") || strings.Contains(lower, "how to") ||
		strings.Contains(lower, "instruction") || strings.Contains(lower, "procedure") {
		result.Type = Procedural
		result.Importance = 0.7
		result.Summary = extractFirstSentence(content)
		return result
	}

	if strings.Contains(lower, "definition") || strings.Contains(lower, "concept") ||
		strings.Contains(lower, "fact") || strings.Contains(lower, "knowledge") ||
		strings.Contains(lower, "is a") {
		result.Type = Semantic
		result.Importance = 0.8
		result.Summary = extractFirstSentence(content)
		return result
	}

	if strings.Contains(lower, "todo") || strings.Contains(lower, "task") ||
		strings.Contains(lower, "current") || strings.Contains(lower, "now") ||
		strings.Contains(lower, "working") {
		result.Type = Working
		result.Importance = 0.9
		result.Summary = extractFirstSentence(content)
		return result
	}

	if strings.Contains(lower, "happened") || strings.Contains(lower, "event") ||
		strings.Contains(lower, "yesterday") || strings.Contains(lower, "today") ||
		strings.Contains(lower, "meeting") {
		result.Type = Episodic
		result.Importance = 0.6
		result.Summary = extractFirstSentence(content)
		return result
	}

	if len(content) < 100 {
		result.Importance = 0.4
		result.Type = Working
	} else if len(content) > 1000 {
		result.Importance = 0.7
	}

	return result
}

// cosineSimilarity computes the cosine similarity between two float32 vectors.
// Returns a value in [-1, 1].
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func importanceForType(mt MemoryType) float64 {
	switch mt {
	case Working:
		return 0.9
	case Episodic:
		return 0.6
	case Semantic:
		return 0.8
	case Procedural:
		return 0.7
	default:
		return 0.5
	}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func extractFirstSentence(s string) string {
	for i, r := range s {
		if r == '.' || r == '!' || r == '?' {
			end := i + 1
			if end > 300 {
				end = 300
			}
			return s[:end]
		}
	}
	return truncate(s, 200)
}
