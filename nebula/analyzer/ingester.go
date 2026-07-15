package analyzer

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/nebula-agent/nebula/engine/manager"
)

// CodeIngester 代码摄取器 — 扫描本地代码库，生成结构化记忆
type CodeIngester struct {
	config *ScanConfig
	memAPI *manager.MemoryAPI
}

// NewCodeIngester 创建代码摄取器
func NewCodeIngester(config *ScanConfig, memAPI *manager.MemoryAPI) *CodeIngester {
	if config.ExcludeDirs == nil {
		config.ExcludeDirs = []string{".git", "node_modules", "vendor", "__pycache__", ".venv", "dist", "build", ".next"}
	}
	if config.MaxFileSize == 0 {
		config.MaxFileSize = 2 << 20 // 2MB
	}
	return &CodeIngester{config: config, memAPI: memAPI}
}

// Scan 扫描项目并返回指纹
func (ci *CodeIngester) Scan() (*ProjectFingerprint, error) {
	fp := &ProjectFingerprint{
		Name:      filepath.Base(ci.config.RootDir),
		Path:      ci.config.RootDir,
		ScannedAt: time.Now().UTC(),
	}

	// 遍历文件树
	var files []fileInfo
	filepath.Walk(ci.config.RootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil { return nil }
		if info.IsDir() {
			for _, excl := range ci.config.ExcludeDirs {
				if info.Name() == excl { return filepath.SkipDir }
			}
			return nil
		}
		if info.Size() > ci.config.MaxFileSize { return nil }
		rel, _ := filepath.Rel(ci.config.RootDir, path)
		files = append(files, fileInfo{path: path, rel: rel, size: info.Size(), ext: filepath.Ext(path)})
		return nil
	})

	// 分析语言分布
	fp.Languages = ci.analyzeLanguages(files)

	// 提取技术栈
	fp.Frameworks, fp.Libraries, fp.BuildTool = ci.detectTechStack(files)

	// 分析目录结构
	fp.DirLayout = ci.buildDirLayout()

	// 识别入口点
	fp.EntryPoint = ci.findEntryPoint(files)

	// 识别关键文件
	fp.KeyFiles = ci.identifyKeyFiles(files)

	// 深度分析: 提取代码模式
	if ci.config.DeepAnalysis {
		fp.Patterns = ci.extractPatterns(files)
	}

	return fp, nil
}

// Ingest 扫描并将结果存入 Nebula
func (ci *CodeIngester) Ingest(sessionID string) ([]*CodeMemory, error) {
	fp, err := ci.Scan()
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}

	var memories []*CodeMemory

	// 1. 存储项目概况
	overview := fmt.Sprintf("项目 %s: 主要使用 %s。",
		fp.Name, ci.langSummary(fp.Languages))
	if len(fp.Frameworks) > 0 {
		overview += fmt.Sprintf("框架: %s。", strings.Join(fp.Frameworks, ", "))
	}
	if len(fp.Libraries) > 0 {
		overview += fmt.Sprintf("关键库: %s。", strings.Join(topN(fp.Libraries, 10), ", "))
	}
	overview += fmt.Sprintf("入口: %s。目录结构: %s", fp.EntryPoint, fp.DirLayout)

	mem := ci.toMemory("project", overview, "", 0.9, []string{"项目概况", "技术栈"})
	memories = append(memories, mem)
	ci.storeMemory(sessionID, mem)

	// 2. 存储每个关键文件
	for _, kf := range fp.KeyFiles {
		content := fmt.Sprintf("文件 %s 的职责: %s。导出: %s。依赖: %s",
			kf.Path, kf.Purpose,
			strings.Join(kf.Exports, ", "),
			strings.Join(kf.DependsOn, ", "))
		mem := ci.toMemory("file", content, kf.Path, 0.6, []string{"文件", "结构"})
		memories = append(memories, mem)
		ci.storeMemory(sessionID, mem)
	}

	// 3. 存储代码模式
	for _, p := range fp.Patterns {
		content := fmt.Sprintf("[%s] %s。示例: %s",
			p.Category, p.Description,
			strings.Join(p.Examples, " | "))
		mem := ci.toMemory("pattern", content, "", 0.7, []string{p.Category, "模式"})
		memories = append(memories, mem)
		ci.storeMemory(sessionID, mem)
	}

	return memories, nil
}

