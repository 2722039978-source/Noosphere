// Package engine — Nebula Agent 核心引擎
//
// 嵌入式使用（类似 SQLite）:
//
//	engine, _ := nebula.Open(nebula.DefaultOptions("./data"))
//	defer engine.Close()
//	session := engine.Session("my-agent")
//	session.Remember(...)
//	results, _ := session.Reminisce(...)
package engine

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/nebula-agent/nebula/engine/cache"
	"github.com/nebula-agent/nebula/engine/index"
	"github.com/nebula-agent/nebula/engine/manager"
	"github.com/nebula-agent/nebula/engine/retrieval"
	"github.com/nebula-agent/nebula/engine/storage/kv"
	"github.com/nebula-agent/nebula/engine/storage/lsm"
	"github.com/nebula-agent/nebula/engine/storage/vector"
)

// Engine Nebula 核心引擎
type Engine struct {
	mu      sync.RWMutex
	opts    *Options
	started bool

	// 存储层
	lsmTree *lsm.LSMTree
	kvStore *kv.Store
	vecIdx  *vector.HNSW

	// 索引层
	invIndex *index.InvertedIndex

	// 嵌入层
	embedder retrieval.Embedder

	// 管理层
	memAPI *manager.MemoryAPI

	// 缓存层
	lruCache *cache.LRU

	// 会话
	sessions map[string]*Session

	// 后台任务
	stopCh chan struct{}
	startTime time.Time
}

// Open 打开或创建引擎
func Open(opts *Options) (*Engine, error) {
	if opts == nil {
		opts = DefaultOptions("nebula-data")
	}

	e := &Engine{
		opts:     opts,
		sessions: make(map[string]*Session),
		stopCh:   make(chan struct{}),
	}

	// 1. 初始化 LSM Tree（持久化存储）
	if opts.DataDir != ":memory:" {
		lsmOpts := &lsm.LSMOptions{
			Dir:             opts.DataDir + "/lsm",
			MemTableSize:    int64(opts.MemTableSize),
			SSTableSize:     int64(opts.SSTableSize),
			BloomFilterBits: opts.BloomFilterBits,
			MaxLevels:       opts.MaxLevels,
			WALEnabled:      opts.WALEnabled,
			WALSyncOnWrite:  opts.WALSyncOnWrite,
		}
		tree, err := lsm.NewLSMTree(lsmOpts)
		if err != nil {
			return nil, fmt.Errorf("engine open lsm: %w", err)
		}
		e.lsmTree = tree
	}

	// 2. 初始化 In-Memory KV Store
	e.kvStore = kv.New(opts.KVShardCount)

	// 3. 初始化向量索引
	e.vecIdx = vector.NewHNSW(vector.HNSWConfig{
		Dimension:   opts.VectorDimension,
		M:           opts.HNSWM,
		EfConstruct: opts.HNSWEfConstruct,
		EfSearch:    opts.HNSWEfSearch,
		MaxElements: opts.HNSWMaxElements,
	})

	// 4. 初始化倒排索引
	e.invIndex = index.NewInvertedIndex()

	// 5. 初始化 Embedder
	switch opts.EmbedderType {
	case "mock":
		e.embedder = retrieval.NewMockEmbedder(opts.VectorDimension)
	case "http":
		e.embedder = retrieval.NewHTTPEmbedder(retrieval.HTTPEmbedderConfig{
			APIURL:    opts.EmbedderAPIURL,
			Model:     opts.EmbedderModel,
			Dimension: opts.VectorDimension,
		})
	default:
		e.embedder = retrieval.NewMockEmbedder(opts.VectorDimension)
	}

	// 6. 初始化 LRU Cache
	e.lruCache = cache.NewLRU(opts.CacheMaxSize)

	// 7. 组装 MemoryAPI
	e.memAPI = manager.NewMemoryAPI(
		e,  // MemoryStore 接口（自身实现）
		e,  // VectorIndex 接口
		e,  // IndexStore 接口
		e,  // Embedder 接口
	)

	e.started = true
	e.startTime = time.Now()

	// 8. 启动后台任务
	go e.ttlSweeper()
	if opts.ConsolidationInterval > 0 {
		go e.consolidationLoop()
	}

	return e, nil
}

