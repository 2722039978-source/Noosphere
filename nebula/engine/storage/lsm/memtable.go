package lsm

import (
	"bytes"
	"math/rand"
	"sync"
)

const (
	maxLevel    = 20   // 最大层数
	probability = 0.25 // 每层的晋升概率
)

// SkipList 基于跳表的有序内存表
//
// 跳表是 LSM Tree MemTable 的标准数据结构，因为：
//  1. 插入/查找 O(log N) 平均复杂度
//  2. 数据天然有序，方便 Flush 到 SSTable 时顺序写出
//  3. 实现比红黑树简单，无锁版本更容易
//  4. 内存占用可控（每个节点平均 1/(1-p) 层 ≈ 1.33 层）
type SkipList struct {
	mu     sync.RWMutex
	head   *skipNode
	level  int     // 当前最大层数
	size   int     // 节点数量
	bytes  int64   // 总字节数
	maxSize int64  // 阈值：超过此值触发 Flush
}

type skipNode struct {
	key   []byte
	value []byte
	next  [maxLevel]*skipNode // 各层的后继指针
}

// NewSkipList 创建跳表
func NewSkipList(maxSize int64) *SkipList {
	head := &skipNode{
		key:  nil, // head 的 key 为 nil，逻辑上是最小值
		next: [maxLevel]*skipNode{},
	}
	return &SkipList{
		head:    head,
		level:   0,
		maxSize: maxSize,
	}
}

// randomLevel 随机生成新节点的层数
// 每一层有 probability 的概率继续晋升
func randomLevel() int {
	level := 1
	for level < maxLevel && rand.Float64() < probability {
		level++
	}
	return level
}

// Insert 插入键值对（如果 key 已存在则更新）
func (sl *SkipList) Insert(key, value []byte) {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	// update[i] 记录每层需要更新指针的前驱节点
	var update [maxLevel]*skipNode
	cur := sl.head

	// 从最高层往下搜索插入位置
	for i := sl.level; i >= 0; i-- {
		for cur.next[i] != nil && bytes.Compare(cur.next[i].key, key) < 0 {
			cur = cur.next[i]
		}
		update[i] = cur
	}

	// 检查是否已存在（在 level 0）
	cur = cur.next[0]

	if cur != nil && bytes.Equal(cur.key, key) {
		// 更新已存在的 key
		sl.bytes -= int64(len(cur.value))
		sl.bytes += int64(len(value))
		cur.value = value
		return
	}

	// 创建新节点
	newLevel := randomLevel() - 1
	if newLevel > sl.level {
		// 新节点比当前最大层数更高，head 需要扩展
		for i := sl.level + 1; i <= newLevel; i++ {
			update[i] = sl.head
		}
		sl.level = newLevel
	}

	node := &skipNode{
		key:   key,
		value: value,
	}

	// 在各层插入
	for i := 0; i <= newLevel; i++ {
		node.next[i] = update[i].next[i]
		update[i].next[i] = node
	}

	sl.size++
	sl.bytes += int64(len(key) + len(value))
}

// Get 查找键值
func (sl *SkipList) Get(key []byte) ([]byte, bool) {
	sl.mu.RLock()
	defer sl.mu.RUnlock()

	cur := sl.head
	for i := sl.level; i >= 0; i-- {
		for cur.next[i] != nil && bytes.Compare(cur.next[i].key, key) < 0 {
			cur = cur.next[i]
		}
	}

	cur = cur.next[0]
	if cur != nil && bytes.Equal(cur.key, key) {
		return cur.value, true
	}
	return nil, false
}

// Delete 删除键值对
func (sl *SkipList) Delete(key []byte) bool {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	var update [maxLevel]*skipNode
	cur := sl.head

	for i := sl.level; i >= 0; i-- {
		for cur.next[i] != nil && bytes.Compare(cur.next[i].key, key) < 0 {
			cur = cur.next[i]
		}
		update[i] = cur
	}

	cur = cur.next[0]
	if cur == nil || !bytes.Equal(cur.key, key) {
		return false
	}

	// 从各层移除
	for i := 0; i <= sl.level; i++ {
		if update[i].next[i] != cur {
			break
		}
		update[i].next[i] = cur.next[i]
	}

	// 降低 level（如果高层变空）
	for sl.level > 0 && sl.head.next[sl.level] == nil {
		sl.level--
	}

	sl.size--
	sl.bytes -= int64(len(cur.key) + len(cur.value))
	return true
}

// Iter 顺序遍历所有键值对
func (sl *SkipList) Iter() *SkipListIterator {
	sl.mu.RLock()
	return &SkipListIterator{
		sl:   sl,
		cur:  sl.head.next[0],
	}
}

// ShouldFlush 判断是否应该 Flush 到磁盘
func (sl *SkipList) ShouldFlush() bool {
	sl.mu.RLock()
	defer sl.mu.RUnlock()
	return sl.bytes >= sl.maxSize
}

// Size 返回节点数量
func (sl *SkipList) Size() int {
	sl.mu.RLock()
	defer sl.mu.RUnlock()
	return sl.size
}

// Bytes 返回总字节数
func (sl *SkipList) Bytes() int64 {
	sl.mu.RLock()
	defer sl.mu.RUnlock()
	return sl.bytes
}

// ─── 迭代器 ───

// SkipListIterator 跳表迭代器
type SkipListIterator struct {
	sl  *SkipList
	cur *skipNode
}

// Valid 当前位置是否有效
func (it *SkipListIterator) Valid() bool {
	return it.cur != nil
}

// Next 移动到下一个节点
func (it *SkipListIterator) Next() {
	if it.cur != nil {
		it.cur = it.cur.next[0]
	}
}

// Key 当前节点的键
func (it *SkipListIterator) Key() []byte {
	if it.cur == nil {
		return nil
	}
	return it.cur.key
}

// Value 当前节点的值
func (it *SkipListIterator) Value() []byte {
	if it.cur == nil {
		return nil
	}
	return it.cur.value
}
