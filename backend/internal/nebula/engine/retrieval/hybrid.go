package retrieval

import "sort"

type ScoredDoc struct {
	Key   string  `json:"key"`
	Score float64 `json:"score"`
}

func RRF(bm25Results, vectorResults []ScoredDoc, k int, topK int) []ScoredDoc {
	if k <= 0 {
		k = 60
	}
	scores := make(map[string]float64)
	for i, r := range bm25Results {
		scores[r.Key] += 1.0 / float64(k+i+1)
	}
	for i, r := range vectorResults {
		scores[r.Key] += 1.0 / float64(k+i+1)
	}
	merged := make([]ScoredDoc, 0, len(scores))
	for key, score := range scores {
		merged = append(merged, ScoredDoc{Key: key, Score: score})
	}
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Score > merged[j].Score
	})
	if len(merged) > topK {
		merged = merged[:topK]
	}
	return merged
}
