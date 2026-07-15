package analyzer

import (
	"fmt"
	"sort"
	"strings"

	"github.com/nebula-agent/nebula/engine/manager"
)

// ContextInjector 上下文注入器
//
// 在模型调用前，根据当前任务自动检索相关记忆，
// 拼接成高质量的系统提示后缀，让模型"知道"它应该怎么写代码。
//
// 使用方式:
//
//	injector := NewContextInjector(session, memAPI)
//	ctx := injector.BuildContext(&TaskContext{
//	    Task:     "添加一个用户登录接口",
//	    Language: "Go",
//	})
//	// 将 ctx.PromptSuffix 拼接到 System Prompt 末尾
//	llmCall(messages, ctx.PromptSuffix)
type ContextInjector struct {
	sessionID string
	memAPI    *manager.MemoryAPI
}

// NewContextInjector 创建上下文注入器
func NewContextInjector(sessionID string, memAPI *manager.MemoryAPI) *ContextInjector {
	return &ContextInjector{sessionID: sessionID, memAPI: memAPI}
}

// BuildContext 根据任务构建注入上下文
func (ci *ContextInjector) BuildContext(task *TaskContext) (*InjectedContext, error) {
	ictx := &InjectedContext{}

	// 1. 检索项目概况
	ci.injectProjectOverview(ictx, task)

	// 2. 检索技术栈
	ci.injectTechStack(ictx, task)

	// 3. 检索相关代码模式
	ci.injectRelevantPatterns(ictx, task)

	// 4. 检索风格偏好
	ci.injectStyle(ictx, task)

	// 5. 检索注意事项
	ci.injectGotchas(ictx, task)

	// 6. 检索与当前任务最相关的记忆
	ci.injectRelevantMemories(ictx, task)

	// 7. 拼接最终的 Prompt 后缀
	ictx.PromptSuffix = ci.buildPromptSuffix(ictx)

	return ictx, nil
}

// ─── 各维度的检索 ───

func (ci *ContextInjector) injectProjectOverview(ictx *InjectedContext, task *TaskContext) {
	opts := &manager.SearchOptions{
		Query:      task.Task + " 项目概况 技术栈",
		TopK:       3,
		Strategy:   manager.HybridSearch,
		MemoryTypes: []manager.MemoryType{manager.SemanticMemory},
		Tags:       []string{"项目概况"},
	}
	results, err := ci.memAPI.Reminisce(ci.sessionID, opts)
	if err != nil || len(results) == 0 { return }

	if len(results) > 0 && results[0].Memory != nil {
		ictx.ProjectOverview = results[0].Memory.Content
	}
}

func (ci *ContextInjector) injectTechStack(ictx *InjectedContext, task *TaskContext) {
	opts := &manager.SearchOptions{
		Query:      "技术栈 框架 库 " + task.Language,
		TopK:       3,
		Strategy:   manager.KeywordSearch,
		MemoryTypes: []manager.MemoryType{manager.SemanticMemory},
	}
	results, err := ci.memAPI.Reminisce(ci.sessionID, opts)
	if err != nil || len(results) == 0 { return }

	var parts []string
	for _, r := range results {
		if r.Memory != nil && r.Score > 0.3 {
			parts = append(parts, r.Memory.Content)
		}
	}
	ictx.TechStack = strings.Join(parts, "; ")
}

func (ci *ContextInjector) injectRelevantPatterns(ictx *InjectedContext, task *TaskContext) {
	// 根据任务提取关键词，搜索匹配的代码模式
	keywords := extractKeywords(task.Task)

	for _, kw := range keywords {
		opts := &manager.SearchOptions{
			Query:      kw + " 模式 代码",
			TopK:       3,
			Strategy:   manager.HybridSearch,
			MemoryTypes: []manager.MemoryType{manager.SemanticMemory},
			Tags:       []string{"模式"},
			Threshold:  0.3,
		}
		results, err := ci.memAPI.Reminisce(ci.sessionID, opts)
		if err != nil { continue }

		for _, r := range results {
			if r.Memory != nil {
				// 解析 CodePattern
				cp := CodePattern{
					Category:    r.Memory.Tags[0],
					Description: r.Memory.Content,
				}
				ictx.RelevantPatterns = append(ictx.RelevantPatterns, cp)
			}
		}
	}

	// 去重
	seen := make(map[string]bool)
	var unique []CodePattern
	for _, p := range ictx.RelevantPatterns {
		if !seen[p.Description] {
			seen[p.Description] = true
			unique = append(unique, p)
		}
	}
	ictx.RelevantPatterns = unique
}

