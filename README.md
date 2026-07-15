<div align="center">

<img src="web/public/logo.svg" alt="Noosphere" width="120" />

# Noosphere（智慧圈）

### 一个持续学习、拥有记忆、与你共同成长的 AI 操作系统

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go 1.26+](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Python 3.10+](https://img.shields.io/badge/Python-3.10+-3776AB?logo=python&logoColor=white)](https://www.python.org)
[![Next.js 14](https://img.shields.io/badge/Next.js-14-black?logo=next.js&logoColor=white)](https://nextjs.org)
[![Docker](https://img.shields.io/badge/Docker-Compose-2496ED?logo=docker&logoColor=white)](docker-compose.yml)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)

> *不是单个 AI Agent。是一个会学习、能记忆、陪你成长的 AI 操作系统。*

[快速开始](#-快速开始) ·
[平台架构](#-平台架构) ·
[工作区介绍](#-四个工作区) ·
[AI Gateway](#-ai-gateway) ·
[文档](docs/) ·
[贡献指南](CONTRIBUTING.md)

</div>

---

## Noosphere 是什么？

Noosphere 不是一夜之间设计出来的。它是**演化出来的**。

最初只是一个"让 AI 学会我的编码风格"的实验。后来加了记忆引擎、加了运维工具——每个项目都是独立的，各自运行在自己的端口上，各自调用各自的模型 API。

直到我们发现自己在三个项目里**写了三遍完全相同的代码**：

| 重复的能力 | 导致的后果 |
|-----------|-----------|
| 模型调用逻辑散落在每个服务里 | **AI Gateway** — 统一模型基础设施 |
| 记忆和上下文管理各自为政 | **Nebula** — 集中式记忆引擎 |
| 用户偏好到处 ad-hoc 存储 | **Workspace 模型** — 结构化个性化 |
| 工具执行每个项目自己造一遍 | **共享 SDK** — 可复用的 Agent 工具集 |

与其维护四个独立 AI 工具，不如把共享的基础设施抽象出来，建成一个**平台**。

```
小项目  →  发现能力重复  →  抽象基础设施  →  Noosphere 平台
```

---

## 平台架构

```
                       Noosphere AI Platform
                            (:3000 单一入口)
 ┌──────────────────────────────────────────────────────────────┐
 │                                                              │
 │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌────────────┐  │
 │  │ CodeLens │  │  Nebula  │  │  DevOps  │  │ AI Gateway │  │
 │  │ 开发者   │  │  记忆    │  │  运维    │  │  平台基础  │  │
 │  │ 智能工作区│  │  引擎    │  │  工作区  │  │  (系统设置) │  │
 │  └────┬─────┘  └────┬─────┘  └────┬─────┘  └─────┬──────┘  │
 │       │             │             │              │          │
 │       └─────────────┼─────────────┼──────────────┘          │
 │                     │             │                         │
 │              ┌──────▼─────────────▼───────┐                 │
 │              │     AI Gateway SDK         │                 │
 │              │   (Go / Python / TS)       │                 │
 │              └─────────────┬──────────────┘                 │
 └────────────────────────────┼────────────────────────────────┘
                              │
                 ┌────────────┼────────────┐
                 ▼            ▼            ▼
           ┌─────────┐  ┌──────────┐  ┌──────────┐
           │OpenAI   │  │DeepSeek  │  │Ollama    │
           │Claude   │  │Gemini    │  │自定义 API│
           └─────────┘  └──────────┘  └──────────┘
```

**所有工作区通过 AI Gateway SDK 调用模型。不直连。一处配置，全局生效。**

---

## 四个工作区

### 🔍 CodeLens — 开发者智能工作区

> 让 AI 学会像你一样写代码。

CodeLens 不只是分析代码。它**学习你的开发人格**：命名习惯、错误处理模式、架构偏好。久而久之，它生成的代码带着你的风格——不是泛泛的"最佳实践"。

- 风格学习：检测命名规范、格式化偏好、惯用写法
- 项目分析：基于 AST 构建调用链和依赖图谱
- 行为画像：记住你如何处理错误、组织模块、管理导入
- 个性化生成：生成符合你个人模式的代码

**目标：AI 生成的代码一次过审。**

---

### ☁️ Nebula Agent — 记忆引擎

> 长期记忆是平台的核心能力，不是附加功能。

Nebula 是 Noosphere 的**记忆主干**。每个工作区、每次对话、每条学到的偏好——全部存储、索引、可检索。会话跨重启持久化。上下文自动管理。

| 记忆类型 | 用途 | 生命周期 |
|---------|------|---------|
| 工作记忆 | 当前任务上下文 | 会话级（默认 5 分钟） |
| 情景记忆 | 过去的交互与决策 | 永久 |
| 语义记忆 | 学到的事实与模式 | 永久 |
| 程序记忆 | 掌握的工作流与技能 | 永久 |

**目标：AI 记得昨天做了什么、能接续中断的任务、越用越聪明。**

---

### ⚙️ DevOps — 运维智能工作区

> 降低运维的心智负担。

日志分析、服务监控、故障诊断——全部 AI 驱动，且能访问你系统的历史记录。DevOps 会把当前故障与 Nebula 中存储的历史解决方案做关联匹配。

- 日志分析：模式感知的日志解析与异常检测
- 服务监控：实时采集 CPU / 内存 / 磁盘 / 网络
- 故障诊断：当前故障与历史故障库自动比对
- 自动修复：建议（并可选择执行）已验证的修复方案

**目标："上次这个报错怎么修的？"——瞬间回答。**

---

### 🔑 AI Gateway — 平台基础设施

> 模型能力与业务逻辑解耦。不是产品，是基础设施。

AI Gateway 不作为独立产品存在。它是平台的**系统设置模块**，入口在导航栏的 Settings 图标 → `/settings/model`。配置一次，所有工作区共用。

| 能力 | 说明 |
|------|------|
| 多提供商 | OpenAI · Claude · Gemini · DeepSeek · Ollama · 自定义 API |
| 统一 SDK | `chat()` / `streamChat()` / `vision()` / `embedding()` |
| 连接测试 | 一键测试延迟 + Token 统计 |
| 调用日志 | 完整审计：每次请求、响应、错误 |
| Token 分析 | 按模型、按工作区的用量统计 |

---

## 平台中的一天

```text
08:00 — 打开 Noosphere。Nebula 自动恢复了昨天的上下文，
        上次改到一半的任务就在眼前。

09:15 — 让 CodeLens 重构 auth 模块。它知道你喜欢显式 error
        返回而不是异常。知道团队的命名规范。生成的代码很对味。

11:30 — 告警响了。DevOps 识别出故障模式，在 Nebula 的故障记忆
        中匹配到三周前的一次类似事件。诊断耗时：30 秒。

14:00 — 在 AI Gateway 设置中新增了一个 Claude API Key。
        三个工作区立刻都能用了。零代码改动。

16:00 — Nebula 已经学了一整天。你的偏好、决策、模式都存下来了。
        明天打开平台，它从昨天结束的地方开始——不是从零开始。
```

---

## 快速开始

### 环境要求

| 工具 | 版本 | 检查命令 |
|------|------|---------|
| **Go** | 1.26+ | `go version` |
| **Python** | 3.10+ | `python --version` |
| **Node.js** | 20+ | `node --version` |
| **Docker**（可选） | latest | `docker --version` |

### 一键启动

```bash
cd Noosphere

# 配置 API Key
copy .env.example .env
# 编辑 .env → 填入 DEEPSEEK_API_KEY
# 或在平台 UI 中配置：Settings → AI Gateway

# 一键启动全部服务
start.bat
```

会打开 4 个终端窗口。访问 **http://localhost:3000**。

### 手动启动

如果 `start.bat` 不适用，依次打开 4 个终端：

**终端 1 — Nebula（记忆引擎 `:8730`）：**
```bash
cd nebula
go run ./cmd/nebula-server --data ./nebula-data --port 8730
```

**终端 2 — DevOps（运维工作区 `:8740`）：**
```bash
cd devops
go run ./cmd/devops-server --port 8740
```

**终端 3 — CodeLens（开发者工作区 `:8765`）：**
```bash
cd codelens
pip install -r requirements.txt        # 首次运行
copy config\.env.example config\.env   # 首次运行
python -m src.main serve
```

**终端 4 — 平台前端（`:3000`）：**
```bash
cd web
npm install       # 首次运行
npm run dev
```

### Docker 部署

```bash
docker compose up -d
```

### 访问地址

| 地址 | 内容 |
|------|------|
| `http://localhost:3000` | 🏠 **平台首页** — Dashboard + 状态总览 |
| `http://localhost:3000/codelens` | 🔍 CodeLens 开发者工作区 |
| `http://localhost:3000/nebula` | ☁️ Nebula 记忆引擎 |
| `http://localhost:3000/devops` | ⚙️ DevOps 运维工作区 |
| `http://localhost:3000/settings/model` | 🔑 AI Gateway 模型配置 |

> **一个端口。一个平台。** 后端服务（`:8765` `:8730` `:8740`）只对内，用户无需关心。

---

## 技术架构

```text
┌──────────────────────────────────────────────────────────┐
│                    表现层                                │
│  Next.js 14 · React 18 · TypeScript · Three.js · GSAP   │
│  Tailwind CSS · Framer Motion · Lucide Icons            │
│  统一端口 (:3000) · App Router                          │
├──────────────────────────────────────────────────────────┤
│                    工作区层                              │
│  ┌────────────┐  ┌────────────┐  ┌────────────────┐    │
│  │  CodeLens  │  │   Nebula   │  │    DevOps      │    │
│  │  Python    │  │   Go       │  │    Go          │    │
│  │  :8765     │  │   :8730    │  │    :8740       │    │
│  └─────┬──────┘  └─────┬──────┘  └──────┬─────────┘    │
├────────┼───────────────┼─────────────────┼──────────────┤
│        └───────────────┼─────────────────┘              │
│                 ┌──────▼──────┐                          │
│                 │ AI Gateway  │   平台基础设施           │
│                 │ /settings/  │   系统设置模块           │
│                 │   model     │   非独立服务             │
│                 └─────────────┘                          │
├──────────────────────────────────────────────────────────┤
│                    基础设施层                            │
│  ┌──────────┐  ┌───────────┐  ┌──────────────────┐     │
│  │ LSM-Tree │  │ HNSW      │  │ NetworkX         │     │
│  │ WAL+SST  │  │ 向量索引  │  │ 知识图谱         │     │
│  │ Go/Python│  │ Go/Python │  │ Python           │     │
│  └──────────┘  └───────────┘  └──────────────────┘     │
│  ┌──────────┐  ┌───────────┐  ┌──────────────────┐     │
│  │ ChromaDB │  │ BM25      │  │ RRF 混合融合     │     │
│  │ 向量数据库│  │ 关键词检索│  │ 多路召回排序     │     │
│  └──────────┘  └───────────┘  └──────────────────┘     │
└──────────────────────────────────────────────────────────┘
```

### 技术栈

| 层级 | 技术 |
|------|------|
| **前端** | Next.js 14 · React 18 · TypeScript · Tailwind CSS · Three.js · GSAP · Framer Motion |
| **后端 (Go)** | Go 1.26 · net/http (标准库) · 自研 LSM-Tree · 自研 HNSW · BM25 · RRF |
| **后端 (Python)** | Python 3.10+ · FastAPI · Tree-sitter · NetworkX · ChromaDB |
| **存储** | 自研 LSM-Tree · 分片内存 KV · ChromaDB · LevelDB |
| **向量检索** | HNSW（Go，零 CGo）· ChromaDB（Python）· Sentence Transformers |
| **LLM** | OpenAI 兼容协议 · 多提供商路由 · 流式 SSE |
| **部署** | Docker Compose · 多阶段构建 · Alpine · CGO_ENABLED=0 |

---

## 项目结构

```
noosphere/
├── web/                        # 🌐 平台前端 (Next.js 14)
│   ├── src/
│   │   ├── app/
│   │   │   ├── page.tsx                     # / — 平台首页
│   │   │   ├── codelens/page.tsx            # /codelens
│   │   │   ├── nebula/page.tsx              # /nebula
│   │   │   ├── devops/page.tsx              # /devops
│   │   │   ├── settings/model/page.tsx      # AI Gateway 设置
│   │   │   └── api/gateway/route.ts         # LLM 代理路由
│   │   ├── components/
│   │   │   ├── dashboard/  · workspace/  · gateway/
│   │   │   ├── layout/     · ui/        · hero/
│   │   │   └── sections/   · three/
│   │   ├── hooks/
│   │   └── lib/            # services · data · gateway · motion
│   └── public/logo.svg     # 项目 Logo
│
├── codelens/                   # 🔍 开发者智能 (Python)
│   ├── src/
│   │   ├── parser/             # Tree-sitter AST (7 种语言)
│   │   ├── indexer/            # 知识图谱 (NetworkX)
│   │   ├── storage/            # LSM-Tree + 向量存储
│   │   ├── rag/                # 混合 RAG + 问答引擎
│   │   ├── agent/              # CodeLensAgent + GitDiff + DocGen
│   │   ├── api/                # FastAPI (:8765) + 仪表盘
│   │   └── workspace/          # 项目扫描 + LLM 验证
│   └── Dockerfile
│
├── nebula/                     # ☁️ 记忆引擎 (Go)
│   ├── engine/
│   │   ├── storage/lsm/        # 自研 LSM-Tree
│   │   ├── storage/kv/         # 分片内存 KV
│   │   ├── storage/vector/     # HNSW (纯 Go)
│   │   ├── retrieval/          # Embedder + 混合检索 + RRF
│   │   ├── index/              # 倒排索引 + BM25
│   │   ├── cache/              # LRU 缓存
│   │   └── manager/            # 4 种记忆类型 + 记忆整合
│   ├── analyzer/               # 代码摄取 + 风格学习
│   ├── api/rest/               # Go HTTP Server (:8730)
│   └── Dockerfile
│
├── devops/                     # ⚙️ 运维智能 (Go)
│   ├── agent/                  # 任务编排 + 工具注册
│   ├── tools/                  # 系统 · 日志 · 服务工具
│   ├── memory/                 # 故障存储 (嵌入 Nebula)
│   ├── metrics/                # 系统指标采集
│   ├── analyzer/               # 日志模式分析
│   ├── api/                    # REST API (:8740)
│   └── Dockerfile
│
├── sdk/aiGateway/              # 🔑 统一 SDK
│   ├── go/client.go            # Go SDK (Nebula / DevOps)
│   └── python/client.py        # Python SDK (CodeLens)
│
├── workspace/                  # 🗂 项目工作区
│   ├── projects/               # 拖入项目 → 自动分析
│   └── llm_config.example.yaml # 多提供商 LLM 配置
│
├── docs/                       # 📚 文档
│   ├── ARCHITECTURE.md         # 架构设计
│   └── API.md                  # API 参考
│
├── start.bat                   # 🚀 一键启动
├── docker-compose.yml          # Docker 编排
├── .env.example                # 环境变量模板
├── LICENSE                     # MIT
└── CONTRIBUTING.md             # 贡献指南
```

---

## 设计哲学

### 平台优先

Noosphere 不是一个 AI 工具集合。它是一个**平台**：共享基础设施、统一模型访问、集中记忆存储、一致的用户体验。每个工作区是平台的扩展，不是独立产品。

### 记忆驱动

记忆是**平台的核心能力**，不是附加功能。Nebula 提供四种记忆类型，全部可索引、可检索、可自动整合。有记忆的 AI 比没有的更有用。

### 个性化 AI

CodeLens 不生成通用代码。它学**你**的规范。Nebula 不存储通用上下文。它存储**你**的历史。平台适配你的工作方式。

### 统一基础设施

一处配置模型。一个 SDK。一个记忆引擎。新增工作区自动继承全部平台能力——模型、记忆、监控——无需重复建设。

### 演化式设计

Noosphere 不是一次性设计出来的。它是演化出来的：

```text
代码风格学习 → 记忆引擎 → 运维工具
                    ↓
              共享能力浮现
                    ↓
              AI Gateway
                    ↓
              Noosphere 平台
```

---

## Roadmap

| 阶段 | 状态 | 内容 |
|------|------|------|
| **基础** | ✅ 完成 | Docker Compose · 统一配置 · Workspace 模型 · AI Gateway |
| **整合** | ✅ 完成 | 多提供商 LLM · SDK (Go/Python/TS) · 项目扫描 · 验证诊断 |
| **智能** | 🚧 进行中 | 跨工作区记忆共享 · Agent 工作流引擎 · MCP Server |
| **生态** | 📋 计划中 | 插件系统 · 风格模板市场 · CI · VSCode 扩展 |

---

## 常见问题

<details>
<summary><b>支持哪些大模型？</b></summary>

OpenAI · Anthropic Claude · Google Gemini · DeepSeek · Ollama（本地）· 任何兼容 OpenAI API 的服务。全部在 <code>/settings/model</code> 一处配置。
</details>

<details>
<summary><b>代码会被上传到云端吗？</b></summary>

不会。所有索引、图谱、记忆存储均在本地（LSM-Tree、ChromaDB、Docker 卷）。只有与你查询相关的代码片段会发送给 LLM 提供商——绝不会是整个代码库。
</details>

<details>
<summary><b>三个工作区必须全部运行吗？</b></summary>

不必。每个工作区独立可用。只跑 CodeLens 做代码分析，或只跑 DevOps 做监控，都能正常工作。一起跑时共享 Nebula 的记忆实现跨域上下文。
</details>

<details>
<summary><b>如何管理多个 LLM API Key？</b></summary>

在 <code>workspace/llm_config.yaml</code> 中配置，或在平台 UI <code>/settings/model</code> 中管理。AI Gateway SDK 自动路由到对应提供商。Key 不会离开你的机器。
</details>

---

<div align="center">

**Noosphere** — *一个持续学习、拥有记忆、与你共同成长的 AI 操作系统。*

[MIT](LICENSE) · Made with ❤️ by Noosphere Contributors · 2026

</div>
