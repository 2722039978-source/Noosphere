package lsm

import (
	"hash/fnv"
	"math"
)

// BloomFilter 布隆过滤器
//
// 每个 SSTable 附带一个 Bloom Filter，用于快速判断一个 key 是否"可能存在"。
// Bloom Filter 保证：如果返回 false，key 绝对不存在；如果返回 true，key 可能存在。
// 这避免了每次查询都要读取磁盘上的 SSTable 文件。
//
// 参数选择:
//   - bitsPerKey=10 → 误判率约 1%
//   - bitsPerKey=14 → 误判率约 0.1%
//
// 默认使用 10 bits/key，在空间和精度之间取得良好平衡。
type BloomFilter struct {
	bits     []byte  // 位数组
	numHashes int    // hash 函数个数
}

// NewBloomFilter 创建布隆过滤器
// n: 预期 key 数量
// bitsPerKey: 每个 key 使用的位数
func NewBloomFilter(n int, bitsPerKey int) *BloomFilter {
	if bitsPerKey < 1 {
		bitsPerKey = 10
	}

	// 计算最优 hash 函数个数: k = (m/n) * ln(2)
	numHashes := int(float64(bitsPerKey) * math.Ln2)
	if numHashes < 1 {
		numHashes = 1
	}
	if numHashes > 30 {
		numHashes = 30
	}

	// 位数组大小
	numBits := n * bitsPerKey
	if numBits < 64 {
		numBits = 64
	}
	numBytes := (numBits + 7) / 8

	return &BloomFilter{
		bits:      make([]byte, numBytes),
		numHashes: numHashes,
	}
}

// Add 向布隆过滤器添加一个 key
func (bf *BloomFilter) Add(key []byte) {
	h := fnv.New64a()
	h.Write(key)
	h1 := h.Sum64()

	h.Reset()
	h.Write([]byte{0})
	h.Write(key)
	h2 := h.Sum64()

	for i := 0; i < bf.numHashes; i++ {
		// 双 hash 技术：hash_i = h1 + i*h2
		pos := (h1 + uint64(i)*h2) % uint64(len(bf.bits)*8)
		bf.bits[pos/8] |= 1 << (pos % 8)
	}
}

// MayContain 检查 key 可能存在（false 表示绝对不存在）
func (bf *BloomFilter) MayContain(key []byte) bool {
	if len(bf.bits) == 0 {
		return false
	}

	h := fnv.New64a()
	h.Write(key)
	h1 := h.Sum64()

	h.Reset()
	h.Write([]byte{0})
	h.Write(key)
	h2 := h.Sum64()

	for i := 0; i < bf.numHashes; i++ {
		pos := (h1 + uint64(i)*h2) % uint64(len(bf.bits)*8)
		if bf.bits[pos/8]&(1<<(pos%8)) == 0 {
			return false
		}
	}
	return true
}

// Bytes 序列化为字节数组（用于写入 SSTable）
func (bf *BloomFilter) Bytes() []byte {
	return bf.bits
}

// LoadBloomFilter 从字节数组加载布隆过滤器
func LoadBloomFilter(data []byte, numHashes int) *BloomFilter {
	if numHashes < 1 {
		numHashes = 1
	}
	return &BloomFilter{
		bits:      data,
		numHashes: numHashes,
	}
}

// NumHashes 返回 hash 函数个数
func (bf *BloomFilter) NumHashes() int {
	return bf.numHashes
}

// Size 返回位数组大小（字节数）
func (bf *BloomFilter) Size() int {
	return len(bf.bits)
}

// FalsePositiveRate 估算误判率
func (bf *BloomFilter) FalsePositiveRate(estimatedKeys int) float64 {
	if len(bf.bits) == 0 || estimatedKeys == 0 {
		return 1.0
	}
	m := float64(len(bf.bits) * 8)
	n := float64(estimatedKeys)
	k := float64(bf.numHashes)
	// P ≈ (1 - e^(-kn/m))^k
	return math.Pow(1-math.Exp(-k*n/m), k)
}
