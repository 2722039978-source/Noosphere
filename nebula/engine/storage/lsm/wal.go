// Package lsm — Write-Ahead Log（预写日志）
//
// WAL 保证数据的持久性：所有写入操作先追加到 WAL 文件，
// 然后才写入内存 MemTable。崩溃恢复时通过重放 WAL 恢复未刷盘的数据。
//
// 格式（每个记录）:
//
//	┌────────┬──────────┬────────┬───────────┬──────────┐
//	│ CRC32  │ SeqNum   │ OpCode │ KeyLen    │ Key      │
//	│ (4B)   │ (8B)     │ (1B)   │ (2B)      │ (var)    │
//	├────────┼──────────┼────────┼───────────┼──────────┤
//	│ ValLen │ Value    │ TTL    │ Timestamp │
//	│ (4B)   │ (var)    │ (8B)   │ (8B)      │
//	└────────┴──────────┴────────┴───────────┴──────────┘
//
// OpCode: 1=Put, 2=Delete
package lsm

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"sync"
)

// OpCode 操作类型
type OpCode byte

const (
	OpPut    OpCode = 1
	OpDelete OpCode = 2
)

// WALRecord 一条 WAL 记录
type WALRecord struct {
	SeqNum    uint64
	Op        OpCode
	Key       []byte
	Value     []byte
	TTL       int64 // 过期时间 unix 时间戳，0 表示永不过期
	Timestamp int64 // 写入时间
}

// WAL Write-Ahead Log
type WAL struct {
	mu       sync.Mutex
	file     *os.File
	path     string
	seqNum   uint64
	syncWrite bool // 每次写入是否 fsync
}

// OpenWAL 打开或创建 WAL 文件
func OpenWAL(path string, syncWrite bool) (*WAL, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("open wal: %w", err)
	}

	wal := &WAL{
		file:      file,
		path:      path,
		syncWrite: syncWrite,
	}

	// 读取已有的最大 seqNum
	if err := wal.recoverSeqNum(); err != nil {
		return nil, err
	}

	return wal, nil
}

// Append 追加一条记录到 WAL
func (w *WAL) Append(rec *WALRecord) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.seqNum++
	rec.SeqNum = w.seqNum

	data := w.encode(rec)

	// 写入长度前缀 + 数据
	lenBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(lenBuf, uint32(len(data)))

	if _, err := w.file.Write(lenBuf); err != nil {
		return fmt.Errorf("wal write length: %w", err)
	}
	if _, err := w.file.Write(data); err != nil {
		return fmt.Errorf("wal write data: %w", err)
	}

	if w.syncWrite {
		if err := w.file.Sync(); err != nil {
			return fmt.Errorf("wal sync: %w", err)
		}
	}

	return nil
}

// Recover 从 WAL 文件中恢复所有记录（用于崩溃恢复）
func (w *WAL) Recover() ([]*WALRecord, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// 读取整个文件
	data, err := os.ReadFile(w.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("wal recover read: %w", err)
	}

	var records []*WALRecord
	offset := 0

	for offset < len(data) {
		if offset+4 > len(data) {
			break // 不完整的长度字段
		}

		recLen := binary.LittleEndian.Uint32(data[offset:])
		offset += 4

		if offset+int(recLen) > len(data) {
			break // 不完整的记录
		}

		rec, err := w.decode(data[offset : offset+int(recLen)])
		if err != nil {
			// 跳过损坏的记录
			offset += int(recLen)
			continue
		}

		records = append(records, rec)
		offset += int(recLen)

		// 更新最大 seqNum
		if rec.SeqNum > w.seqNum {
			w.seqNum = rec.SeqNum
		}
	}

	return records, nil
}

// Truncate 清空 WAL（MemTable 成功刷盘后调用）
func (w *WAL) Truncate() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.file.Close(); err != nil {
		return fmt.Errorf("wal truncate close: %w", err)
	}

	// 重新创建空文件
	file, err := os.Create(w.path)
	if err != nil {
		return fmt.Errorf("wal truncate create: %w", err)
	}
	w.file = file
	w.seqNum = 0

	return nil
}

