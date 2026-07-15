<div align="center">

<img src="docs/icon.svg" alt="Noosphere" width="80" />

# Noosphere

### An AI Operating Platform
#### for Personalized Development, Memory &amp; Operations Intelligence

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go 1.26+](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Python 3.10+](https://img.shields.io/badge/Python-3.10+-3776AB?logo=python&logoColor=white)](https://www.python.org)
[![Next.js 14](https://img.shields.io/badge/Next.js-14-black?logo=next.js&logoColor=white)](https://nextjs.org)
[![Docker](https://img.shields.io/badge/Docker-Compose-2496ED?logo=docker&logoColor=white)](docker-compose.yml)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)

> *Not a single AI Agent. An AI Operating Platform that learns, remembers, and grows with you.*

[Getting Started](#-getting-started) ·
[Architecture](#-platform-architecture) ·
[Workspaces](#-workspaces) ·
[AI Gateway](#-ai-gateway) ·
[Documentation](docs/) ·
[Contributing](CONTRIBUTING.md)

</div>

---

## What is Noosphere?

Noosphere is a **unified AI Operating Platform** born from the evolution of multiple independent AI projects. As each project grew, we discovered the same capabilities being built over and over:

| Repeated Capability | Result |
|---------------------|--------|
| Model calling logic in every service | **AI Gateway** — unified model infrastructure |
| Memory &amp; context management scattered everywhere | **Nebula** — centralized memory engine |
| User preferences stored ad-hoc | **Workspace Model** — structured personalization |
| Tool execution built per-project | **Shared SDK** — reusable agent toolkit |

Rather than maintaining four separate AI tools, we abstracted the shared infrastructure and built a **platform**.

```
Small Projects  →  Shared Capabilities  →  Platform Abstraction  →  Noosphere
```

---

## Platform Architecture

```
                          Noosphere AI Platform
                               (:3000)
    ┌─────────────────────────────────────────────────────────────┐
    │                                                             │
    │   ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌───────────┐ │
    │   │ CodeLens │  │  Nebula  │  │OpsToolkit│  │AI Gateway │ │
    │   │Developer │  │ Memory   │  │  AIOps   │  │Platform   │ │
    │   │Intellig. │  │ Engine   │  │Workspace │  │Infrastr.  │ │
    │   │Workspace │  │          │  │          │  │  (Settings)│ │
    │   └────┬─────┘  └────┬─────┘  └────┬─────┘  └─────┬─────┘ │
    │        │             │             │              │        │
    │        └─────────────┼─────────────┼──────────────┘        │
    │                      │             │                       │
    │               ┌──────▼─────────────▼───────┐               │
    │               │    AI Gateway SDK          │               │
    │               │    (Go / Python / TS)      │               │
    │               └─────────────┬──────────────┘               │
    │                             │                              │
    └─────────────────────────────┼──────────────────────────────┘
                                  │
                    ┌─────────────┼─────────────┐
                    ▼             ▼             ▼
              ┌─────────┐  ┌──────────┐  ┌──────────┐
              │OpenAI   │  │DeepSeek  │  │Ollama    │
              │Claude   │  │Gemini    │  │Custom API│
              └─────────┘  └──────────┘  └──────────┘
```

**Every Workspace calls models through the AI Gateway SDK. No direct API access. One configuration, everywhere.**

---

## Workspaces

Noosphere is organized into **Workspaces** — each a dedicated AI-powered environment for a specific domain.

### 🔍 CodeLens — Developer Intelligence Workspace

> *Let AI learn to code like you do.*

CodeLens doesn't just analyze code. It **learns your development persona**: naming conventions, error handling patterns, architectural preferences, and coding habits. Over time, it generates code that matches your style — not generic "best practices."

| Capability | Description |
|------------|-------------|
| Style Learning | Detects naming conventions, formatting, and idiom preferences |
| Project Structure Analysis | Builds AST-based knowledge graphs with call chains and dependency maps |
| Behavioral Profile | Remembers how you handle errors, structure modules, and organize imports |
| Personalized Generation | Generates code matching your established patterns |

**Goal:** Make AI-generated code pass your review on the first attempt.

---

### ☁️ Nebula Agent — Memory Engine

> *Long-term memory as a core platform capability.*

Nebula is the **memory backbone** of Noosphere. Every Workspace, every conversation, every learned preference is stored, indexed, and retrievable. Agent sessions persist across restarts. Context is managed automatically.

| Memory Type | Purpose | TTL |
|-------------|---------|-----|
| Working Memory | Active task context | Session |
| Episodic Memory | Past interactions &amp; decisions | Permanent |
| Semantic Memory | Learned facts &amp; patterns | Permanent |
| Procedural Memory | Learned workflows &amp; skills | Permanent |

**Goal:** Enable AI that remembers yesterday's work, picks up interrupted tasks, and grows more helpful over time.

---

### ⚙️ Ops Toolkit — AIOps Workspace

> *Reduce the cognitive load of operations.*

Log analysis, service monitoring, fault diagnosis — all powered by AI with access to your system's history. The Ops Toolkit correlates current incidents with past resolutions stored in Nebula's memory.

| Capability | Description |
|------------|-------------|
| Log Analysis | Pattern-aware log parsing with anomaly detection |
| Service Monitoring | Real-time metrics collection (CPU, memory, disk, network) |
| Fault Diagnosis | Compares current incidents against stored fault history |
| Automated Remediation | Suggests (and optionally executes) verified fixes |

**Goal:** "How did we fix this last time?" — answered instantly.

---

### 🔑 AI Gateway — Platform Infrastructure

> *Model capabilities decoupled from business logic.*

AI Gateway is **not a product**. It is platform infrastructure — accessible from `Settings → AI Gateway` in the platform navigation. Configure once, usable from every Workspace.

| Feature | Description |
|---------|-------------|
| Multi-Provider | OpenAI · Claude · Gemini · DeepSeek · Ollama · Custom API |
| Unified SDK | `chat()` / `streamChat()` / `vision()` / `embedding()` |
| Model Testing | One-click connectivity check with latency &amp; token stats |
| Call Logging | Full audit trail: every request, response, and error |
| Token Analytics | Per-model and per-workspace usage statistics |
| API Doc Import | Parse Swagger / OpenAPI / Markdown into structured endpoints |

```
CodeLens ──┐
Nebula  ──┤── AI Gateway SDK ──► Your Models
OpsToolkit─┘
```

---

## How It Works: A Day in the Life

```text
08:00 — You open Noosphere. Nebula restores yesterday's context.
        The task you were working on is right where you left it.

09:15 — You ask CodeLens to refactor the auth module. It knows
        you prefer explicit error returns over exceptions. It knows
        your team's naming conventions. The generated code matches.

11:30 — An alert fires. Ops Toolkit identifies the pattern, searches
        Nebula's fault memory, and surfaces the resolution from
        an incident three weeks ago. Diagnosis time: 30 seconds.

14:00 — You add a new Claude API key in AI Gateway settings.
        All three Workspaces now have access. No code changes needed.

16:00 — Nebula has been learning all day. Your preferences,
        decisions, and patterns are stored. Tomorrow, it starts
        from where you left off — not from zero.
```

---

## Getting Started

### Prerequisites

| Tool | Version | Check |
|------|---------|-------|
| **Go** | 1.26+ | `go version` |
| **Python** | 3.10+ | `python --version` |
| **Node.js** | 20+ | `node --version` |
| **Docker** (optional) | latest | `docker --version` |

### One-Click Launch

```bash
cd Noosphere

# Configure your LLM API key
copy .env.example .env
# Edit .env → add your DEEPSEEK_API_KEY
# (Or configure later via Settings → AI Gateway in the UI)

# Launch everything
start.bat
```

This opens 4 terminal windows — one per service. Visit **http://localhost:3000**.

### Manual Launch

If `start.bat` doesn't work on your system, start each service in its own terminal:

**Terminal 1 — Nebula (:8730):**
```bash
cd nebula
go run ./cmd/nebula-server --data ./nebula-data --port 8730
```

**Terminal 2 — DevOps (:8740):**
```bash
cd devops
go run ./cmd/devops-server --port 8740
```

**Terminal 3 — CodeLens (:8765):**
```bash
cd codelens
pip install -r requirements.txt        # first time only
copy config\.env.example config\.env   # first time only
python -m src.main serve
```

**Terminal 4 — Platform Frontend (:3000):**
```bash
cd web
npm install       # first time only
npm run dev
```

### Docker Compose

```bash
docker compose up -d
```

### Access

| URL | What |
|-----|------|
| `http://localhost:3000` | **Noosphere Platform** — Dashboard · Workspaces · Settings |
| `http://localhost:3000/codelens` | CodeLens Workspace |
| `http://localhost:3000/nebula` | Nebula Workspace |
| `http://localhost:3000/devops` | Ops Toolkit Workspace |
| `http://localhost:3000/settings/model` | AI Gateway Configuration |

> **One port, one platform.** Backend APIs run internally — users never need to know about `:8765`, `:8730`, or `:8740`.

<details>
<summary><b>CodeLens</b> (Python 3.10+)</summary>

```bash
cd codelens
pip install -r requirements.txt
cp config/.env.example config/.env
python -m src.main serve
# API: http://localhost:8765  ·  Docs: http://localhost:8765/docs
```
</details>

<details>
<summary><b>Nebula Engine</b> (Go 1.26+)</summary>

```bash
cd nebula
export DEEPSEEK_API_KEY=sk-xxx    # or: set DEEPSEEK_API_KEY=sk-xxx
go run ./cmd/nebula-server --data ./nebula-data --port 8730
# API + Dashboard: http://localhost:8730
```
</details>

<details>
<summary><b>Ops Toolkit</b> (Go 1.26+)</summary>

```bash
cd devops
go run ./cmd/devops-server --port 8740
# API + Dashboard: http://localhost:8740/web/
```
</details>

---

## Technical Architecture

```text
┌──────────────────────────────────────────────────────────┐
│                    PRESENTATION LAYER                    │
│  Next.js 14 · React 18 · TypeScript · Three.js · GSAP   │
│  Tailwind CSS · Framer Motion · Lucide Icons            │
│  Single Port (:3000) · App Router · SSR                 │
├──────────────────────────────────────────────────────────┤
│                    WORKSPACE LAYER                       │
│  ┌─────────────┐  ┌──────────────┐  ┌────────────────┐  │
│  │  CodeLens   │  │   Nebula     │  │  Ops Toolkit   │  │
│  │  Python     │  │   Go         │  │  Go            │  │
│  │  :8765      │  │   :8730      │  │  :8740         │  │
│  └──────┬──────┘  └──────┬───────┘  └───────┬────────┘  │
├─────────┼────────────────┼──────────────────┼────────────┤
│         └────────────────┼──────────────────┘            │
│                   ┌──────▼──────┐                        │
│                   │ AI Gateway  │   Platform Infra       │
│                   │ SDK + API   │   (included, not       │
│                   │ Route       │    standalone)         │
│                   └─────────────┘                        │
├──────────────────────────────────────────────────────────┤
│                    INFRASTRUCTURE LAYER                   │
│  ┌────────────┐  ┌───────────┐  ┌──────────────────┐    │
│  │ LSM-Tree   │  │ HNSW      │  │ NetworkX         │    │
│  │ (WAL+SST)  │  │ Vector    │  │ Knowledge Graph  │    │
│  │ Go/Python  │  │ Index     │  │ Python           │    │
│  └────────────┘  └───────────┘  └──────────────────┘    │
│  ┌────────────┐  ┌───────────┐  ┌──────────────────┐    │
│  │ ChromaDB   │  │ BM25      │  │ RRF Fusion       │    │
│  │ Vector DB  │  │ Keyword   │  │ Hybrid Search    │    │
│  └────────────┘  └───────────┘  └──────────────────┘    │
└──────────────────────────────────────────────────────────┘
```

### Technology Stack

| Layer | Technology |
|-------|-----------|
| **Frontend** | Next.js 14 · React 18 · TypeScript · Tailwind CSS · Three.js · Framer Motion · GSAP |
| **Backend (Go)** | Go 1.26 · net/http (stdlib) · LSM-Tree (self-built) · HNSW (self-built) · BM25 · RRF |
| **Backend (Python)** | Python 3.10+ · FastAPI · Tree-sitter · NetworkX · ChromaDB · urllib (stdlib) |
| **Storage** | Self-built LSM-Tree · In-Memory Sharded KV · ChromaDB · LevelDB |
| **Vector Search** | HNSW (Go, zero CGo) · ChromaDB (Python) · Sentence Transformers |
| **LLM Integration** | OpenAI-compatible protocol · Multi-provider routing · Streaming SSE |
| **Deployment** | Docker Compose · Multi-stage builds · Alpine-based images · CGO_ENABLED=0 |

---

## Project Structure

```
noosphere/
├── web/                        # 🌐 Platform Frontend (Next.js 14)
│   ├── src/
│   │   ├── app/
│   │   │   ├── page.tsx                     # / — Dashboard
│   │   │   ├── codelens/page.tsx            # /codelens
│   │   │   ├── nebula/page.tsx              # /nebula
│   │   │   ├── devops/page.tsx              # /devops
│   │   │   ├── settings/model/page.tsx      # /settings/model — AI Gateway
│   │   │   └── api/gateway/route.ts         # LLM proxy route
│   │   ├── components/
│   │   │   ├── dashboard/  · workspace/  · gateway/
│   │   │   ├── layout/     · ui/        · hero/
│   │   │   └── sections/   · three/
│   │   ├── hooks/          # useServiceStatus · useLiveMetrics
│   │   └── lib/            # services · data · gateway · motion
│   └── Dockerfile
│
├── codelens/                   # 🔍 Developer Intelligence (Python)
│   ├── src/
│   │   ├── parser/             # Tree-sitter AST (7 languages)
│   │   ├── indexer/            # Knowledge Graph (NetworkX)
│   │   ├── storage/            # LSM-Tree + Vector Store
│   │   ├── rag/                # Hybrid RAG + QA Engine
│   │   ├── agent/              # CodeLensAgent + GitDiff + DocGen
│   │   ├── api/                # FastAPI (:8765) + Dashboard
│   │   └── workspace/          # Project scanner + LLM validator
│   └── Dockerfile
│
├── nebula/                     # ☁️ Memory Engine (Go)
│   ├── engine/
│   │   ├── storage/lsm/        # Self-built LSM-Tree
│   │   ├── storage/kv/         # Sharded In-Memory KV
│   │   ├── storage/vector/     # HNSW (pure Go)
│   │   ├── retrieval/          # Embedder + Hybrid + RRF
│   │   ├── index/              # Inverted Index + BM25
│   │   ├── cache/              # LRU Cache
│   │   └── manager/            # 4 Memory Types + Consolidation
│   ├── analyzer/               # Code Ingestion + Style Learning
│   ├── api/rest/               # Go HTTP Server (:8730)
│   └── Dockerfile
│
├── devops/                     # ⚙️ AIOps (Go)
│   ├── agent/                  # Task Orchestration + Tool Registry
│   ├── tools/                  # System · Log · Service tools
│   ├── memory/                 # Fault Store (embedded Nebula)
│   ├── metrics/                # System Metrics Collector
│   ├── analyzer/               # Log Pattern Analyzer
│   ├── api/                    # REST API (:8740)
│   └── Dockerfile
│
├── sdk/aiGateway/              # 🔑 Unified AI Gateway SDK
│   ├── go/client.go            # Go SDK (Nebula / DevOps)
│   └── python/client.py        # Python SDK (CodeLens)
│
├── workspace/                  # 🗂 Project Inbox
│   ├── projects/               # Drop projects here → auto-analyze
│   └── llm_config.example.yaml # Multi-provider LLM config
│
├── docs/                       # 📚 Documentation
│   ├── ARCHITECTURE.md         # Deep architecture design
│   └── API.md                  # Complete API reference
│
├── docker-compose.yml          # One-command full deployment
├── .env.example                # Environment template
├── LICENSE                     # MIT
└── CONTRIBUTING.md             # Contributor guide
```

---

## Design Philosophy

### Platform First

Noosphere is not a collection of AI tools stitched together. It is a **platform**: shared infrastructure, unified model access, centralized memory, and a single user experience. Each Workspace is an extension of the platform, not a separate product.

### Memory Driven

Memory is a **core platform capability**, not an afterthought. Nebula provides working, episodic, semantic, and procedural memory types — all indexed, searchable, and automatically consolidated. Agents that remember yesterday are more useful today.

### Personalized AI

CodeLens doesn't generate generic code. It learns **your** conventions. Nebula doesn't store generic context. It stores **your** history. The platform adapts to how you work, not the other way around.

### Unified Infrastructure

One model configuration. One SDK. One memory engine. Build a new Workspace and it inherits all platform capabilities — models, memory, monitoring — without reinventing any wheel.

### Evolutionary Design

Noosphere wasn't designed upfront. It evolved:

```text
Code Style Learning  →  Memory Engine  →  Ops Toolkit
                                            ↓
                                    Shared capabilities
                                    became visible
                                            ↓
                                       AI Gateway
                                            ↓
                                       Noosphere
```

This evolution continues. Each Workspace that joins the platform strengthens the shared infrastructure for all others.

---

## Roadmap

| Phase | Status | Focus |
|-------|--------|-------|
| **Foundation** | ✅ Complete | Docker Compose · Unified config · Workspace model · AI Gateway |
| **Integration** | ✅ Complete | Multi-provider LLM · SDK (Go/Python/TS) · Project scanner · Validation |
| **Intelligence** | 🚧 In Progress | Cross-workspace memory sharing · Agent workflow engine · MCP Server |
| **Ecosystem** | 📋 Planned | Plugin system · Style template marketplace · GitHub Actions CI · VSCode extension |

---

## Contributing

Noosphere is evolving. We welcome contributions at every level:

- **Workspace ideas** — new domains where AI + Memory can help
- **Provider integrations** — add support for new LLM providers
- **Memory patterns** — improve how Nebula consolidates and retrieves
- **Documentation** — help us tell the story better

Start by reading [CONTRIBUTING.md](CONTRIBUTING.md), then open an issue or PR.

---

## FAQ

<details>
<summary><b>What LLMs does Noosphere support?</b></summary>

OpenAI · Anthropic Claude · Google Gemini · DeepSeek · Ollama (local) · any OpenAI-compatible API. All configured in one place: <code>/settings/model</code>.
</details>

<details>
<summary><b>Does my code leave my machine?</b></summary>

No. All indexing, knowledge graphs, and memory storage are local (LSM-Tree, ChromaDB, Docker volumes). Only the specific code snippets relevant to your query are sent to the LLM provider — never the entire codebase.
</details>

<details>
<summary><b>Do I need to run all three Workspaces?</b></summary>

No. Each Workspace is independently useful. Run just CodeLens for code intelligence, or just Ops Toolkit for monitoring. Together they share Nebula's memory for cross-domain context.
</details>

<details>
<summary><b>How does the platform handle multiple LLM keys?</b></summary>

Configure all your API keys in <code>workspace/llm_config.yaml</code> or via the Settings UI at <code>/settings/model</code>. The AI Gateway SDK automatically routes calls to the appropriate provider. Keys never leave your machine.
</details>

---

<div align="center">

**Noosphere** — *An AI Operating Platform that learns, remembers, and grows with you.*

[MIT](LICENSE) · Made with ❤️ by Noosphere Contributors · 2026

</div>
