# 架构设计

> Noosphere 是一个 **AI 操作系统**，不是一个独立工具的拼装。
> 本文档描述平台拓扑、层级职责、数据流和设计决策。

---

## 平台拓扑

```
                     Noosphere Platform (:3000)
        ┌──────────────────────────────────────────────────┐
        │              Next.js 14 · 单一入口               │
        │                                                  │
        │  /              平台 Dashboard                   │
        │  /codelens      CodeLens 开发者工作区            │
        │  /nebula        Nebula 记忆引擎                  │
        │  /devops        DevOps 运维工作区                │
        │  /settings/model AI Gateway（系统设置）          │
        │  /api/gateway   LLM 代理路由                     │
        └──────┬──────────┬──────────┬─────────────────────┘
               │          │          │
          ┌────▼────┐ ┌───▼────┐ ┌──▼─────┐
          │CodeLens │ │ Nebula │ │ DevOps │
          │ :8765   │ │ :8730  │ │ :8740  │
          │ Python  │ │  Go    │ │  Go    │
          └─────────┘ └────────┘ └────────┘
               内部 API —— 不对用户暴露
```

**关键设计决策：**

- **单一端口 `:3000`** — 用户只访问一个地址。所有页面通过 Next.js App Router 统一路由。
- **AI Gateway 不是独立服务** — 嵌入为 `/settings/model`（管理界面）+ `/api/gateway`（LLM 代理路由）。不占独立端口。
- **后端 API 对内运行** — CodeLens / Nebula / DevOps 作为内部服务，平台通过 iframe 嵌入或直连 API。用户无需知道 `:8765`、`:8730`、`:8740`。
- **端口分配**：`3000`（平台前端）、`8765/8766`（CodeLens API + WebSocket）、`8730`（Nebula API）、`8740`（DevOps API）。

---

## 分层架构

```
┌──────────────────────────────────────────────────────────┐
│                    表现层                                │
│  Next.js 14 · React 18 · TypeScript · Three.js · GSAP   │
│  Tailwind CSS · Framer Motion · Lucide Icons            │
│  单一端口 (:3000) · App Router                          │
├──────────────────────────────────────────────────────────┤
│                    工作区层                              │
│  ┌────────────┐  ┌────────────┐  ┌────────────────┐    │
│  │  CodeLens  │  │   Nebula   │  │    DevOps      │    │
│  │  开发者智能 │  │   记忆引擎 │  │   运维智能     │    │
│  │  Python    │  │   Go       │  │    Go          │    │
│  └─────┬──────┘  └─────┬──────┘  └──────┬─────────┘    │
├────────┼───────────────┼─────────────────┼──────────────┤
│        └───────────────┼─────────────────┘              │
│                 ┌──────▼──────┐                          │
│                 │ AI Gateway  │   平台基础设施           │
│                 │ /settings/  │   系统设置 · 非独立服务  │
│                 │   model     │                          │
│                 │ /api/gateway│   LLM 代理路由           │
│                 └─────────────┘                          │
├──────────────────────────────────────────────────────────┤
│                    基础设施层                            │
│  ┌──────────┐  ┌───────────┐  ┌──────────────────┐     │
│  │ LSM-Tree │  │ HNSW      │  │ NetworkX         │     │
│  │ WAL+SST  │  │ 向量索引  │  │ 知识图谱         │     │
│  │ Go/Python│  │           │  │ Python           │     │
│  └──────────┘  └───────────┘  └──────────────────┘     │
│  ┌──────────┐  ┌───────────┐  ┌──────────────────┐     │
│  │ ChromaDB │  │ BM25      │  │ RRF 混合融合     │     │
│  │ 向量数据库│  │ 关键词检索│  │ 多路召回排序     │     │
│  └──────────┘  └───────────┘  └──────────────────┘     │
└──────────────────────────────────────────────────────────┘
```

---

## Layer 1 — CodeLens（Python，开发者智能工作区）

**职责：把平面代码变成可查询的结构化知识。**

