# API 参考

所有服务通过 **Noosphere 平台 `:3000`** 统一访问。后端 API 对内运行，开发时可直连调试。

| 服务 | 平台入口 | 内部地址 | 文档 |
|------|---------|---------|------|
| 🔑 AI Gateway | `/settings/model`（UI）+ `/api/gateway`（API） | `:3000`（内嵌） | — |
| 🔍 CodeLens | `/codelens`（UI） | `:8765` | http://localhost:8765/docs（Swagger） |
| ☁️ Nebula | `/nebula`（UI） | `:8730` | http://localhost:8730 |
| ⚙️ DevOps | `/devops`（UI） | `:8740` | http://localhost:8740/web/ |

---

## 🔑 AI Gateway（`/api/gateway` · 平台内嵌）

AI Gateway 不是独立服务。它作为 Next.js API Route 运行在平台 `:3000` 内部。

### 模型管理

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/gateway/models` | 列出已配置模型 |
| `POST` | `/api/gateway/models` | 添加或更新模型 |
| `DELETE` | `/api/gateway/models/{id}` | 删除模型 |
| `POST` | `/api/gateway/models/{id}/test` | 测试模型连通性 |

### 统一 LLM 调用

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/api/gateway/chat` | 聊天补全——自动路由到用户配置的模型 |
| `POST` | `/api/gateway/chat/stream` | 流式聊天（SSE） |
| `POST` | `/api/gateway/vision` | 视觉/图像理解 |
| `POST` | `/api/gateway/embedding` | 文本嵌入 |

### 监控

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/gateway/health` | 健康检查 |
| `GET` | `/api/gateway/stats` | 按模型和项目的用量统计 |
| `GET` | `/api/gateway/logs` | 最近调用日志 |

```bash
# 测试模型连通性
curl -X POST http://localhost:3000/api/gateway/models/deepseek-v4/test

# 通过 Gateway 聊天
curl -X POST http://localhost:3000/api/gateway/chat \
  -H "Content-Type: application/json" \
  -d '{"messages":[{"role":"user","content":"你好"}],"project":"codelens"}'

# 查看用量统计
curl http://localhost:3000/api/gateway/stats
```

---

## 🔍 CodeLens（`:8765` · 开发者智能工作区）

所有路由前缀 `/api/v1`。

### 索引与状态

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/v1/health` | 健康检查 |
| `GET` | `/api/v1/status` | 索引状态（文件数 / 实体数 / 关系数） |
| `POST` | `/api/v1/index` | 构建/重建项目索引 |

### 代码理解

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/api/v1/qa` | 基于 RAG 的代码问答 |
| `POST` | `/api/v1/search` | 语义搜索代码实体 |
| `POST` | `/api/v1/analyze/file` | 单文件结构分析 |
| `POST` | `/api/v1/analyze/explain` | 代码片段解释 |
| `WS` | `/api/v1/ws/chat` | 流式对话（WebSocket） |

### 图谱查询

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/v1/call-chain/{entity}` | 追踪调用链 |
| `GET` | `/api/v1/impact/{entity}` | 影响分析 |
| `GET` | `/api/v1/knowledge-graph/stats` | 图谱统计 |
| `GET` | `/api/v1/knowledge-graph/node/{name}` | 查询单个节点 |
| `GET` | `/api/v1/knowledge-graph/export` | 导出完整图谱 |

### 工程辅助

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/api/v1/git/diff` | Git Diff 风险分析 |
| `POST` | `/api/v1/docs/generate` | 自动生成项目文档 |
| `GET` | `/api/v1/issues` | 潜在问题列表 |

### 工作区与验证

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/api/v1/workspace/scan` | 扫描 workspace 中的项目 |
| `GET` | `/api/v1/workspace/projects` | 列出已发现项目 |
| `GET` | `/api/v1/workspace/projects/{name}` | 项目详情 |
| `POST` | `/api/v1/workspace/projects/{name}/analyze` | 分析指定项目 |
| `POST` | `/api/v1/workspace/validate/llm` | 测试 LLM 连通性 |
| `POST` | `/api/v1/workspace/validate/tools` | 验证工具链 I/O |
| `POST` | `/api/v1/workspace/validate/all` | 全量系统诊断 |

