# CodeLens AI - 智能代码理解平台

<p align="center">
  <img src="https://img.shields.io/badge/version-1.0.0-blue" alt="Version">
  <img src="https://img.shields.io/badge/python-3.10+-green" alt="Python">
  <img src="https://img.shields.io/badge/license-MIT-orange" alt="License">
</p>

> 基于代码语义分析与 Agent 的智能代码理解平台 —— 面向开发者的 AI Code Intelligence 工具

通过解析代码结构、构建项目知识图谱，并结合 **RAG** 与 **Agent** 技术，实现大型代码库理解、问答、调用链分析和自动文档生成。

---

## 📋 目录

- [核心特性](#-核心特性)
- [系统架构](#-系统架构)
- [快速开始](#-快速开始)
- [使用指南](#-使用指南)
- [API 接口](#-api-接口)
- [模块详解](#-模块详解)
- [配置说明](#-配置说明)
- [开发指南](#-开发指南)

---

## 🚀 核心特性

### 1. 多语言代码解析
- 基于 **Tree-sitter** 实现 7 种语言的 AST 解析
- 支持：Python、JavaScript、TypeScript、Java、C/C++、Go、Rust
- 自动提取函数、类、变量、接口、导入关系等代码实体
- 计算圈复杂度、函数签名、文档字符串等元信息

### 2. 代码知识图谱
- 构建多维代码关系图（调用关系、继承关系、依赖关系、包含关系）
- 支持调用链追踪（Call Chain Analysis）
- 支持影响分析（Impact Analysis）
- 可导出为 JSON 或交互式 HTML 可视化

### 3. LSM-Tree KV 存储
- 高性能键值存储引擎，自动选择 LevelDB / RocksDB / 内置实现
- MemTable + SSTable 分层架构设计
- 支持批量写入、前缀扫描、范围查询
- WAL 模式保证数据持久性

### 4. RAG 代码问答
- 语义向量搜索 + 知识图谱结构搜索 + 关键词搜索的**混合检索**
- 智能查询类型识别（调用链 / 结构分析 / Bug 定位 / 影响分析）
- 支持 OpenAI / Anthropic 等多种 LLM 后端
- 检索结果自动上下文扩展和来源引用

### 5. Git Diff 分析 Agent
- 变更代码的 AST 差异分析
- 基于知识图谱的变更影响追踪
- 四级风险评估（Low / Medium / High / Critical）
- 自动生成变更摘要和审查建议

### 6. 自动文档生成
- 函数/类/模块/项目多级文档自动生成
- Mermaid 调用关系图
- API 参考文档
- 目录树可视化

### 7. Web API & 交互界面
- FastAPI RESTful API（20+ 端点）
- WebSocket 实时问答
- **工作区管理**：扫描 → 识别 → 分析 → 验证
- **LLM 验证**：多提供商连通性测试 + 工具链 I/O 诊断
- 内置 Web Chat 界面
- 自动生成 Swagger / ReDoc API 文档

---

## 🏗️ 系统架构

```
┌─────────────────────────────────────────────────────────────┐
│                       CodeLens AI                           │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌──────────────┐  ┌──────────────────┐   │
│  │  Web UI     │  │  REST API    │  │  WebSocket       │   │
│  │  (HTML/JS)  │  │  (FastAPI)   │  │  (Real-time QA)  │   │
│  └──────┬──────┘  └──────┬───────┘  └────────┬─────────┘   │
│         └────────────────┼───────────────────┘              │
│                          ▼                                   │
│  ┌──────────────────────────────────────────────────────┐   │
│  │                Agent Layer                           │   │
│  │  ┌──────────────┐ ┌────────────┐ ┌───────────────┐  │   │
│  │  │ CodeLensAgent│ │GitDiffAgent│ │ DocGenerator  │  │   │
│  │  └──────┬───────┘ └─────┬──────┘ └───────┬───────┘  │   │
│  └─────────┼───────────────┼────────────────┼──────────┘   │
│            ▼               ▼                ▼               │
│  ┌──────────────────────────────────────────────────────┐   │
│  │                RAG Engine                            │   │
│  │  ┌──────────────┐ ┌────────────┐ ┌───────────────┐  │   │
│  │  │ CodeEmbedder │ │ Retriever  │ │  QA Engine    │  │   │
│  │  └──────┬───────┘ └─────┬──────┘ └───────┬───────┘  │   │
│  └─────────┼───────────────┼────────────────┼──────────┘   │
│            ▼               ▼                ▼               │
│  ┌──────────────────────────────────────────────────────┐   │
│  │              Storage Layer                           │   │
│  │  ┌──────────────┐ ┌────────────────────────────┐    │   │
│  │  │  LSM Store   │ │      Vector Store          │    │   │
│  │  │ (KV Storage) │ │   (ChromaDB/SentenceTF)    │    │   │
│  │  └──────────────┘ └────────────────────────────┘    │   │
│  └──────────────────────────────────────────────────────┘   │
│            ▼                                                 │
│  ┌──────────────────────────────────────────────────────┐   │
│  │              Index Layer                             │   │
│  │  ┌──────────────┐ ┌────────────────────────────┐    │   │
│  │  │ Knowledge    │ │     Index Builder           │    │   │
│  │  │ Graph        │ │  (Parallel Processing)     │    │   │
│  │  │ (NetworkX)   │ │                             │    │   │
│  │  └──────────────┘ └────────────────────────────┘    │   │
│  └──────────────────────────────────────────────────────┘   │
│            ▼                                                 │
│  ┌──────────────────────────────────────────────────────┐   │
│  │              Parser Layer                            │   │
│  │  ┌──────────────────┐ ┌────────────────────────────┐│   │
│  │  │ TreeSitterParser │ │     AST Extractor          ││   │
│  │  │ (7 Languages)    │ │  (Entity/Relation Extract) ││   │
│  │  └──────────────────┘ └────────────────────────────┘│   │
│  └──────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

### 数据流

```
Source Code → Tree-sitter AST → Entity/Relation Extraction
                                    ↓
                ┌───────────────────────────────────┐
                │       Knowledge Graph (NX)         │
                │  Nodes: Functions, Classes, ...    │
                │  Edges: Calls, Inherits, Imports   │
                └───────────────┬───────────────────┘
                                ↓
         ┌──────────────────────────────────────────┐
         │           Dual Storage                    │
         │  ┌─────────────┐  ┌──────────────────┐   │
         │  │  LSM Store  │  │  Vector Store    │   │
         │  │ (Structured)│  │  (Embeddings)    │   │
         │  └─────────────┘  └────────┬─────────┘   │
         └────────────────────────────┼────────────┘
                                      ↓
         ┌──────────────────────────────────────────┐
         │           RAG Retrieval                   │
         │  Semantic + Structural + Keyword Hybrid   │
         └────────────────┬─────────────────────────┘
                          ↓
         ┌──────────────────────────────────────────┐
         │        LLM-Augmented Generation           │
         │     Code Q&A | Impact Analysis | Docs     │
         └──────────────────────────────────────────┘
```

---

## 🔧 快速开始

### 环境要求

- **Python** 3.10+
- **Git** (用于 Git Diff 分析功能)
- **Windows** / **Linux** / **macOS**

### 安装步骤

#### 1. 克隆或进入项目目录

```bash
cd "CodeLens AI"
```

#### 2. 创建虚拟环境（推荐）

```bash
python -m venv venv

# Windows
venv\Scripts\activate

# Linux/macOS
source venv/bin/activate
```

#### 3. 安装依赖

```bash
pip install -r requirements.txt
```

#### 4. 配置 LLM API 密钥（可选，用于 AI 问答）

```bash
# Windows PowerShell
$env:OPENAI_API_KEY = "sk-your-api-key"

# Linux/macOS
export OPENAI_API_KEY="sk-your-api-key"
```

#### 5. 验证安装

```bash
python -m src.main info
```

### 一键启动

```bash
# Windows
run.bat

# 或直接
python -m src.main serve
```

启动后访问：
- **Web UI**: http://localhost:8765
- **API 文档**: http://localhost:8765/docs
- **ReDoc**: http://localhost:8765/redoc

---

## 📖 使用指南

### 命令行使用

CodeLens AI 提供了丰富的命令行接口：

#### 索引项目

```bash
# 索引当前目录
python -m src.main index

# 索引指定项目
python -m src.main index --project /path/to/project

# 指定语言和排除目录
python -m src.main index -p ./myproject -l python -l javascript -e node_modules -e .git
```

#### 代码问答

```bash
# 交互式问答
python -m src.main ask -p ./myproject

# 指定查询类型
python -m src.main ask -p ./myproject -t call_chain -q "How does authentication work?"
```

#### 调用链追踪

```bash
# 追踪函数调用链
python -m src.main trace -p ./myproject -e authenticate -d 10
```

#### 影响分析

```bash
# 分析修改某个实体的影响
python -m src.main impact -p ./myproject -e User
```

#### 生成文档

```bash
# 生成项目文档
python -m src.main docs -p ./myproject -o ./docs/project.md
```

#### Git Diff 分析

```bash
# 分析未暂存的变更
python -m src.main diff -p ./myproject

# 分析暂存的变更
python -m src.main diff -p ./myproject --staged

# 分析两个分支的差异
python -m src.main diff -p ./myproject --base main --target feature/new-api
```

#### 启动 Web 服务

```bash
# 启动 API 服务器
python -m src.main serve -p ./myproject

# 指定端口和热重载
python -m src.main serve -p ./myproject --port 9000 --reload
```

### Python API 使用

```python
from src.agent.code_agent import CodeLensAgent, AgentConfig, AgentAction

# 1. 初始化 Agent
config = AgentConfig(
    project_root="/path/to/project",
    parallel=True,
    max_workers=4,
)
agent = CodeLensAgent(config)

# 2. 索引项目
result = agent.execute(AgentAction.INDEX_PROJECT)
print(f"索引完成: {result.execution_time_ms:.0f}ms")

# 3. 代码问答
response = agent.ask("What is the overall architecture of this project?")
print(response.answer)
for source in response.sources:
    print(f"  Source: {source['file']}:{source['line']}")

# 4. 追踪调用链
result = agent.execute(AgentAction.TRACE_CALLS, entity_name="main")
print(f"Callers: {result.data['callers']}")
print(f"Callees: {result.data['callees']}")

# 5. 影响分析
result = agent.execute(AgentAction.ANALYZE_IMPACT, entity_name="UserService")
print(f"Total affected: {result.data['total']}")

# 6. 高级查询（直接使用知识图谱）
kg = agent.get_knowledge_graph()
call_chain = kg.get_call_chain("authenticate", max_depth=5)
impact = kg.get_impact_analysis("Database")
```

---

## 🔌 API 接口

### REST API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/v1/health` | 健康检查 |
| `GET` | `/api/v1/status` | 系统状态 |
| `POST` | `/api/v1/index` | 索引项目 |
| `POST` | `/api/v1/qa` | 代码问答 |
| `POST` | `/api/v1/search` | 搜索代码库 |
| `POST` | `/api/v1/analyze/file` | 分析文件 |
| `POST` | `/api/v1/analyze/explain` | 解释代码 |
| `GET` | `/api/v1/call-chain/{entity}` | 调用链追踪 |
| `GET` | `/api/v1/impact/{entity}` | 影响分析 |
| `POST` | `/api/v1/git/diff` | Git Diff 分析 |
| `POST` | `/api/v1/docs/generate` | 生成文档 |
| `GET` | `/api/v1/knowledge-graph/stats` | 知识图谱统计 |
| `GET` | `/api/v1/knowledge-graph/node/{name}` | 获取图谱节点 |
| `GET` | `/api/v1/knowledge-graph/export` | 导出知识图谱 |
| `GET` | `/api/v1/issues` | 代码问题检测 |
| `POST` | `/api/v1/workspace/scan` | 🆕 扫描工作区项目 |
| `GET` | `/api/v1/workspace/projects` | 🆕 列出已发现项目 |
| `GET` | `/api/v1/workspace/projects/{name}` | 🆕 项目详情 |
| `POST` | `/api/v1/workspace/projects/{name}/analyze` | 🆕 分析指定项目 |
| `POST` | `/api/v1/workspace/validate/llm` | 🆕 LLM 连通性验证 |
| `POST` | `/api/v1/workspace/validate/tools` | 🆕 工具链 I/O 验证 |
| `POST` | `/api/v1/workspace/validate/all` | 🆕 一键全量诊断 |
| `GET` | `/api/v1/workspace/config/llm` | 🆕 LLM 配置状态 |
| `WS` | `/api/v1/ws/chat` | WebSocket 实时问答 |

### API 调用示例

```bash
# 索引项目
curl -X POST http://localhost:8765/api/v1/index \
  -H "Content-Type: application/json" \
  -d '{"project_root": "."}'

# 代码问答
curl -X POST http://localhost:8765/api/v1/qa \
  -H "Content-Type: application/json" \
  -d '{"question": "Explain the authentication flow", "query_type": "call_chain"}'

# 调用链追踪
curl http://localhost:8765/api/v1/call-chain/authenticate?max_depth=5

# 影响分析
curl http://localhost:8765/api/v1/impact/User

# Git Diff 分析
curl -X POST http://localhost:8765/api/v1/git/diff \
  -H "Content-Type: application/json" \
  -d '{"base_ref": "main", "target_ref": "feature/new-api"}'

# WebSocket 问答
# 使用 wscat 或浏览器 WebSocket 客户端连接
# ws://localhost:8765/api/v1/ws/chat
```

---

## 📁 模块详解

### 项目目录结构

```
CodeLens AI/
├── config/
│   └── settings.yaml              # 全局配置文件
├── src/
│   ├── __init__.py
│   ├── main.py                    # CLI 入口点
│   ├── parser/                    # 代码解析模块
│   │   ├── __init__.py
│   │   ├── tree_sitter_parser.py  # Tree-sitter 多语言解析器
│   │   └── ast_extractor.py       # AST 实体/关系提取
│   ├── indexer/                   # 索引模块
│   │   ├── __init__.py
│   │   ├── knowledge_graph.py     # 代码知识图谱 (NetworkX)
│   │   └── index_builder.py       # 项目索引构建器
│   ├── storage/                   # 存储模块
│   │   ├── __init__.py
│   │   ├── lsm_store.py          # LSM-Tree KV 存储
│   │   └── vector_store.py       # 向量存储 (ChromaDB)
│   ├── rag/                       # RAG 检索增强模块
│   │   ├── __init__.py
│   │   ├── embeddings.py         # 代码嵌入
│   │   ├── retriever.py          # 混合检索器
│   │   └── qa_engine.py          # 代码问答引擎
│   ├── agent/                     # Agent 模块
│   │   ├── __init__.py
│   │   ├── code_agent.py         # 核心智能 Agent
│   │   ├── git_diff_analyzer.py  # Git Diff 分析 Agent
│   │   └── doc_generator.py      # 自动文档生成器
│   ├── workspace/                 # 🆕 工作区模块
│   │   ├── __init__.py
│   │   ├── manager.py            # 工作区管理 + 项目扫描 + LLM 配置
│   │   └── validator.py          # LLM 连通性验证 + 工具链 I/O 诊断
│   └── api/                       # Web API 模块
│       ├── __init__.py
│       ├── server.py             # FastAPI 服务器 + Web UI
│       └── routes.py             # API 路由 (20+ 端点)
├── tests/
│   ├── __init__.py
│   └── test_parser.py           # 解析器单元测试
├── docs/                         # 文档输出目录
├── data/                         # 数据存储目录
│   ├── lsm_store/               # LSM 存储数据
│   └── vector_store/            # 向量存储数据
├── requirements.txt              # Python 依赖
├── run.bat                       # Windows 一键启动脚本
└── README.md                     # 项目文档
```

### 核心模块说明

#### 1. Parser 模块 (`src/parser/`)
- **TreeSitterParser**: 封装 Tree-sitter，提供 7 种语言的统一解析接口
- **ASTExtractor**: 从 AST 中提取结构化实体（13 种实体类型）和关系（8 种关系类型）

#### 2. Indexer 模块 (`src/indexer/`)
- **KnowledgeGraph**: 基于 NetworkX 的有向图，支持调用链、继承链、影响分析
- **IndexBuilder**: 并行项目扫描和索引构建，支持增量更新

#### 3. Storage 模块 (`src/storage/`)
- **LSMStore**: LSM-Tree KV 存储，自动选择 LevelDB → RocksDB → 内置实现
- **VectorStore**: 向量存储，自动选择 ChromaDB → 内置回退方案

#### 4. RAG 模块 (`src/rag/`)
- **CodeEmbedder**: 代码实体向量化，支持多种嵌入策略
- **CodeRetriever**: 语义 + 结构 + 关键词混合检索，支持上下文扩展
- **QACodeEngine**: 查询意图识别 → 多策略检索 → 增强提示构建 → LLM 生成

#### 5. Agent 模块 (`src/agent/`)
- **CodeLensAgent**: 核心 Agent，协调所有子系统完成分析任务
- **GitDiffAnalyzer**: AST 级 Diff 分析 + 知识图谱影响追踪 + 风险评估
- **DocGenerator**: 多级文档自动生成 + Mermaid 调用图

#### 6. API 模块 (`src/api/`)
- **APIServer**: FastAPI 服务器，内置 Web Chat UI
- **Routes**: 14 个 API 端点 + WebSocket 实时问答

---

## ⚙️ 配置说明

### 主配置文件: `config/settings.yaml`

```yaml
# 项目配置
project:
  name: "CodeLens AI"
  workspace_root: "./workspace"

# 解析配置
parser:
  languages: [python, javascript, typescript, java, cpp, go, rust]
  max_file_size_mb: 10
  exclude_dirs: [node_modules, __pycache__, .git]

# LSM 存储配置
storage:
  engine: "leveldb"        # leveldb | rocksdb | sqlite_lsm | auto
  db_path: "./data/lsm_store"
  cache_size_mb: 256
  write_buffer_mb: 64

# RAG 配置
rag:
  embedding_model: "all-MiniLM-L6-v2"
  top_k: 10
  chunk_size: 1000
  chunk_overlap: 200

# LLM 配置
llm:
  provider: "openai"       # openai | anthropic | local
  model: "gpt-4o"
  temperature: 0.1

# API 服务配置
api:
  host: "0.0.0.0"
  port: 8765
```

### 环境变量

| 变量 | 说明 |
|------|------|
| `DEEPSEEK_API_KEY` | DeepSeek API 密钥（兼容方式） |
| `DEEPSEEK_MODEL` | DeepSeek 模型名（默认 deepseek-v4-pro） |
| `DEEPSEEK_BASE_URL` | DeepSeek API 地址 |
| `OPENAI_API_KEY` | OpenAI API 密钥（兼容方式） |
| `ANTHROPIC_API_KEY` | Anthropic API 密钥（兼容方式） |
| `WORKSPACE_DIR` | 工作区目录路径（默认 ../workspace） |

> 🆕 推荐使用 `workspace/llm_config.yaml` 管理多提供商 LLM 配置，详见 [workspace/llm_config.example.yaml](../workspace/llm_config.example.yaml)。

---

## 🔬 技术亮点

### 1. LSM-Tree 存储引擎设计

```
写入路径:
  Put(K,V) → MemTable (OrderedDict) → flush → Level 0 SSTable
                                              → Compaction → Level N

读取路径:
  Get(K) → MemTable (hit?) → Level 0 → Level 1 → ... → Level N
```

- MemTable 基于 Python OrderedDict 实现 O(1) 插入和有序遍历
- SSTable 使用 SQLite WAL 模式实现持久化和高效查询
- 分层合并策略保证读放大可控
- 支持自动回退：LevelDB → RocksDB → 内置实现

### 2. 混合检索策略

```
Query → ┬─ 语义搜索 (Vector Similarity) ────┐
        ├─ 结构搜索 (Knowledge Graph) ──────┤ → 加权融合 → 重排序
        └─ 关键词搜索 (Full Text) ──────────┘
        权重: 0.5 / 0.3 / 0.2
```

### 3. 智能查询意图识别

系统通过正则模式匹配自动识别 6 种查询类型，选择合适的检索策略和上下文构建方式。

### 4. 变更影响传导分析

Git Diff → 实体变更识别 → AST 差异比较 → 知识图谱反向依赖追踪 → 多级风险评级

---

## 🧪 测试

```bash
# 运行所有测试
python -m pytest tests/ -v

# 运行特定测试
python -m pytest tests/test_parser.py -v

# 带覆盖率
python -m pytest tests/ --cov=src --cov-report=html
```

---

## 📝 开发计划

- [x] 工作区项目扫描与自动识别
- [x] 多 LLM 提供商支持（DeepSeek / OpenAI / Anthropic / Ollama）
- [x] LLM 连通性验证 + 工具链 I/O 诊断
- [ ] VSCode 扩展集成
- [ ] 多仓库联合分析
- [ ] 代码变更预测与建议
- [ ] 更多语言支持（Ruby, Kotlin, Swift 等）
- [ ] 分布式索引支持（超大型代码库）
- [ ] CI/CD 集成插件
- [ ] 代码审查自动评论

---

## 🤝 贡献

欢迎贡献代码、提出问题或建议！

---

## 📄 许可证

MIT License

---

<p align="center">
  <b>CodeLens AI</b> — Making Large Codebases Understandable
</p>
