package analyzer

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nebula-agent/nebula/engine/manager"
)

// StyleLearner 个人编码风格学习器
//
// 不从配置文件中读规则，而是从实际代码中"观察"并归纳个人编码习惯。
// 核心思路：你不告诉它规则，它看你的代码自己学会。
type StyleLearner struct {
	root  string
	memAPI *manager.MemoryAPI
}

// NewStyleLearner 创建风格学习器
func NewStyleLearner(root string, memAPI *manager.MemoryAPI) *StyleLearner {
	return &StyleLearner{root: root, memAPI: memAPI}
}

// Learn 学习并返回个人风格画像
func (sl *StyleLearner) Learn() (*StyleProfile, error) {
	sp := &StyleProfile{
		ExplicitRules: make([]string, 0),
		Gotchas:       make([]string, 0),
	}

	// 收集所有代码文件
	var goFiles, pyFiles, tsFiles, jsFiles []string
	filepath.Walk(sl.root, func(path string, info os.FileInfo, err error) error {
		if err != nil { return nil }
		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" || name == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}
		switch filepath.Ext(path) {
		case ".go": goFiles = append(goFiles, path)
		case ".py": pyFiles = append(pyFiles, path)
		case ".ts", ".tsx": tsFiles = append(tsFiles, path)
		case ".js", ".jsx": jsFiles = append(jsFiles, path)
		}
		return nil
	})

	// 根据主要语言选择分析策略
	primary := sl.detectPrimaryLang(goFiles, pyFiles, tsFiles, jsFiles)
	switch primary {
	case "go":
		sl.learnGoStyle(sp, goFiles)
	case "python":
		sl.learnPythonStyle(sp, pyFiles)
	case "typescript":
		sl.learnTSStyle(sp, tsFiles, jsFiles)
	}

	// 通用的注意事项检测
	sl.detectGotchas(sp)

	return sp, nil
}

// Ingest 学习并存入 Nebula
func (sl *StyleLearner) Ingest(sessionID string) ([]*CodeMemory, error) {
	sp, err := sl.Learn()
	if err != nil { return nil, err }

	var memories []*CodeMemory

	// 命名约定
	content := fmt.Sprintf("命名规范: %s。变量前缀: %s。接口/抽象命名: %s。枚举风格: %s。",
		sp.NamingConvention,
		strings.Join(sp.VarPrefixes, ", "),
		sp.InterfacePrefix,
		sp.EnumStyle,
	)
	mem := &CodeMemory{ID: manager.NewID(), Category: "风格", Content: content, Importance: 0.85, CreatedAt: time.Now().UTC(), Tags: []string{"命名", "风格"}}
	memories = append(memories, mem)
	sl.store(sessionID, mem)

	// 错误处理
	content = fmt.Sprintf("错误处理方式: %s", sp.ErrorHandling)
	mem = &CodeMemory{ID: manager.NewID(), Category: "风格", Content: content, Importance: 0.85, CreatedAt: time.Now().UTC(), Tags: []string{"错误处理", "风格"}}
	memories = append(memories, mem)
	sl.store(sessionID, mem)

	// 惯用库
	if len(sp.PreferredLibs) > 0 {
		content = fmt.Sprintf("惯用库: %s。避免使用: %s",
			strings.Join(sp.PreferredLibs, ", "),
			strings.Join(sp.AvoidedLibs, ", "))
		mem = &CodeMemory{ID: manager.NewID(), Category: "风格", Content: content, Importance: 0.8, CreatedAt: time.Now().UTC(), Tags: []string{"库偏好", "风格"}}
		memories = append(memories, mem)
		sl.store(sessionID, mem)
	}

	// 代码组织
	content = fmt.Sprintf("文件组织方式: %s。测试文件模式: %s。注释风格: %s。导入顺序: %s。",
		sp.FileOrgStyle, sp.TestFilePattern, sp.CommentStyle, sp.ImportOrderRule)
	mem = &CodeMemory{ID: manager.NewID(), Category: "风格", Content: content, Importance: 0.75, CreatedAt: time.Now().UTC(), Tags: []string{"组织", "风格"}}
	memories = append(memories, mem)
	sl.store(sessionID, mem)

	// 偏好模式
	if len(sp.FavoredPatterns) > 0 {
		content = fmt.Sprintf("偏好的设计模式: %s", strings.Join(sp.FavoredPatterns, ", "))
		mem = &CodeMemory{ID: manager.NewID(), Category: "风格", Content: content, Importance: 0.7, CreatedAt: time.Now().UTC(), Tags: []string{"设计模式", "风格"}}
		memories = append(memories, mem)
		sl.store(sessionID, mem)
	}

	// 显式规则 + 注意事项
	for _, rule := range sp.ExplicitRules {
		mem = &CodeMemory{ID: manager.NewID(), Category: "规则", Content: rule, Importance: 0.9, CreatedAt: time.Now().UTC(), Tags: []string{"规则", "显式"}}
		memories = append(memories, mem)
		sl.store(sessionID, mem)
	}
	for _, gotcha := range sp.Gotchas {
		mem = &CodeMemory{ID: manager.NewID(), Category: "注意事项", Content: gotcha, Importance: 0.95, CreatedAt: time.Now().UTC(), Tags: []string{"注意", "坑"}}
		memories = append(memories, mem)
		sl.store(sessionID, mem)
	}

	return memories, nil
}

