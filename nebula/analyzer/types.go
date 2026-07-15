// Package analyzer — 代码摄取 + 风格学习 + 上下文注入
//
// 让 Nebula 从"存储引擎"变成真正能指导模型做事的智能记忆系统。
//
// 三大核心能力:
//   1. CodeIngester  — 扫描本地代码库 → 提取结构化记忆存入 Nebula
//   2. StyleLearner  — 识别个人编码特色(命名/库偏好/错误处理等)
//   3. ContextInjector — 查询相关记忆 → 自动注入到模型 Prompt 中
package analyzer

import "time"

// ─── 项目指纹 (Project Fingerprint) ───

// ProjectFingerprint 项目指纹 — 对代码库的完整"理解"
type ProjectFingerprint struct {
	Name        string            `json:"name"`
	Path        string            `json:"path"`
	ScannedAt   time.Time         `json:"scanned_at"`

	// 技术栈
	Languages   []LanguageInfo    `json:"languages"`
	Frameworks  []string          `json:"frameworks"`
	Libraries   []string          `json:"libraries"`
	BuildTool   string            `json:"build_tool"`

	// 项目结构
	DirLayout   string            `json:"dir_layout"`        // 目录树摘要
	EntryPoint  string            `json:"entry_point"`       // 入口文件
	KeyFiles    []KeyFileInfo     `json:"key_files"`         // 关键文件及其职责

	// 代码模式
	Patterns    []CodePattern     `json:"patterns"`          // 识别到的代码模式
	ArchNotes   []string          `json:"arch_notes"`        // 架构特点
}

// LanguageInfo 语言信息
type LanguageInfo struct {
	Name       string  `json:"name"`
	Exts       []string `json:"exts"`
	FileCount  int     `json:"file_count"`
	LineCount  int     `json:"line_count"`
	Percentage float64 `json:"percentage"`
}

// KeyFileInfo 关键文件信息
type KeyFileInfo struct {
	Path        string   `json:"path"`
	Purpose     string   `json:"purpose"`       // 推断的职责
	Exports     []string `json:"exports"`       // 导出的函数/类/接口
	DependsOn   []string `json:"depends_on"`    // 依赖的其他关键文件
}

// CodePattern 代码模式
type CodePattern struct {
	Category    string   `json:"category"`       // "naming" | "error_handling" | "structure" | "testing" | "import"
	Description string   `json:"description"`
	Examples    []string `json:"examples"`       // 实际代码片段
	Frequency   int      `json:"frequency"`      // 出现次数
}

// ─── 个人风格画像 (Personal Style Profile) ───

// StyleProfile 个人编码风格画像
type StyleProfile struct {
	// 命名习惯
	NamingConvention string   `json:"naming_convention"`     // "camelCase" | "snake_case" | "PascalCase" | "mixed"
	VarPrefixes      []string `json:"var_prefixes"`          // 如 ["m_", "p", "g"] 等
	InterfacePrefix  string   `json:"interface_prefix"`      // "I" | "" (Go: "er" suffix)
	EnumStyle        string   `json:"enum_style"`            // "UPPER_CASE" | "PascalCase" | "iota"

	// 惯用模式
	ErrorHandling   string   `json:"error_handling"`         // "explicit_return" | "try_catch" | "result_type" | "panic_recover"
	PreferredLibs   []string `json:"preferred_libs"`         // 最常用的库
	AvoidedLibs     []string `json:"avoided_libs"`           // 明确避免的库
	FavoredPatterns []string `json:"favored_patterns"`       // 偏好的设计模式

	// 代码组织
	FileOrgStyle     string `json:"file_org_style"`          // "one_class_per_file" | "module_based" | "feature_based"
	TestFilePattern  string `json:"test_file_pattern"`       // "*_test.go" | "test_*.py" | "__tests__/"
	CommentStyle     string `json:"comment_style"`           // "doc_comment" | "inline" | "minimal" | "verbose"
	ImportOrderRule  string `json:"import_order_rule"`       // "stdlib_first" | "alphabetical" | "grouped_by_type"

	// 从代码中提取的显式规则
	ExplicitRules    []string `json:"explicit_rules"`        // 从注释/配置中提取的编码规范
	Gotchas          []string `json:"gotchas"`               // 项目中需要注意的坑
}

// ─── 代码记忆条目 ───

// CodeMemory 存入 Nebula 的代码记忆单元
type CodeMemory struct {
	ID        string    `json:"id"`
	Category  string    `json:"category"`   // "project" | "pattern" | "style" | "gotcha" | "file"
	Content   string    `json:"content"`    // 记忆内容（文本描述）
	CodeSnippet string  `json:"code_snippet,omitempty"` // 相关代码片段
	FilePath  string    `json:"file_path,omitempty"`
	Tags      []string  `json:"tags"`
	Importance float64  `json:"importance"`
	CreatedAt time.Time `json:"created_at"`
}

// ─── 上下文注入 ───

// TaskContext 当前任务上下文
type TaskContext struct {
	Task        string   `json:"task"`          // 用户的任务描述，如 "添加一个用户认证中间件"
	Language    string   `json:"language"`      // 目标语言
	RelevantFiles []string `json:"relevant_files,omitempty"` // 正在编辑的文件
	CurrentFile string   `json:"current_file,omitempty"`
}

// InjectedContext 注入到 Prompt 中的上下文
type InjectedContext struct {
	ProjectOverview  string          `json:"project_overview"`   // 项目概况
	TechStack        string          `json:"tech_stack"`         // 技术栈说明
	RelevantPatterns []CodePattern   `json:"relevant_patterns"`  // 相关代码模式
	StyleNotes       []string        `json:"style_notes"`        // 风格注意事项
	RelevantMemories []CodeMemory    `json:"relevant_memories"`  // 相关记忆
	Gotchas          []string        `json:"gotchas"`            // 需要避开的坑
	PromptSuffix     string          `json:"prompt_suffix"`      // 拼接好的 Prompt 后缀
}

// ─── 扫描配置 ───

// ScanConfig 代码扫描配置
type ScanConfig struct {
	RootDir       string   `json:"root_dir"`
	ExcludeDirs   []string `json:"exclude_dirs"`    // 排除的目录
	ExcludeFiles  []string `json:"exclude_files"`   // 排除的文件模式
	MaxFileSize   int64    `json:"max_file_size"`   // 最大文件大小(字节)
	DeepAnalysis  bool     `json:"deep_analysis"`   // 是否深度分析（提取模式）
	StyleLearning bool     `json:"style_learning"`  // 是否学习个人风格
}
