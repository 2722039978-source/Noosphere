package lsm

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// LSMTree Log-Structured Merge Tree 存储引擎
//
// 核心设计:
//   - 写入: 先写 WAL → 再写 MemTable (SkipList) → 返回
//   - 读取: MemTable → Immutable MemTable → SSTable (L0→L1→...→Ln)
//   - 后台: MemTable 满 → 冻结 → Flush 为 L0 SSTable → Compaction 合并到高层
//
// 层级结构:
//   - L0: 直接 Flush 自 MemTable，不同 SSTable 之间 key 可能重叠
//   - L1~Ln: 每层内部 key 不重叠，每层大小约为上层的 10 倍
type LSMTree struct {
	mu sync.RWMutex

	// 路径
	dir      string
	walPath  string

	// MemTable
	mem       *SkipList   // 当前可写的 MemTable
	immutable *SkipList   // 正在 Flush 的只读 MemTable（Flush 完成后为 nil）

	// WAL
	wal        *WAL
	walEnabled bool

	// SSTable 层级
	levels     [][]*SSTableMeta // levels[0] = L0, levels[1] = L1, ...
	maxLevels  int

	// Compaction 控制
	compacting  bool
	nextFileNum int

	// 配置
	opts *LSMOptions
}

// LSMOptions LSM Tree 配置
type LSMOptions struct {
	Dir             string
	MemTableSize    int64
	SSTableSize     int64
	BloomFilterBits int
	MaxLevels       int
	WALEnabled      bool
	WALSyncOnWrite  bool
}

// NewLSMTree 创建或打开 LSM Tree
func NewLSMTree(opts *LSMOptions) (*LSMTree, error) {
	if err := os.MkdirAll(opts.Dir, 0755); err != nil {
		return nil, fmt.Errorf("lsm create dir: %w", err)
	}

	walPath := filepath.Join(opts.Dir, "nebula.wal")

	lsm := &LSMTree{
		dir:        opts.Dir,
		walPath:    walPath,
		mem:        NewSkipList(opts.MemTableSize),
		walEnabled: opts.WALEnabled,
		levels:     make([][]*SSTableMeta, opts.MaxLevels),
		maxLevels:  opts.MaxLevels,
		opts:       opts,
	}

	// 打开 WAL
	if opts.WALEnabled {
		wal, err := OpenWAL(walPath, opts.WALSyncOnWrite)
		if err != nil {
			return nil, fmt.Errorf("lsm open wal: %w", err)
		}
		lsm.wal = wal
	}

	// 恢复未刷盘的数据
	if err := lsm.recover(); err != nil {
		return nil, fmt.Errorf("lsm recover: %w", err)
	}

	// 扫描已有 SSTable
	if err := lsm.loadSSTables(); err != nil {
		return nil, fmt.Errorf("lsm load sstables: %w", err)
	}

	return lsm, nil
}

// Put 写入键值对
func (lsm *LSMTree) Put(key, value []byte) error {
	lsm.mu.Lock()
	defer lsm.mu.Unlock()

	// 写 WAL
	if lsm.walEnabled {
		rec := &WALRecord{
			Op:    OpPut,
			Key:   key,
			Value: value,
		}
		if err := lsm.wal.Append(rec); err != nil {
			return fmt.Errorf("lsm put wal: %w", err)
		}
	}

	// 写 MemTable
	lsm.mem.Insert(key, value)

	// 检查是否需要 Flush
	if lsm.mem.ShouldFlush() && lsm.immutable == nil {
		lsm.immutable = lsm.mem
		lsm.mem = NewSkipList(lsm.opts.MemTableSize)
		go lsm.flush()
	}

	return nil
}

// Get 读取键值
func (lsm *LSMTree) Get(key []byte) ([]byte, bool, error) {
	lsm.mu.RLock()
	mem := lsm.mem
	imm := lsm.immutable
	levels := lsm.levels
	lsm.mu.RUnlock()

	// 1. 查当前 MemTable
	if val, ok := mem.Get(key); ok {
		return val, true, nil
	}

	// 2. 查 Immutable MemTable
	if imm != nil {
		if val, ok := imm.Get(key); ok {
			return val, true, nil
		}
	}

	// 3. 查 SSTable 层级（L0 → Ln）
	for level := 0; level < len(levels); level++ {
		for _, meta := range levels[level] {
			if !meta.MightContain(key) {
				continue
			}
			reader := NewSSTableReader(meta)
			val, found, err := reader.Get(key)
			if err != nil {
				return nil, false, err
			}
			if found {
				return val, true, nil
			}
		}
	}

	return nil, false, nil
}

