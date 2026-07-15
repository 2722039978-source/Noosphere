// Nebula Agent — 轻量级嵌入式 AI Agent Memory Engine
//
// 用法:
//
//	# 启动服务器（持久化模式）
//	nebula-server --data ./nebula-data --port 8730
//
//	# 纯内存模式（数据不持久化）
//	nebula-server --memory --port 8730
//
//	# 嵌入式使用（代码中直接引用 engine 包）
//	import "github.com/nebula-agent/nebula/engine"
package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/nebula-agent/nebula/api/rest"
	"github.com/nebula-agent/nebula/engine"
	"github.com/nebula-agent/nebula/service"
)

func main() {
	dataDir := flag.String("data", "nebula-data", "数据存储目录")
	memory := flag.Bool("memory", false, "纯内存模式（数据不持久化）")
	port := flag.String("port", "8730", "HTTP 服务端口")
	_ = flag.String("embedder", "mock", "Embedding 提供者: mock|http")
	_ = flag.String("embedder-url", "http://localhost:11434/api/embeddings", "Embedding API 地址")
	_ = flag.String("embedder-model", "nomic-embed-text", "Embedding 模型名")
	apiKey := flag.String("api-key", "", "DeepSeek API Key（留空时读取环境变量 DEEPSEEK_API_KEY）")
	flag.Parse()

	// 密钥安全读取：命令行参数 > 环境变量，绝不硬编码
	if *apiKey == "" {
		*apiKey = os.Getenv("DEEPSEEK_API_KEY")
	}

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println(`
  ┌┐ ┐─┐┐─┐┬─┐┬  ┌─┐
  ││ │─│└─┐│  │  ├─┤
  └┘ ┘ └──┘┘  ┴  ┴ ┴
  Nebula Agent — Memory Engine v0.1.0
  `)

	// 创建引擎
	var opts *engine.Options
	if *memory {
		opts = engine.MemoryOptions()
		log.Println("[Nebula] Mode: In-Memory (data will not persist)")
	} else {
		opts = engine.DefaultOptions(*dataDir)
		log.Printf("[Nebula] Mode: Persistent (data dir: %s)", *dataDir)
	}

	eng, err := engine.Open(opts)
	if err != nil {
		log.Fatalf("Failed to open engine: %v", err)
	}
	defer eng.Close()

	log.Println("[Nebula] Engine started successfully")

	// 创建 Service
	svc := service.New(eng, nil)

	// 启动 REST API
	addr := ":" + *port
	server := rest.NewServerWithLLM(svc, addr, *apiKey)

	// 优雅关闭
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("[Nebula] Shutting down...")
		eng.Close()
		os.Exit(0)
	}()

	if err := server.Start(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
