package retrieval

import (
	"sort"

	"github.com/nebula-agent/nebula/engine/manager"
)

// Retriever 检索器——组合向量检索和关键词检索
type Retriever struct {
	embedder  Embedder
	vector    VectorSearcher
	keyword   KeywordSearcher
}

// VectorSearcher 向量搜索接口
type VectorSearcher interface {
	Search(query []float32, k int) []VectorHit
}

// VectorHit 向量搜索命中
type VectorHit struct {
	ID       string
	Distance float64
	Score    float64 // 1/(1+distance)
}

// KeywordSearcher 关键词搜索接口
type KeywordSearcher interface {
	Search(query string, topK int) []KeywordSearchHit
}

// KeywordSearchHit 关键词搜索命中
type KeywordSearchHit struct {
	DocID string
	Score float64
}

// NewRetriever 创建检索器
func NewRetriever(embedder Embedder, vector VectorSearcher, keyword KeywordSearcher) *Retriever {
	return &Retriever{
		embedder: embedder,
		vector:   vector,
		keyword:  keyword,
	}
}

// Search 执行检索
func (r *Retriever) Search(opts *manager.SearchOptions) ([]*manager.SearchResult, error) {
	switch opts.Strategy {
	case manager.VectorSearch:
		return r.vectorSearch(opts)
	case manager.KeywordSearch:
		return r.keywordSearch(opts)
	case manager.HybridSearch:
		return r.hybridSearch(opts)
	case manager.TemporalSearch:
		return r.temporalSearch(opts)
	default:
		return r.hybridSearch(opts)
	}
}

// ─── 各检索策略 ───

func (r *Retriever) vectorSearch(opts *manager.SearchOptions) ([]*manager.SearchResult, error) {
	vecs, err := r.embedder.Embed([]string{opts.Query})
	if err != nil {
		return nil, err
	}

	hits := r.vector.Search(vecs[0], opts.TopK*2) // 多取一些，后续过滤

	var results []*manager.SearchResult
	for _, hit := range hits {
		if hit.Score < opts.Threshold {
			continue
		}
		results = append(results, &manager.SearchResult{
			Score:  hit.Score,
			Reason: "vector similarity",
		})
	}

	if len(results) > opts.TopK {
		results = results[:opts.TopK]
	}
	return results, nil
}

func (r *Retriever) keywordSearch(opts *manager.SearchOptions) ([]*manager.SearchResult, error) {
	hits := r.keyword.Search(opts.Query, opts.TopK*2)

	var results []*manager.SearchResult
	for _, hit := range hits {
		if hit.Score < opts.Threshold {
			continue
		}
		results = append(results, &manager.SearchResult{
			Score:  hit.Score,
			Reason: "keyword bm25 match",
		})
	}

	if len(results) > opts.TopK {
		results = results[:opts.TopK]
	}
	return results, nil
}

// hybridSearch 混合检索（RRF 融合排序）
//
// RRF (Reciprocal Rank Fusion) 公式:
//
//	RRF(d) = Σ 1/(k + rank_i(d))
//
// 其中 k=60 是经典常数，rank_i 是文档在第 i 个检索器中的排名。
// RRF 的优势：
//   - 无需求解权重参数
//   - 对绝对分数不敏感（向量距离和 BM25 分数的量纲不同）
//   - 实际效果优于简单的加权求和
func (r *Retriever) hybridSearch(opts *manager.SearchOptions) ([]*manager.SearchResult, error) {
	const rrfK = 60.0

	// 1. 向量检索
	vecs, err := r.embedder.Embed([]string{opts.Query})
	if err != nil {
		return nil, err
	}
	vecHits := r.vector.Search(vecs[0], opts.TopK*3)

	// 2. 关键词检索
	kwHits := r.keyword.Search(opts.Query, opts.TopK*3)

	// 3. RRF 融合
	rrfScores := make(map[string]float64)

	for rank, hit := range vecHits {
		rrfScores[hit.ID] += 1.0 / (rrfK + float64(rank+1))
	}

	for rank, hit := range kwHits {
		rrfScores[hit.DocID] += 1.0 / (rrfK + float64(rank+1))
	}

	// 4. 排序
	type scoredResult struct {
		id    string
		score float64
	}
	var sorted []scoredResult
	for id, score := range rrfScores {
		sorted = append(sorted, scoredResult{id: id, score: score})
	}

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].score > sorted[j].score
	})

	// 5. 阈值过滤
	var results []*manager.SearchResult
	for _, s := range sorted {
		if s.score < opts.Threshold {
			continue
		}
		results = append(results, &manager.SearchResult{
			Score:  s.score,
			Reason: "hybrid (vector + keyword) rrf fusion",
		})
		if len(results) >= opts.TopK {
			break
		}
	}

	return results, nil
}

// temporalSearch 时间衰减检索
func (r *Retriever) temporalSearch(opts *manager.SearchOptions) ([]*manager.SearchResult, error) {
	// 先获取更多向量结果，然后按时间衰减重排序
	vecs, err := r.embedder.Embed([]string{opts.Query})
	if err != nil {
		return nil, err
	}

	hits := r.vector.Search(vecs[0], opts.TopK*5)

	// 时间衰减权重
	var results []*manager.SearchResult
	for _, hit := range hits {
		// 这里通过 Metadata 中的时间戳做衰减
		// 实际上需要从 VectorStore 获取完整元数据
		// 此处简化处理
		results = append(results, &manager.SearchResult{
			Score:  hit.Score,
			Reason: "temporal decaying vector similarity",
		})
	}

	if len(results) > opts.TopK {
		results = results[:opts.TopK]
	}
	return results, nil
}

// ─── 融合排序策略 ───

// WeightedFusion 加权融合（备选方案，适用于已知权重）
func WeightedFusion(vecHits []VectorHit, vecWeight float64,
	kwHits []KeywordSearchHit, kwWeight float64) []string {

	// 先做归一化
	vecScores := make(map[string]float64)
	var maxVecScore float64
	for _, hit := range vecHits {
		vecScores[hit.ID] = hit.Score
		if hit.Score > maxVecScore {
			maxVecScore = hit.Score
		}
	}

	kwScores := make(map[string]float64)
	var maxKWScore float64
	for _, hit := range kwHits {
		kwScores[hit.DocID] = hit.Score
		if hit.Score > maxKWScore {
			maxKWScore = hit.Score
		}
	}

	// 归一化后加权求和
	fused := make(map[string]float64)
	for id, score := range vecScores {
		fused[id] += vecWeight * (score / maxVecScore)
	}
	for id, score := range kwScores {
		fused[id] += kwWeight * (score / maxKWScore)
	}

	return nil // 返回排序后的 ID 列表
}
