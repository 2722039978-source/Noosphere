// Nebula Code — 终端原生记忆管理工具
//
// 像 git 一样在终端直接管理 AI 记忆，支持管道和脚本集成。
//
// 命令:
//   nebula ingest  <路径>       扫描项目，学习代码特征
//   nebula remember <内容>      快速存储一条记忆
//   nebula recall   <关键词>    检索相关记忆
//   nebula context  <任务>      生成上下文 Prompt（可管道输出）
//   nebula summary              查看记忆摘要
//   nebula export   [格式]      导出全部记忆（json|markdown）
//   nebula import   <文件>      从文件导入记忆
//   nebula forget   <ID>        删除指定记忆
//   nebula style                查看学到的编码风格
//   nebula gotchas              查看项目注意事项
//
// 管道示例 (PowerShell):
//   nebula context "添加认证中间件" | Set-Clipboard
//   nebula export json | jq '.memories[] | select(.category=="风格")'
//
// 管道示例 (bash):
//   nebula context "refactor DB layer" | pbcopy
//   nebula export json | jq '.memories[] | select(.tags[] | contains("注意"))'
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nebula-agent/nebula/analyzer"
	"github.com/nebula-agent/nebula/engine"
	"github.com/nebula-agent/nebula/engine/manager"
)

// ─── 通用配置 ───

const (
	defaultDataDir   = "nebula-memory"
	defaultSession   = "code-project"
	envDataDir       = "NEBULA_DATA_DIR"
	envSession       = "NEBULA_SESSION"
)

func getDataDir() string {
	if d := os.Getenv(envDataDir); d != "" { return d }
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".nebula", "memory")
}

func getSession() string {
	if s := os.Getenv(envSession); s != "" { return s }
	cwd, _ := os.Getwd()
	// 每个项目目录自动对应一个 session
	return "project-" + filepath.Base(cwd)
}

// ─── 主入口 ───

func main() {
	if len(os.Args) < 2 {
		printUsage()
		return
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "ingest", "scan":
		cmdIngest(args)
	case "remember", "mem", "add":
		cmdRemember(args)
	case "recall", "search", "find":
		cmdRecall(args)
	case "context", "ctx", "prompt":
		cmdContext(args)
	case "summary", "info":
		cmdSummary(args)
	case "export", "dump":
		cmdExport(args)
	case "import", "load":
		cmdImport(args)
	case "forget", "rm", "delete":
		cmdForget(args)
	case "style":
		cmdStyle(args)
	case "gotchas", "warn":
		cmdGotchas(args)
	case "serve":
		fmt.Println("请使用 nebula-server 启动完整服务: go run ./cmd/nebula-server --memory")
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

// ═══════════════════════════════════════════
// ingest — 扫描项目，学习代码
// ═══════════════════════════════════════════
func cmdIngest(args []string) {
	fs := flag.NewFlagSet("ingest", flag.ExitOnError)
	dir := fs.String("dir", ".", "项目根目录")
	session := fs.String("session", getSession(), "会话标识")
	noDeep := fs.Bool("no-deep", false, "跳过深度模式分析")
	noStyle := fs.Bool("no-style", false, "跳过风格学习")
	fs.Parse(args)

	eng := mustOpen()
	defer eng.Close()

	db := &dbAdapter{eng: eng}
	memAPI := manager.NewMemoryAPI(db, db, db, db)

	fmt.Fprintf(os.Stderr, "◈ 正在扫描: %s\n", *dir)
	fmt.Fprintf(os.Stderr, "  ├─ 分析语言与技术栈...\n")

	ingester := analyzer.NewCodeIngester(&analyzer.ScanConfig{
		RootDir:       *dir,
		DeepAnalysis:  !*noDeep,
		StyleLearning: !*noStyle,
	}, memAPI)

	memories, err := ingester.Ingest(*session)
	if err != nil {
		log.Fatalf("扫描失败: %v", err)
	}
	fmt.Fprintf(os.Stderr, "  ├─ 提取了 %d 条代码记忆\n", len(memories))

	if !*noStyle {
		fmt.Fprintf(os.Stderr, "  ├─ 学习个人编码风格...\n")
		learner := analyzer.NewStyleLearner(*dir, memAPI)
		styleMems, err := learner.Ingest(*session)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  │  警告: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "  ├─ 识别了 %d 条风格特征\n", len(styleMems))
		}
	}

	fmt.Fprintf(os.Stderr, "  └─ ✓ 完成，会话: %s\n\n", *session)
	printSummary(memAPI, *session)
}

