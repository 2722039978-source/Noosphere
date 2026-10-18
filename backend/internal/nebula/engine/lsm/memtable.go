package lsm

import (
	"bytes"
	"math/rand"
	"sync"
)

const maxLevel = 12

type skipNode struct {
	key   []byte
	value []byte
	next  []*skipNode
}

type MemTable struct {
	mu    sync.RWMutex
	head  *skipNode
	level int
	size  int
}

func NewMemTable() *MemTable {
	return &MemTable{
		head: &skipNode{next: make([]*skipNode, maxLevel)},
	}
}

func (m *MemTable) Put(key, value []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	update := make([]*skipNode, maxLevel)
	node := m.head
	for i := m.level - 1; i >= 0; i-- {
		for node.next[i] != nil && bytes.Compare(node.next[i].key, key) < 0 {
			node = node.next[i]
		}
		update[i] = node
	}
	if node = node.next[0]; node != nil && bytes.Equal(node.key, key) {
		m.size -= len(node.value)
		node.value = value
		m.size += len(value)
		return
	}
	lvl := randomLevel()
	if lvl > m.level {
		for i := m.level; i < lvl; i++ {
			update[i] = m.head
		}
		m.level = lvl
	}
	node = &skipNode{key: key, value: value, next: make([]*skipNode, lvl)}
	for i := 0; i < lvl; i++ {
		node.next[i] = update[i].next[i]
		update[i].next[i] = node
	}
	m.size += len(key) + len(value)
}

func (m *MemTable) Get(key []byte) ([]byte, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	node := m.head
	for i := m.level - 1; i >= 0; i-- {
		for node.next[i] != nil && bytes.Compare(node.next[i].key, key) < 0 {
			node = node.next[i]
		}
	}
	node = node.next[0]
	if node != nil && bytes.Equal(node.key, key) {
		return node.value, true
	}
	return nil, false
}

func (m *MemTable) Delete(key []byte) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	update := make([]*skipNode, maxLevel)
	node := m.head
	for i := m.level - 1; i >= 0; i-- {
		for node.next[i] != nil && bytes.Compare(node.next[i].key, key) < 0 {
			node = node.next[i]
		}
		update[i] = node
	}
	node = node.next[0]
	if node != nil && bytes.Equal(node.key, key) {
		for i := 0; i < m.level; i++ {
			if update[i].next[i] != node {
				break
			}
			update[i].next[i] = node.next[i]
		}
		m.size -= len(node.key) + len(node.value)
		return true
	}
	return false
}

func (m *MemTable) Size() int { m.mu.RLock(); defer m.mu.RUnlock(); return m.size }

func (m *MemTable) ForEach(fn func(key, value []byte) bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for node := m.head.next[0]; node != nil; node = node.next[0] {
		if !fn(node.key, node.value) {
			return
		}
	}
}

func (m *MemTable) Snapshot() []kvPair {
	m.mu.RLock()
	defer m.mu.RUnlock()
	pairs := make([]kvPair, 0)
	for node := m.head.next[0]; node != nil; node = node.next[0] {
		pairs = append(pairs, kvPair{key: clone(node.key), value: clone(node.value)})
	}
	return pairs
}

type kvPair struct {
	key, value []byte
}

func clone(b []byte) []byte {
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

func randomLevel() int {
	lvl := 1
	for rand.Intn(2) == 0 && lvl < maxLevel {
		lvl++
	}
	return lvl
}