func (ci *ContextInjector) injectStyle(ictx *InjectedContext, task *TaskContext) {
	opts := &manager.SearchOptions{
		Query:      "命名规范 错误处理 代码组织 编码风格",
		TopK:       5,
		Strategy:   manager.HybridSearch,
		MemoryTypes: []manager.MemoryType{manager.SemanticMemory},
		Tags:       []string{"风格"},
		Threshold:  0.3,
	}
	results, err := ci.memAPI.Reminisce(ci.sessionID, opts)
	if err != nil { return }

	for _, r := range results {
		if r.Memory != nil {
			ictx.StyleNotes = append(ictx.StyleNotes, r.Memory.Content)
		}
	}
}

func (ci *ContextInjector) injectGotchas(ictx *InjectedContext, task *TaskContext) {
	opts := &manager.SearchOptions{
		Query:      task.Task + " 注意事项 坑 不要",
		TopK:       5,
		Strategy:   manager.HybridSearch,
		MemoryTypes: []manager.MemoryType{manager.SemanticMemory},
		Tags:       []string{"注意"},
		Threshold:  0.3,
	}
	results, err := ci.memAPI.Reminisce(ci.sessionID, opts)
	if err != nil { return }

	for _, r := range results {
		if r.Memory != nil {
			ictx.Gotchas = append(ictx.Gotchas, r.Memory.Content)
		}
	}
}

func (ci *ContextInjector) injectRelevantMemories(ictx *InjectedContext, task *TaskContext) {
	opts := &manager.SearchOptions{
		Query:      task.Task,
		TopK:       5,
		Strategy:   manager.HybridSearch,
		MemoryTypes: []manager.MemoryType{manager.EpisodicMemory, manager.SemanticMemory},
		Threshold:  0.4,
	}
	results, err := ci.memAPI.Reminisce(ci.sessionID, opts)
	if err != nil { return }

	for _, r := range results {
		if r.Memory != nil {
			cm := CodeMemory{
				ID:       r.Memory.ID,
				Content:  r.Memory.Content,
				Category: r.Memory.Tags[0],
			}
			ictx.RelevantMemories = append(ictx.RelevantMemories, cm)
		}
	}
}

// ─── Prompt 拼接 ───

func (ci *ContextInjector) buildPromptSuffix(ictx *InjectedContext) string {
	var parts []string

	parts = append(parts, "\n## 项目上下文（由 Nebula 记忆引擎注入）\n")

	if ictx.ProjectOverview != "" {
		parts = append(parts, fmt.Sprintf("### 项目概况\n%s\n", ictx.ProjectOverview))
	}

	if ictx.TechStack != "" {
		parts = append(parts, fmt.Sprintf("### 技术栈\n%s\n", ictx.TechStack))
	}

	if len(ictx.StyleNotes) > 0 {
		parts = append(parts, "### 编码风格偏好\n请严格遵循以下个人编码风格：")
		for i, note := range ictx.StyleNotes {
			parts = append(parts, fmt.Sprintf("%d. %s", i+1, note))
		}
		parts = append(parts, "")
	}

	if len(ictx.RelevantPatterns) > 0 {
		parts = append(parts, "### 相关代码模式\n以下是项目中的惯用模式，请保持一致：")
		for _, p := range ictx.RelevantPatterns {
			parts = append(parts, fmt.Sprintf("- [%s] %s", p.Category, p.Description))
		}
		parts = append(parts, "")
	}

	if len(ictx.Gotchas) > 0 {
		parts = append(parts, "### ⚠ 特别注意\n以下是项目中需要注意的坑，请避免：")
		for _, g := range ictx.Gotchas {
			parts = append(parts, fmt.Sprintf("- ⚠ %s", g))
		}
		parts = append(parts, "")
	}

	if len(ictx.RelevantMemories) > 0 {
		parts = append(parts, "### 相关上下文\n")
		for _, m := range ictx.RelevantMemories {
			parts = append(parts, fmt.Sprintf("- %s", m.Content))
		}
		parts = append(parts, "")
	}

	parts = append(parts, "---\n请基于以上项目上下文生成代码。务必遵循项目的命名规范、错误处理方式和代码组织结构。")

	return strings.Join(parts, "\n")
}