// Close 关闭引擎
func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.started = false
	close(e.stopCh)

	if e.lsmTree != nil {
		if err := e.lsmTree.Close(); err != nil {
			return err
		}
	}
	e.kvStore.Close()

	return nil
}

// Session 获取或创建会话
func (e *Engine) Session(id string) *Session {
	e.mu.Lock()
	defer e.mu.Unlock()

	if sess, ok := e.sessions[id]; ok {
		return sess
	}

	sess := &Session{
		id:     id,
		engine: e,
	}
	e.sessions[id] = sess
	return sess
}

// Stats 引擎统计
func (e *Engine) Stats() *manager.Stats {
	stats := &manager.Stats{
		CacheMaxSize: e.opts.CacheMaxSize,
		CacheSize:    e.lruCache.Size(),
		CacheHitRate: e.lruCache.HitRate(),
		VectorCount:  int64(e.vecIdx.Size()),
		Uptime:       time.Since(e.startTime).String(),
	}

	// 遍历所有会话
	for _, sess := range e.sessions {
		ss := e.memAPI.SessionStats(sess.id)
		stats.TotalMemories += int64(ss.TotalKeys)
		stats.WorkingCount += int64(ss.WorkingCount)
		stats.EpisodicCount += int64(ss.EpisodicCount)
		stats.SemanticCount += int64(ss.SemanticCount)
		stats.ProceduralCount += int64(ss.ProceduralCount)
	}

	return stats
}

// ─── MemoryStore 接口实现 ───

func (e *Engine) Put(key string, memory *manager.Memory) error {
	data, err := json.Marshal(memory)
	if err != nil {
		return err
	}

	// 写 In-Memory KV（热数据）
	e.kvStore.Set(key, data, memory.TTL())

	// 写 LSM Tree（持久化）
	if e.lsmTree != nil {
		if err := e.lsmTree.Put([]byte(key), data); err != nil {
			return err
		}
	}

	// 更新 Cache
	e.lruCache.Put(key, memory)

	return nil
}

func (e *Engine) Get(key string) (*manager.Memory, error) {
	// 1. 查 Cache
	if val, ok := e.lruCache.Get(key); ok {
		if m, ok := val.(*manager.Memory); ok {
			return m, nil
		}
	}

	// 2. 查 In-Memory KV
	data, ok := e.kvStore.Get(key)
	if !ok && e.lsmTree != nil {
		// 3. 查 LSM Tree
		lsmData, found, err := e.lsmTree.Get([]byte(key))
		if err != nil {
			return nil, err
		}
		if found {
			data = lsmData
		}
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("key not found: %s", key)
	}

	m, err := manager.DeserializeMemory(data)
	if err != nil {
		return nil, err
	}

	// 回填 Cache
	e.lruCache.Put(key, m)

	return m, nil
}

func (e *Engine) Del(key string) error {
	e.kvStore.Del(key)
	e.lruCache.Del(key)
	if e.lsmTree != nil {
		return e.lsmTree.Delete([]byte(key))
	}
	return nil
}

func (e *Engine) KeysByPrefix(prefix string) ([]string, error) {
	return e.kvStore.KeysByPrefix(prefix), nil
}

// ─── Embedder 接口实现（委托）───

func (e *Engine) Embed(texts []string) ([][]float32, error) {
	return e.embedder.Embed(texts)
}

func (e *Engine) Dimension() int {
	return e.embedder.Dimension()
}

// ─── VectorIndex 接口实现（委托给 HNSW）───

