package manager

import (
	"crypto/rand"
	"fmt"
	"sync/atomic"
	"time"
)

// 全局计数器，用于在同一毫秒内区分 ID
var idCounter uint32

// NewID 生成时间有序的唯一 ID（UUID v7 风格）
// 格式: {unix_ms_hex}-{random_hex}
// 示例: 01932a5b0000-7f3a1b2c4d5e
// 特性:
//   - 按时间可排序（前缀是毫秒时间戳）
//   - 全局唯一（后半段是随机数 + 计数器）
//   - 纯 Go 实现，不依赖 UUID 库
func NewID() string {
	now := time.Now().UTC()
	ms := now.UnixMilli()

	// 原子递增计数器
	cnt := atomic.AddUint32(&idCounter, 1)

	// 随机部分：crypto/rand 保证唯一性
	randomBytes := make([]byte, 8)
	_, _ = rand.Read(randomBytes)

	return fmt.Sprintf("%013x-%04x%04x%04x",
		ms,
		uint16(randomBytes[0])<<8|uint16(randomBytes[1]),
		uint16(randomBytes[2])<<8|uint16(randomBytes[3]),
		cnt&0xFFFF,
	)
}

// ParseIDTimestamp 从 ID 中提取时间戳
func ParseIDTimestamp(id string) (time.Time, error) {
	if len(id) < 13 {
		return time.Time{}, fmt.Errorf("invalid id: too short")
	}
	var ms int64
	_, err := fmt.Sscanf(id[:13], "%x", &ms)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse id timestamp: %w", err)
	}
	return time.UnixMilli(ms).UTC(), nil
}