// ─── Go 风格分析 ───

func (sl *StyleLearner) learnGoStyle(sp *StyleProfile, files []string) {
	sp.NamingConvention = "PascalCase(导出) / camelCase(私有)"
	sp.FileOrgStyle = "按功能分包"
	sp.TestFilePattern = "*_test.go"
	sp.ImportOrderRule = "标准库 → 第三方 → 本地"

	var (
		errPattern    = make(map[string]int)
		importSections int
		usesInterfaces bool
		usesDI         bool
		sampleContent  string
	)

	for _, f := range files {
		text, err := readFileText(f, 50000)
		if err != nil { continue }
		sampleContent += text[:min(2000, len(text))]

		// 错误处理模式
		if strings.Contains(text, "if err != nil") {
			errPattern["显式错误返回"]++
		}
		if strings.Contains(text, "errors.Wrap") || strings.Contains(text, "fmt.Errorf") {
			errPattern["错误包装(errors.Wrap/fmt.Errorf)"]++
		}
		if strings.Contains(text, "panic(") {
			errPattern["panic"]++
		}

		// 接口使用
		if strings.Contains(text, "interface {") {
			usesInterfaces = true
		}
		if strings.Contains(text, "New") && strings.Contains(text, "func New") {
			usesDI = true
		}

		// 导入分组
		if strings.Contains(text, "import (") {
			importSections++
		}
	}

	// 错误处理偏好
	if errPattern["显式错误返回"] > errPattern["panic"]*3 {
		sp.ErrorHandling = "Go 标准显式错误返回(if err != nil)，不使用 panic"
	} else if errPattern["panic"] > 0 {
		sp.ErrorHandling = "混合使用显式错误返回和 panic"
	}

	// 设计模式
	if usesInterfaces {
		sp.FavoredPatterns = append(sp.FavoredPatterns, "依赖接口而非具体实现")
	}
	if usesDI {
		sp.FavoredPatterns = append(sp.FavoredPatterns, "构造函数注入(NewXxx)")
	}

	// 接口命名(Go 惯用 er 后缀)
	erPattern := regexp.MustCompile(`type\s+(\w+er)\s+interface`)
	erMatches := erPattern.FindAllStringSubmatch(sampleContent, -1)
	if len(erMatches) > 0 {
		sp.InterfacePrefix = "单方法接口用 -er 后缀(Reader/Writer/Closer 风格)"
	}

	// 检测常用库
	libPattern := regexp.MustCompile(`"github\.com/([\w-]+)/([\w-]+)"`)
	libMatches := libPattern.FindAllStringSubmatch(sampleContent, -1)
	libCounts := make(map[string]int)
	for _, m := range libMatches {
		libCounts[m[2]]++
	}
	for lib, count := range libCounts {
		if count >= 2 {
			sp.PreferredLibs = append(sp.PreferredLibs, lib)
		}
	}

	// 检测显式编码规则(从注释中提取)
	rulePattern := regexp.MustCompile(`(?i)(?:规则|规范|注意|约定|规范|rule|convention|note|warning)[:：]\s*(.+)`)
	ruleMatches := rulePattern.FindAllStringSubmatch(sampleContent, -1)
	for _, m := range ruleMatches {
		sp.ExplicitRules = append(sp.ExplicitRules, m[1])
	}
}

// ─── Python 风格分析 ───

func (sl *StyleLearner) learnPythonStyle(sp *StyleProfile, files []string) {
	var (
		snakeCount      int
		camelCount      int
		typeHintCount   int
		dataclassCount  int
		asyncCount      int
		sampleContent   string
	)

	for _, f := range files {
		text, err := readFileText(f, 50000)
		if err != nil { continue }
		sampleContent += text[:min(2000, len(text))]

		snakeCount += len(regexp.MustCompile(`def\s+[a-z][a-z0-9]*(_[a-z0-9]+)*`).FindAllString(text, -1))
		camelCount += len(regexp.MustCompile(`def\s+[a-z]+[A-Z]`).FindAllString(text, -1))
		typeHintCount += len(regexp.MustCompile(`:\s*(str|int|float|bool|list|dict|Optional|Union|Any)\b`).FindAllString(text, -1))
		dataclassCount += len(regexp.MustCompile(`@dataclass`).FindAllString(text, -1))
		asyncCount += len(regexp.MustCompile(`async\s+def`).FindAllString(text, -1))
	}

	if snakeCount > camelCount*2 {
		sp.NamingConvention = "PEP 8 snake_case (函数/变量) + PascalCase (类名)"
	}

	if typeHintCount > 10 {
		sp.FavoredPatterns = append(sp.FavoredPatterns, "使用类型标注(type hints)")
	}
	if dataclassCount > 0 {
		sp.FavoredPatterns = append(sp.FavoredPatterns, "使用 @dataclass")
	}
	if asyncCount > 3 {
		sp.FavoredPatterns = append(sp.FavoredPatterns, "异步编程(async/await)")
	}

	sp.ErrorHandling = "try/except 异常处理"
	sp.FileOrgStyle = "模块化(.py 文件 + __init__.py)"
	sp.TestFilePattern = "test_*.py 或 *_test.py"
	sp.ImportOrderRule = "标准库 → 第三方 → 本地"

	// 常用库
	for _, lib := range []string{"fastapi", "flask", "django", "sqlalchemy", "pydantic", "pytest", "httpx", "requests"} {
		if strings.Contains(sampleContent, lib) {
			sp.PreferredLibs = append(sp.PreferredLibs, lib)
		}
	}
}