// ─── 语言分析 ───

type fileInfo struct {
	path, rel string
	size      int64
	ext       string
}

var extToLang = map[string]string{
	".go": "Go", ".py": "Python", ".js": "JavaScript", ".ts": "TypeScript",
	".tsx": "TypeScript", ".jsx": "JavaScript", ".rs": "Rust", ".java": "Java",
	".kt": "Kotlin", ".swift": "Swift", ".c": "C", ".cpp": "C++", ".h": "C/C++ Header",
	".hpp": "C++ Header", ".cs": "C#", ".rb": "Ruby", ".php": "PHP",
	".vue": "Vue", ".svelte": "Svelte", ".sql": "SQL", ".sh": "Shell",
	".yaml": "YAML", ".yml": "YAML", ".toml": "TOML", ".json": "JSON",
	".md": "Markdown", ".css": "CSS", ".scss": "SCSS", ".html": "HTML",
	".proto": "Protobuf", ".graphql": "GraphQL", ".Makefile": "Makefile",
}

func (ci *CodeIngester) analyzeLanguages(files []fileInfo) []LanguageInfo {
	langMap := make(map[string]*LanguageInfo)
	for _, f := range files {
		langName := detectLanguage(f)
		if langName == "" { continue }
		if _, ok := langMap[langName]; !ok {
			langMap[langName] = &LanguageInfo{Name: langName}
		}
		li := langMap[langName]
		li.FileCount++
		li.Exts = appendUnique(li.Exts, f.ext)
	}

	totalFiles := len(files)
	var langs []LanguageInfo
	for _, li := range langMap {
		li.Percentage = float64(li.FileCount) / float64(totalFiles) * 100
		langs = append(langs, *li)
	}
	sort.Slice(langs, func(i, j int) bool { return langs[i].FileCount > langs[j].FileCount })
	return langs
}

func detectLanguage(f fileInfo) string {
	// 特殊文件名
	base := filepath.Base(f.path)
	switch base {
	case "Makefile", "Dockerfile", "CMakeLists.txt":
		return "Build"
	case "go.mod":
		return "Go Module"
	case "Cargo.toml":
		return "Rust Config"
	case "package.json":
		return "Node Config"
	}
	if lang, ok := extToLang[f.ext]; ok {
		return lang
	}
	return ""
}

// ─── 技术栈检测 ───

// 常见框架/库的检测规则
var frameworkDetectors = []struct {
	file    string
	content string
	label   string
}{
	{"go.mod", "gin-gonic/gin", "Gin"},
	{"go.mod", "fiber", "Fiber"},
	{"go.mod", "echo", "Echo"},
	{"go.mod", "grpc", "gRPC"},
	{"package.json", "react", "React"},
	{"package.json", "vue", "Vue"},
	{"package.json", "next", "Next.js"},
	{"package.json", "express", "Express"},
	{"package.json", "fastify", "Fastify"},
	{"requirements.txt", "fastapi", "FastAPI"},
	{"requirements.txt", "flask", "Flask"},
	{"requirements.txt", "django", "Django"},
	{"pyproject.toml", "fastapi", "FastAPI"},
	{"Cargo.toml", "actix-web", "Actix"},
	{"Cargo.toml", "axum", "Axum"},
	{"Cargo.toml", "rocket", "Rocket"},
}