```
源代码 → Tree-sitter AST 解析 → 实体提取
                                    ↓
                ┌───────────────────────────────────┐
                │       知识图谱 (NetworkX)         │
                │  节点：函数、类、方法等           │
                │  边：调用、继承、导入关系         │
                └───────────────┬───────────────────┘
                                ↓
         ┌──────────────────────────────────────────┐
         │           双存储                         │
         │  ┌─────────────┐  ┌──────────────────┐   │
         │  │  LSM 存储   │  │  向量存储        │   │
         │  │  (结构化)   │  │  (ChromaDB)      │   │
         │  └─────────────┘  └────────┬─────────┘   │
         └────────────────────────────┼────────────┘
                                      ↓
         ┌──────────────────────────────────────────┐
         │           RAG 混合检索                   │
         │  语义 + 结构 + 关键词                    │
         └────────────────┬─────────────────────────┘
                          ↓
         ┌──────────────────────────────────────────┐
         │         LLM 增强生成                     │
         │  (通过 AI Gateway /api/gateway/chat)     │
         └──────────────────────────────────────────┘
```

| 组件 | 技术 | 作用 |
|------|------|------|
| 解析层 | Tree-sitter（7 种语言） | 语法级精确解析，非正则匹配 |
| 图谱层 | NetworkX | 调用图 / 继承图 / 依赖图 |
| 存储层 | LSM-Tree + ChromaDB | 实体 KV 存储 + 语义向量检索 |
| RAG | 混合 RRF 融合 | 图谱检索 + 向量检索 + 关键词检索 |
| LLM | AI Gateway 代理 | 所有 LLM 调用统一走 `/api/gateway/chat` |

**核心设计**：查询"谁调用了 X"不走 LLM，直接 O(1) 图谱查询。这是 99.6% token 节省的来源。LLM 只在最后做自然语言综合。

---

## Layer 2 — Nebula（Go，记忆引擎）

**职责：让 AI 拥有长期记忆——四种记忆类型，一个引擎。**

| 记忆类型 | 用途 | 生命周期 |
|---------|------|---------|
| 工作记忆 | 当前任务上下文，类比 CPU 寄存器 | 会话级（默认 5 分钟） |
| 情景记忆 | 过去的交互与决策，类比内存 | 永久 |
| 语义记忆 | 学到的事实与模式，类比长期记忆 | 永久 |
| 程序记忆 | 掌握的工作流与技能，类比肌肉记忆 | 永久 |

**引擎核心（零外部依赖）：**

| 组件 | 技术 | 作用 |
|------|------|------|
| LSM-Tree | 自研（WAL + SkipList MemTable + SSTable + Bloom Filter） | 持久化记忆存储 |
| HNSW | 纯 Go，零 CGo | 近似最近邻语义检索 |
| BM25 | 自研倒排索引 | 精确关键词匹配 |
| RRF | Reciprocal Rank Fusion | 语义 + 关键词双路融合排序 |
| Embedder | 可插拔（mock / Ollama / OpenAI 兼容） | 文本向量化 |

**核心设计**：`engine` 包可嵌入——DevOps 以 Go module 方式直接引用（`replace ../nebula`）。服务模式与嵌入模式共用同一套引擎代码。

---

## Layer 3 — DevOps（Go，运维智能工作区）

**职责：把一次性的故障处理变成可复用的组织记忆。**

```
系统指标 / 服务 / 日志
          ↓
    ┌─────┴─────┐
    │ 采集器    │  CPU · 内存 · 磁盘 · 网络 · 进程
    │ 分析器    │  日志模式识别 + LLM 根因推理
    └─────┬─────┘
          ↓
    ┌─────┴─────┐
    │  Agent    │  诊断编排 + 工具调度
    └─────┬─────┘
          ↓
    ┌─────┴──────────────────┐
    │  工具注册中心           │
    │  系统 · 日志 · 服务     │  可扩展工具框架
    └─────┬──────────────────┘
          ↓
    ┌─────┴─────┐
    │ 故障存储  │  故障 → 诊断 → 解决方案三元组
    │ (Nebula)  │  语义检索历史相似故障
    └───────────┘
```

