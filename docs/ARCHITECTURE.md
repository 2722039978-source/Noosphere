# Architecture Design

> Noosphere is an **AI Operating Platform**, not a collection of independent tools.
> This document describes the platform topology, layer responsibilities, data flow, and design decisions.

---

## Platform Topology

```
                     Noosphere Platform (:3000)
        ┌──────────────────────────────────────────────────┐
        │              Next.js 14 · Single Entry Point      │
        │                                                  │
        │  /              Dashboard                        │
        │  /codelens      CodeLens Workspace               │
        │  /nebula        Nebula Workspace                 │
        │  /devops        DevOps Workspace                 │
        │  /settings/model AI Gateway (Settings)           │
        │  /api/gateway   LLM Proxy Route                  │
        └──────┬──────────┬──────────┬─────────────────────┘
               │          │          │
          ┌────▼────┐ ┌───▼────┐ ┌──▼─────┐
          │CodeLens │ │ Nebula │ │ DevOps │
          │ :8765   │ │ :8730  │ │ :8740  │
          │ Python  │ │  Go    │ │  Go    │
          └─────────┘ └────────┘ └────────┘
               Internal API services (not user-facing)
```

**Key design decisions:**

- **Single port `:3000`** — Users access one URL. All pages routed through Next.js App Router.
- **AI Gateway is NOT a standalone service** — Embedded as `/settings/model` (UI) + `/api/gateway` (LLM proxy route). No separate port.
- **Backend APIs are internal** — CodeLens/Nebula/DevOps run on their ports for the platform to consume. Users never need to know about `:8765`, `:8730`, or `:8740`.
- **Port allocation**: `3000` (platform), `8765/8766` (CodeLens), `8730` (Nebula), `8740` (DevOps).

---

## Layer Architecture

```
┌──────────────────────────────────────────────────────────┐
│                 PRESENTATION LAYER                       │
│  Next.js 14 · React 18 · TypeScript · Three.js · GSAP   │
│  Tailwind CSS · Framer Motion · Lucide Icons            │
│  Single Port (:3000) · App Router                       │
├──────────────────────────────────────────────────────────┤
│                  WORKSPACE LAYER                         │
│  ┌────────────┐  ┌────────────┐  ┌──────────────────┐   │
│  │ CodeLens   │  │  Nebula    │  │  DevOps          │   │
│  │ Developer  │  │  Memory    │  │  AIOps           │   │
│  │ Intelligence│  │  Engine   │  │  Workspace       │   │
│  │ Python     │  │  Go        │  │  Go              │   │
│  └─────┬──────┘  └─────┬──────┘  └───────┬──────────┘   │
├────────┼───────────────┼──────────────────┼──────────────┤
│        └───────────────┼──────────────────┘              │
│                 ┌──────▼──────┐                          │
│                 │ AI Gateway  │   Platform Infrastructure│
│                 │ (Settings)  │   /settings/model        │
│                 │ /api/gateway│   LLM proxy route        │
│                 └─────────────┘                          │
├──────────────────────────────────────────────────────────┤
│                 INFRASTRUCTURE LAYER                     │
│  ┌──────────┐  ┌───────────┐  ┌──────────────────┐     │
│  │ LSM-Tree │  │ HNSW      │  │ NetworkX         │     │
│  │ WAL+SST  │  │ Vector    │  │ Knowledge Graph  │     │
│  │ Go/Python│  │ Index     │  │ Python           │     │
│  └──────────┘  └───────────┘  └──────────────────┘     │
│  ┌──────────┐  ┌───────────┐  ┌──────────────────┐     │
│  │ ChromaDB │  │ BM25      │  │ RRF Fusion       │     │
│  │ Vector DB│  │ Keyword   │  │ Hybrid Search    │     │
│  └──────────┘  └───────────┘  └──────────────────┘     │
└──────────────────────────────────────────────────────────┘
```

---

## Layer 1 — CodeLens (Python, Developer Intelligence Workspace)

**Mission: Turn flat code into queryable structured knowledge.**

```
Source Code → Tree-sitter AST → Entity Extraction
                                    ↓
                ┌───────────────────────────────────┐
                │       Knowledge Graph (NetworkX)   │
                │  Nodes: Functions, Classes, ...    │
                │  Edges: Calls, Inherits, Imports   │
                └───────────────┬───────────────────┘
                                ↓
         ┌──────────────────────────────────────────┐
         │           Dual Storage                    │
         │  ┌─────────────┐  ┌──────────────────┐   │
         │  │  LSM Store  │  │  Vector Store    │   │
         │  │ (Structured)│  │  (ChromaDB)      │   │
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
         │  (via AI Gateway /api/gateway/chat)      │
         └──────────────────────────────────────────┘
```

| Component | Technology | Role |
|-----------|-----------|------|
| Parser | Tree-sitter (7 languages) | Syntax-level AST, not regex |
| Graph | NetworkX | Call/Inheritance/Dependency graphs |
| Storage | LSM-Tree + ChromaDB | Entity KV + Semantic vector search |
| RAG | Hybrid RRF Fusion | Graph + Vector + Keyword retrieval |
| LLM | AI Gateway proxy | All LLM calls go through `/api/gateway/chat` |

---

## Layer 2 — Nebula (Go, Memory Engine)

**Mission: Give AI long-term memory — four memory types, one engine.**

