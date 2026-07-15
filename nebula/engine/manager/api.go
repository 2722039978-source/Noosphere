// Package manager — Memory API：记忆的 CRUD 和生命周期管理
package manager

import (
	"encoding/json"
	"fmt"
	"time"
)

// MemoryStore 记忆存储接口（底层可以是 LSM Tree + KV + Vector 的组合）
type MemoryStore interface {
	Put(key string, memory *Memory) error
	Get(key string) (*Memory, error)
	Del(key string) error
	KeysByPrefix(prefix string) ([]string, error)
}

// VectorIndex 向量索引接口
type VectorIndex interface {
	Add(id int, vector []float32, key string) error
	Search(query []float32, k int) []VectorSearchHit
	Delete(id int)
}

// VectorSearchHit 向量搜索命中
type VectorSearchHit struct {
	ID       int
	Distance float64
	Score    float64
	Key      string // 对应 KV Store 的 key
}

// IndexStore 倒排索引接口
type IndexStore interface {
	Index(docID string, text string)
	Remove(docID string)
	KeywordSearch(query string, topK int) []KeywordSearchResult
}

// KeywordSearchResult 关键词搜索结果
type KeywordSearchResult struct {
	DocID string
	Score float64
}

// Embedder 嵌入接口
type Embedder interface {
	Embed(texts []string) ([][]float32, error)
	Dimension() int
}

// MemoryAPI 面向 Agent 的记忆操作接口
type MemoryAPI struct {
	store   MemoryStore
	vector  VectorIndex
	index   IndexStore
	embedder Embedder

	// 统计
	totalMemories int64
}

// NewMemoryAPI 创建 Memory API
func NewMemoryAPI(store MemoryStore, vector VectorIndex, index IndexStore, embedder Embedder) *MemoryAPI {
	return &MemoryAPI{
		store:    store,
		vector:   vector,
		index:    index,
		embedder: embedder,
	}
}

// Remember 存储一条记忆
func (api *MemoryAPI) Remember(m *Memory) error {
	// 1. 生成 Embedding（如果不是 WorkingMemory）
	if m.Type != WorkingMemory && m.Embedding == nil && m.Content != "" {
		vecs, err := api.embedder.Embed([]string{m.Content})
		if err != nil {
			return fmt.Errorf("remember embed: %w", err)
		}
		if len(vecs) > 0 {
			m.Embedding = make([]float64, len(vecs[0]))
			for i, v := range vecs[0] {
				m.Embedding[i] = float64(v)
			}
		}
	}

	// 2. 持久化到 KV Store
	key := m.Key()
	if err := api.store.Put(key, m); err != nil {
		return fmt.Errorf("remember store: %w", err)
	}

	// 3. 加入向量索引（如果有 embedding）
	if m.Embedding != nil && len(m.Embedding) > 0 {
		vec32 := make([]float32, len(m.Embedding))
		for i, v := range m.Embedding {
			vec32[i] = float32(v)
		}
		// 使用简单的 hash ID 映射
		vectorID := fnvHashID(m.ID)
		if err := api.vector.Add(vectorID, vec32, key); err != nil {
			// 向量索引失败不阻塞存储
			// log.Printf("vector index add failed: %v", err)
		}
	}

	// 4. 加入倒排索引（关键词检索）
	if m.Content != "" {
		api.index.Index(m.ID, m.Content)
	}

	api.totalMemories++
	return nil
}

// Recall 根据 ID 召回记忆
func (api *MemoryAPI) Recall(id string, sessionID string) (*Memory, error) {
	// 尝试所有类型
	for _, mtype := range []MemoryType{WorkingMemory, EpisodicMemory, SemanticMemory, ProceduralMemory} {
		key := keyPrefix(sessionID, mtype, id)
		m, err := api.store.Get(key)
		if err != nil {
			continue
		}
		if m != nil {
			m.Touch()
			return m, nil
		}
	}
	return nil, fmt.Errorf("memory not found: %s", id)
}

// Forget 遗忘一条记忆
func (api *MemoryAPI) Forget(id string, sessionID string) error {
	for _, mtype := range []MemoryType{WorkingMemory, EpisodicMemory, SemanticMemory, ProceduralMemory} {
		key := keyPrefix(sessionID, mtype, id)
		if err := api.store.Del(key); err != nil {
			continue
		}
		api.index.Remove(id)
		return nil
	}
	return fmt.Errorf("memory not found: %s", id)
}

