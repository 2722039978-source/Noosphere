package manager

import (
	"time"
)

// Lifecycle 记忆生命周期管理
type Lifecycle struct {
	// TTL 配置
	DefaultWorkingTTL    time.Duration
	MaxEpisodicAge       time.Duration // 情景记忆最大保留时间
	ConsolidationTrigger int           // 触发整合的计数阈值
}

// DefaultLifecycle 默认生命周期配置
func DefaultLifecycle() *Lifecycle {
	return &Lifecycle{
		DefaultWorkingTTL:    5 * time.Minute,
		MaxEpisodicAge:       30 * 24 * time.Hour, // 30 天
		ConsolidationTrigger: 1000,
	}
}

// ShouldConsolidate 判断是否应该整合
func (l *Lifecycle) ShouldConsolidate(episodicCount int) bool {
	return episodicCount >= l.ConsolidationTrigger
}

// ─── 记忆整合 (Consolidation) ───

// Consolidator 记忆整合器
// 将多条旧的情景记忆总结为一条语义记忆
type Consolidator struct {
	store    MemoryStore
	embedder Embedder
	lifecycle *Lifecycle

	// Summarizer 是可插拔的总结函数
	// 默认使用简单的模板拼接
	Summarizer func(episodes []*Memory) string
}

// NewConsolidator 创建整合器
func NewConsolidator(store MemoryStore, embedder Embedder, lc *Lifecycle) *Consolidator {
	if lc == nil {
		lc = DefaultLifecycle()
	}
	return &Consolidator{
		store:     store,
		embedder:  embedder,
		lifecycle: lc,
		Summarizer: defaultSummarizer,
	}
}

// Consolidate 对会话执行记忆整合
// 返回新创建的语义记忆
func (c *Consolidator) Consolidate(sessionID string, episodes []*Memory) (*Memory, error) {
	if len(episodes) == 0 {
		return nil, nil
	}

	// 1. 提取所有来源 ID
	sourceIDs := make([]string, len(episodes))
	for i, ep := range episodes {
		sourceIDs[i] = ep.ID
	}

	// 2. 总结内容
	summary := c.Summarizer(episodes)

	// 3. 创建语义记忆
	semantic := NewSemanticMemory(sessionID, summary, sourceIDs)
	semantic.AbstractionLevel = 1
	semantic.Importance = 0.8

	// 标记来源记忆为已整合
	for _, ep := range episodes {
		// 降低重要性表示已被总结
		ep.Importance *= 0.3
		// 更新存储
		_ = c.store.Put(ep.Key(), ep)
	}

	return semantic, nil
}

// SelectCandidates 选择需要整合的候选记忆
func (c *Consolidator) SelectCandidates(sessionID string, maxAge time.Duration, limit int) ([]*Memory, error) {
	// 扫描对应会话的 episodic 记忆
	prefix := "nebula:" + sessionID + ":episodic:"
	keys, err := c.store.KeysByPrefix(prefix)
	if err != nil {
		return nil, err
	}

	var candidates []*Memory
	cutoff := time.Now().UTC().Add(-maxAge)

	for _, k := range keys {
		m, err := c.store.Get(k)
		if err != nil || m == nil {
			continue
		}

		// 只选择足够旧的、低访问的记忆
		if m.CreatedAt.Before(cutoff) && m.AccessCnt < 2 {
			candidates = append(candidates, m)
		}

		if len(candidates) >= limit {
			break
		}
	}

	return candidates, nil
}

// ─── 默认总结器 ───

func defaultSummarizer(episodes []*Memory) string {
	if len(episodes) == 0 {
		return ""
	}

	// 简单拼接所有内容（生产环境可使用 LLM 总结）
	result := "Consolidated memories:\n"
	for i, ep := range episodes {
		if i >= 10 {
			result += "... (and more)\n"
			break
		}
		result += "- " + truncate(ep.Content, 200) + "\n"
	}
	return result
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