**核心设计**：诊断时先检索历史故障记忆，再调用 LLM——"上周见过的故障"直接命中方案，无需重新推理。

---

## AI Gateway — 平台基础设施

AI Gateway 不是独立产品，是平台的**系统设置模块**。

```
CodeLens ──┐
Nebula  ──┤── AI Gateway (/api/gateway) ──► 用户模型
DevOps  ──┘
```

| 功能 | 位置 |
|------|------|
| 模型管理 UI | `/settings/model` |
| LLM 代理 | `/api/gateway/chat`（Next.js API Route） |
| 模型配置文件 | `workspace/llm_config.yaml` |
| Go SDK | `sdk/aiGateway/go/client.go` |
| Python SDK | `sdk/aiGateway/python/client.py` |

**所有工作区的 LLM 调用统一走 Gateway。禁止直连模型 API。**

---

## 数据流：平台中的一天

```text
1. 用户打开 http://localhost:3000
   → Next.js 渲染 Dashboard
   → useServiceStatus 轮询三个后端健康状态
   → Dashboard 展示在线状态 + 当前模型 + Token 用量

2. 用户导航到 /codelens
   → WorkspaceLayout 以 iframe 嵌入 CodeLens :8765
   → CodeLens 聊天调用 /api/gateway/chat（Next.js API 路由）
   → API Route 读取模型配置，路由到用户指定的 LLM 提供商
   → 返回响应，记录 Token 日志

3. 用户在 /settings/model 配置新模型
   → 表单提交到 /api/gateway/models
   → 写入 workspace/llm_config.yaml
   → 所有工作区立刻可用新模型

4. Nebula 后端（Go）需要 Embedding
   → 调用 Go SDK：client.Embedding(...)
   → SDK 直接读取 workspace/llm_config.yaml
   → 路由到配置的提供商
   → 后端服务无需 HTTP 代理
```

---

## 数据与安全

| 事项 | 策略 |
|------|------|
| API Key | `workspace/llm_config.yaml`（gitignored）、`.env`、环境变量 —— 三级读取 |
| 数据落盘 | 全部本地：CodeLens → `codelens-data`；Nebula → `nebula-data`；DevOps → `devops-memory` |
| 出网流量 | 仅问答/诊断时向 LLM 发送相关片段，索引与检索全程本地 |
| 容器构建 | Go 多阶段构建（`golang:1.26-alpine` → `alpine:3.20`），产物 ~10MB，`CGO_ENABLED=0` 静态编译 |

## 构建备忘

- **DevOps 构建上下文必须是仓库根目录**：它通过 `replace github.com/nebula-agent/nebula => ../nebula` 引用 Nebula 源码。`docker-compose.yml` 中 `context: .`，`dockerfile: devops/Dockerfile`。
- Go 服务从工作目录 `./web` 提供静态控制台。
- CodeLens 的 `plyvel` 依赖 LevelDB C 库，Dockerfile 通过 `libleveldb-dev` 解决；本地安装失败时 `run.bat` 自动回退最小安装模式。

## 使用方法

### 启动平台

```bash
cd Noosphere

# 一键启动 (Windows)
start.bat

# 或 Docker
docker compose up -d

# 或手动（4 个终端）
# 终端1: cd nebula && go run ./cmd/nebula-server --port 8730
# 终端2: cd devops && go run ./cmd/devops-server --port 8740
# 终端3: cd codelens && python -m src.main serve
# 终端4: cd web && npm run dev
```

### 配置模型

1. 打开 `http://localhost:3000/settings/model`
2. 添加 LLM 提供商（OpenAI / Claude / DeepSeek / Ollama）
3. 测试连通性
4. 所有工作区立刻可用

### 验证健康状态

```bash
curl http://localhost:3000/api/gateway/health
curl http://localhost:8765/api/v1/health
curl http://localhost:8730/health
curl http://localhost:8740/health
```