```bash
# 代码问答
curl -X POST http://localhost:8765/api/v1/qa \
  -H "Content-Type: application/json" \
  -d '{"question": "认证模块的调用链是怎样的？"}'

# 追踪调用链
curl "http://localhost:8765/api/v1/call-chain/authenticate?max_depth=5"

# 扫描项目
curl -X POST http://localhost:8765/api/v1/workspace/scan
```

---

## ☁️ Nebula（`:8730` · 记忆引擎）

### 会话与记忆

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/health` | 健康检查 |
| `GET` | `/api/v1/stats` | 引擎统计 |
| `POST` | `/api/v1/sessions` | 创建会话 |
| `GET` | `/api/v1/sessions` | 列出会话 |
| `DELETE` | `/api/v1/sessions/{id}` | 删除会话 |
| `POST` | `/api/v1/sessions/{id}/memories` | 存储记忆 |
| `GET` | `/api/v1/sessions/{id}/memories/{mid}` | 获取记忆 |
| `DELETE` | `/api/v1/sessions/{id}/memories/{mid}` | 删除记忆 |
| `POST` | `/api/v1/sessions/{id}/search` | 检索记忆（`mode`: hybrid/vector/keyword/temporal） |
| `GET` | `/api/v1/sessions/{id}/stats` | 会话统计 |

### AI 对话

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/api/v1/chat` | 带记忆自动注入的对话 |

```bash
# 创建会话并存一条记忆
curl -X POST http://localhost:8730/api/v1/sessions \
  -H "Content-Type: application/json" -d '{"session_id": "my-agent"}'

curl -X POST http://localhost:8730/api/v1/sessions/my-agent/memories \
  -H "Content-Type: application/json" \
  -d '{"content": "错误处理使用显式 error 返回，不用 panic","type":"semantic","importance":0.8}'

# 语义检索
curl -X POST http://localhost:8730/api/v1/sessions/my-agent/search \
  -H "Content-Type: application/json" \
  -d '{"query":"错误处理规范","top_k":5}'
```

---

## ⚙️ DevOps（`:8740` · 运维智能工作区）

所有路由前缀 `/api/v1/devops`。

### 状态与指标

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/health` | 健康检查 |
| `GET` | `/api/v1/devops/status` | Agent 状态 |
| `GET` | `/api/v1/devops/metrics` | 实时 CPU / 内存 / 磁盘 / 网络 |

### 诊断与工具

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/api/v1/devops/diagnose` | 自然语言故障诊断 |
| `GET` | `/api/v1/devops/tools` | 列出已注册工具 |
| `POST` | `/api/v1/devops/tools/execute` | 执行指定工具 |

### 日志与故障记忆

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/api/v1/devops/logs/analyze` | 日志异常分析 |
| `POST` | `/api/v1/devops/logs/search` | 日志检索 |
| `GET` | `/api/v1/devops/faults` | 故障经验列表 |
| `POST` | `/api/v1/devops/faults/search` | 语义检索历史故障 |

```bash
# 诊断
curl -X POST http://localhost:8740/api/v1/devops/diagnose \
  -H "Content-Type: application/json" \
  -d '{"query":"最近 1 小时 CPU 异常升高","session_id":"ops-1"}'

# 检索历史故障
curl -X POST http://localhost:8740/api/v1/devops/faults/search \
  -H "Content-Type: application/json" \
  -d '{"query":"数据库连接池耗尽"}'
```

---

## 认证说明

所有 API 面向**本地 / 内网使用**，无内置认证。如需公网暴露，请置于反向代理（Nginx / Caddy）之后自行添加认证。

### LLM Key 配置（三级优先级）

1. `workspace/llm_config.yaml` — 多提供商配置（推荐，支持 DeepSeek / OpenAI / Anthropic / Ollama）
2. `.env` 文件 — `DEEPSEEK_API_KEY`
3. 环境变量 — `DEEPSEEK_API_KEY`

**推荐通过 UI 管理**：打开平台 `/settings/model` 即可。

## 快速启动

```bash
# 一键启动全部服务
cd Noosphere && start.bat

# 或逐个启动
cd nebula    && go run ./cmd/nebula-server --port 8730
cd devops    && go run ./cmd/devops-server --port 8740
cd codelens  && python -m src.main serve
cd web       && npm run dev

# 打开 http://localhost:3000
```
