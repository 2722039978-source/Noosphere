package lsm

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"sort"
)

// SSTable (Sorted String Table) — 不可变的有序磁盘文件
//
// 格式:
//
//	┌──────────────┬──────────────┬──────────────┐
//	│  Data Block  │  Data Block  │     ...      │
//	│  (4KB)       │  (4KB)       │              │
//	├──────────────┴──────────────┴──────────────┤
//	│  Filter Block (Bloom Filter)               │
//	├────────────────────────────────────────────┤
//	│  Index Block (key → block_offset)          │
//	├────────────────────────────────────────────┤
//	│  Footer (index_offset, filter_offset, crc) │
//	└────────────────────────────────────────────┘
//
// 每个 Data Block 内部存储多个有序 key-value 对：
//
//	┌────────┬──────────┬──────────┬─────┬────────┐
//	│ pairs  │ key1     │ val1     │ ... │ CRC32  │
//	│ count  │ len(2B)  │ len(4B)  │     │ (4B)   │
//	│ (4B)   │ + data   │ + data   │     │        │
//	└────────┴──────────┴──────────┴─────┴────────┘

const (
	blockSize      = 4 * 1024 // 每个 Data Block 4KB
	footerSize     = 4 + 8 + 8 + 4 // index_offset + filter_offset + index_size + crc
)

// Entry 一个键值对
type Entry struct {
	Key   []byte
	Value []byte
}

// SSTableWriter 用于构建 SSTable
type SSTableWriter struct {
	path    string
	entries []Entry // 积累的条目
	curSize int64
}

// NewSSTableWriter 创建 SSTable 写入器
func NewSSTableWriter(path string) *SSTableWriter {
	return &SSTableWriter{
		path:    path,
		entries: make([]Entry, 0, 1024),
	}
}

// Add 添加一个键值对（调用者保证按 key 有序添加）
func (w *SSTableWriter) Add(key, value []byte) {
	w.entries = append(w.entries, Entry{Key: key, Value: value})
	w.curSize += int64(len(key) + len(value))
}

// Finish 写入磁盘并关闭
func (w *SSTableWriter) Finish() (*SSTableMeta, error) {
	file, err := os.Create(w.path)
	if err != nil {
		return nil, fmt.Errorf("sstable create: %w", err)
	}
	defer file.Close()

	// 构建 Bloom Filter
	bf := NewBloomFilter(len(w.entries), 10)
	for _, e := range w.entries {
		bf.Add(e.Key)
	}

	// 写入 Data Blocks + 构建索引
	var indexEntries []indexEntry
	blockBuf := new(bytes.Buffer)
	entriesInBlock := 0
	blockOffset := int64(0)

	flushBlock := func() error {
		if blockBuf.Len() == 0 {
			return nil
		}
		// 写入 CRC32
		crc := crc32Checksum(blockBuf.Bytes())
		if err := binary.Write(blockBuf, binary.LittleEndian, crc); err != nil {
			return err
		}

		// 记录索引
		if entriesInBlock > 0 {
			lastEntry := w.entries[len(indexEntries)] // 不好，改用更直接的方式
			_ = lastEntry
		}

		if _, err := file.Write(blockBuf.Bytes()); err != nil {
			return err
		}
		blockOffset += int64(blockBuf.Len())
		blockBuf.Reset()
		entriesInBlock = 0
		return nil
	}

	for i, e := range w.entries {
		if blockBuf.Len() == 0 {
			// 记录这个 block 的起始 key
			indexEntries = append(indexEntries, indexEntry{
				Key:         e.Key,
				BlockOffset: blockOffset,
			})
		}

		// 写入 key-value 到当前 block
		if err := binary.Write(blockBuf, binary.LittleEndian, uint16(len(e.Key))); err != nil {
			return nil, err
		}
		blockBuf.Write(e.Key)
		if err := binary.Write(blockBuf, binary.LittleEndian, uint32(len(e.Value))); err != nil {
			return nil, err
		}
		blockBuf.Write(e.Value)
		entriesInBlock++

		// Block 满了就刷盘
		if blockBuf.Len() >= blockSize && i < len(w.entries)-1 {
			if err := flushBlock(); err != nil {
				return nil, err
			}
		}
	}

	// 刷最后一个 block
	if err := flushBlock(); err != nil {
		return nil, err
	}

	// 写入 Bloom Filter
	filterOffset := blockOffset
	filterData := bf.Bytes()
	if _, err := file.Write(filterData); err != nil {
		return nil, fmt.Errorf("sstable write filter: %w", err)
	}

	// 写入 Index Block
	indexOffset := filterOffset + int64(len(filterData))
	indexSize := 0
	for _, ie := range indexEntries {
		keyLen := uint16(len(ie.Key))
		binary.Write(file, binary.LittleEndian, keyLen)
		file.Write(ie.Key)
		binary.Write(file, binary.LittleEndian, ie.BlockOffset)
		indexSize += 2 + len(ie.Key) + 8
	}

	// 写入 Footer
	footer := make([]byte, footerSize)
	binary.LittleEndian.PutUint32(footer[0:4], crc32Checksum([]byte{})) // 占位
	binary.LittleEndian.PutUint64(footer[4:12], uint64(indexOffset))
	binary.LittleEndian.PutUint64(footer[12:20], uint64(filterOffset))
	footer[20] = byte(bf.NumHashes())

	// 计算整体 CRC
	file.Seek(0, 0)
	allData := make([]byte, indexOffset+int64(indexSize))
	file.Read(allData)
	totalCRC := crc32Checksum(allData)
	binary.LittleEndian.PutUint32(footer[0:4], totalCRC)

	if _, err := file.Write(footer); err != nil {
		return nil, fmt.Errorf("sstable write footer: %w", err)
	}

	return &SSTableMeta{
		Path:        w.path,
		MinKey:      w.entries[0].Key,
		MaxKey:      w.entries[len(w.entries)-1].Key,
		EntryCount:  len(w.entries),
		FileSize:    indexOffset + int64(indexSize) + footerSize,
		BloomFilter: bf,
		IndexOffset: indexOffset,
		FilterOffset: filterOffset,
	}, nil
}

