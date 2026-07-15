// Package memory — Nebula Memory 集成
//
// 将故障记录和处理方案持久化到 Nebula 记忆引擎。
// 使用情景记忆存储完整故障记录，语义记忆存储可复用的解决方案知识。
// 后续同类问题可通过混合检索快速匹配历史案例。
package memory

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/devops-agent/agent"
	"github.com/nebula-agent/nebula/engine"
	nebulaMgr "github.com/nebula-agent/nebula/engine/manager"
)

// FaultStore 故障记录存储——基于 Nebula 记忆引擎
type FaultStore struct {
	engine    *engine.Engine
	sessionID string
}

// NewFaultStore 创建故障记录存储
func NewFaultStore(eng *engine.Engine, sessionID string) *FaultStore {
	if sessionID == "" {
		sessionID = "devops-agent"
	}
	return &FaultStore{engine: eng, sessionID: sessionID}
}

// SaveFault 保存故障记录到 Nebula Memory
//
// 双写策略：
//   - 情景记忆：完整的故障 JSON，tag 化方便检索
//   - 语义记忆（已解决时）：提炼的解决方案知识，可跨会话复用
func (fs *FaultStore) SaveFault(record *agent.FaultRecord) error {
	if record == nil {
		return fmt.Errorf("fault record is nil")
	}

	sess := fs.engine.Session(fs.sessionID)

	// 1. 情景记忆——完整记录
	faultJSON, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("序列化故障记录失败: %w", err)
	}

	tags := append(record.Tags, "fault", record.Severity, record.ServerName)
	episodic := nebulaMgr.NewEpisodicMemory(fs.sessionID, string(faultJSON), 0.9, tags)
	episodic.Metadata = map[string]string{
		"fault_id":    record.ID,
		"title":       record.Title,
		"severity":    record.Severity,
		"server":      record.ServerName,
		"resolved":    fmt.Sprintf("%t", record.Resolved),
		"occurred_at": record.OccurredAt.Format(time.RFC3339),
	}

	if err := sess.Remember(episodic); err != nil {
		return fmt.Errorf("保存情景记忆失败: %w", err)
	}

	// 2. 语义记忆——已解决的知识
	if record.Resolved && record.Solution != "" {
		solutionContent := fmt.Sprintf(
			"故障: %s\n根因: %s\n解决方案: %s\n严重度: %s\n标签: %v",
			record.Title, record.RootCause, record.Solution, record.Severity, record.Tags,
		)
		semantic := nebulaMgr.NewSemanticMemory(fs.sessionID, solutionContent, []string{record.ID})
		semantic.Importance = 0.95
		semantic.Tags = append(tags, "solution", "knowledge")
		semantic.Metadata = map[string]string{"fault_id": record.ID, "severity": record.Severity, "category": "solution"}
		if err := sess.Remember(semantic); err != nil {
			log.Printf("[FaultStore] 保存语义记忆失败(非致命): %v", err)
		}
	}

	log.Printf("[FaultStore] ✅ 故障记录已保存: %s (resolved=%t)", record.ID, record.Resolved)
	return nil
}

// SearchSimilarFaults 搜索历史相似故障
//
// 使用 Nebula 混合检索（RRF 融合向量+关键词），
// 在已有故障记录中查找与当前问题最相关的历史案例。
func (fs *FaultStore) SearchSimilarFaults(query string, limit int) ([]*agent.FaultRecord, error) {
	if limit <= 0 {
		limit = 5
	}
	sess := fs.engine.Session(fs.sessionID)
	results, err := sess.Reminisce(&nebulaMgr.SearchOptions{
		Query:       query,
		TopK:        limit,
		Strategy:    nebulaMgr.HybridSearch,
		MemoryTypes: []nebulaMgr.MemoryType{nebulaMgr.EpisodicMemory},
		Tags:        []string{"fault"},
		Threshold:   0.3,
	})
	if err != nil {
		return nil, fmt.Errorf("搜索历史故障失败: %w", err)
	}

	var faults []*agent.FaultRecord
	for _, r := range results {
		if r.Memory == nil {
			continue
		}
		var record agent.FaultRecord
		if err := json.Unmarshal([]byte(r.Memory.Content), &record); err != nil {
			continue
		}
		faults = append(faults, &record)
	}
	return faults, nil
}

// SearchSolutions 搜索历史解决方案（语义记忆）
func (fs *FaultStore) SearchSolutions(query string, limit int) ([]*nebulaMgr.SearchResult, error) {
	if limit <= 0 {
		limit = 5
	}
	sess := fs.engine.Session(fs.sessionID)
	return sess.Reminisce(&nebulaMgr.SearchOptions{
		Query:       query + " 解决方案",
		TopK:        limit,
		Strategy:    nebulaMgr.HybridSearch,
		MemoryTypes: []nebulaMgr.MemoryType{nebulaMgr.SemanticMemory},
		Tags:        []string{"solution"},
		Threshold:   0.3,
	})
}

// MarkResolved 标记故障已解决并更新方案
func (fs *FaultStore) MarkResolved(faultID string, solution string) error {
	sess := fs.engine.Session(fs.sessionID)
	results, err := sess.Reminisce(&nebulaMgr.SearchOptions{
		Query: faultID, TopK: 1, Strategy: nebulaMgr.KeywordSearch,
		MemoryTypes: []nebulaMgr.MemoryType{nebulaMgr.EpisodicMemory},
		Tags:        []string{"fault"}, Threshold: 0,
	})
	if err != nil || len(results) == 0 {
		return fmt.Errorf("未找到故障记录: %s", faultID)
	}

	var record agent.FaultRecord
	if err := json.Unmarshal([]byte(results[0].Memory.Content), &record); err != nil {
		return fmt.Errorf("解析故障记录失败: %w", err)
	}
	record.Resolved = true
	record.ResolvedAt = time.Now()
	record.Solution = solution
	return fs.SaveFault(&record)
}

// GetFaultStats 获取故障统计
func (fs *FaultStore) GetFaultStats() (map[string]int, error) {
	sess := fs.engine.Session(fs.sessionID)
	results, err := sess.Reminisce(&nebulaMgr.SearchOptions{
		Query: "", TopK: 100, Strategy: nebulaMgr.KeywordSearch,
		MemoryTypes: []nebulaMgr.MemoryType{nebulaMgr.EpisodicMemory},
		Tags:        []string{"fault"}, Threshold: 0,
	})
	if err != nil {
		return nil, err
	}

	stats := map[string]int{"total": len(results), "critical": 0, "high": 0, "medium": 0, "low": 0, "resolved": 0, "unresolved": 0}
	for _, r := range results {
		if r.Memory == nil || r.Memory.Metadata == nil {
			continue
		}
		if sev, ok := r.Memory.Metadata["severity"]; ok {
			stats[sev]++
		}
		if r.Memory.Metadata["resolved"] == "true" {
			stats["resolved"]++
		} else {
			stats["unresolved"]++
		}
	}
	return stats, nil
}
