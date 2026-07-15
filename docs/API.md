# API 参考

三个服务均为独立 REST API，可单独调用，也可组合使用。

| 服务 | Base URL | 交互式文档 |
|------|----------|-----------|
| 🔍 CodeLens | `http://localhost:8765` | http://localhost:8765/docs （Swagger） |
| ☁️ Nebula | `http://localhost:8730` | 控制台 http://localhost:8730 |
| ⚙️ DevOps | `http://localhost:8740` | 控制台 http://localhost:8740/web/ |

---

## 🔍 CodeLens（`:8765`，前缀 `/api/v1`）

### 索引与状态

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/v1/health` | 健康检查 |
| `GET` | `/api/v1/status` | 索引状态（文件数 / 实体数 / 关系数） |
| `POST` | `/api/v1/index` | 构建 / 重建项目索引 |

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
| `GET` | `/api/v1/call-chain/{entity_name}` | 追踪调用链 |
| `GET` | `/api/v1/impact/{entity_name}` | 修改影响分析（谁会受影响） |
| `GET` | `/api/v1/knowledge-graph/stats` | 图谱统计 |
| `GET` | `/api/v1/knowledge-graph/node/{name}` | 查询单个节点及其关系 |
| `GET` | `/api/v1/knowledge-graph/export` | 导出完整图谱 |

### 工程辅助

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/api/v1/git/diff` | Git Diff 风险分析 |
| `POST` | `/api/v1/docs/generate` | 自动生成项目文档 |
| `GET` | `/api/v1/issues` | 潜在问题列表 |

```bash
# 示例：谁调用了 authenticate？
curl "http://localhost:8765/api/v1/call-chain/authenticate"

# 示例：代码问答
curl -X POST http://localhost:8765/api/v1/qa \
  -H "Content-Type: application/json" \
  -d '{"question": "用户认证的完整流程是怎样的？"}'
```

---

## ☁️ Nebula（`:8730`）

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
| `POST` | `/api/v1/sessions/{id}/search` | 检索记忆（`mode`: `hybrid` / `vector` / `keyword` / `temporal`） |
| `GET` | `/api/v1/sessions/{id}/stats` | 会话统计 |

### AI 对话

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/api/v1/chat` | 带记忆自动注入的 DeepSeek 对话（需配置 `DEEPSEEK_API_KEY`） |

```bash
# 示例：存一条记忆，再语义检索
curl -X POST http://localhost:8730/api/v1/sessions \
  -H "Content-Type: application/json" -d '{"name": "my-project"}'

curl -X POST http://localhost:8730/api/v1/sessions/{id}/search \
  -H "Content-Type: application/json" \
  -d '{"query": "错误处理规范", "mode": "hybrid", "top_k": 5}'
```

---

## ⚙️ DevOps（`:8740`，前缀 `/api/v1/devops`）

### 状态与指标

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/health` | 健康检查 |
| `GET` | `/api/v1/devops/status` | Agent 状态 |
| `GET` | `/api/v1/devops/metrics` | 实时系统指标（CPU / 内存 / 磁盘 / 网络） |

### 诊断与工具

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/api/v1/devops/diagnose` | 自然语言故障诊断（结合历史故障记忆 + LLM 推理） |
| `GET` | `/api/v1/devops/tools` | 列出已注册的运维工具 |
| `POST` | `/api/v1/devops/tools/execute` | 执行指定工具 |

### 日志与故障记忆

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/api/v1/devops/logs/analyze` | 日志异常检测与分析 |
| `POST` | `/api/v1/devops/logs/search` | 日志检索 |
| `GET` | `/api/v1/devops/faults` | 故障经验列表 |
| `POST` | `/api/v1/devops/faults/search` | 语义检索历史故障（"上次这个报错怎么解决的"） |

```bash
# 示例：诊断 + 检索历史故障
curl -X POST http://localhost:8740/api/v1/devops/diagnose \
  -H "Content-Type: application/json" \
  -d '{"question": "最近 1 小时 CPU 异常升高"}'

curl -X POST http://localhost:8740/api/v1/devops/faults/search \
  -H "Content-Type: application/json" \
  -d '{"query": "数据库连接池耗尽"}'
```

---

## 认证说明

当前版本所有 API **面向本地 / 内网使用，无内置认证**。如需公网暴露，请置于反向代理（Nginx / Caddy）之后并自行添加认证层。LLM 相关端点依赖环境变量 `DEEPSEEK_API_KEY`（见根目录 [.env.example](../.env.example)）。