// indexEntry 索引条目
type indexEntry struct {
	Key         []byte
	BlockOffset int64
}

// ─── SSTable 元数据 ───

// SSTableMeta SSTable 元信息
type SSTableMeta struct {
	Path        string
	MinKey      []byte
	MaxKey      []byte
	EntryCount  int
	FileSize    int64
	BloomFilter *BloomFilter
	IndexOffset int64
	FilterOffset int64
	Level       int // 所属层级（用于 Compaction）
}

// MightContain 检查 key 是否可能在此 SSTable 中
func (m *SSTableMeta) MightContain(key []byte) bool {
	// 1. 范围检查
	if bytes.Compare(key, m.MinKey) < 0 || bytes.Compare(key, m.MaxKey) > 0 {
		return false
	}
	// 2. Bloom Filter 检查
	return m.BloomFilter.MayContain(key)
}

// ─── SSTable 读取器 ───

// SSTableReader 读取 SSTable 文件
type SSTableReader struct {
	meta *SSTableMeta
}

// NewSSTableReader 打开 SSTable
func NewSSTableReader(meta *SSTableMeta) *SSTableReader {
	return &SSTableReader{meta: meta}
}

// Get 从 SSTable 中查找 key
func (r *SSTableReader) Get(key []byte) ([]byte, bool, error) {
	// 先检查 Bloom Filter
	if !r.meta.MightContain(key) {
		return nil, false, nil
	}

	// 读取文件
	data, err := os.ReadFile(r.meta.Path)
	if err != nil {
		return nil, false, fmt.Errorf("sstable read: %w", err)
	}

	// 解析索引
	indexes := r.parseIndex(data)

	// 二分查找定位 Data Block
	blockIdx := sort.Search(len(indexes), func(i int) bool {
		return bytes.Compare(indexes[i].Key, key) >= 0
	})

	// 调整：key 可能在 blockIdx-1 的 block 中
	if blockIdx >= len(indexes) || (blockIdx > 0 && bytes.Compare(indexes[blockIdx].Key, key) > 0) {
		blockIdx--
	}
	if blockIdx < 0 {
		return nil, false, nil
	}

	// 在选定的 block 中线性搜索
	offset := indexes[blockIdx].BlockOffset
	var nextOffset int64
	if blockIdx+1 < len(indexes) {
		nextOffset = indexes[blockIdx+1].BlockOffset
	} else {
		nextOffset = r.meta.FilterOffset
	}

	return r.searchInBlock(data[offset:nextOffset], key)
}

// parseIndex 解析索引块
func (r *SSTableReader) parseIndex(data []byte) []indexEntry {
	// 读取 Footer 获取 index 位置和大小
	if len(data) < footerSize {
		return nil
	}

	footerStart := len(data) - footerSize
	indexOffset := int64(binary.LittleEndian.Uint64(data[footerStart+4:]))
	filterOffset := int64(binary.LittleEndian.Uint64(data[footerStart+12:]))

	indexData := data[indexOffset:filterOffset]
	var entries []indexEntry
	pos := 0

	for pos < len(indexData) {
		if pos+2 > len(indexData) {
			break
		}
		keyLen := binary.LittleEndian.Uint16(indexData[pos:])
		pos += 2
		if pos+int(keyLen)+8 > len(indexData) {
			break
		}
		key := make([]byte, keyLen)
		copy(key, indexData[pos:pos+int(keyLen)])
		pos += int(keyLen)
		blockOffset := int64(binary.LittleEndian.Uint64(indexData[pos:]))
		pos += 8
		entries = append(entries, indexEntry{Key: key, BlockOffset: blockOffset})
	}
	return entries
}

// searchInBlock 在 Data Block 中线性搜索 key
func (r *SSTableReader) searchInBlock(blockData []byte, key []byte) ([]byte, bool, error) {
	pos := 0
	for pos < len(blockData)-4 { // 最后 4B 是 CRC
		if pos+2 > len(blockData)-4 {
			break
		}
		keyLen := binary.LittleEndian.Uint16(blockData[pos:])
		pos += 2
		if pos+int(keyLen)+4 > len(blockData) {
			break
		}
		curKey := blockData[pos : pos+int(keyLen)]
		pos += int(keyLen)

		valLen := binary.LittleEndian.Uint32(blockData[pos:])
		pos += 4
		if pos+int(valLen) > len(blockData)-4 {
			break
		}
		curVal := blockData[pos : pos+int(valLen)]
		pos += int(valLen)

		if bytes.Equal(curKey, key) {
			return curVal, true, nil
		}

		// 由于数据有序，如果当前 key > 目标 key，后面的也不可能匹配
		if bytes.Compare(curKey, key) > 0 {
			return nil, false, nil
		}
	}
	return nil, false, nil
}

// ─── 辅助函数 ───

func crc32Checksum(data []byte) uint32 {
	// 简单 CRC 实现（生产环境可考虑用 hash/crc32）
	crc := uint32(0xFFFFFFFF)
	for _, b := range data {
		crc ^= uint32(b)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xEDB88320
			} else {
				crc >>= 1
			}
		}
	}
	return crc ^ 0xFFFFFFFF
}

// ─── Entry 排序辅助 ───

// SortEntries 按键排序
func SortEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		return bytes.Compare(entries[i].Key, entries[j].Key) < 0
	})
}
