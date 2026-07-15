# ◈ Nebula Agent — 星云记忆引擎

> **让每一次对话，都拥有记忆。**
>
> 扫描代码库 → 学习编码风格 → 自动注入到 AI 对话中。
> 零外部依赖，一个 Go 二进制文件搞定。Apple 设计风格前端仪表盘。

---

## 设计语言

**Apple Design · 暗色宇宙主题**

Nebula Agent 前端仪表盘采用 Apple 设计语言打造——玻璃拟态面板、SF 系统字体、弹性弹簧动画、暗色星空背景。六种元素联动让数据流动可视化：悬停指标卡片时架构图对应节点发光、记忆写入时拓扑图产生涟漪扩散、滚动时顶部渐变进度条同步推进。

| 设计要素 | 实现 |
|----------|------|
| 玻璃拟态 | `backdrop-filter: blur(24px)` + 半透明背景 |
| 字体系统 | SF Pro Display / SF Pro Text / SF Mono |
| 动画曲线 | `cubic-bezier(0.34, 1.56, 0.64, 1)` 弹性弹簧 |
| 色彩方案 | 紫蓝渐变 (#8B78FF → #64D8FF) 深色底 |
| 暗色星空 | 80 粒子 Canvas 动画背景 |
| 元素联动 | 6 种跨组件交互（卡片↔SVG、写入↔拓扑…） |

---

## 它解决什么问题？

### 你每天都在经历的场景：

```
你打开 ChatGPT/DeepSeek/Claude，想让它帮你写代码：

你: "帮我给项目加一个认证中间件"
AI: "你的项目用什么语言？什么框架？有什么编码规范？..."

你: "用 Go，Gin 框架，JWT 认证，统一返回 {code, data, msg} 格式"
AI: "好的...(生成了不符合你风格的代码)"

                        ↓ 每次对话都要重复解释 ↓
                        ↓ 生成的代码风格总是不对 ↓
                        ↓ AI 根本不了解你的项目 ↓
```

### 有了 Nebula 之后：

```
你: "帮我给项目加一个认证中间件"
AI: "好的。根据你的项目(Gin+JWT，PascalCase/camelCase，显式 error 返回，
     统一 {code,data,msg} 格式)，这是代码: ..."

                        ↑ AI 已经"知道"你的项目了 ↑
                        ↑ 代码风格自动匹配 ↑
                        ↑ 0 次重复解释 ↑
```

---

## 怎么做到的？

```
┌─────────────────────────────────────────────────┐
│                  你的代码库                       │
│         (Go / Python / TypeScript / ...)         │
└──────────────────────┬──────────────────────────┘
                       │
         ① nebula ingest --dir .   ← 扫描项目
                       │
                       ▼
┌─────────────────────────────────────────────────┐
│              Nebula 记忆引擎                      │
│                                                  │
│   🧠 学到了什么？                                  │
│   ├─ 语言: Go(85%), TypeScript(12%), ...          │
│   ├─ 框架: Gin, GORM, JWT, Viper                 │
│   ├─ 命名: PascalCase(导出) + camelCase(私有)      │
│   ├─ 错误处理: 显式 error 返回，不用 panic          │
│   ├─ 偏好库: gin, gorm, zap, validator            │
│   ├─ API 格式: {code, data, message}              │
│   └─ 注意事项: JWT secret 在环境变量，不要硬编码      │
│                                                  │
│   存储: LSM Tree(持久化) + KV(热数据) + HNSW(向量)  │
└──────────────────────┬──────────────────────────┘
                       │
         ② 当你向 AI 提问时，自动检索相关记忆
                       │
                       ▼
┌─────────────────────────────────────────────────┐
│          自动拼接的 System Prompt                  │
│                                                  │
│  ## 项目上下文（由 Nebula 记忆引擎注入）            │
│  ### 技术栈: Go, Gin, GORM, JWT                   │
│  ### 编码风格: PascalCase/camelCase，显式error返回  │
│  ### ⚠ 注意事项: JWT secret 在环境变量中            │
│  ### 相关模式: API 统一返回 {code, data, message}   │
│  ...                                             │
└──────────────────────┬──────────────────────────┘
                       │
         ③ 拼接后发给 AI → AI 返回符合你风格的代码
                       │
                       ▼
                   你的 AI 工具
            (ChatGPT / DeepSeek / Claude / ...)
```

---

## 三种使用方式

### 方式 1：浏览器直接对话（最简单）

```bash
# 双击 start.bat 或执行：
.\nebula-server.exe --memory --port 8730
# 打开 http://localhost:8730 → 全新 Apple 风格仪表盘
```

内置 DeepSeek V4 Pro，**自动注入项目记忆**，打开即用。右侧 AI 编程助手采用 iMessage 风格对话框。

### 方式 2：生成上下文 → 粘贴到任何 AI 工具

```bash
# 生成上下文，复制到剪贴板
go run ./cmd/nebula-code context "添加用户认证中间件" --lang go | clip

# 打开 ChatGPT/DeepSeek/Claude → 粘贴到对话框 → 直接提问
```

适合已经习惯用网页版 AI 工具的用户。

### 方式 3：嵌入你的代码中

```go
import "github.com/nebula-agent/nebula/engine"

eng, _ := engine.Open(engine.MemoryOptions())
sess := eng.Session("my-project")

// 存储记忆
sess.RememberEpisodic("用户喜欢 Python", 0.8, []string{"preference"})

// 检索记忆 → 拼接到你的 AI 调用中
results, _ := sess.Reminisce(&manager.SearchOptions{
    Query: "编程语言偏好", TopK: 5,
})
// 将 results 拼接到 System Prompt
```

---

## 核心能力一览

| 🧠 能力 | 做什么 | 技术实现 |
|----------|--------|----------|
| **扫描项目** | 自动识别语言/框架/库/入口文件/关键模块 | 文件扩展名映射 + go.mod/package.json 解析 |
| **学习风格** | 提取命名规范、错误处理、库偏好、设计模式 | 正则模式匹配 + 频率统计 |
| **发现注意事项** | 从 WARNING/XXX/TODO/注意 注释中提取 | AST 注释扫描 |
| **存储记忆** | 四种类型：工作(临时)·情景(事件)·语义(知识)·程序(技能) | LSM Tree + KV + HNSW 向量索引 |
| **语义检索** | 模糊的自然语言查询 | HNSW 向量近似最近邻 |
| **关键词检索** | 精确匹配函数名/变量名/标签 | BM25 倒排索引 + 中英分词 |
| **混合检索** | 向量 + 关键词 + 时间 三者融合排序 | RRF (Reciprocal Rank Fusion) |
| **注入上下文** | 自动拼接项目记忆到 AI System Prompt | ContextInjector → Prompt 后缀 |
| **AI 对话** | iMessage 风格界面 + 记忆自动注入 | DeepSeek V4 Pro API 代理 |
| **导出/导入** | 团队共享记忆，JSON/Markdown 格式 | `nebula export` / `nebula import` |

---

## 架构

```
                        AI 智能体 (Agent)
                              │
        ┌─────────────────────┼─────────────────────┐
        │                     │                     │
   Python SDK              Go SDK             REST API
        │                     │                     │
        └─────────────────────┼─────────────────────┘
                              │
                    ┌─────────────────┐
                    │  Memory Service │  ← 会话管理 · 多租户
                    └────────┬────────┘
                             │
                    ┌────────┴────────┐
                    │    L1 Cache     │  ← 热点数据 LRU
                    └────────┬────────┘
                             │
        ┌────────────────────┼────────────────────┐
        │            Memory Engine                │
        ├──────────┬─────────┬────────────────────┤
        │ Storage  │Retrieval│      Manager       │
        ├──────────┼─────────┼────────────────────┤
        │ LSM Tree │Embedder │ Memory API (CRUD)  │
        │ KV Hash  │HNSW     │ Lifecycle (TTL)    │
        │ Vector   │BM25+RRF │ Consolidation      │
        └──────────┴─────────┴────────────────────┘
```

---

## 记忆类型

| 类型 | TTL | 存储 | 用途 | 类比 |
|------|-----|------|------|------|
| ⚡ **工作记忆** | 5分钟 | KV | 当前任务状态 | CPU 寄存器 |
| 📖 **情景记忆** | 30天 | KV+向量+倒排 | 对话/事件记录 | RAM |
| 💎 **语义记忆** | 永久 | KV+向量 | 知识/事实 | 硬盘 |
| 🔧 **程序记忆** | 永久 | KV 结构化 | 技能/流程 | BIOS |

旧的情景记忆 → 整合引擎自动压缩 → 语义记忆（模拟人脑记忆巩固）

---

## 前端仪表盘 · Apple 设计系统

### 视觉特性

- **Hero 区** — 渐变标题「让每一次对话，都拥有记忆」，紫色→粉色→金色渐变
- **能力卡片** — 4 张玻璃拟态卡片，悬停上浮 + 点击联动架构视图
- **监控面板** — 4 列数据卡片，颜色编码：紫=总量、蓝=情景、绿=语义、青=向量
- **记忆操作** — Pill 分段控件切换写入/检索/浏览，Apple 风格表单
- **AI 编程助手** — iMessage 风格对话框，紫色渐变用户气泡，注入开关
- **HNSW 拓扑图** — 4 层轨道旋转节点，悬停弹出玻璃详情卡，写入触发涟漪
- **架构 SVG** — 3 个可切换视图，SVG 节点与仪表盘卡片联动高亮
- **事件日志流** — 实时滚动，新条目左边框动画进场

### 六种元素联动

| 联动 | 触发方式 | 视觉反馈 |
|------|----------|----------|
| 卡片 → 架构图 | 悬停指标卡片 | SVG 对应节点发光 + 临近卡片变暗 |
| 能力卡 → 架构视图 | 点击记忆类型卡片 | 自动切换到对应架构 SVG 视图 |
| 记忆写入 → 拓扑图 | 提交记忆成功 | Canvas 涟漪从中心扩散 |
| 数据刷新 → 卡片链 | 定时轮询 / 手动 | 数字弹性跳动，4 列错开 120ms |
| 滚动 → 进度条 | 页面滚动 | 顶部 2px 渐变进度条 |
| 环形图 → 卡片 | 悬停 Hero 环形图 | 对应指标卡片高亮上浮 |

### 文件结构

```
web/
├── index.html       SPA 主页面 · Hero + 能力卡 + 监控 + 操作 + 拓扑
├── css/style.css     Apple 设计系统 · 玻璃拟态 · 暗色宇宙主题
└── js/app.js         联动事件总线 · 粒子星空 · 弹性动画
```

---

## 技术栈

| 模块 | 技术 | 为什么 |
|------|------|--------|
| 存储引擎 | 自研 LSM Tree (WAL+SkipList+SSTable+Bloom) | 零依赖，可控格式 |
| KV 缓存 | 32路分片 sync.Map + TTL 最小堆 | O(1)读写，低锁竞争 |
| 向量索引 | 自研纯 Go HNSW | 无CGo，可交叉编译 ARM |
| 关键词检索 | BM25 倒排索引 + 中英分词 | 精确匹配，200行代码 |
| HTTP | Go net/http 标准库 | 零框架依赖 |
| 前端 | 原生 HTML/CSS/JS + Canvas | 零 Node.js 依赖，Apple 设计语言 |
| AI 模型 | DeepSeek V4 Pro (可替换) | OpenAI 兼容协议 |

**设计原则：一个二进制文件，import 即用，零外部依赖。**

---

## 项目结构

```
nebula-agent/
├── engine/                     ← 核心引擎（可嵌入）
│   ├── storage/lsm/              LSM Tree
│   ├── storage/kv/               分片锁 KV
│   ├── storage/vector/           纯 Go HNSW
│   ├── retrieval/                Embedder + 混合检索
│   ├── index/                    倒排索引 + BM25
│   ├── manager/                  四种记忆 + 整合
│   └── cache/                    LRU
│
├── analyzer/                   ← 代码分析
│   ├── ingester.go               扫描项目 → 结构化记忆
│   ├── style.go                  学习个人编码风格
│   └── injector.go               检索记忆 → Prompt 后缀
│
├── api/rest/                   ← REST API
│   ├── server.go                 路由 + CORS
│   └── chat.go                   DeepSeek 对话代理
│
├── cmd/
│   ├── nebula-server/          启动 Web 服务
│   └── nebula-code/            CLI 终端工具
│
├── web/                        ← 前端仪表盘 (Apple Design)
│   ├── index.html                SPA 主页面
│   ├── css/style.css             Apple 玻璃拟态系统
│   └── js/app.js                 联动 + 粒子 + 弹簧动画
│
├── sdk/go/                     Go SDK
├── sdk/python/                 Python SDK
│
├── start.bat                   ← 一键启动脚本
├── NEBULA_AGENT.md             ← 本文档
└── NEBULA_AGENT.html           ← 浏览器可开版本
```

---

## 快速开始

### 启动 Web 服务

```bash
# 方式 1：双击 start.bat

# 方式 2：命令行
cd "Nebula Agent"
.\nebula-server.exe --memory --port 8730
# 浏览器 http://localhost:8730 → Apple 风格仪表盘
```

### CLI 三步走

```bash
# 1. 扫描项目
go run ./cmd/nebula-code ingest --dir .

# 2. 查看学了什么
go run ./cmd/nebula-code style
go run ./cmd/nebula-code gotchas

# 3. 生成 AI 上下文（粘贴到任何 AI 工具）
go run ./cmd/nebula-code context "添加认证中间件" --lang go
```

### 嵌入代码

```go
import "github.com/nebula-agent/nebula/engine"
eng, _ := engine.Open(engine.MemoryOptions())
sess := eng.Session("my-project")
sess.RememberEpisodic("用户喜欢 Python", 0.8, []string{"preference"})
```

---

## 落地场景

| 场景 | 核心记忆 | 效果 |
|------|---------|------|
| 🤖 个人 AI 助手 | 情景 + 语义 | 跨会话记忆，越用越懂你 |
| 💬 智能客服 | 情景 + 语义 | 客户历史自动召回 |
| 💻 代码助手 | 工作 + 程序 + 语义 | 理解项目 + 学习风格 |
| 🎮 游戏 NPC | 情景 + 语义 | 记住玩家行为 |
| 📚 RAG 知识库 | 语义 + 向量 | 混合检索比纯向量准 |
| 🌐 边缘 IoT | 全四种 | ~15MB 二进制 |

---

> **Nebula Agent** — *让 AI 真正懂你的项目。扫描一次，受益每次对话。*
>
> v0.2.0 | Go | 零依赖 | MIT | Apple Design