func (ci *CodeIngester) detectTechStack(files []fileInfo) (frameworks, libraries []string, buildTool string) {
	foundFw := make(map[string]bool)
	foundLib := make(map[string]bool)

	for _, f := range files {
		base := filepath.Base(f.rel)

		// 检测构建工具
		switch base {
		case "go.mod": buildTool = "Go Modules"
		case "package.json": buildTool = "npm/yarn/pnpm"
		case "Cargo.toml": buildTool = "Cargo"
		case "Makefile": buildTool = firstOf(buildTool, "Make")
		case "CMakeLists.txt": buildTool = firstOf(buildTool, "CMake")
		case "pyproject.toml": buildTool = firstOf(buildTool, "Poetry/PDM")
		case "requirements.txt": buildTool = firstOf(buildTool, "pip")
		}

		// 只读小文件
		if f.size > 50000 { continue }

		content, err := os.ReadFile(f.path)
		if err != nil { continue }
		text := string(content)

		// 检测框架和库
		for _, det := range frameworkDetectors {
			if base == det.file && strings.Contains(text, det.content) {
				if det.file == "go.mod" || det.file == "Cargo.toml" || strings.Contains(det.label, "/") {
					foundLib[det.label] = true
				} else {
					foundFw[det.label] = true
				}
			}
		}
	}

	for fw := range foundFw { frameworks = append(frameworks, fw) }
	for lib := range foundLib { libraries = append(libraries, lib) }
	sort.Strings(frameworks)
	sort.Strings(libraries)
	return
}

// ─── 目录结构 ───

func (ci *CodeIngester) buildDirLayout() string {
	entries, err := os.ReadDir(ci.config.RootDir)
	if err != nil { return "" }

	var parts []string
	for _, e := range entries {
		if !e.IsDir() { continue }
		if contains(ci.config.ExcludeDirs, e.Name()) { continue }
		parts = append(parts, e.Name())
	}
	return strings.Join(parts, "/")
}

// ─── 入口点 ───

func (ci *CodeIngester) findEntryPoint(files []fileInfo) string {
	candidates := []string{"main.go", "main.py", "index.js", "index.ts", "app.py", "server.js", "src/main.rs", "main.rs"}
	for _, c := range candidates {
		for _, f := range files {
			if f.rel == c || strings.HasSuffix(f.rel, "/"+c) {
				return f.rel
			}
		}
	}
	if len(files) > 0 { return files[0].rel }
	return ""
}

// ─── 关键文件识别 ───

func (ci *CodeIngester) identifyKeyFiles(files []fileInfo) []KeyFileInfo {
	var kfs []KeyFileInfo

	for _, f := range files {
		base := filepath.Base(f.path)
		ext := f.ext

		// 跳过大文件和二进制
		if f.size > 200000 { continue }

		// 识别关键文件
		isKey := false
		purpose := ""

		switch {
		case base == "main.go" || base == "main.py" || base == "index.js":
			isKey = true
			purpose = "入口文件"
		case strings.Contains(base, "router") || strings.Contains(base, "route"):
			isKey = true
			purpose = "路由定义"
		case strings.Contains(base, "model") || strings.Contains(base, "schema"):
			isKey = true
			purpose = "数据模型"
		case strings.Contains(base, "config") || strings.Contains(base, "setting"):
			isKey = true
			purpose = "配置管理"
		case strings.Contains(base, "middleware"):
			isKey = true
			purpose = "中间件"
		case strings.Contains(base, "handler") || strings.Contains(base, "controller"):
			isKey = true
			purpose = "请求处理器"
		case strings.Contains(base, "service") || strings.Contains(base, "usecase"):
			isKey = true
			purpose = "业务逻辑"
		}

		if !isKey { continue }

		// 提取导出的符号
		exports := ci.extractExports(f.path, ext)

		kf := KeyFileInfo{
			Path:    f.rel,
			Purpose: purpose,
			Exports: exports,
		}
		kfs = append(kfs, kf)
	}

	return kfs
}

