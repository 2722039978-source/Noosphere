// DevOps Agent — 运维智能体 Web 服务入口
//
// 启动一体化运维 Agent 服务：
//   - 指标采集（CPU/内存/磁盘/进程）
//   - 工具调用（系统命令/日志查询/服务管理）
//   - Nebula Memory 故障记忆存储
//   - REST API + 明日方舟风格 Web 仪表盘
//
// 使用方式:
//
//	go run ./cmd/devops-server --port 8740
//	浏览器打开 http://localhost:8740/web/
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/devops-agent/agent"
	devopsAPI "github.com/devops-agent/api"
	"github.com/devops-agent/metrics"
	"github.com/devops-agent/tools"

	"github.com/nebula-agent/nebula/engine"
)

func main() {
	port := flag.Int("port", 8740, "HTTP 服务端口")
	memoryDir := flag.String("memory-dir", "./devops-memory", "Nebula 记忆引擎数据目录")
	noMemory := flag.Bool("no-memory", false, "禁用 Nebula 记忆引擎")
	flag.Parse()

	log.SetFlags(log.Ltime | log.Lshortfile)
	log.Printf("◈ DevOps Agent 运维智能体 v0.1.0")
	log.Printf("◈ 端口: %d | 记忆引擎: %s", *port, *memoryDir)

	// ─── 1. 初始化 Nebula 记忆引擎 ───
	var eng *engine.Engine
	if !*noMemory {
		opts := engine.DefaultOptions(*memoryDir)
		opts.WALEnabled = true
		opts.WALSyncOnWrite = true
		opts.TTLCheckFreq = 60

		var err error
		eng, err = engine.Open(opts)
		if err != nil {
			log.Printf("⚠ 记忆引擎启动失败（将以无记忆模式运行）: %v", err)
			eng = nil
		} else {
			defer eng.Close()
			log.Printf("◈ Nebula 记忆引擎已就绪 — LSM-Tree + HNSW + BM25")
		}
	}

	// ─── 2. 创建 DevOps Agent ───
	devopsAgent := agent.NewDevOpsAgent(eng, nil)

	// 注册所有运维工具
	log.Printf("◈ 正在注册运维工具...")
	tools.RegisterSystemTools(devopsAgent.Registry())
	tools.RegisterLogTools(devopsAgent.Registry())
	tools.RegisterServiceTools(devopsAgent.Registry())
	log.Printf("◈ 已注册 %d 个运维工具", devopsAgent.Registry().Count())

	// ─── 3. 初始化系统指标采集 ───
	collector := metrics.NewSystemCollector()
	snapshot, err := collector.Collect()
	if err == nil {
		log.Printf("◈ 系统指标采集就绪 — CPU: %.1f%% | 内存: %.1f%% | 磁盘: %.1f%%",
			snapshot.CPU.UsagePercent, snapshot.Memory.UsagePercent, snapshot.Disk.UsagePercent)
	}

	// ─── 4. 注册 API 路由 ───
	mux := http.NewServeMux()

	// DevOps API
	apiHandler := devopsAPI.NewDevOpsHandler(devopsAgent)
	apiHandler.RegisterRoutes(mux)

	// 健康检查
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok","service":"devops-agent","version":"0.1.0"}`)
	})

	// 静态文件 —— 前端仪表盘
	fs := http.FileServer(http.Dir("./web"))
	mux.Handle("/web/", http.StripPrefix("/web/", fs))
	// 根路径重定向
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/web/", http.StatusFound)
			return
		}
		fs.ServeHTTP(w, r)
	})

	// ─── 5. 启动服务 ───
	addr := fmt.Sprintf(":%d", *port)
	log.Printf("◈ DevOps Agent HTTP 服务启动: http://localhost%s", addr)
	log.Printf("◈ 仪表盘: http://localhost%s/web/", addr)
	log.Printf("◈ API 文档: http://localhost%s/web/ (查看页面底部)", addr)

	// 优雅退出
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("◈ 正在关闭 DevOps Agent...")
		os.Exit(0)
	}()

	if err := http.ListenAndServe(addr, corsMiddleware(mux)); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}

// corsMiddleware CORS 中间件
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
