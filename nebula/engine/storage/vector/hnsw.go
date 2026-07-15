// Package vector — HNSW (Hierarchical Navigable Small World) 向量索引
//
// HNSW 是当前最优秀的近似最近邻搜索算法之一，特点:
//   - 构建: O(N log N)，查询: O(log N)
//   - 多层图结构，高层用于快速导航，底层用于精确搜索
//   - 零外部依赖，纯 Go 实现，可交叉编译
//
// 算法原理:
//   1. 每个节点随机分配一个层级（指数分布）
//   2. 查询时从最高层的入口点开始，逐层向下搜索
//   3. 每层使用贪心搜索，维护一个候选集合和已访问集合
//   4. 底层执行精细搜索（efSearch 参数控制精度/速度权衡）
//
// 参考: Malkov & Yashunin, "Efficient and robust approximate nearest neighbor
//       search using Hierarchical Navigable Small World graphs" (2018)
package vector

import (
	"math"
	"math/rand"
	"sync"
)

// HNSW 向量索引
type HNSW struct {
	mu sync.RWMutex

	// 配置
	dim         int     // 向量维度
	M           int     // 每层最大连接数
	efConstruct int     // 构建时的搜索宽度
	efSearch    int     // 查询时的搜索宽度
	maxElements int     // 容量上限
	ml          float64 // 层级分配参数 1/ln(2)

	// 数据
	nodes    []*hnswNode // 所有节点（ID = 数组索引）
	entryID  int         // 入口点 ID
	maxLevel int         // 当前最高层级

	// 删除标记（惰性删除）
	deleted map[int]bool
}

// hnswNode HNSW 节点
type hnswNode struct {
	id        int
	vector    []float32
	layers    []map[int]float32 // layers[level] = {neighbor_id → distance}，用 float32 存距离
	metadata  string            // 关联的元数据 key
	deleted   bool
}

// HNSWConfig HNSW 配置
type HNSWConfig struct {
	Dimension     int
	M             int // 每层连接数，默认 16
	EfConstruct   int // 构建搜索宽度，默认 200
	EfSearch      int // 查询搜索宽度，默认 100
	MaxElements   int // 最大元素数，默认 1000000
}

// NewHNSW 创建 HNSW 索引
func NewHNSW(cfg HNSWConfig) *HNSW {
	if cfg.M < 2 {
		cfg.M = 16
	}
	if cfg.EfConstruct < cfg.M {
		cfg.EfConstruct = cfg.M * 10
	}
	if cfg.EfSearch < cfg.M {
		cfg.EfSearch = cfg.M * 5
	}
	if cfg.MaxElements < 1 {
		cfg.MaxElements = 1_000_000
	}

	return &HNSW{
		dim:         cfg.Dimension,
		M:           cfg.M,
		efConstruct: cfg.EfConstruct,
		efSearch:    cfg.EfSearch,
		maxElements: cfg.MaxElements,
		ml:          1.0 / math.Log(float64(cfg.M)),
		nodes:        make([]*hnswNode, 0, 1024),
		entryID:     -1,
		maxLevel:    -1,
		deleted:     make(map[int]bool),
	}
}

// Add 添加向量
func (h *HNSW) Add(id int, vector []float32, metadata string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(vector) != h.dim {
		return ErrDimensionMismatch
	}

	node := &hnswNode{
		id:       id,
		vector:   make([]float32, h.dim),
		layers:   nil,
		metadata: metadata,
	}
	copy(node.vector, vector)

	h.nodes = append(h.nodes, node)
	idx := len(h.nodes) - 1

	// 随机分配层级
	level := h.randomLevel()
	node.layers = make([]map[int]float32, level+1)
	for l := 0; l <= level; l++ {
		node.layers[l] = make(map[int]float32)
	}

	// 第一个节点
	if h.entryID == -1 {
		h.entryID = idx
		h.maxLevel = level
		return nil
	}

	// 从最高公共层级开始搜索
	ep := h.entryID
	for lc := h.maxLevel; lc > level; lc-- {
		ep = h.searchLayer(node.vector, ep, 1, lc)[0].id
	}

	// 逐层插入
	for lc := level; lc >= 0; lc-- {
		if lc <= h.maxLevel {
			candidates := h.searchLayer(node.vector, ep, h.efConstruct, lc)
			// 选择 M 个最近邻居连接
			m := h.M
			if lc == 0 {
				m = h.M * 2 // 底层可以有 2 倍连接
			}
			neighbors := h.selectNeighbors(candidates, m)

			// 双向连接
			for _, n := range neighbors {
				node.layers[lc][n.id] = n.dist
				if h.nodes[n.id].layers[lc] == nil {
					h.nodes[n.id].layers[lc] = make(map[int]float32)
				}
				h.nodes[n.id].layers[lc][idx] = n.dist

				// 如果邻居连接数超过 M，修剪最远的
				if len(h.nodes[n.id].layers[lc]) > m {
					h.pruneConnections(n.id, lc, m)
				}
			}
			ep = candidates[0].id
		} else {
			// 创建新层级
			if lc > h.maxLevel {
				h.maxLevel = lc
				h.entryID = idx
			}
		}
	}

	return nil
}