// extractExports 从文件中提取导出的函数/类/接口名
func (ci *CodeIngester) extractExports(path, ext string) []string {
	content, err := os.ReadFile(path)
	if err != nil { return nil }

	var exports []string
	text := string(content)

	switch ext {
	case ".go":
		// func Xxx, type Xxx, interface Xxx
		re := regexp.MustCompile(`(?m)^func\s+(\p{Lu}\w*)`)
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			exports = append(exports, m[1])
		}
		re = regexp.MustCompile(`(?m)^type\s+(\p{Lu}\w*)`)
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			exports = append(exports, "type "+m[1])
		}
	case ".py":
		re := regexp.MustCompile(`(?m)^(?:async\s+)?def\s+(\w+)`)
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			exports = append(exports, "def "+m[1])
		}
		re = regexp.MustCompile(`(?m)^class\s+(\w+)`)
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			exports = append(exports, "class "+m[1])
		}
	case ".ts", ".tsx", ".js", ".jsx":
		re := regexp.MustCompile(`(?m)^(?:export\s+)?(?:async\s+)?function\s+(\w+)`)
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			exports = append(exports, m[1])
		}
		re = regexp.MustCompile(`(?m)^(?:export\s+)?class\s+(\w+)`)
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			exports = append(exports, "class "+m[1])
		}
	}

	if len(exports) > 15 { exports = exports[:15] }
	return exports
}

// ─── 代码模式提取 ───

func (ci *CodeIngester) extractPatterns(files []fileInfo) []CodePattern {
	patterns := make(map[string]*CodePattern)

	for _, f := range files {
		if f.size > 100000 { continue }
		content, err := os.ReadFile(f.path)
		if err != nil { continue }
		text := string(content)

		// 命名模式
		ci.detectNamingPatterns(text, f.ext, patterns)

		// 错误处理模式
		ci.detectErrorHandling(text, f.ext, patterns)

		// 导入模式
		ci.detectImportPatterns(text, f.ext, patterns)

		// 测试模式
		ci.detectTestPatterns(text, f.ext, f.rel, patterns)
	}

	var result []CodePattern
	for _, p := range patterns {
		result = append(result, *p)
	}
	return result
}

func (ci *CodeIngester) detectNamingPatterns(text, ext string, patterns map[string]*CodePattern) {
	switch ext {
	case ".go":
		// 检测 Go 命名: 是否大量使用大写导出
		exported := regexp.MustCompile(`\b[A-Z][a-z]+\w*\b`).FindAllString(text, -1)
		unexported := regexp.MustCompile(`\b[a-z][a-z]+\w*\b`).FindAllString(text, -1)
		if len(exported) > 0 && len(unexported) > 0 {
			ratio := float64(len(exported)) / float64(len(exported)+len(unexported))
			desc := fmt.Sprintf("Go 导出符号比例 %.0f%%，采用标准 Go 命名规范(PascalCase 导出, camelCase 私有)", ratio*100)
			addPattern(patterns, "命名规范", desc, exported[:min(3, len(exported))], len(exported))
		}
	case ".py":
		// 检测 snake_case vs camelCase
		snake := regexp.MustCompile(`\b[a-z][a-z0-9]*(_[a-z0-9]+)+\b`).FindAllString(text, -1)
		camel := regexp.MustCompile(`\b[a-z]+[A-Z][a-zA-Z]*\b`).FindAllString(text, -1)
		if len(snake) > len(camel)*2 {
			addPattern(patterns, "命名规范", "主要使用 snake_case 命名(Python 社区标准)", []string{}, len(snake))
		}
	}
}

func (ci *CodeIngester) detectErrorHandling(text, ext string, patterns map[string]*CodePattern) {
	switch ext {
	case ".go":
		// Go: if err != nil
		errChecks := regexp.MustCompile(`if\s+err\s*!=\s*nil`).FindAllString(text, -1)
		if len(errChecks) > 3 {
			addPattern(patterns, "错误处理", "使用 Go 标准显式错误检查(err != nil), 不使用 panic", []string{"if err != nil { return ... }"}, len(errChecks))
		}
	case ".py":
		tryExcept := regexp.MustCompile(`try\s*:`).FindAllString(text, -1)
		if len(tryExcept) > 0 {
			addPattern(patterns, "错误处理", "使用 try/except 异常处理", []string{}, len(tryExcept))
		}
	case ".ts", ".tsx":
		tryCatch := regexp.MustCompile(`try\s*\{`).FindAllString(text, -1)
		resultType := regexp.MustCompile(`Result<|Either<|Ok\(|Err\(`).FindAllString(text, -1)
		if len(resultType) > 0 {
			addPattern(patterns, "错误处理", "使用 Result/Either 类型处理错误(Rust风格)", []string{}, len(resultType))
		} else if len(tryCatch) > 0 {
			addPattern(patterns, "错误处理", "使用 try/catch 异常处理", []string{}, len(tryCatch))
		}
	}
}