// ═══════════════════════════════════════════
// remember — 快速存储记忆
// ═══════════════════════════════════════════
func cmdRemember(args []string) {
	fs := flag.NewFlagSet("remember", flag.ExitOnError)
	session := fs.String("session", getSession(), "会话标识")
	memType := fs.String("type", "semantic", "记忆类型: episodic|semantic|working|procedural")
	tags := fs.String("tags", "", "标签(逗号分隔)")
	importance := fs.Float64("importance", 0.7, "重要性 0-1")
	fs.Parse(args)

	content := strings.Join(fs.Args(), " ")
	if content == "" {
		// 尝试从 stdin 读取
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			data, _ := io.ReadAll(os.Stdin)
			content = strings.TrimSpace(string(data))
		}
	}
	if content == "" {
		fmt.Fprintln(os.Stderr, "请提供记忆内容（参数或管道）")
		fmt.Fprintln(os.Stderr, "  例: nebula remember '用户偏好 Python' --tags=python,偏好")
		fmt.Fprintln(os.Stderr, "  例: echo '用户偏好 Python' | nebula remember --tags=python")
		return
	}

	eng := mustOpen()
	defer eng.Close()

	db := &dbAdapter{eng: eng}
	memAPI := manager.NewMemoryAPI(db, db, db, db)

	mtype := manager.MemoryTypeFromString(*memType)
	mem := manager.NewMemory(*session, mtype, content)
	mem.Importance = *importance
	if *tags != "" {
		for _, t := range strings.Split(*tags, ",") {
			mem.Tags = append(mem.Tags, strings.TrimSpace(t))
		}
	}

	if err := memAPI.Remember(mem); err != nil {
		log.Fatalf("存储失败: %v", err)
	}

	// 输出 ID 方便后续引用
	result := map[string]string{"id": mem.ID, "status": "stored"}
	json.NewEncoder(os.Stdout).Encode(result)
}

// ═══════════════════════════════════════════
// recall — 检索记忆
// ═══════════════════════════════════════════
func cmdRecall(args []string) {
	fs := flag.NewFlagSet("recall", flag.ExitOnError)
	session := fs.String("session", getSession(), "会话标识")
	topK := fs.Int("top", 10, "返回数量")
	strategy := fs.String("strategy", "hybrid", "检索策略: hybrid|vector|keyword")
	fs.Parse(args)

	query := strings.Join(fs.Args(), " ")
	if query == "" {
		fmt.Fprintln(os.Stderr, "请提供检索关键词")
		return
	}

	eng := mustOpen()
	defer eng.Close()

	db := &dbAdapter{eng: eng}
	memAPI := manager.NewMemoryAPI(db, db, db, db)

	opts := &manager.SearchOptions{
		Query:    query,
		TopK:     *topK,
		Strategy: strategyFromString(*strategy),
	}
	results, err := memAPI.Reminisce(*session, opts)
	if err != nil {
		log.Fatalf("检索失败: %v", err)
	}

	// JSON Lines 输出（方便管道处理）
	for _, r := range results {
		item := map[string]interface{}{
			"id":      r.Memory.ID,
			"type":    r.Memory.Type.String(),
			"content": r.Memory.Content,
			"score":   r.Score,
			"tags":    r.Memory.Tags,
			"created": r.Memory.CreatedAt,
		}
		json.NewEncoder(os.Stdout).Encode(item)
	}
}

