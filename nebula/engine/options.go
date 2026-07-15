// Package engine — 可嵌入的 Nebula 核心引擎
package engine

import (
	"time"
)

// Options 引擎配置（函数式选项模式）
type Options struct {
	// 数据路径：":memory:" 表示纯内存模式
	DataDir string

	// LSM Tree 配置
	MemTableSize    int           // MemTable 最大字节数（默认 64MB）
	SSTableSize     int           // SSTable 目标字节数（默认 32MB）
	BloomFilterBits int           // Bloom Filter 每个 key 的位数（默认 10）
	MaxLevels       int           // 最大层级数（默认 7）
	WALEnabled      bool          // 是否启用 WAL（默认 true）
	WALSyncOnWrite  bool          // 每次写入是否 fsync WAL（默认 false，批量提交）

	// KV Store 配置
	KVShardCount int // 分片数，减少锁竞争（默认 32）
	TTLCheckFreq time.Duration // TTL 过期检查频率（默认 1 秒）

	// 向量索引配置
	VectorDimension int // Embedding 向量维度（默认 1536）
	HNSWM           int // HNSW 每层连接数（默认 16）
	HNSWEfConstruct int // HNSW 构建时的搜索宽度（默认 200）
	HNSWEfSearch    int // HNSW 查询时的搜索宽度（默认 100）
	HNSWMaxElements int // HNSW 最大元素数（默认 1000000）

	// Cache 配置
	CacheMaxSize int // LRU Cache 最大条目数（默认 10000）

	// 搜索配置
	DefaultTopK int     // 默认返回结果数（默认 10）
	MinScore    float64 // 最低相关性分数阈值（默认 0.0）

	// 整合配置
	ConsolidationInterval time.Duration // 记忆整合间隔（默认 5 分钟）
	MaxEpisodicBeforeConsolidation int  // 触发整合的 episodic 记忆数阈值

	// Embedding 配置
	EmbedderType   string // "http" | "onnx" | "mock"
	EmbedderAPIURL string // HTTP Embedding API 地址
	EmbedderModel  string // Embedding 模型名
}

// DefaultOptions 返回默认配置
func DefaultOptions(dataDir string) *Options {
	return &Options{
		DataDir:                      dataDir,
		MemTableSize:                 64 << 20, // 64 MB
		SSTableSize:                  32 << 20, // 32 MB
		BloomFilterBits:              10,
		MaxLevels:                    7,
		WALEnabled:                   true,
		WALSyncOnWrite:               false,
		KVShardCount:                 32,
		TTLCheckFreq:                 time.Second,
		VectorDimension:              1536,
		HNSWM:                        16,
		HNSWEfConstruct:              200,
		HNSWEfSearch:                 100,
		HNSWMaxElements:              1_000_000,
		CacheMaxSize:                 10000,
		DefaultTopK:                  10,
		MinScore:                     0.0,
		ConsolidationInterval:        5 * time.Minute,
		MaxEpisodicBeforeConsolidation: 10000,
		EmbedderType:                 "http",
		EmbedderAPIURL:               "http://localhost:11434/api/embeddings",
		EmbedderModel:                "nomic-embed-text",
	}
}

// MemoryOptions 纯内存模式（数据不持久化）
func MemoryOptions() *Options {
	opts := DefaultOptions(":memory:")
	opts.WALEnabled = false
	return opts
}
