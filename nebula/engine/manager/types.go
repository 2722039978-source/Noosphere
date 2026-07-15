// Package manager — Memory 类型定义与生命周期管理
package manager

import (
	"time"
)

// MemoryType 记忆类型枚举
type MemoryType uint8

const (
	WorkingMemory    MemoryType = 0 // 工作记忆：当前任务的临时状态，TTL 短，高频访问
	EpisodicMemory   MemoryType = 1 // 情景记忆：原始对话/事件记录，带时间上下文
	SemanticMemory   MemoryType = 2 // 语义记忆：整合后的知识/事实，持久存储
	ProceduralMemory MemoryType = 3 // 程序记忆：如何做的流程/技能模式
)

func (m MemoryType) String() string {
	switch m {
	case WorkingMemory:
		return "working"
	case EpisodicMemory:
		return "episodic"
	case SemanticMemory:
		return "semantic"
	case ProceduralMemory:
		return "procedural"
	default:
		return "unknown"
	}
}

// MemoryTypeFromString 从字符串解析记忆类型
func MemoryTypeFromString(s string) MemoryType {
	switch s {
	case "working":
		return WorkingMemory
	case "episodic":
		return EpisodicMemory
	case "semantic":
		return SemanticMemory
	case "procedural":
		return ProceduralMemory
	default:
		return EpisodicMemory
	}
}

// Memory 核心记忆结构
type Memory struct {
	ID        string     `json:"id" msgpack:"id"`
	SessionID string     `json:"session_id" msgpack:"session_id"`
	Type      MemoryType `json:"type" msgpack:"type"`
	Content   string     `json:"content" msgpack:"content"`

	// Embedding 向量（由引擎自动填充）
	Embedding []float64 `json:"embedding,omitempty" msgpack:"embedding,omitempty"`

	// 元数据
	Tags       []string          `json:"tags,omitempty" msgpack:"tags,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty" msgpack:"metadata,omitempty"`
	Importance float64           `json:"importance" msgpack:"importance"` // 重要性 [0, 1]

	// 生命周期
	CreatedAt time.Time `json:"created_at" msgpack:"created_at"`
	ExpiresAt time.Time `json:"expires_at,omitempty" msgpack:"expires_at,omitempty"` // 零值表示永不过期
	AccessCnt int64     `json:"access_cnt" msgpack:"access_cnt"`                     // 访问计数

	// 语义记忆特有字段
	AbstractionLevel int      `json:"abstraction_level,omitempty" msgpack:"abstraction_level,omitempty"` // 抽象层级
	SourceIDs        []string `json:"source_ids,omitempty" msgpack:"source_ids,omitempty"`               // 来源情景记忆 ID 列表

	// 程序记忆特有字段
	Trigger      string   `json:"trigger,omitempty" msgpack:"trigger,omitempty"`             // 触发条件
	Steps        []string `json:"steps,omitempty" msgpack:"steps,omitempty"`                 // 步骤序列
	SuccessRate  float64  `json:"success_rate,omitempty" msgpack:"success_rate,omitempty"`   // 成功率
	InvokeCount  int64    `json:"invoke_count,omitempty" msgpack:"invoke_count,omitempty"`   // 调用次数
}

// NewMemory 创建一个新的记忆对象
func NewMemory(sessionID string, mtype MemoryType, content string) *Memory {
	return &Memory{
		ID:         NewID(),
		SessionID:  sessionID,
		Type:       mtype,
		Content:    content,
		Importance: 0.5,
		CreatedAt:  time.Now().UTC(),
		AccessCnt:  0,
	}
}

// NewEpisodicMemory 创建情景记忆
func NewEpisodicMemory(sessionID, content string, importance float64, tags []string) *Memory {
	m := NewMemory(sessionID, EpisodicMemory, content)
	m.Importance = clampImportance(importance)
	m.Tags = tags
	return m
}

// NewSemanticMemory 创建语义记忆
func NewSemanticMemory(sessionID, content string, sourceIDs []string) *Memory {
	m := NewMemory(sessionID, SemanticMemory, content)
	m.SourceIDs = sourceIDs
	m.AbstractionLevel = 1
	m.Importance = 0.8
	return m
}