func (e *Engine) Add(id int, vector []float32, key string) error {
	return e.vecIdx.Add(id, vector, key)
}

// VectorSearchHit 来自 vector 包的搜索命中
func (e *Engine) Search(query []float32, k int) []manager.VectorSearchHit {
	hits := e.vecIdx.Search(query, k)
	results := make([]manager.VectorSearchHit, len(hits))
	for i, h := range hits {
		results[i] = manager.VectorSearchHit{
			ID:       h.ID,
			Distance: h.Distance,
			Score:    h.Score,
			Key:      h.Metadata,
		}
	}
	return results
}

func (e *Engine) Delete(id int) {
	e.vecIdx.Delete(id)
}

// ─── IndexStore 接口实现（委托给倒排索引）───

func (e *Engine) Index(docID string, text string) {
	e.invIndex.Index(docID, text)
}

func (e *Engine) Remove(docID string) {
	e.invIndex.Remove(docID)
}

// KeywordSearch (for IndexStore interface)
func (e *Engine) KeywordSearch(query string, topK int) []manager.KeywordSearchResult {
	hits := e.invIndex.Search(query, topK)
	results := make([]manager.KeywordSearchResult, len(hits))
	for i, h := range hits {
		results[i] = manager.KeywordSearchResult{
			DocID: h.DocID,
			Score: h.Score,
		}
	}
	return results
}

// ─── 后台任务 ───

func (e *Engine) ttlSweeper() {
	ticker := time.NewTicker(e.opts.TTLCheckFreq)
	defer ticker.Stop()
	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			// KV Store 内部有自己的 TTL Sweeper
		}
	}
}

func (e *Engine) consolidationLoop() {
	ticker := time.NewTicker(e.opts.ConsolidationInterval)
	defer ticker.Stop()
	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			// TODO: 实现记忆整合
			// e.consolidate()
		}
	}
}

// ─── Session ───

// Session 会话——Agent 的操作入口
type Session struct {
	id     string
	engine *Engine
}

// ID 返回会话 ID
func (s *Session) ID() string { return s.id }

// Remember 存储记忆
func (s *Session) Remember(m *manager.Memory) error {
	m.SessionID = s.id
	return s.engine.memAPI.Remember(m)
}

// Recall 召回记忆
func (s *Session) Recall(id string) (*manager.Memory, error) {
	return s.engine.memAPI.Recall(id, s.id)
}

// Forget 遗忘记忆
func (s *Session) Forget(id string) error {
	return s.engine.memAPI.Forget(id, s.id)
}

// Reminisce 语义检索
func (s *Session) Reminisce(opts *manager.SearchOptions) ([]*manager.SearchResult, error) {
	return s.engine.memAPI.Reminisce(s.id, opts)
}

// Stats 会话统计
func (s *Session) Stats() *manager.SessionStats {
	return s.engine.memAPI.SessionStats(s.id)
}

// Clear 清除所有记忆
func (s *Session) Clear() error {
	prefix := "nebula:" + s.id + ":"
	keys := s.engine.kvStore.KeysByPrefix(prefix)
	for _, k := range keys {
		s.engine.Del(k)
	}
	return nil
}

// ─── 便捷函数 ───

// RememberEpisodic 存储情景记忆（快捷方式）
func (s *Session) RememberEpisodic(content string, importance float64, tags []string) error {
	m := manager.NewEpisodicMemory(s.id, content, importance, tags)
	return s.Remember(m)
}

// RememberSemantic 存储语义记忆（快捷方式）
func (s *Session) RememberSemantic(content string, sourceIDs []string) error {
	m := manager.NewSemanticMemory(s.id, content, sourceIDs)
	return s.Remember(m)
}

// RememberWorking 存储工作记忆（快捷方式）
func (s *Session) RememberWorking(key, value string, ttl time.Duration) error {
	m := manager.NewWorkingMemory(s.id, key, value, ttl)
	return s.Remember(m)
}
