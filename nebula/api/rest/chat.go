package rest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/nebula-agent/nebula/analyzer"
	"github.com/nebula-agent/nebula/engine/manager"
)

// ChatHandler AI 对话处理器（DeepSeek V4 + Nebula 记忆自动注入）
type ChatHandler struct {
	memAPI *manager.MemoryAPI
	apiKey string
	apiURL string
	model  string
}

// NewChatHandler 创建对话处理器
func NewChatHandler(memAPI *manager.MemoryAPI, apiKey string) *ChatHandler {
	return &ChatHandler{
		memAPI: memAPI,
		apiKey: apiKey,
		apiURL: "https://api.deepseek.com/chat/completions",
		model:  "deepseek-v4-pro",
	}
}

// ─── ChatRequest / ChatResponse ───

type chatRequest struct {
	SessionID     string        `json:"session_id"`
	Message       string        `json:"message"`
	Language      string        `json:"language"`
	History       []chatMessage `json:"history"`
	InjectContext bool          `json:"inject_context"`
	Stream        bool          `json:"stream"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (h *ChatHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "只支持 POST 请求")
		return
	}

	// 检查 API Key
	if h.apiKey == "" {
		writeError(w, http.StatusInternalServerError, "未配置 DeepSeek API Key，启动时请加 --api-key")
		return
	}

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "JSON 解析失败: "+err.Error())
		return
	}

	if req.Message == "" {
		writeError(w, http.StatusBadRequest, "消息不能为空")
		return
	}
	if req.Language == "" {
		req.Language = "go"
	}
	if req.SessionID == "" {
		req.SessionID = "default"
	}

	// ─── 构建系统提示词 ───
	systemPrompt := h.buildSystemPrompt(req.SessionID, req.Language, req.InjectContext)

	// ─── 构建消息 ───
	messages := []chatMessage{
		{Role: "system", Content: systemPrompt},
	}
	for _, msg := range req.History {
		messages = append(messages, msg)
	}
	messages = append(messages, chatMessage{Role: "user", Content: req.Message})

	// ─── 调用 DeepSeek ───
	aiReq := map[string]interface{}{
		"model":           h.model,
		"messages":        messages,
		"stream":          false,
		"temperature":     0.3,
		"max_tokens":      4096,
		"reasoning_effort": "medium",
	}

	body, _ := json.Marshal(aiReq)
	httpReq, _ := http.NewRequest("POST", h.apiURL, bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+h.apiKey)

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		log.Printf("[Chat] DeepSeek 请求失败: %v", err)
		writeError(w, http.StatusBadGateway, "DeepSeek API 请求失败: "+err.Error())
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		log.Printf("[Chat] DeepSeek API 返回 %d: %s", resp.StatusCode, string(respBody))
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"reply":  fmt.Sprintf("⚠ DeepSeek API 错误 (%d): %s", resp.StatusCode, string(respBody[:min(300, len(respBody))])),
			"model":  h.model,
			"status": "error",
		})
		return
	}

	// 解析响应
	var aiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &aiResp); err != nil {
		writeError(w, http.StatusInternalServerError, "解析 DeepSeek 响应失败: "+err.Error())
		return
	}

	reply := ""
	if len(aiResp.Choices) > 0 {
		reply = aiResp.Choices[0].Message.Content
	}

	log.Printf("[Chat] 对话完成 — 回复长度: %d 字符", len(reply))

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"reply":                reply,
		"model":                h.model,
		"status":               "ok",
		"context_injected":     req.InjectContext,
		"system_prompt_length": len(systemPrompt),
	})
}

// ─── 系统提示词构建 ───

func (h *ChatHandler) buildSystemPrompt(sessionID, language string, inject bool) string {
	base := `你是一位专业的编程助手，同时也是 **Nebula Agent（星云智能体）** 项目的专家。

## 当前项目：Nebula Agent
你正在帮助开发的是一个 **轻量级嵌入式 AI Agent Memory Engine**（类 SQLite + Redis + 向量检索的组合），用纯 Go 实现，零外部依赖。

### 项目架构
- **engine/storage/lsm/** — 自研 LSM Tree 存储引擎（WAL + SkipList MemTable + SSTable + Bloom Filter + Leveled Compaction）
- **engine/storage/kv/** — 分片锁 In-Memory KV Store（32路分片 + TTL堆 + FNV Hash）
- **engine/storage/vector/** — 纯 Go HNSW 向量索引（零 CGo，可交叉编译）
- **engine/retrieval/** — Embedder 接口 + 混合检索（RRF 融合排序：向量 + BM25 + 时间衰减）
- **engine/index/** — 倒排索引 + BM25 关键词检索（中英分词）
- **engine/manager/** — 四种记忆类型（工作/情景/语义/程序）+ 记忆整合引擎
- **engine/cache/** — LRU 热点缓存
- **engine/engine.go** — 核心引擎，实现 MemoryStore/VectorIndex/IndexStore/Embedder 四个接口
- **analyzer/** — 代码摄取器 + 风格学习器 + 上下文注入器
- **api/rest/** — REST API（纯 Go net/http，零框架）
- **cmd/nebula-server/** — 独立服务入口
- **cmd/nebula-code/** — CLI 终端记忆管理工具
- **web/** — 莱茵生命风格前端仪表盘（原生 HTML/CSS/JS）
- **sdk/go/**, **sdk/python/** — Go/Python SDK

### 设计原则
1. **零外部依赖** — LSM/HNSW/KV/HTTP 全部自研纯 Go
2. **可嵌入** — 类似 SQLite 的 import 即用体验
3. **四种记忆类型** — 对应认知心理学的工作/情景/语义/程序记忆
4. **混合检索** — RRF 融合向量语义搜索 + BM25 关键词搜索
5. **记忆整合** — 旧情景记忆自动压缩为语义记忆
6. **前后端分离** — Go REST API + 原生前端（莱茵生命明日方舟风格）

### 关键约定
- 命名：Go 标准 PascalCase（导出）/ camelCase（私有）
- 错误处理：显式 error 返回，不使用 panic
- 测试：Go 表驱动测试
- 文件组织：按功能分包（engine/storage/lsm/, engine/retrieval/, etc.）
- 接口优先：核心组件通过接口组合

## 回答原则
1. 生成的代码必须符合上述项目架构和编码规范
2. 优先使用项目已有的模块和接口，不引入外部依赖
3. 回复简洁精准，给出可直接使用、能编译的代码
4. 理解项目四大记忆类型和混合检索的设计意图`

	if !inject || sessionID == "" || h.memAPI == nil {
		return base
	}

	// 安全地获取记忆
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Chat] 记忆注入 panic (已恢复): %v", r)
		}
	}()

	injector := analyzer.NewContextInjector(sessionID, h.memAPI)
	ctx, err := injector.BuildContext(&analyzer.TaskContext{
		Task:     "编程助手上下文",
		Language: language,
	})
	if err != nil || ctx == nil {
		return base
	}

	var parts []string
	parts = append(parts, base)

	if ctx.ProjectOverview != "" {
		parts = append(parts, "\n## 当前项目\n"+ctx.ProjectOverview)
	}
	if ctx.TechStack != "" {
		parts = append(parts, "\n## 技术栈\n"+ctx.TechStack)
	}
	if len(ctx.StyleNotes) > 0 {
		parts = append(parts, "\n## 编码风格（必须遵守）\n"+strings.Join(ctx.StyleNotes, "\n"))
	}
	if len(ctx.Gotchas) > 0 {
		parts = append(parts, "\n## ⚠ 注意事项\n"+strings.Join(ctx.Gotchas, "\n"))
	}

	return strings.Join(parts, "\n")
}

// ─── 流式未使用但保留接口 ───

func (h *ChatHandler) handleSSE(w http.ResponseWriter, resp *http.Response) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)

	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil { break }
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") { continue }
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			fmt.Fprintf(w, "data: {\"done\":true}\n\n")
			flusher.Flush()
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
}