// ─── TypeScript 风格分析 ───

func (sl *StyleLearner) learnTSStyle(sp *StyleProfile, tsFiles, jsFiles []string) {
	sp.NamingConvention = "camelCase(变量/函数) + PascalCase(类/组件/接口)"
	sp.FileOrgStyle = "按功能或路由分组"
	sp.TestFilePattern = "*.test.ts 或 *.spec.ts"

	var sampleContent string
	for _, f := range tsFiles {
		text, _ := readFileText(f, 30000)
		sampleContent += text[:min(2000, len(text))]
	}
	for _, f := range jsFiles {
		text, _ := readFileText(f, 30000)
		sampleContent += text[:min(2000, len(text))]
	}

	// 检测 React vs Vue vs Svelte
	if strings.Contains(sampleContent, "react") || strings.Contains(sampleContent, "jsx") {
		sp.FavoredPatterns = append(sp.FavoredPatterns, "React 组件式开发")
	}
	if strings.Contains(sampleContent, "vue") {
		sp.FavoredPatterns = append(sp.FavoredPatterns, "Vue 单文件组件")
	}

	// 检测异步模式
	if strings.Contains(sampleContent, "async/await") {
		sp.ErrorHandling = "async/await + try/catch"
	}

	// 接口命名(是否加 I 前缀)
	iCount := len(regexp.MustCompile(`\binterface\s+I\w+`).FindAllString(sampleContent, -1))
	noICount := len(regexp.MustCompile(`\binterface\s+[A-Z][^I]\w+`).FindAllString(sampleContent, -1))
	if iCount > noICount {
		sp.InterfacePrefix = "接口加 I 前缀 (IUser, IConfig)"
	} else {
		sp.InterfacePrefix = "接口不加前缀 (User, Config)"
	}
}

// ─── 注意事项检测 ───

func (sl *StyleLearner) detectGotchas(sp *StyleProfile) {
	// 扫描项目中的 WARNING/XXX/TODO/HACK/FIXME 注释
	gotchaPattern := regexp.MustCompile(`(?i)(?:WARNING|XXX|HACK|FIXME|小心|注意|坑|陷阱|不要|禁止)[:：]?\s*(.+)`)

	filepath.Walk(sl.root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() { return nil }
		if info.Size() > 100000 { return nil }
		text, err := readFileText(path, 80000)
		if err != nil { return nil }

		matches := gotchaPattern.FindAllStringSubmatch(text, -1)
		for _, m := range matches {
			if len(m) > 1 {
				gotcha := strings.TrimSpace(m[1])
				if len(gotcha) > 5 && len(gotcha) < 200 {
					sp.Gotchas = append(sp.Gotchas, gotcha)
				}
			}
		}
		return nil
	})

	if len(sp.Gotchas) > 20 {
		sp.Gotchas = sp.Gotchas[:20]
	}
}

// ─── 辅助 ───

func (sl *StyleLearner) detectPrimaryLang(goFiles, pyFiles, tsFiles, jsFiles []string) string {
	counts := map[string]int{
		"go": len(goFiles), "python": len(pyFiles),
		"typescript": len(tsFiles) + len(jsFiles),
	}
	maxLang, maxCount := "", 0
	for lang, count := range counts {
		if count > maxCount { maxLang, maxCount = lang, count }
	}
	return maxLang
}

func (sl *StyleLearner) store(sessionID string, cm *CodeMemory) {
	mem := &manager.Memory{
		ID:         cm.ID,
		SessionID:  sessionID,
		Type:       manager.SemanticMemory,
		Content:    cm.Content,
		Tags:       cm.Tags,
		Importance: cm.Importance,
		CreatedAt:  cm.CreatedAt,
	}
	_ = sl.memAPI.Remember(mem)
}

func readFileText(path string, maxSize int64) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil { return "", err }
	if int64(len(data)) > maxSize {
		return string(data[:maxSize]), nil
	}
	return string(data), nil
}
