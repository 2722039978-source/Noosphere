package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/noosphere/backend/internal/nebula/engine/embed"
	"github.com/noosphere/backend/internal/nebula/engine/lsm"
	"github.com/noosphere/backend/internal/nebula/engine/retrieval"
	"github.com/noosphere/backend/internal/nebula/engine/textindex"
	"github.com/noosphere/backend/internal/nebula/engine/vector"
)

// Engine is the Nebula memory engine combining LSM-Tree, HNSW vectors, and BM25.
type Engine struct {
	mu         sync.RWMutex
	writeMu    sync.Mutex // serialize mutations without holding the read/index lock across HTTP
	saveMu     sync.Mutex
	closeOnce  sync.Once
	doneCh     chan struct{}
	closed     bool
	indexTexts map[string]string
	lsm        *lsm.LSM
	hnsw       *vector.HNSW
	embedder   embed.Embedder
	dataDir    string

	// BM25 pre-built inverted index
	invertedIndex map[string]map[string]int // term -> docKey -> term frequency
	docLengths    map[string]int            // docKey -> token count
	totalDocs     int

	// Lifecycle
	stopCh chan struct{}
}

type Options struct {
	DataDir  string
	EmbedDim int
}

type EngineStats struct {
	KeyCount    int64 `json:"key_count"`
	DiskBytes   int64 `json:"disk_bytes"`
	WalEntries  int64 `json:"wal_entries"`
	VectorCount int   `json:"vector_count"`
	SSTCount    int   `json:"sst_count"`
	IndexTerms  int   `json:"index_terms"`
}

type SearchResult struct {
	Key   string  `json:"key"`
	Score float64 `json:"score"`
}

func DefaultOptions(dataDir string) *Options {
	return &Options{DataDir: dataDir, EmbedDim: 128}
}

func Open(opts *Options) (*Engine, error) {
	l, err := lsm.OpenLSM(opts.DataDir)
	if err != nil {
		return nil, fmt.Errorf("lsm: %w", err)
	}

	hnsw := vector.NewHNSW(16, 50)

	e := &Engine{
		lsm:           l,
		hnsw:          hnsw,
		embedder:      embed.NewMockEmbedder(opts.EmbedDim),
		dataDir:       opts.DataDir,
		invertedIndex: make(map[string]map[string]int),
		docLengths:    make(map[string]int),
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
		indexTexts:    make(map[string]string),
	}

	// Rebuild BM25 index from LSM data
	e.rebuildIndex()
	if err := e.SetEmbedder(e.embedder); err != nil {
		l.Close()
		return nil, err
	}

	// Periodically save vectors
	go e.periodicSave()

	log.Printf("[engine] opened: %d docs, %d vectors, %d terms in index",
		e.totalDocs, e.hnsw.Stats()["nodes"], len(e.invertedIndex))
	return e, nil
}

func (e *Engine) SetEmbedder(emb embed.Embedder) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	go func() {
		select {
		case <-e.stopCh:
			cancel()
		case <-ctx.Done():
		}
	}()
	return e.setEmbedder(ctx, emb)
}

func (e *Engine) setEmbedder(ctx context.Context, emb embed.Embedder) error {
	if emb == nil {
		return fmt.Errorf("embedder is nil")
	}
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	if e.closed {
		return fmt.Errorf("engine closed")
	}
	cache := e.vectorCache(embeddingIdentity(emb))
	fresh := vector.NewHNSW(16, 50)
	var dim int
	keys := make([]string, 0, len(e.indexTexts))
	for key := range e.indexTexts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return err
		}
		text := e.indexTexts[key]
		if strings.TrimSpace(text) == "" {
			continue
		}
		vec := cache[key].Vector
		if cache[key].Text != text || len(vec) == 0 {
			var err error
			if contextual, ok := emb.(interface {
				EmbedContext(context.Context, string) ([]float32, error)
			}); ok {
				vec, err = contextual.EmbedContext(ctx, text)
			} else {
				vec, err = emb.Embed(text)
			}
			if err != nil {
				return fmt.Errorf("rebuild vector %s: %w", key, err)
			}
		}
		if len(vec) == 0 || (dim != 0 && len(vec) != dim) {
			return fmt.Errorf("inconsistent embedding dimensions")
		}
		dim = len(vec)
		fresh.Insert(key, vec)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	e.mu.Lock()
	e.embedder, e.hnsw = emb, fresh
	e.mu.Unlock()
	return nil
}

// Embedder returns the current embedder so callers (e.g. Pipeline/Extractor)
// can use it for classification tasks.
func (e *Engine) Embedder() embed.Embedder {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.embedder
}

// Put stores a key-value pair and indexes the raw value text.
func (e *Engine) Put(key string, value []byte) error {
	return e.PutText(key, value, string(value))
}

