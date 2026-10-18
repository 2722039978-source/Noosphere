package vector

import (
	"math"
	"math/rand"
	"sort"
	"strings"
	"sync"
)

type Node struct {
	ID    string
	Vec   []float32
	Edges map[string]float32
}

type HNSW struct {
	mu       sync.RWMutex
	nodes    map[string]*Node
	entryID  string
	M        int
	EfSearch int
	dim      int
}

func NewHNSW(M, efSearch int) *HNSW {
	if M <= 0 {
		M = 16
	}
	if efSearch <= 0 {
		efSearch = 50
	}
	return &HNSW{
		nodes:    make(map[string]*Node),
		M:        M,
		EfSearch: efSearch,
	}
}

func (h *HNSW) Insert(id string, vec []float32) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(vec) == 0 || (h.dim != 0 && len(vec) != h.dim) {
		return
	}
	if h.dim == 0 {
		h.dim = len(vec)
	}
	node := &Node{ID: id, Vec: vec, Edges: make(map[string]float32)}
	if len(h.nodes) == 0 {
		h.entryID = id
		h.nodes[id] = node
		return
	}
	neighbors := h.searchLayer(vec, h.EfSearch)
	for _, n := range neighbors {
		dist := cosineDist(vec, h.nodes[n].Vec)
		node.Edges[n] = dist
		if len(h.nodes[n].Edges) < h.M*2 {
			h.nodes[n].Edges[id] = dist
		}
	}
	h.nodes[id] = node
}

func (h *HNSW) Search(vec []float32, topK int) []SearchResult {
	return h.SearchScoped(vec, topK, "")
}

// Scoped searches score only the selected namespace. Global ANN candidates
// cannot guarantee recall after filtering, especially for a small project.
func (h *HNSW) SearchScoped(vec []float32, topK int, prefix string) []SearchResult {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.nodes) == 0 || len(vec) != h.dim || topK <= 0 {
		return nil
	}
	neighbors := h.searchLayer(vec, h.EfSearch)
	if prefix != "" {
		neighbors = nil
		for id := range h.nodes {
			if strings.HasPrefix(id, prefix) {
				neighbors = append(neighbors, id)
			}
		}
	}
	results := make([]SearchResult, 0)
	for _, n := range neighbors {
		node := h.nodes[n]
		dist := cosineDist(vec, node.Vec)
		score := 1.0 / (1.0 + float64(dist))
		results = append(results, SearchResult{Key: n, Score: score})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if len(results) > topK {
		results = results[:topK]
	}
	return results
}

// searchLayer performs best-first graph traversal: repeatedly expand the
// closest unexpanded candidate, adding its unvisited neighbors. The previous
// version pre-marked the entry as visited and never expanded its edges, so
// every search degenerated to returning only the entry node.
func (h *HNSW) searchLayer(vec []float32, ef int) []string {
	if h.entryID == "" || h.nodes[h.entryID] == nil {
		return nil
	}
	type cand struct {
		id   string
		dist float32
	}
	visited := map[string]bool{h.entryID: true}
	expanded := make(map[string]bool)
	heap := []cand{{id: h.entryID, dist: cosineDist(vec, h.nodes[h.entryID].Vec)}}

	for len(expanded) < ef {
		// find the closest unexpanded candidate
		bi := -1
		for i, c := range heap {
			if expanded[c.id] {
				continue
			}
			if bi == -1 || c.dist < heap[bi].dist {
				bi = i
			}
		}
		if bi == -1 {
			break // graph fully explored
		}
		cur := heap[bi]
		expanded[cur.id] = true
		for nb := range h.nodes[cur.id].Edges {
			if !visited[nb] && h.nodes[nb] != nil {
				visited[nb] = true
				heap = append(heap, cand{id: nb, dist: cosineDist(vec, h.nodes[nb].Vec)})
			}
		}
	}

	sort.Slice(heap, func(i, j int) bool { return heap[i].dist < heap[j].dist })
	if len(heap) > ef {
		heap = heap[:ef]
	}
	ids := make([]string, len(heap))
	for i, c := range heap {
		ids[i] = c.id
	}
	return ids
}

type SearchResult struct {
	Key   string  `json:"key"`
	Score float64 `json:"score"`
}

func cosineDist(a, b []float32) float32 {
	if len(a) == 0 || len(a) != len(b) {
		return 1
	}
	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 1.0
	}
	return 1.0 - dot/float32(math.Sqrt(float64(normA)*float64(normB)))
}

// Delete removes graph references as well as the node. Rebuild connectivity
// from live vectors so deleting an entry/bridge cannot strand other records.
func (h *HNSW) Delete(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.nodes[id]; !ok {
		return
	}
	delete(h.nodes, id)
	fresh := NewHNSW(h.M, h.EfSearch)
	keys := make([]string, 0, len(h.nodes))
	for key := range h.nodes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fresh.Insert(key, h.nodes[key].Vec)
	}
	h.nodes, h.entryID, h.dim = fresh.nodes, fresh.entryID, fresh.dim
}

func (h *HNSW) Stats() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return map[string]interface{}{
		"nodes": len(h.nodes),
		"dim":   h.dim,
	}
}

// NodeData represents a node for export/import.
type NodeData struct {
	ID  string    `json:"id"`
	Vec []float32 `json:"vec"`
}

// ExportNodes returns all nodes for serialization.
func (h *HNSW) ExportNodes() []NodeData {
	h.mu.RLock()
	defer h.mu.RUnlock()
	nodes := make([]NodeData, 0, len(h.nodes))
	for id, n := range h.nodes {
		nodes = append(nodes, NodeData{ID: id, Vec: n.Vec})
	}
	return nodes
}

// ImportNodes bulk-inserts nodes from deserialized data.
func (h *HNSW) ImportNodes(nodes []NodeData) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, nd := range nodes {
		if len(nd.Vec) > 0 {
			h.dim = len(nd.Vec)
		}
		node := &Node{ID: nd.ID, Vec: nd.Vec, Edges: make(map[string]float32)}
		if len(h.nodes) == 0 {
			h.entryID = nd.ID
			h.nodes[nd.ID] = node
			continue
		}
		neighbors := h.searchLayer(nd.Vec, h.EfSearch)
		for _, nID := range neighbors {
			dist := cosineDist(nd.Vec, h.nodes[nID].Vec)
			node.Edges[nID] = dist
			if len(h.nodes[nID].Edges) < h.M*2 {
				h.nodes[nID].Edges[nd.ID] = dist
			}
		}
		h.nodes[nd.ID] = node
	}
}

func RandomVec(dim int) []float32 {
	v := make([]float32, dim)
	for i := range v {
		v[i] = rand.Float32()*2 - 1
	}
	return v
}