// Delete 删除键值（写入墓碑标记）
func (lsm *LSMTree) Delete(key []byte) error {
	lsm.mu.Lock()
	defer lsm.mu.Unlock()

	if lsm.walEnabled {
		rec := &WALRecord{
			Op:  OpDelete,
			Key: key,
		}
		if err := lsm.wal.Append(rec); err != nil {
			return fmt.Errorf("lsm delete wal: %w", err)
		}
	}

	// 写入墓碑（nil value 表示删除）
	lsm.mem.Insert(key, nil)

	if lsm.mem.ShouldFlush() && lsm.immutable == nil {
		lsm.immutable = lsm.mem
		lsm.mem = NewSkipList(lsm.opts.MemTableSize)
		go lsm.flush()
	}

	return nil
}

// Close 关闭 LSM Tree
func (lsm *LSMTree) Close() error {
	lsm.mu.Lock()
	defer lsm.mu.Unlock()

	// 强制 Flush 当前 MemTable
	if lsm.mem.Size() > 0 {
		lsm.immutable = lsm.mem
		lsm.mem = NewSkipList(lsm.opts.MemTableSize)
		lsm.mu.Unlock()
		lsm.flush()
		lsm.mu.Lock()
	}

	if lsm.wal != nil {
		if err := lsm.wal.Close(); err != nil {
			return err
		}
	}

	return nil
}

// ─── 内部方法 ───

// recover 从 WAL 恢复未刷盘的数据
func (lsm *LSMTree) recover() error {
	if lsm.wal == nil {
		return nil
	}

	records, err := lsm.wal.Recover()
	if err != nil {
		return err
	}

	for _, rec := range records {
		switch rec.Op {
		case OpPut:
			lsm.mem.Insert(rec.Key, rec.Value)
		case OpDelete:
			lsm.mem.Insert(rec.Key, nil)
		}
	}

	// 恢复后清空 WAL
	if lsm.mem.Size() > 0 {
		return lsm.wal.Truncate()
	}

	return nil
}

// flush 将 Immutable MemTable 刷到 SSTable (L0)
func (lsm *LSMTree) flush() {
	lsm.mu.RLock()
	imm := lsm.immutable
	if imm == nil {
		lsm.mu.RUnlock()
		return
	}
	lsm.mu.RUnlock()

	filename := fmt.Sprintf("sst_%06d.sst", lsm.nextFileNum)
	lsm.nextFileNum++
	path := filepath.Join(lsm.dir, filename)

	writer := NewSSTableWriter(path)
	iter := imm.Iter()
	for iter.Valid() {
		writer.Add(iter.Key(), iter.Value())
		iter.Next()
	}

	meta, err := writer.Finish()
	if err != nil {
		// Flush 失败，此时数据还在 WAL 中，下次启动会恢复
		return
	}
	meta.Level = 0

	// 将 SSTable 加入 L0
	lsm.mu.Lock()
	lsm.levels[0] = append(lsm.levels[0], meta)
	lsm.immutable = nil
	lsm.mu.Unlock()

	// 清空 WAL（已安全刷盘）
	if lsm.walEnabled && lsm.wal != nil {
		_ = lsm.wal.Truncate()
	}

	// 触发 Compaction
	go lsm.maybeCompact()
}

// loadSSTables 扫描并加载已有的 SSTable 文件
func (lsm *LSMTree) loadSSTables() error {
	pattern := filepath.Join(lsm.dir, "sst_*.sst")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}

	// 获取最大编号
	for _, f := range files {
		var num int
		_, err := fmt.Sscanf(filepath.Base(f), "sst_%d.sst", &num)
		if err != nil {
			continue
		}
		if num >= lsm.nextFileNum {
			lsm.nextFileNum = num + 1
		}

		// 简化：所有已有 SSTable 加载到 L0
		// 生产环境需要从 manifest 读取 level 信息
		info, err := os.Stat(f)
		if err != nil {
			continue
		}

		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}

		meta := &SSTableMeta{
			Path:       f,
			EntryCount: 0,
			FileSize:   info.Size(),
			Level:      0,
		}

		// 读取 footer
		if len(data) >= footerSize {
			footerStart := len(data) - footerSize
			filterOffset := int64(binary.LittleEndian.Uint64(data[footerStart+12:]))
			numHashes := int(data[footerStart+20])

			// 读取 Bloom Filter
			if filterOffset < int64(len(data)) {
				filterData := data[filterOffset : footerStart-1]
				meta.BloomFilter = LoadBloomFilter(filterData, numHashes)
			}
		}

		lsm.levels[0] = append(lsm.levels[0], meta)
	}

	return nil
}

// maybeCompact 检查并执行 Compaction
func (lsm *LSMTree) maybeCompact() {
	lsm.mu.Lock()
	if lsm.compacting {
		lsm.mu.Unlock()
		return
	}
	lsm.compacting = true
	lsm.mu.Unlock()

	defer func() {
		lsm.mu.Lock()
		lsm.compacting = false
		lsm.mu.Unlock()
	}()

	// 简化策略：L0 超过 4 个 SSTable 时触发
	lsm.mu.RLock()
	l0Count := len(lsm.levels[0])
	lsm.mu.RUnlock()

	if l0Count > 4 {
		lsm.compactLevel(0)
	}
}