// PutText stores a key-value pair but indexes only indexText for BM25 and
// vector search. Use this when value is a serialized record (e.g. JSON with
// embedding arrays) so index statistics are not polluted by non-content tokens.
func (e *Engine) PutText(key string, value []byte, indexText string) error {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	if e.closed {
		return fmt.Errorf("engine closed")
	}
	var vec []float32
	var embedErr error
	if strings.TrimSpace(indexText) != "" {
		vec, embedErr = e.embedder.Embed(indexText)
	}
	stored, err := json.Marshal(indexedRecord{Format: recordFormat, Value: value, Text: indexText})
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.lsm.Put([]byte(key), stored); err != nil {
		return err
	}

	// Update BM25 inverted index
	e.removeFromIndex(key)
	e.addToIndex(key, indexText)
	e.indexTexts[key] = indexText

	// Update vector index
	e.hnsw.Delete(key)
	if embedErr != nil {
		log.Printf("[engine] embed error; keyword retrieval available: %v", embedErr)
		return nil
	}
	e.hnsw.Insert(key, vec)

	return nil
}

// Get retrieves a value by key.
func (e *Engine) Get(key string) ([]byte, error) {
	v, ok := e.lsm.Get([]byte(key))
	if !ok {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	value, _ := decodeRecord(v)
	return value, nil
}

// Delete removes a key and its associated index entries.
func (e *Engine) Delete(key string) error {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	if e.closed {
		return fmt.Errorf("engine closed")
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.lsm.Delete([]byte(key)); err != nil {
		return err
	}
	e.removeFromIndex(key)
	delete(e.indexTexts, key)
	e.hnsw.Delete(key)
	return nil
}

// Embed generates a vector for the given text.
func (e *Engine) Embed(text string) ([]float32, error) {
	// Snapshot the embedder under RLock: reading e.embedder directly would
	// race with SetEmbedder swapping it concurrently.
	e.mu.RLock()
	emb := e.embedder
	e.mu.RUnlock()
	return emb.Embed(text)
}

// Search performs hybrid BM25 + vector search using RRF fusion.
func (e *Engine) Search(query string, topK int) ([]retrieval.ScoredDoc, error) {
	return e.SearchScoped(query, topK, "")
}

// SearchScoped performs hybrid search limited to keys with the given prefix.
// BM25 skips non-matching docs during scoring; HNSW results are filtered
// before RRF fusion. This prevents recall degradation when many sessions exist.
func (e *Engine) SearchScoped(query string, topK int, keyPrefix string) ([]retrieval.ScoredDoc, error) {
	return e.SearchScopedContext(context.Background(), query, topK, keyPrefix)
}

func (e *Engine) SearchScopedContext(ctx context.Context, query string, topK int, keyPrefix string) ([]retrieval.ScoredDoc, error) {
	if topK <= 0 {
		topK = 10
	}

	// Keep the embedder and graph as a matched pair during a concurrent model swap.
	e.mu.RLock()
	emb, graph := e.embedder, e.hnsw
	e.mu.RUnlock()
	queryVec, err := embedContext(ctx, emb, query)
	if err != nil {
		return nil, err
	}
	e.mu.RLock()
	defer e.mu.RUnlock()

	// BM25 with prefix filter during scoring
	bm25 := e.bm25SearchScoped(query, topK*3, keyPrefix)
	bm25Docs := make([]retrieval.ScoredDoc, len(bm25))
	for i, r := range bm25 {
		bm25Docs[i] = retrieval.ScoredDoc{Key: r.Key, Score: r.Score}
	}

	// Vector search with post-filter. Hits below minVectorScore (cosine
	// similarity ~0.15) are dropped: hash-embedding dimension collisions give
	// unrelated texts a small non-zero similarity that would otherwise leak
	// into RRF fusion as noise.
	vecResults := graph.SearchScoped(queryVec, topK*3, keyPrefix)
	vecDocs := make([]retrieval.ScoredDoc, 0, len(vecResults))
	for _, r := range vecResults {
		if _, live := e.indexTexts[r.Key]; !live {
			continue
		}
		if r.Score <= minVectorScore {
			continue
		}
		if keyPrefix == "" || strings.HasPrefix(r.Key, keyPrefix) {
			vecDocs = append(vecDocs, retrieval.ScoredDoc{Key: r.Key, Score: r.Score})
		}
	}

	fused := retrieval.RRF(bm25Docs, vecDocs, 60, topK)
	if keyPrefix != "" {
		log.Printf("[engine] search scoped: prefix=%s query=%q bm25=%d vec_raw=%d vec_filtered=%d fused=%d",
			keyPrefix, query, len(bm25Docs), len(vecResults), len(vecDocs), len(fused))
	}
	return fused, nil
}

// Stats returns engine statistics.
func (e *Engine) Stats() EngineStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	ls := e.lsm.Stats()
	vs := e.hnsw.Stats()
	return EngineStats{
		KeyCount:    int64(e.totalDocs),
		DiskBytes:   ls.DiskBytes,
		WalEntries:  ls.WalEntries,
		VectorCount: vs["nodes"].(int),
		SSTCount:    ls.SSTCount,
		IndexTerms:  len(e.invertedIndex),
	}
}

// Close gracefully shuts down the engine.
func (e *Engine) Close() error {
	e.closeOnce.Do(func() {
		close(e.stopCh)
		<-e.doneCh
		e.writeMu.Lock()
		defer e.writeMu.Unlock()
		e.closed = true
		e.saveVectors()
		e.lsm.Close()
	})
	return nil
}

// --- BM25 Inverted Index ---

const (
	k1Param = 1.2
	bParam  = 0.75

	// minVectorScore: vector hits must exceed this (score = 1/(1+dist)) to
	// enter RRF fusion. 0.54 ≈ cosine similarity 0.15.
	minVectorScore = 0.54
)

func (e *Engine) addToIndex(docKey, text string) {
	tokens := tokenize(text)
	e.docLengths[docKey] = len(tokens)
	for _, t := range tokens {
		if e.invertedIndex[t] == nil {
			e.invertedIndex[t] = make(map[string]int)
		}
		e.invertedIndex[t][docKey]++
	}
	e.totalDocs++
}

func (e *Engine) removeFromIndex(docKey string) {
	if _, exists := e.docLengths[docKey]; !exists {
		return
	}
	for term, docs := range e.invertedIndex {
		delete(docs, docKey)
		if len(docs) == 0 {
			delete(e.invertedIndex, term)
		}
	}
	delete(e.docLengths, docKey)
	if e.totalDocs > 0 {
		e.totalDocs--
	}
}

func (e *Engine) rebuildIndex() {
	e.invertedIndex = make(map[string]map[string]int)
	e.docLengths = make(map[string]int)
	e.totalDocs = 0
	e.lsm.ForEach(func(k, v []byte) bool {
		_, text := decodeRecord(v)
		e.indexTexts[string(k)] = text
		e.addToIndex(string(k), text)
		return true
	})
}

// bm25Search uses the pre-built inverted index for O(terms * avg_posting) search.
func (e *Engine) bm25Search(query string, topK int) []SearchResult {
	return e.bm25SearchScoped(query, topK, "")
}

// bm25SearchScoped scores only documents matching the key prefix.
func (e *Engine) bm25SearchScoped(query string, topK int, keyPrefix string) []SearchResult {
	terms := tokenize(query)
	if len(terms) == 0 || e.totalDocs == 0 {
		return nil
	}

	// Compute average document length
	var totalLen int
	for _, l := range e.docLengths {
		totalLen += l
	}
	avgDL := float64(totalLen) / float64(e.totalDocs)
	N := float64(e.totalDocs)

	// Compute IDF for query terms
	idf := make(map[string]float64)
	for _, t := range terms {
		if docs, ok := e.invertedIndex[t]; ok {
			df := float64(len(docs))
			idf[t] = math.Log(1 + (N-df+0.5)/(df+0.5))
		}
	}

	// Score candidate documents (filtered by prefix if specified)
	scores := make(map[string]float64)
	for _, t := range terms {
		idfT := idf[t]
		if idfT == 0 {
			continue
		}
		for doc, tf := range e.invertedIndex[t] {
			if keyPrefix != "" && !strings.HasPrefix(doc, keyPrefix) {
				continue
			}
			dl := float64(e.docLengths[doc])
			if dl == 0 {
				dl = 1
			}
			ft := float64(tf)
			score := idfT * (ft * (k1Param + 1)) / (ft + k1Param*(1-bParam+bParam*dl/avgDL))
			scores[doc] += score
		}
	}

	// Sort and return topK
	results := make([]SearchResult, 0, len(scores))
	for doc, score := range scores {
		results = append(results, SearchResult{Key: doc, Score: score})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if len(results) > topK {
		results = results[:topK]
	}
	return results
}

// --- Vector Persistence ---

func (e *Engine) loadVectors() {
	path := filepath.Join(e.dataDir, "vectors.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var nodes []vector.NodeData
	if err := json.Unmarshal(data, &nodes); err != nil {
		log.Printf("[engine] vector load error: %v", err)
		return
	}
	e.hnsw.ImportNodes(nodes)
	log.Printf("[engine] loaded %d vectors from disk", len(nodes))
}

func (e *Engine) saveVectors() {
	e.saveMu.Lock()
	defer e.saveMu.Unlock()
	e.mu.RLock()
	nodes := e.hnsw.ExportNodes()
	snapshot := vectorSnapshot{Model: embeddingIdentity(e.embedder), Entries: make(map[string]cachedVector, len(nodes))}
	for _, node := range nodes {
		snapshot.Entries[node.ID] = cachedVector{Text: e.indexTexts[node.ID], Vector: node.Vec}
	}
	e.mu.RUnlock()
	data, err := json.Marshal(snapshot)
	if err != nil {
		log.Printf("[engine] vector marshal error: %v", err)
		return
	}
	path := filepath.Join(e.dataDir, "vectors.json")
	if err := atomicWrite(path, data); err != nil {
		log.Printf("[engine] vector save error: %v", err)
		return
	}
	log.Printf("[engine] saved %d vectors to disk", len(nodes))
}

func (e *Engine) periodicSave() {
	defer close(e.doneCh)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			e.saveVectors()
		case <-e.stopCh:
			return
		}
	}
}

// --- Tokenization ---

func tokenize(text string) []string {
	return textindex.Tokens(text)
}