// ═══════════════════════════════════════════
// context — 生成 AI Prompt 后缀
// ═══════════════════════════════════════════
func cmdContext(args []string) {
	fs := flag.NewFlagSet("context", flag.ExitOnError)
	session := fs.String("session", getSession(), "会话标识")
	lang := fs.String("lang", "go", "目标语言")
	format := fs.String("format", "prompt", "输出格式: prompt|json")
	fs.Parse(args)

	task := strings.Join(fs.Args(), " ")
	if task == "" {
		fmt.Fprintln(os.Stderr, "请提供任务描述")
		fmt.Fprintln(os.Stderr, "  例: nebula context '添加用户认证中间件' --lang go")
		return
	}

	eng := mustOpen()
	defer eng.Close()

	db := &dbAdapter{eng: eng}
	memAPI := manager.NewMemoryAPI(db, db, db, db)
	injector := analyzer.NewContextInjector(*session, memAPI)

	ctx, err := injector.BuildContext(&analyzer.TaskContext{Task: task, Language: *lang})
	if err != nil {
		log.Fatalf("构建上下文失败: %v", err)
	}

	switch *format {
	case "json":
		json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
			"project":    ctx.ProjectOverview,
			"tech_stack": ctx.TechStack,
			"styles":     ctx.StyleNotes,
			"patterns":   ctx.RelevantPatterns,
			"gotchas":    ctx.Gotchas,
		})
	default:
		fmt.Println(ctx.PromptSuffix)
	}
}

// ═══════════════════════════════════════════
// summary — 记忆摘要
// ═══════════════════════════════════════════
func cmdSummary(args []string) {
	fs := flag.NewFlagSet("summary", flag.ExitOnError)
	session := fs.String("session", getSession(), "会话标识")
	fs.Parse(args)

	eng := mustOpen()
	defer eng.Close()

	db := &dbAdapter{eng: eng}
	memAPI := manager.NewMemoryAPI(db, db, db, db)

	printSummary(memAPI, *session)
}

// ═══════════════════════════════════════════
// export — 导出记忆
// ═══════════════════════════════════════════
func cmdExport(args []string) {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	session := fs.String("session", getSession(), "会话标识")
	format := fs.String("format", "json", "导出格式: json|jsonl|markdown")
	output := fs.String("output", "-", "输出文件 (默认 stdout)")
	fs.Parse(args)

	eng := mustOpen()
	defer eng.Close()

	db := &dbAdapter{eng: eng}

	// 获取所有记忆
	allKeys, _ := eng.KeysByPrefix("nebula:" + *session + ":")
	var memories []*manager.Memory
	for _, k := range allKeys {
		m, _ := db.Get(k)
		if m != nil {
			memories = append(memories, m)
		}
	}

	var out *os.File = os.Stdout
	if *output != "-" {
		f, err := os.Create(*output)
		if err != nil { log.Fatalf("无法创建文件: %v", err) }
		defer f.Close()
		out = f
	}

	switch *format {
	case "jsonl":
		for _, m := range memories {
			json.NewEncoder(out).Encode(m)
		}
	case "markdown":
		fmt.Fprintln(out, "# Nebula Memory Export\n")
		fmt.Fprintf(out, "**Session**: %s | **Total**: %d memories\n\n", *session, len(memories))
		for _, m := range memories {
			fmt.Fprintf(out, "## [%s] %s\n", m.Type.String(), strings.Join(m.Tags, ", "))
			fmt.Fprintf(out, "%s\n\n", m.Content)
			fmt.Fprintf(out, "> ID: `%s` | Importance: %.1f | Created: %s\n\n", m.ID, m.Importance, m.CreatedAt)
		}
	default:
		export := map[string]interface{}{
			"session":    *session,
			"count":      len(memories),
			"memories":   memories,
			"exported_at": "now",
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		enc.Encode(export)
	}
}

// ═══════════════════════════════════════════
// import — 导入记忆
// ═══════════════════════════════════════════
func cmdImport(args []string) {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	session := fs.String("session", getSession(), "会话标识")
	fs.Parse(args)

	inputFile := fs.Arg(0)
	if inputFile == "" {
		fmt.Fprintln(os.Stderr, "请指定导入文件")
		fmt.Fprintln(os.Stderr, "  例: nebula import memories.json")
		return
	}

	data, err := os.ReadFile(inputFile)
	if err != nil { log.Fatalf("读取文件失败: %v", err) }

	var memories []*manager.Memory
	if err := json.Unmarshal(data, &memories); err != nil {
		// 尝试解析为 {memories: [...]} 格式
		var wrapper struct {
			Memories []*manager.Memory `json:"memories"`
		}
		if err := json.Unmarshal(data, &wrapper); err != nil {
			log.Fatalf("JSON 解析失败: %v", err)
		}
		memories = wrapper.Memories
	}

	eng := mustOpen()
	defer eng.Close()

	db := &dbAdapter{eng: eng}
	memAPI := manager.NewMemoryAPI(db, db, db, db)

	imported := 0
	for _, m := range memories {
		m.SessionID = *session
		if err := memAPI.Remember(m); err != nil {
			fmt.Fprintf(os.Stderr, "跳过 %s: %v\n", m.ID, err)
			continue
		}
		imported++
	}

	fmt.Fprintf(os.Stderr, "✓ 成功导入 %d 条记忆 (总计 %d 条)\n", imported, len(memories))
	json.NewEncoder(os.Stdout).Encode(map[string]int{"imported": imported})
}

// ═══════════════════════════════════════════
// forget — 删除记忆
// ═══════════════════════════════════════════
func cmdForget(args []string) {
	fs := flag.NewFlagSet("forget", flag.ExitOnError)
	session := fs.String("session", getSession(), "会话标识")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "请提供要删除的记忆 ID")
		fmt.Fprintln(os.Stderr, "  例: nebula forget 01932a5b0000-7f3a1b2c")
		return
	}

	id := fs.Arg(0)

	eng := mustOpen()
	defer eng.Close()

	db := &dbAdapter{eng: eng}
	memAPI := manager.NewMemoryAPI(db, db, db, db)

	if err := memAPI.Forget(id, *session); err != nil {
		log.Fatalf("删除失败: %v", err)
	}
	fmt.Printf("✓ 已删除: %s\n", id)
}