// compactLevel 合并指定层级到下一层
func (lsm *LSMTree) compactLevel(level int) {
	lsm.mu.Lock()
	if level >= lsm.maxLevels-1 {
		lsm.mu.Unlock()
		return
	}

	if len(lsm.levels[level]) == 0 {
		lsm.mu.Unlock()
		return
	}

	// 取出当前层所有 SSTable
	tables := lsm.levels[level]
	lsm.levels[level] = nil
	lsm.mu.Unlock()

	// 合并排序 → 写入新 SSTable
	filename := fmt.Sprintf("sst_%06d.sst", lsm.nextFileNum)
	lsm.nextFileNum++
	path := filepath.Join(lsm.dir, filename)

	writer := NewSSTableWriter(path)

	// 多路归并
	var iterators []*SSTableIterator
	for _, meta := range tables {
		iterators = append(iterators, NewSSTableIterator(meta))
	}

	merger := NewMergeIterator(iterators)
	for merger.Valid() {
		writer.Add(merger.Key(), merger.Value())
		merger.Next()
	}

	meta, err := writer.Finish()
	if err != nil {
		return
	}
	meta.Level = level + 1

	// 加入下一层
	lsm.mu.Lock()
	lsm.levels[level+1] = append(lsm.levels[level+1], meta)
	lsm.mu.Unlock()

	// 删除旧的 SSTable 文件
	for _, oldMeta := range tables {
		os.Remove(oldMeta.Path)
	}
}

// ─── 迭代器（用于 Compaction 和遍历）───

// SSTableIterator SSTable 全量迭代器
type SSTableIterator struct {
	data    []byte
	pos     int
	curKey  []byte
	curVal  []byte
	valid   bool
}

// NewSSTableIterator 创建 SSTable 迭代器
func NewSSTableIterator(meta *SSTableMeta) *SSTableIterator {
	data, err := os.ReadFile(meta.Path)
	if err != nil {
		return &SSTableIterator{valid: false}
	}

	it := &SSTableIterator{
		data:  data,
		pos:   0,
		valid: true,
	}
	it.Next() // 移动到第一条
	return it
}

func (it *SSTableIterator) Valid() bool { return it.valid }
func (it *SSTableIterator) Key() []byte  { return it.curKey }
func (it *SSTableIterator) Value() []byte { return it.curVal }

func (it *SSTableIterator) Next() {
	if it.pos >= len(it.data)-4 {
		it.valid = false
		return
	}

	if it.pos+2 > len(it.data)-4 {
		it.valid = false
		return
	}

	keyLen := binary.LittleEndian.Uint16(it.data[it.pos:])
	it.pos += 2
	if it.pos+int(keyLen)+4 > len(it.data) {
		it.valid = false
		return
	}

	it.curKey = make([]byte, keyLen)
	copy(it.curKey, it.data[it.pos:it.pos+int(keyLen)])
	it.pos += int(keyLen)

	valLen := binary.LittleEndian.Uint32(it.data[it.pos:])
	it.pos += 4
	if it.pos+int(valLen) > len(it.data)-4 {
		it.valid = false
		return
	}

	it.curVal = make([]byte, valLen)
	copy(it.curVal, it.data[it.pos:it.pos+int(valLen)])
	it.pos += int(valLen)
}

// MergeIterator 多路归并迭代器
type MergeIterator struct {
	iterators []*SSTableIterator
	curIdx    int
}

// NewMergeIterator 创建多路归并迭代器
func NewMergeIterator(iters []*SSTableIterator) *MergeIterator {
	mi := &MergeIterator{
		iterators: iters,
		curIdx:    -1,
	}
	mi.findSmallest()
	return mi
}

func (mi *MergeIterator) Valid() bool {
	return mi.curIdx >= 0 && mi.iterators[mi.curIdx].Valid()
}

func (mi *MergeIterator) Key() []byte {
	if !mi.Valid() {
		return nil
	}
	return mi.iterators[mi.curIdx].Key()
}

func (mi *MergeIterator) Value() []byte {
	if !mi.Valid() {
		return nil
	}
	return mi.iterators[mi.curIdx].Value()
}

func (mi *MergeIterator) Next() {
	if mi.Valid() {
		mi.iterators[mi.curIdx].Next()
	}
	mi.findSmallest()
}

func (mi *MergeIterator) findSmallest() {
	mi.curIdx = -1
	var smallestKey []byte

	for i, it := range mi.iterators {
		if !it.Valid() {
			continue
		}
		if mi.curIdx == -1 || bytes.Compare(it.Key(), smallestKey) < 0 {
			mi.curIdx = i
			smallestKey = it.Key()
		}
	}
}