// ─── 关键词提取 ───

// extractKeywords 从任务描述中提取关键词
func extractKeywords(task string) []string {
	// 中文分词：按常见分隔符切分
	words := strings.FieldsFunc(task, func(r rune) bool {
		return r == ' ' || r == '，' || r == ',' || r == '。' || r == '；' || r == ';' || r == '、'
	})

	// 过滤太短的词
	var keywords []string
	for _, w := range words {
		w = strings.TrimSpace(w)
		if len([]rune(w)) >= 2 && len(w) > 1 {
			keywords = append(keywords, w)
		}
	}

	// 排序（更长的词更可能是有意义的关键词）
	sort.Slice(keywords, func(i, j int) bool {
		return len([]rune(keywords[i])) > len([]rune(keywords[j]))
	})

	if len(keywords) > 5 {
		keywords = keywords[:5]
	}
	return keywords
}

// ─── 快速 Prompt 生成（便捷方法）───

// QuickPrompt 从会话快速生成上下文 Prompt
// 适用于简单场景：只需传入会话和任务，自动检索并构建 Prompt
func (ci *ContextInjector) QuickPrompt(task string, language string) string {
	ctx, err := ci.BuildContext(&TaskContext{Task: task, Language: language})
	if err != nil || ctx == nil {
		return ""
	}
	return ctx.PromptSuffix
}

// Summarize 生成会话的知识摘要（供人类阅读）
func (ci *ContextInjector) Summarize() string {
	var b strings.Builder

	b.WriteString("## Nebula 记忆引擎 — 知识摘要\n\n")

	// 项目概况
	opts := &manager.SearchOptions{
		Query: "项目概况", TopK: 3, Strategy: manager.HybridSearch,
		MemoryTypes: []manager.MemoryType{manager.SemanticMemory},
	}
	results, _ := ci.memAPI.Reminisce(ci.sessionID, opts)
	if len(results) > 0 {
		b.WriteString("### 📋 项目\n")
		for _, r := range results {
			b.WriteString(fmt.Sprintf("- %s\n", r.Memory.Content))
		}
		b.WriteString("\n")
	}

	// 风格
	opts = &manager.SearchOptions{
		Query: "编码风格", TopK: 5, Strategy: manager.HybridSearch,
		MemoryTypes: []manager.MemoryType{manager.SemanticMemory}, Tags: []string{"风格"},
	}
	results, _ = ci.memAPI.Reminisce(ci.sessionID, opts)
	if len(results) > 0 {
		b.WriteString("### 🎨 编码风格\n")
		for _, r := range results {
			b.WriteString(fmt.Sprintf("- %s\n", r.Memory.Content))
		}
		b.WriteString("\n")
	}

	// 注意事项
	opts = &manager.SearchOptions{
		Query: "注意事项", TopK: 5, Strategy: manager.HybridSearch,
		MemoryTypes: []manager.MemoryType{manager.SemanticMemory}, Tags: []string{"注意"},
	}
	results, _ = ci.memAPI.Reminisce(ci.sessionID, opts)
	if len(results) > 0 {
		b.WriteString("### ⚠ 注意事项\n")
		for _, r := range results {
			b.WriteString(fmt.Sprintf("- ⚠ %s\n", r.Memory.Content))
		}
	}

	return b.String()
}