func (ci *CodeIngester) detectImportPatterns(text, ext string, patterns map[string]*CodePattern) {
	switch ext {
	case ".go":
		// 检测是否分了 stdlib/第三方/本地 三组
		imports := regexp.MustCompile(`"(.*?)"`).FindAllStringSubmatch(text, -1)
		hasStdlib := false
		hasThirdParty := false
		for _, im := range imports {
			pkg := im[1]
			if !strings.Contains(pkg, ".") && !strings.Contains(pkg, "/") {
				hasStdlib = true
			} else if strings.Contains(pkg, ".") {
				hasThirdParty = true
			}
		}
		if hasStdlib && hasThirdParty {
			addPattern(patterns, "导入顺序", "Go 导入按 stdlib → 第三方 → 本地 分组", []string{}, 1)
		}
	}
}

func (ci *CodeIngester) detectTestPatterns(text, ext, rel string, patterns map[string]*CodePattern) {
	switch {
	case strings.Contains(rel, "_test.go"):
		addPattern(patterns, "测试", "Go 表驱动测试(table-driven tests)", []string{"func TestXxx(t *testing.T)"}, 1)
	case strings.Contains(rel, "test_") || strings.Contains(rel, "_test.py"):
		addPattern(patterns, "测试", "Python pytest 风格测试", []string{"def test_xxx():"}, 1)
	}
}

// ─── 辅助函数 ───

func (ci *CodeIngester) toMemory(category, content, filePath string, importance float64, tags []string) *CodeMemory {
	return &CodeMemory{
		ID:         manager.NewID(),
		Category:   category,
		Content:    content,
		FilePath:   filePath,
		Tags:       tags,
		Importance: importance,
		CreatedAt:  time.Now().UTC(),
	}
}

func (ci *CodeIngester) storeMemory(sessionID string, cm *CodeMemory) {
	mem := &manager.Memory{
		ID:         cm.ID,
		SessionID:  sessionID,
		Type:       manager.SemanticMemory,
		Content:    cm.Content,
		Tags:       cm.Tags,
		Importance: cm.Importance,
		CreatedAt:  cm.CreatedAt,
		Metadata: map[string]string{
			"category":  cm.Category,
			"file_path": cm.FilePath,
		},
	}
	_ = ci.memAPI.Remember(mem)
}

func (ci *CodeIngester) langSummary(langs []LanguageInfo) string {
	if len(langs) == 0 { return "未知" }
	parts := make([]string, len(langs))
	for i, l := range langs {
		parts[i] = fmt.Sprintf("%s(%.0f%%)", l.Name, l.Percentage)
	}
	return strings.Join(parts, ", ")
}

func addPattern(patterns map[string]*CodePattern, category, desc string, examples []string, freq int) {
	key := category + ":" + desc
	if p, ok := patterns[key]; ok {
		p.Frequency += freq
	} else {
		patterns[key] = &CodePattern{Category: category, Description: desc, Examples: examples, Frequency: freq}
	}
}

func appendUnique(slice []string, item string) []string {
	for _, s := range slice {
		if s == item { return slice }
	}
	return append(slice, item)
}

func topN(items []string, n int) []string {
	if len(items) <= n { return items }
	return items[:n]
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item { return true }
	}
	return false
}

func firstOf(vals ...string) string {
	for _, v := range vals {
		if v != "" { return v }
	}
	return ""
}

func min(a, b int) int {
	if a < b { return a }
	return b
}

// scanFileContent 读取文件内容
func scanFileContent(path string, maxSize int64) (string, error) {
	f, err := os.Open(path)
	if err != nil { return "", err }
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var lines []string
	var total int64
	for scanner.Scan() {
		total += int64(len(scanner.Bytes()))
		if total > maxSize { break }
		lines = append(lines, scanner.Text())
	}
	return strings.Join(lines, "\n"), scanner.Err()
}