// NewWorkingMemory 创建工作记忆
func NewWorkingMemory(sessionID, key, value string, ttl time.Duration) *Memory {
	m := NewMemory(sessionID, WorkingMemory, value)
	m.Metadata = map[string]string{"key": key}
	m.ExpiresAt = time.Now().UTC().Add(ttl)
	return m
}

// NewProceduralMemory 创建程序记忆
func NewProceduralMemory(sessionID, trigger string, steps []string) *Memory {
	m := NewMemory(sessionID, ProceduralMemory, "")
	m.Trigger = trigger
	m.Steps = steps
	m.SuccessRate = 1.0
	return m
}

// IsExpired 检查记忆是否过期
func (m *Memory) IsExpired() bool {
	if m.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().UTC().After(m.ExpiresAt)
}

// TTL 返回剩余生存时间（过期返回 0）
func (m *Memory) TTL() time.Duration {
	if m.ExpiresAt.IsZero() {
		return 0
	}
	d := time.Until(m.ExpiresAt)
	if d < 0 {
		return 0
	}
	return d
}

// Touch 更新访问计数
func (m *Memory) Touch() {
	m.AccessCnt++
}

// Key 返回该记忆在 KV Store 中的键前缀
func (m *Memory) Key() string {
	return keyPrefix(m.SessionID, m.Type, m.ID)
}

// ─── 辅助函数 ───

func clampImportance(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// keyPrefix 生成 KV Store 的 key 前缀
// 格式: nebula:{session_id}:{type}:{id}
func keyPrefix(sessionID string, mtype MemoryType, id string) string {
	// 预分配容量，避免字符串拼接的多次内存分配
	buf := make([]byte, 0, 7+len(sessionID)+1+10+1+len(id))
	buf = append(buf, "nebula:"...)
	buf = append(buf, sessionID...)
	buf = append(buf, ':')
	buf = append(buf, mtype.String()...)
	buf = append(buf, ':')
	buf = append(buf, id...)
	return string(buf)
}

// ─── SearchOptions 检索参数 ───

// SearchStrategy 检索策略
type SearchStrategy uint8

const (
	VectorSearch  SearchStrategy = 0 // 纯向量检索
	KeywordSearch SearchStrategy = 1 // 纯关键词检索
	HybridSearch  SearchStrategy = 2 // 混合检索（RRF 融合排序）
	TemporalSearch SearchStrategy = 3 // 时间衰减检索
)

// SearchOptions 检索参数
type SearchOptions struct {
	Query       string           `json:"query"`
	Strategy    SearchStrategy   `json:"strategy"`
	TopK        int              `json:"top_k"`
	MemoryTypes []MemoryType     `json:"memory_types,omitempty"` // 为空则检索所有类型
	Threshold   float64          `json:"threshold"`              // 最低分数阈值
	TimeRange   *TimeRange       `json:"time_range,omitempty"`
	Tags        []string         `json:"tags,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// TimeRange 时间范围
type TimeRange struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// SearchResult 检索结果
type SearchResult struct {
	Memory *Memory `json:"memory"`
	Score  float64 `json:"score"`
	Reason string  `json:"reason,omitempty"` // 为什么返回这条结果
}

// ─── 统计信息 ───

// Stats 引擎统计信息
type Stats struct {
	TotalMemories   int64   `json:"total_memories"`
	WorkingCount    int64   `json:"working_count"`
	EpisodicCount   int64   `json:"episodic_count"`
	SemanticCount   int64   `json:"semantic_count"`
	ProceduralCount int64   `json:"procedural_count"`
	VectorCount     int64   `json:"vector_count"`
	CacheHitRate    float64 `json:"cache_hit_rate"`
	CacheSize       int     `json:"cache_size"`
	CacheMaxSize    int     `json:"cache_max_size"`
	DiskUsed        int64   `json:"disk_used_bytes"`
	Uptime          string  `json:"uptime"`
}