// Reminisce 语义检索（回忆相关记忆）
func (api *MemoryAPI) Reminisce(sessionID string, opts *SearchOptions) ([]*SearchResult, error) {
	if opts.TopK <= 0 {
		opts.TopK = 10
	}

	// 1. Embedding 查询
	vecs, err := api.embedder.Embed([]string{opts.Query})
	if err != nil {
		return nil, fmt.Errorf("reminisce embed: %w", err)
	}

	// 2. 向量搜索
	vecHits := api.vector.Search(vecs[0], opts.TopK*3)

	// 3. 关键词搜索
	kwHits := api.index.KeywordSearch(opts.Query, opts.TopK*3)

	// 4. RRF 融合
	rrfScores := make(map[string]float64)
	const rrfK = 60.0

	for rank, hit := range vecHits {
		rrfScores[hit.Key] += 1.0 / (rrfK + float64(rank+1))
	}
	for rank, hit := range kwHits {
		// 从 docID 构建 key
		for _, mtype := range []MemoryType{EpisodicMemory, SemanticMemory, ProceduralMemory} {
			key := keyPrefix(sessionID, mtype, hit.DocID)
			rrfScores[key] += 1.0 / (rrfK + float64(rank+1))
		}
	}

	// 5. 排序 + 加载
	type scoredKey struct {
		key   string
		score float64
	}
	var sorted []scoredKey
	for k, s := range rrfScores {
		sorted = append(sorted, scoredKey{key: k, score: s})
	}

	// 简单排序
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].score > sorted[i].score {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	// 取 topK + 从 KV Store 加载完整 Memory
	var results []*SearchResult
	for _, s := range sorted {
		if len(results) >= opts.TopK {
			break
		}
		if s.score < opts.Threshold {
			continue
		}

		m, err := api.store.Get(s.key)
		if err != nil || m == nil {
			continue
		}

		// 类型过滤
		if len(opts.MemoryTypes) > 0 {
			found := false
			for _, mt := range opts.MemoryTypes {
				if m.Type == mt {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// 时间过滤
		if opts.TimeRange != nil {
			if m.CreatedAt.Before(opts.TimeRange.From) || m.CreatedAt.After(opts.TimeRange.To) {
				continue
			}
		}

		// 标签过滤
		if len(opts.Tags) > 0 {
			tagMatch := false
			for _, qt := range opts.Tags {
				for _, mt := range m.Tags {
					if qt == mt {
						tagMatch = true
						break
					}
				}
				if tagMatch {
					break
				}
			}
			if !tagMatch {
				continue
			}
		}

		m.Touch()
		results = append(results, &SearchResult{
			Memory: m,
			Score:  s.score,
			Reason: "hybrid rrf fusion",
		})
	}

	return results, nil
}

// SessionStats 获取会话统计
func (api *MemoryAPI) SessionStats(sessionID string) *SessionStats {
	prefix := "nebula:" + sessionID + ":"
	keys, _ := api.store.KeysByPrefix(prefix)

	stats := &SessionStats{
		TotalKeys: len(keys),
	}

	for _, k := range keys {
		m, err := api.store.Get(k)
		if err != nil || m == nil {
			continue
		}
		switch m.Type {
		case WorkingMemory:
			stats.WorkingCount++
		case EpisodicMemory:
			stats.EpisodicCount++
		case SemanticMemory:
			stats.SemanticCount++
		case ProceduralMemory:
			stats.ProceduralCount++
		}
	}

	return stats
}

// SessionStats 会话统计
type SessionStats struct {
	TotalKeys      int
	WorkingCount   int
	EpisodicCount  int
	SemanticCount  int
	ProceduralCount int
}

// ─── 辅助 ───

func fnvHashID(id string) int {
	h := uint32(2166136261)
	for i := 0; i < len(id); i++ {
		h ^= uint32(id[i])
		h *= 16777619
	}
	return int(h & 0x7FFFFFFF) // 保持正数
}

// ─── JSON 序列化辅助（用于 KV Store）───

// SerializeMemory 序列化 Memory 为 JSON
func SerializeMemory(m *Memory) ([]byte, error) {
	return json.Marshal(m)
}

// DeserializeMemory 从 JSON 反序列化 Memory
func DeserializeMemory(data []byte) (*Memory, error) {
	var m Memory
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// Now 便于测试的时间获取
var Now = time.Now