// Close 关闭 WAL
func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}

// SeqNum 返回当前最大序列号
func (w *WAL) SeqNum() uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.seqNum
}

// ─── 内部编解码 ───

func (w *WAL) encode(rec *WALRecord) []byte {
	// 估算大小
	size := 4 + 8 + 1 + 2 + len(rec.Key) + 4 + len(rec.Value) + 8 + 8
	buf := make([]byte, 0, size)

	// CRC32 占位（最后填充）
	buf = append(buf, 0, 0, 0, 0)

	// SeqNum (8B)
	tmp := make([]byte, 8)
	binary.LittleEndian.PutUint64(tmp, rec.SeqNum)
	buf = append(buf, tmp...)

	// OpCode (1B)
	buf = append(buf, byte(rec.Op))

	// KeyLen (2B) + Key
	keyLen := make([]byte, 2)
	binary.LittleEndian.PutUint16(keyLen, uint16(len(rec.Key)))
	buf = append(buf, keyLen...)
	buf = append(buf, rec.Key...)

	// ValLen (4B) + Value
	valLen := make([]byte, 4)
	binary.LittleEndian.PutUint32(valLen, uint32(len(rec.Value)))
	buf = append(buf, valLen...)
	buf = append(buf, rec.Value...)

	// TTL (8B)
	binary.LittleEndian.PutUint64(tmp, uint64(rec.TTL))
	buf = append(buf, tmp...)

	// Timestamp (8B)
	binary.LittleEndian.PutUint64(tmp, uint64(rec.Timestamp))
	buf = append(buf, tmp...)

	// 计算并写入 CRC32
	crc := crc32.ChecksumIEEE(buf[4:])
	binary.LittleEndian.PutUint32(buf[:4], crc)

	return buf
}

func (w *WAL) decode(data []byte) (*WALRecord, error) {
	if len(data) < 4+8+1+2 {
		return nil, fmt.Errorf("record too short: %d bytes", len(data))
	}

	// 校验 CRC32
	expectedCRC := binary.LittleEndian.Uint32(data[:4])
	actualCRC := crc32.ChecksumIEEE(data[4:])
	if expectedCRC != actualCRC {
		return nil, fmt.Errorf("crc mismatch: expected %08x, got %08x", expectedCRC, actualCRC)
	}

	offset := 4

	rec := &WALRecord{}
	rec.SeqNum = binary.LittleEndian.Uint64(data[offset:])
	offset += 8

	rec.Op = OpCode(data[offset])
	offset++

	keyLen := binary.LittleEndian.Uint16(data[offset:])
	offset += 2
	if offset+int(keyLen) > len(data) {
		return nil, fmt.Errorf("key overflow")
	}
	rec.Key = make([]byte, keyLen)
	copy(rec.Key, data[offset:offset+int(keyLen)])
	offset += int(keyLen)

	valLen := binary.LittleEndian.Uint32(data[offset:])
	offset += 4
	if offset+int(valLen) > len(data) {
		return nil, fmt.Errorf("value overflow")
	}
	rec.Value = make([]byte, valLen)
	copy(rec.Value, data[offset:offset+int(valLen)])
	offset += int(valLen)

	rec.TTL = int64(binary.LittleEndian.Uint64(data[offset:]))
	offset += 8

	rec.Timestamp = int64(binary.LittleEndian.Uint64(data[offset:]))

	return rec, nil
}

// recoverSeqNum 从已有 WAL 文件中恢复最大序列号
func (w *WAL) recoverSeqNum() error {
	records, err := w.Recover()
	if err != nil {
		return err
	}
	for _, rec := range records {
		if rec.SeqNum > w.seqNum {
			w.seqNum = rec.SeqNum
		}
	}
	return nil
}
