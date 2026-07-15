// Package cache — LRU / LFU 缓存层
package cache

import (
	"container/list"
	"sync"
)

// LRU 最近最少使用缓存
type LRU struct {
	mu       sync.RWMutex
	maxSize  int
	items    map[string]*list.Element
	evictList *list.List

	// 统计
	hits   uint64
	misses uint64
}

// entry 缓存条目
type entry struct {
	key   string
	value interface{}
}

// NewLRU 创建 LRU 缓存
func NewLRU(maxSize int) *LRU {
	if maxSize < 1 {
		maxSize = 1000
	}
	return &LRU{
		maxSize:   maxSize,
		items:     make(map[string]*list.Element),
		evictList: list.New(),
	}
}

// Get 获取缓存值
func (c *LRU) Get(key string) (interface{}, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.evictList.MoveToFront(elem)
		c.hits++
		return elem.Value.(*entry).value, true
	}
	c.misses++
	return nil, false
}

// Put 放入缓存
func (c *LRU) Put(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 更新已存在的 key
	if elem, ok := c.items[key]; ok {
		c.evictList.MoveToFront(elem)
		elem.Value.(*entry).value = value
		return
	}

	// 创建新条目
	ent := &entry{key: key, value: value}
	elem := c.evictList.PushFront(ent)
	c.items[key] = elem

	// 淘汰
	if c.evictList.Len() > c.maxSize {
		c.removeOldest()
	}
}

// Del 删除缓存条目
func (c *LRU) Del(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.evictList.Remove(elem)
		delete(c.items, key)
	}
}

// Size 返回当前条目数
func (c *LRU) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.evictList.Len()
}

// HitRate 返回命中率
func (c *LRU) HitRate() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	total := c.hits + c.misses
	if total == 0 {
		return 0
	}
	return float64(c.hits) / float64(total)
}

// removeOldest 移除最久未使用的条目（不加锁）
func (c *LRU) removeOldest() {
	elem := c.evictList.Back()
	if elem != nil {
		c.evictList.Remove(elem)
		ent := elem.Value.(*entry)
		delete(c.items, ent.key)
	}
}
