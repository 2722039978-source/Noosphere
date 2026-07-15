// Package index — 倒排索引 + BM25 关键词检索
//
// 用途:
//   - 全文关键词搜索
//   - 与向量检索组合实现混合检索
//   - BM25 评分是信息检索领域的经典算法
//
// BM25 公式:
//
//	score(D,Q) = Σ IDF(qi) * TF(qi,D) * (k1+1) / (TF(qi,D) + k1*(1-b+b*|D|/avgDL))
//
//	IDF(qi) = log((N - n(qi) + 0.5) / (n(qi) + 0.5) + 1)
package index

import (
	"sort"
	"strings"
	"sync"
	"unicode"
)

// InvertedIndex 倒排索引
type InvertedIndex struct {
	mu       sync.RWMutex
	postings map[string][]PostingEntry // term → 文档列表
	docs     map[string]*DocInfo       // docID → 文档信息
	totalLen int                       // 所有文档总词数
}

// PostingEntry 倒排列表中的一个条目
type PostingEntry struct {
	DocID     string
	TermFreq  int     // 该词在此文档中出现次数
	Positions []int   // 词的位置（用于短语搜索）
}

// DocInfo 文档信息
type DocInfo struct {
	ID      string
	Len     int      // 文档词数
	Terms   []string // 文档包含的词
}

// NewInvertedIndex 创建倒排索引
func NewInvertedIndex() *InvertedIndex {
	return &InvertedIndex{
		postings: make(map[string][]PostingEntry),
		docs:     make(map[string]*DocInfo),
	}
}

// Index 索引文档
func (idx *InvertedIndex) Index(docID string, text string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// 删除旧索引（如果存在）
	if oldDoc, ok := idx.docs[docID]; ok {
		idx.removeDoc(docID, oldDoc)
	}

	tokens := tokenize(text)
	if len(tokens) == 0 {
		return
	}

	// 统计词频
	termFreq := make(map[string]int)
	var termOrder []string

	for _, token := range tokens {
		if _, exists := termFreq[token]; !exists {
			termOrder = append(termOrder, token)
		}
		termFreq[token]++
	}

	// 构建倒排
	for term, freq := range termFreq {
		entry := PostingEntry{
			DocID:    docID,
			TermFreq: freq,
		}
		idx.postings[term] = append(idx.postings[term], entry)
	}

	// 记录文档信息
	idx.docs[docID] = &DocInfo{
		ID:    docID,
		Len:   len(tokens),
		Terms: termOrder,
	}
	idx.totalLen += len(tokens)
}

// Remove 从索引中移除文档
func (idx *InvertedIndex) Remove(docID string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if doc, ok := idx.docs[docID]; ok {
		idx.removeDoc(docID, doc)
	}
}

func (idx *InvertedIndex) removeDoc(docID string, doc *DocInfo) {
	for _, term := range doc.Terms {
		entries := idx.postings[term]
		for i, entry := range entries {
			if entry.DocID == docID {
				// 移除该条目
				idx.postings[term] = append(entries[:i], entries[i+1:]...)
				if len(idx.postings[term]) == 0 {
					delete(idx.postings, term)
				}
				break
			}
		}
	}
	idx.totalLen -= doc.Len
	delete(idx.docs, docID)
}

// Search BM25 搜索
func (idx *InvertedIndex) Search(query string, topK int) []KeywordHit {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	tokens := tokenize(query)
	if len(tokens) == 0 || len(idx.docs) == 0 {
		return nil
	}

	// BM25 参数
	k1 := 1.2
	b := 0.75
	avgDL := float64(idx.totalLen) / float64(len(idx.docs))
	N := float64(len(idx.docs))

	// 计算每个文档的 BM25 分数
	scores := make(map[string]float64)

	// 统计 query 中的词频
	queryTF := make(map[string]int)
	for _, t := range tokens {
		queryTF[t]++
	}

	for term, qtf := range queryTF {
		entries, ok := idx.postings[term]
		if !ok {
			continue
		}

		// IDF
		nq := float64(len(entries))
		idf := idfBM25(N, nq)

		for _, entry := range entries {
			doc := idx.docs[entry.DocID]
			if doc == nil {
				continue
			}

			// TF 部分
			tf := float64(entry.TermFreq)
			docLen := float64(doc.Len)

			numerator := tf * (k1 + 1)
			denominator := tf + k1*(1-b+b*docLen/avgDL)
			tfScore := numerator / denominator

			// Query TF 增强
			scores[entry.DocID] += idf * tfScore * float64(qtf)
		}
	}

	// 排序
	hits := make([]KeywordHit, 0, len(scores))
	for docID, score := range scores {
		hits = append(hits, KeywordHit{
			DocID: docID,
			Score: score,
		})
	}

	sort.Slice(hits, func(i, j int) bool {
		return hits[i].Score > hits[j].Score
	})

	if len(hits) > topK {
		hits = hits[:topK]
	}

	return hits
}

// KeywordHit 关键词搜索结果
type KeywordHit struct {
	DocID string
	Score float64
}

// DocCount 返回索引中的文档数
func (idx *InvertedIndex) DocCount() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.docs)
}

// AvgDocLen 返回平均文档长度
func (idx *InvertedIndex) AvgDocLen() float64 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	if len(idx.docs) == 0 {
		return 0
	}
	return float64(idx.totalLen) / float64(len(idx.docs))
}

// ─── IDF 计算 ───

func idfBM25(N, nq float64) float64 {
	if nq == 0 {
		return 0
	}
	return (N - nq + 0.5) / (nq + 0.5)
}

// ─── 分词器 ───

// tokenize 简单的中英文混合分词
func tokenize(text string) []string {
	text = strings.ToLower(text)
	var tokens []string
	var current []rune

	flush := func() {
		if len(current) > 0 {
			tokens = append(tokens, string(current))
			current = current[:0]
		}
	}

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current = append(current, r)
		} else if unicode.Is(unicode.Han, r) {
			// 中文字符：flush 英文 buffer, 每个中文字单独成词
			flush()
			tokens = append(tokens, string(r))
		} else {
			// 分隔符：flush buffer
			flush()
		}
	}
	flush()

	// 去停用词 + 最短长度过滤
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "can": true, "shall": true,
		"to": true, "of": true, "in": true, "for": true, "on": true,
		"with": true, "at": true, "by": true, "from": true, "as": true,
		"and": true, "or": true, "not": true, "but": true, "if": true,
		"it": true, "its": true, "this": true, "that": true, "these": true,
		"的": true, "了": true, "在": true, "是": true, "我": true,
		"有": true, "和": true, "就": true, "不": true, "人": true,
		"都": true, "一": true, "一个": true, "上": true, "也": true,
		"很": true, "到": true, "说": true, "要": true, "去": true,
	}

	filtered := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if !stopWords[t] && len(t) > 0 {
			filtered = append(filtered, t)
		}
	}

	return filtered
}