// ═══════════════════════════════════════════
// style — 查看编码风格
// ═══════════════════════════════════════════
func cmdStyle(args []string) {
	fs := flag.NewFlagSet("style", flag.ExitOnError)
	session := fs.String("session", getSession(), "会话标识")
	fs.Parse(args)

	eng := mustOpen()
	defer eng.Close()

	db := &dbAdapter{eng: eng}
	memAPI := manager.NewMemoryAPI(db, db, db, db)
	injector := analyzer.NewContextInjector(*session, memAPI)

	ctx, _ := injector.BuildContext(&analyzer.TaskContext{Task: "风格", Language: "go"})

	fmt.Println("## 编码风格\n")
	for i, s := range ctx.StyleNotes {
		fmt.Printf("%d. %s\n", i+1, s)
	}
	if len(ctx.StyleNotes) == 0 {
		fmt.Println("(未学习到风格特征，请先运行 `nebula ingest`)")
	}
}

// ═══════════════════════════════════════════
// gotchas — 查看注意事项
// ═══════════════════════════════════════════
func cmdGotchas(args []string) {
	fs := flag.NewFlagSet("gotchas", flag.ExitOnError)
	session := fs.String("session", getSession(), "会话标识")
	fs.Parse(args)

	eng := mustOpen()
	defer eng.Close()

	db := &dbAdapter{eng: eng}
	memAPI := manager.NewMemoryAPI(db, db, db, db)
	injector := analyzer.NewContextInjector(*session, memAPI)

	ctx, _ := injector.BuildContext(&analyzer.TaskContext{Task: "注意事项", Language: "go"})

	fmt.Println("## ⚠ 项目注意事项\n")
	for i, g := range ctx.Gotchas {
		fmt.Printf("%d. ⚠ %s\n", i+1, g)
	}
	if len(ctx.Gotchas) == 0 {
		fmt.Println("(未发现显式的注意事项，请先运行 `nebula ingest`)")
	}
}

// ═══════════════════════════════════════════
// 辅助函数
// ═══════════════════════════════════════════

func mustOpen() *engine.Engine {
	eng, err := engine.Open(engine.DefaultOptions(getDataDir()))
	if err != nil {
		log.Fatalf("启动引擎失败: %v", err)
	}
	return eng
}

func printSummary(memAPI *manager.MemoryAPI, session string) {
	injector := analyzer.NewContextInjector(session, memAPI)
	fmt.Println(injector.Summarize())
}

func strategyFromString(s string) manager.SearchStrategy {
	switch s {
	case "vector": return manager.VectorSearch
	case "keyword": return manager.KeywordSearch
	case "temporal": return manager.TemporalSearch
	default: return manager.HybridSearch
	}
}