| Memory Type | Purpose | TTL |
|-------------|---------|-----|
| Working | Active task context, like CPU registers | Session (5min default) |
| Episodic | Past interactions & decisions, like RAM | Permanent |
| Semantic | Learned facts & patterns, like long-term memory | Permanent |
| Procedural | Learned workflows & skills, like muscle memory | Permanent |

**Core engine (zero external dependencies):**

| Component | Technology | Role |
|-----------|-----------|------|
| LSM-Tree | Self-built (WAL + SkipList MemTable + SSTable + Bloom Filter) | Persistent memory storage |
| HNSW | Pure Go, zero CGo | Approximate nearest neighbor search |
| BM25 | Self-built inverted index | Exact keyword matching |
| RRF | Reciprocal Rank Fusion | Hybrid semantic + keyword ranking |
| Embedder | Pluggable (mock / Ollama / OpenAI-compatible) | Text → vector conversion |

**Key design:** The `engine` package is embeddable — DevOps imports it as a Go module (`replace ../nebula`). Same engine code serves both standalone and embedded modes.

---

## Layer 3 — DevOps (Go, AIOps Workspace)

**Mission: Turn one-off incident response into reusable organizational memory.**

```
System Metrics / Services / Logs
          ↓
    ┌─────┴─────┐
    │ Collector │  CPU · Memory · Disk · Network · Processes
    │ Analyzer  │  Log pattern detection + LLM root cause
    └─────┬─────┘
          ↓
    ┌─────┴─────┐
    │  Agent    │  Diagnosis orchestration + Tool dispatch
    └─────┬─────┘
          ↓
    ┌─────┴──────────────────┐
    │  Tool Registry          │
    │  system · log · service │  Extensible tool framework
    └─────┬──────────────────┘
          ↓
    ┌─────┴─────┐
    │Fault Store│  Fault → Diagnosis → Solution triples
    │(Nebula)   │  Semantic search for similar past incidents
    └───────────┘
```

**Key design:** Diagnosis searches historical fault memory BEFORE calling LLM — "how did we fix this last time?" is answered instantly.

---

## AI Gateway — Platform Infrastructure (NOT a standalone product)

AI Gateway is platform infrastructure, accessible from `Settings → AI Gateway`.

```
CodeLens ──┐
Nebula  ──┤── AI Gateway (/api/gateway) ──► User's Models
DevOps  ──┘
```

| Feature | Location |
|---------|----------|
| Model management UI | `/settings/model` |
| LLM proxy | `/api/gateway/chat` (Next.js API Route) |
| Model config file | `workspace/llm_config.yaml` |
| SDK (Go) | `sdk/aiGateway/go/client.go` |
| SDK (Python) | `sdk/aiGateway/python/client.py` |

**All Workspace LLM calls go through the Gateway.** No direct model API access.

---

## Data Flow: A Day in the Platform

```text
1. User opens http://localhost:3000
   → Next.js renders Dashboard
   → useServiceStatus polls CodeLens/Nebula/DevOps health
   → Dashboard shows online status + current model + token usage

2. User navigates to /codelens
   → WorkspaceLayout renders with iframe embedding CodeLens :8765
   → CodeLens chat calls /api/gateway/chat (Next.js API route)
   → API route reads model config, routes to user's LLM provider
   → Response returned with token stats logged

3. User configures a new model at /settings/model
   → Form submits to /api/gateway/models
   → Config written to workspace/llm_config.yaml
   → All Workspaces immediately have access to the new model

4. Nebula backend (Go) needs an embedding
   → Uses Go SDK: client.Embedding(...)
   → SDK reads workspace/llm_config.yaml directly
   → Routes call to configured provider
   → No HTTP proxy needed for backend services
```

---

## Data & Security

| Concern | Strategy |
|---------|----------|
| API Keys | `workspace/llm_config.yaml` (gitignored), `.env`, env vars — three-tier read |
| Data storage | All local: CodeLens → Docker volume `codelens-data`; Nebula → `nebula-data`; DevOps → `devops-memory` |
| Network egress | Only relevant code snippets sent to LLM provider during Q&A/diagnosis. Indexing and retrieval are fully local. |
| Container builds | Go services: multi-stage (`golang:1.26-alpine` → `alpine:3.20`), ~10MB artifacts, `CGO_ENABLED=0` static compilation |

## Build Notes

- **DevOps build context must be repository root**: It references Nebula via `replace github.com/nebula-agent/nebula => ../nebula`. `docker-compose.yml` sets `context: .` and `dockerfile: devops/Dockerfile`.
- Go services serve static web consoles from `./web/` in their working directory.
- CodeLens's `plyvel` requires LevelDB C library — `libleveldb-dev` in Dockerfile; on local install failure, `run.bat` falls back to minimal mode.

---

## Usage

### Start the Platform

```bash
cd Noosphere

# One-click launch (Windows)
start.bat

# Or: Docker Compose
docker compose up -d

# Or: Manual (4 terminals)
# Terminal 1: cd nebula && go run ./cmd/nebula-server --port 8730
# Terminal 2: cd devops && go run ./cmd/devops-server --port 8740
# Terminal 3: cd codelens && python -m src.main serve
# Terminal 4: cd web && npm run dev
```

### Configure Models

1. Open `http://localhost:3000/settings/model`
2. Add your LLM provider (OpenAI / Claude / DeepSeek / Ollama)
3. Test connectivity
4. All Workspaces now have access

### Verify Health

```bash
curl http://localhost:3000/api/gateway/health
curl http://localhost:8765/api/v1/health
curl http://localhost:8730/health
curl http://localhost:8740/health
```
