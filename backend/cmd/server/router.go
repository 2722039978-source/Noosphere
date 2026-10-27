package main

import (
	"encoding/json"
	"net/http"
	"os"
	"time"

	"github.com/noosphere/backend/internal/core"
	"github.com/noosphere/backend/internal/gateway"
	"github.com/noosphere/backend/internal/modellab"
	"github.com/noosphere/backend/internal/modelops"
	"github.com/noosphere/backend/internal/workspaces"
)

func registerModelEngineeringRoutes(mux *http.ServeMux, lab *modellab.Service, ops *modelops.Service, manager *workspaces.Manager) {
	lab.RegisterRoutes(mux, manager)
	ops.RegisterRoutes(mux, manager)
}

// registerRoutes wires every API endpoint onto the mux.
// Go 1.22+ method+path patterns give per-endpoint 404/405 semantics for free.
//
// 路由总表 — 全项目端点唯一权威声明处:
//
//	GET    /health                          统一健康检查
//	GET    /api/gateway/health              Gateway 健康
//	POST   /api/gateway/reload              重载 models.yaml
//	GET    /api/gateway/models              模型列表
//	POST   /api/gateway/models              添加/更新模型
//	DELETE /api/gateway/models/{id}         删除模型
//	POST   /api/gateway/models/{id}/test    连通性测试
//	POST   /api/gateway/chat                非流式对话
//	POST   /api/gateway/chat/stream         流式对话 (SSE)
//	GET    /api/gateway/stats               调用统计
//	GET    /api/gateway/logs                调用日志
//	GET    /api/v1/stats                    Nebula 统计
//	GET    /api/v1/sessions                 会话列表
//	POST   /api/v1/sessions                 创建会话
//	*      /api/v1/sessions/{id}/...        会话内操作 (nebula)
//	*      /api/v1/devops/*                 运维端点 (devops)
//	*      /api/v1/codelens/*               代码分析 (代理至 Python)
func registerRoutes(mux *http.ServeMux, lc *core.Lifecycle, gw *gateway.Gateway) {
	// ---- health ----
	mux.HandleFunc("GET /health", healthHandler(lc))

	// ---- gateway ----
	mux.HandleFunc("GET /api/gateway/health", gw.HandleHealth)
	mux.HandleFunc("POST /api/gateway/reload", gw.HandleReload)
	mux.HandleFunc("GET /api/gateway/models", gw.HandleListModels)
	mux.HandleFunc("POST /api/gateway/models", gw.HandleAddModel)
	mux.HandleFunc("DELETE /api/gateway/models/{id}", gw.HandleDeleteModel)
	mux.HandleFunc("POST /api/gateway/models/{id}/test", gw.HandleTestModel)
	mux.HandleFunc("GET /api/gateway/selection", gw.HandleModelSelection)
	mux.HandleFunc("POST /api/gateway/selection", gw.HandleSelectModel)
	mux.HandleFunc("POST /api/gateway/chat", gw.HandleChat)
	mux.HandleFunc("POST /api/gateway/chat/stream", gw.HandleChatStream) // SSE: 自管响应写出
	mux.HandleFunc("GET /api/gateway/stats", gw.HandleStats)
	mux.HandleFunc("GET /api/gateway/logs", gw.HandleLogs)
}

// healthHandler returns server status, version, and module states as JSON.
func healthHandler(lc *core.Lifecycle) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":      "ok",
			"instance_id": os.Getenv("NOOSPHERE_BACKEND_INSTANCE"),
			"version":     Version,
			"modules":     lc.Snapshot(),
			"time":        time.Now().UTC().Format(time.RFC3339),
		})
	}
}