// dbAdapter 将 Engine 适配为 MemoryStore / VectorIndex / IndexStore 接口
type dbAdapter struct {
	eng *engine.Engine
}

func (d *dbAdapter) Put(key string, m *manager.Memory) error {
	data, _ := json.Marshal(m)
	d.eng.Put(key, m)
	_ = data
	return nil
}
func (d *dbAdapter) Get(key string) (*manager.Memory, error) { return d.eng.Get(key) }
func (d *dbAdapter) Del(key string) error                    { return d.eng.Del(key) }
func (d *dbAdapter) KeysByPrefix(prefix string) ([]string, error) {
	return d.eng.KeysByPrefix(prefix)
}

// VectorIndex
func (d *dbAdapter) Add(id int, vec []float32, key string) error { return d.eng.Add(id, vec, key) }
func (d *dbAdapter) Search(query []float32, k int) []manager.VectorSearchHit {
	return d.eng.Search(query, k)
}
func (d *dbAdapter) Delete(id int) { d.eng.Delete(id) }

// IndexStore
func (d *dbAdapter) Index(docID, text string) { d.eng.Index(docID, text) }
func (d *dbAdapter) Remove(docID string)       { d.eng.Remove(docID) }
func (d *dbAdapter) KeywordSearch(query string, topK int) []manager.KeywordSearchResult {
	return d.eng.KeywordSearch(query, topK)
}

// Embedder
func (d *dbAdapter) Embed(texts []string) ([][]float32, error) { return d.eng.Embed(texts) }
func (d *dbAdapter) Dimension() int                            { return d.eng.Dimension() }

// 确保接口实现一致
var _ manager.VectorIndex = (*dbAdapter)(nil)
var _ manager.IndexStore = (*dbAdapter)(nil)

func printUsage() {
	fmt.Println(`Nebula Code — 终端原生 AI 记忆管理

用法:
  nebula ingest   [--dir .] [--session 项目名]        扫描项目，学习代码特征
  nebula remember <内容> [--tags=标签] [--type=类型]    存储一条记忆（支持管道输入）
  nebula recall   <关键词> [--top=10]                  语义检索记忆
  nebula context  <任务描述> [--lang=go]               生成 AI Prompt 上下文
  nebula summary  [--session 项目名]                   查看记忆摘要
  nebula export   [--format=json|markdown] [--output=文件]  导出记忆
  nebula import   <文件> [--session 项目名]            从文件导入记忆
  nebula forget   <记忆ID>                             删除指定记忆
  nebula style                                         查看学到的编码风格
  nebula gotchas                                       查看项目注意事项

环境变量:
  NEBULA_DATA_DIR    记忆存储目录（默认: ~/.nebula/memory）
  NEBULA_SESSION     默认会话标识（默认: project-当前目录名）

示例 — 日常开发工作流:

  # 1. 首次使用：扫描你的项目
  nebula ingest --dir . --session my-backend

  # 2. 随时补充记忆
  nebula remember "用户认证用 JWT，secret 在环境变量 JWT_SECRET"
  nebula remember "数据库迁移用 golang-migrate，不要手动改 schema" --tags=数据库,规则
  echo "所有 API 统一返回 {code, data, message} 格式" | nebula remember

  # 3. 编码前获取上下文
  nebula context "添加用户注销接口" --lang go
  nebula context "重构订单模块" --lang go | Set-Clipboard   # PowerShell: 复制到剪贴板

  # 4. 查看到目前为止学了什么
  nebula summary
  nebula style
  nebula gotchas

  # 5. 检索特定记忆
  nebula recall "JWT 认证"
  nebula recall "数据库" --strategy=keyword --top=5

  # 6. 导出/导入（团队共享或备份）
  nebula export --format markdown --output project-memory.md
  nebula export --format json --output memories.json
  nebula import memories.json --session new-project

  # 7. 清理
  nebula forget <记忆ID>

数据目录: ~/.nebula/memory/
会话隔离: 不同项目的记忆通过 session 参数隔离
持久化: 默认写入磁盘，可通过 NEBULA_DATA_DIR 自定义位置`)
}
