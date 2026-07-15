// Package kv — 分片锁 In-Memory KV Store
//
// 设计目标:
//   - O(1) 平均读写延迟（HashMap）
//   - 分片锁减少高并发下的竞争
//   - 内置 TTL 支持（时间轮 + 堆）
//   - 可选持久化（通过 LSM Tree）
//
// 使用场景:
//   - 工作记忆（高频访问、短 TTL）
//   - 会话状态缓存
//   - 热点数据快速查询
package kv

import (
	"container/heap"
	"sync"
	"time"
)

// Store 分片内存 KV Store
type Store struct {
	shards    []*shard
	shardMask uint32
	ttlHeap   *ttlHeap
	ttlMu     sync.Mutex
	stopCh    chan struct{}
}

// shard 单个分片（内部使用 sync.RWMutex）
type shard struct {
	mu   sync.RWMutex
	data map[string]*item
}

// item 存储项
type item struct {
	value     []byte
	expiresAt time.Time // 零值表示永不过期
}

// New 创建 KV Store
func New(shardCount int) *Store {
	if shardCount < 1 {
		shardCount = 32
	}
	// 分片数向上取 2 的幂
	shardCount = nextPowerOfTwo(shardCount)

	shards := make([]*shard, shardCount)
	for i := range shards {
		shards[i] = &shard{
			data: make(map[string]*item),
		}
	}

	s := &Store{
		shards:    shards,
		shardMask: uint32(shardCount - 1),
		ttlHeap:   newTTLHeap(),
		stopCh:    make(chan struct{}),
	}

	go s.ttlSweeper()

	return s
}

// Set 设置键值对（可选 TTL）
func (s *Store) Set(key string, value []byte, ttl time.Duration) {
	sh := s.getShard(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()

	it := &item{value: value}
	if ttl > 0 {
		it.expiresAt = time.Now().UTC().Add(ttl)
		s.pushTTL(key, it.expiresAt)
	} else if existing, ok := sh.data[key]; ok && !existing.expiresAt.IsZero() {
		// 之前有 TTL，现在覆盖为无 TTL — 需要从堆中移除
		// 简化处理：标记为过期，由 sweeper 清理
	}

	sh.data[key] = it
}

// Get 获取键值
func (s *Store) Get(key string) ([]byte, bool) {
	sh := s.getShard(key)
	sh.mu.RLock()
	defer sh.mu.RUnlock()

	it, ok := sh.data[key]
	if !ok {
		return nil, false
	}

	// 检查是否过期
	if !it.expiresAt.IsZero() && time.Now().UTC().After(it.expiresAt) {
		return nil, false
	}

	return it.value, true
}

// Del 删除键
func (s *Store) Del(key string) bool {
	sh := s.getShard(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()

	_, ok := sh.data[key]
	delete(sh.data, key)
	return ok
}

// Exists 检查键是否存在
func (s *Store) Exists(key string) bool {
	_, ok := s.Get(key)
	return ok
}

// KeysByPrefix 按前缀搜索键
func (s *Store) KeysByPrefix(prefix string) []string {
	var keys []string
	for _, sh := range s.shards {
		sh.mu.RLock()
		for k := range sh.data {
			if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
				keys = append(keys, k)
			}
		}
		sh.mu.RUnlock()
	}
	return keys
}

// DeleteByPrefix 按前缀批量删除
func (s *Store) DeleteByPrefix(prefix string) int {
	count := 0
	for _, sh := range s.shards {
		sh.mu.Lock()
		for k := range sh.data {
			if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
				delete(sh.data, k)
				count++
			}
		}
		sh.mu.Unlock()
	}
	return count
}

// Size 返回 key 总数
func (s *Store) Size() int {
	count := 0
	for _, sh := range s.shards {
		sh.mu.RLock()
		count += len(sh.data)
		sh.mu.RUnlock()
	}
	return count
}

// Close 停止后台任务
func (s *Store) Close() {
	close(s.stopCh)
}

// ─── 分片 ───

func (s *Store) getShard(key string) *shard {
	h := fnv32Hash(key)
	return s.shards[h&s.shardMask]
}

// ─── TTL ───

func (s *Store) pushTTL(key string, expiresAt time.Time) {
	s.ttlMu.Lock()
	defer s.ttlMu.Unlock()
	heap.Push(s.ttlHeap, ttlEntry{key: key, expiresAt: expiresAt})
}

func (s *Store) ttlSweeper() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.cleanupExpired()
		}
	}
}

func (s *Store) cleanupExpired() {
	now := time.Now().UTC()
	s.ttlMu.Lock()
	defer s.ttlMu.Unlock()

	for s.ttlHeap.Len() > 0 {
		entry := s.ttlHeap.entries[0]
		if entry.expiresAt.After(now) {
			break
		}

		heap.Pop(s.ttlHeap)

		// 再次检查（可能在 Pop 之前被手动更新了）
		sh := s.getShard(entry.key)
		sh.mu.Lock()
		if it, ok := sh.data[entry.key]; ok {
			if !it.expiresAt.IsZero() && !it.expiresAt.After(now) {
				delete(sh.data, entry.key)
			}
		}
		sh.mu.Unlock()
	}
}

// ─── TTL 堆 ───

type ttlEntry struct {
	key       string
	expiresAt time.Time
}

type ttlHeap struct {
	entries []ttlEntry
}

func newTTLHeap() *ttlHeap {
	return &ttlHeap{entries: make([]ttlEntry, 0)}
}

func (h *ttlHeap) Len() int           { return len(h.entries) }
func (h *ttlHeap) Less(i, j int) bool { return h.entries[i].expiresAt.Before(h.entries[j].expiresAt) }
func (h *ttlHeap) Swap(i, j int)      { h.entries[i], h.entries[j] = h.entries[j], h.entries[i] }

func (h *ttlHeap) Push(x any) {
	h.entries = append(h.entries, x.(ttlEntry))
}

func (h *ttlHeap) Pop() any {
	old := h.entries
	n := len(old)
	x := old[n-1]
	h.entries = old[:n-1]
	return x
}

// ─── 辅助 ───

func fnv32Hash(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

func nextPowerOfTwo(n int) int {
	if n <= 1 {
		return 1
	}
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	return n + 1
}
