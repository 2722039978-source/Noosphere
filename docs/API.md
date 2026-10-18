# Noosphere API 参考

Go sidecar 默认监听 `127.0.0.1:8080`。前端桌面应用通常通过 Tauri command bridge 调用这些能力；下表用于后端联调和集成测试。

## 健康与 Gateway

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/health` | 返回版本、时间和所有模块状态 |
| GET | `/api/gateway/health` | Gateway 健康检查 |
| POST | `/api/gateway/reload` | 重新加载模型配置 |
| GET | `/api/gateway/models` | 获取模型列表 |
| POST | `/api/gateway/models` | 添加或更新模型 |
| DELETE | `/api/gateway/models/{id}` | 删除模型 |
| POST | `/api/gateway/models/{id}/test` | 测试模型连通性 |
| POST | `/api/gateway/chat` | 非流式对话 |
| POST | `/api/gateway/chat/stream` | SSE 流式对话 |
| GET | `/api/gateway/stats` | 获取调用统计 |
| GET | `/api/gateway/logs` | 获取调用日志 |

## Nebula

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/stats` | 记忆引擎统计 |
| GET | `/api/v1/sessions` | 记忆会话列表 |
| POST | `/api/v1/sessions` | 创建记忆会话 |
| 多种 | `/api/v1/sessions/{id}/...` | 会话内的写入、查询和管理操作 |

## DevOps 与 CodeLens

| 方法 | 路径 | 说明 |
|---|---|---|
| 多种 | `/api/v1/devops/*` | 指标、工具、状态和诊断 |
| 多种 | `/api/v1/codelens/*` | 代理至 Python CodeLens worker |

## 请求约定

服务为每个请求返回 `X-Request-ID`。浏览器请求的 `Origin` 必须出现在 `server.allowed_origins` 中；本地进程间请求可以不发送 `Origin`。错误响应应使用 JSON 结构，未匹配路由由统一错误出口处理。