// Search 查询 K 个最近邻
func (h *HNSW) Search(query []float32, k int) []SearchHit {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.entryID == -1 || k <= 0 {
		return nil
	}

	if len(query) != h.dim {
		return nil
	}

	ep := h.entryID

	// 从最高层降到第 1 层
	for lc := h.maxLevel; lc > 0; lc-- {
		ep = h.searchLayer(query, ep, 1, lc)[0].id
	}

	// 第 0 层精细搜索
	ef := h.efSearch
	if ef < k {
		ef = k
	}
	candidates := h.searchLayer(query, ep, ef, 0)

	// 取 top K
	if len(candidates) > k {
		candidates = candidates[:k]
	}

	hits := make([]SearchHit, len(candidates))
	for i, c := range candidates {
		hits[i] = SearchHit{
			ID:       c.id,
			Distance: float64(c.dist),
			Score:    1.0 / (1.0 + float64(c.dist)), // 距离 → 相似度
			Metadata: h.nodes[c.id].metadata,
		}
	}

	return hits
}

// Delete 标记删除
func (h *HNSW) Delete(id int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.deleted[id] = true
}

// Size 返回活跃节点数
func (h *HNSW) Size() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.nodes) - len(h.deleted)
}

// ─── 内部方法 ───

// candidate 候选节点
type candidate struct {
	id   int
	dist float32
}

// SearchHit 搜索结果
type SearchHit struct {
	ID       int
	Distance float64
	Score    float64
	Metadata string
}

// distance 计算 L2 距离
func (h *HNSW) distance(a, b []float32) float32 {
	var sum float32
	for i := range a {
		diff := a[i] - b[i]
		sum += diff * diff
	}
	return float32(math.Sqrt(float64(sum)))
}

// randomLevel 随机生成层级（指数分布）
func (h *HNSW) randomLevel() int {
	r := -math.Log(rand.Float64()) * h.ml
	return int(r)
}

// searchLayer 在指定层级搜索最近邻
func (h *HNSW) searchLayer(query []float32, ep int, ef int, level int) []candidate {
	visited := make(map[int]bool)
	visited[ep] = true

	// 候选集（大顶堆 → 小顶堆表示不方便，用 slice + 排序）
	candidates := []candidate{{id: ep, dist: h.distance(query, h.nodes[ep].vector)}}
	// 结果集
	results := []candidate{candidates[0]}

	for {
		// 选择未探索的最近候选
		changed := false

		var closest *candidate
		closestIdx := -1
		for i := range candidates {
			if visited[candidates[i].id] {
				continue
			}
			if closest == nil || candidates[i].dist < closest.dist {
				closest = &candidates[i]
				closestIdx = i
			}
		}

		if closest == nil {
			break
		}
		visited[closest.id] = true

		// 取结果集中最远的距离
		farthestDist := results[len(results)-1].dist

		// 如果候选比结果集中最远的还远，且结果已满 ef，终止
		if closest.dist > farthestDist && len(results) >= ef {
			break
		}

		// 探索邻居
		for neighborID := range h.nodes[closest.id].layers[level] {
			if visited[neighborID] {
				continue
			}
			if h.deleted[neighborID] {
				continue
			}
			visited[neighborID] = true
			dist := h.distance(query, h.nodes[neighborID].vector)
			candidates = append(candidates, candidate{id: neighborID, dist: dist})

			// 插入结果集并保持有序
			results = insertSorted(results, candidate{id: neighborID, dist: dist}, ef)
			changed = true
		}

		// 标记为已探索
		if closestIdx >= 0 {
			// 从候选集中移除
			_ = changed
		}

		if !changed {
			break
		}
	}

	return results
}

// selectNeighbors 从候选集中选择 m 个最佳邻居
func (h *HNSW) selectNeighbors(candidates []candidate, m int) []candidate {
	if len(candidates) <= m {
		return candidates
	}
	return candidates[:m]
}

// pruneConnections 修剪节点的连接（保留距离最近的 m 个）
func (h *HNSW) pruneConnections(nodeID int, level int, m int) {
	conns := h.nodes[nodeID].layers[level]
	if len(conns) <= m {
		return
	}

	// 收集所有连接并排序
	type conn struct {
		id   int
		dist float32
	}
	var list []conn
	for nid, dist := range conns {
		list = append(list, conn{id: nid, dist: dist})
	}

	// 按距离排序，保留最近的 m 个
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].dist < list[i].dist {
				list[i], list[j] = list[j], list[i]
			}
		}
	}

	// 移除多余的
	if len(list) > m {
		for _, c := range list[m:] {
			delete(conns, c.id)
		}
	}
}

// ─── 辅助 ───

// insertSorted 有序插入（按距离升序），保持最多 size 个元素
func insertSorted(slice []candidate, c candidate, maxSize int) []candidate {
	// 二分查找插入位置
	lo, hi := 0, len(slice)
	for lo < hi {
		mid := (lo + hi) / 2
		if slice[mid].dist < c.dist {
			lo = mid + 1
		} else {
			hi = mid
		}
	}

	// 去重：如果 ID 已存在，更新
	for i := range slice {
		if slice[i].id == c.id {
			if c.dist < slice[i].dist {
				slice[i].dist = c.dist
			}
			return slice
		}
	}

	// 插入
	slice = append(slice, candidate{})
	copy(slice[lo+1:], slice[lo:])
	slice[lo] = c

	// 截断
	if len(slice) > maxSize {
		slice = slice[:maxSize]
	}

	return slice
}

// ─── 错误 ───

var ErrDimensionMismatch = &VectorError{"dimension mismatch"}

// VectorError 向量错误
type VectorError struct {
	Msg string
}

func (e *VectorError) Error() string {
	return "vector: " + e.Msg
}
