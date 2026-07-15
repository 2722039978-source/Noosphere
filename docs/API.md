# API Reference

All services are accessible through the **Noosphere Platform at `:3000`**. Backend APIs are internal but can be accessed directly for development.

| Service | Platform Route | Internal Base | Docs |
|---------|---------------|---------------|------|
| 🔑 AI Gateway | `/settings/model` (UI) + `/api/gateway` (API) | `:3000` (embedded) | — |
| 🔍 CodeLens | `/codelens` (UI) | `:8765` | http://localhost:8765/docs |
| ☁️ Nebula | `/nebula` (UI) | `:8730` | http://localhost:8730 |
| ⚙️ DevOps | `/devops` (UI) | `:8740` | http://localhost:8740/web/ |

---

## 🔑 AI Gateway (`/api/gateway` · Platform Embedded)

AI Gateway is not a standalone service. It runs as a Next.js API Route inside the platform at `:3000`.

### Model Management

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/gateway/models` | List configured models |
| `POST` | `/api/gateway/models` | Add or update a model |
| `DELETE` | `/api/gateway/models/{id}` | Remove a model |
| `POST` | `/api/gateway/models/{id}/test` | Test model connectivity |

### Unified LLM Calls

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/gateway/chat` | Chat completion — auto-routes to configured provider |
| `POST` | `/api/gateway/chat/stream` | Streaming chat (SSE) |
| `POST` | `/api/gateway/vision` | Vision / image understanding |
| `POST` | `/api/gateway/embedding` | Text embeddings |

### Monitoring

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/gateway/health` | Health check |
| `GET` | `/api/gateway/stats` | Usage stats by model and project |
| `GET` | `/api/gateway/logs` | Recent call logs |

```bash
# Test a model
curl -X POST http://localhost:3000/api/gateway/models/deepseek-v4/test

# Chat through Gateway
curl -X POST http://localhost:3000/api/gateway/chat \
  -H "Content-Type: application/json" \
  -d '{"messages":[{"role":"user","content":"Hello"}],"project":"codelens"}'

# Get usage stats
curl http://localhost:3000/api/gateway/stats
```

---

## 🔍 CodeLens (`:8765` · Developer Intelligence Workspace)

All routes prefixed with `/api/v1`.

### Index & Status

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/health` | Health check |
| `GET` | `/api/v1/status` | Index status (files / entities / relations) |
| `POST` | `/api/v1/index` | Build / rebuild project index |

### Code Understanding

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/qa` | RAG-based code Q&A |
| `POST` | `/api/v1/search` | Semantic code search |
| `POST` | `/api/v1/analyze/file` | Single-file structure analysis |
| `POST` | `/api/v1/analyze/explain` | Code snippet explanation |
| `WS` | `/api/v1/ws/chat` | Streaming chat (WebSocket) |

### Graph Queries

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/call-chain/{entity}` | Trace call chain |
| `GET` | `/api/v1/impact/{entity}` | Impact analysis |
| `GET` | `/api/v1/knowledge-graph/stats` | Graph statistics |
| `GET` | `/api/v1/knowledge-graph/node/{name}` | Query a single node |
| `GET` | `/api/v1/knowledge-graph/export` | Export full graph |

### Engineering

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/git/diff` | Git Diff risk analysis |
| `POST` | `/api/v1/docs/generate` | Auto-generate project docs |
| `GET` | `/api/v1/issues` | Potential code issues |

### Workspace & Validation

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/workspace/scan` | Scan workspace for projects |
| `GET` | `/api/v1/workspace/projects` | List discovered projects |
| `GET` | `/api/v1/workspace/projects/{name}` | Project details |
| `POST` | `/api/v1/workspace/projects/{name}/analyze` | Analyze a project |
| `POST` | `/api/v1/workspace/validate/llm` | Test LLM connectivity |
| `POST` | `/api/v1/workspace/validate/tools` | Validate tool chain I/O |
| `POST` | `/api/v1/workspace/validate/all` | Full system diagnostic |

```bash
# Q&A
curl -X POST http://localhost:8765/api/v1/qa \
  -H "Content-Type: application/json" \
  -d '{"question": "How does authentication work?"}'

# Trace call chain
curl "http://localhost:8765/api/v1/call-chain/authenticate?max_depth=5"

# Scan projects
curl -X POST http://localhost:8765/api/v1/workspace/scan
```

---

## ☁️ Nebula (`:8730` · Memory Engine)

### Sessions & Memory

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Health check |
| `GET` | `/api/v1/stats` | Engine statistics |
| `POST` | `/api/v1/sessions` | Create session |
| `GET` | `/api/v1/sessions` | List sessions |
| `DELETE` | `/api/v1/sessions/{id}` | Delete session |
| `POST` | `/api/v1/sessions/{id}/memories` | Store memory |
| `GET` | `/api/v1/sessions/{id}/memories/{mid}` | Get memory |
| `DELETE` | `/api/v1/sessions/{id}/memories/{mid}` | Delete memory |
| `POST` | `/api/v1/sessions/{id}/search` | Search memories (hybrid/vector/keyword/temporal) |
| `GET` | `/api/v1/sessions/{id}/stats` | Session statistics |

### AI Chat

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/chat` | Chat with auto memory injection |

```bash
# Create session & store memory
curl -X POST http://localhost:8730/api/v1/sessions \
  -H "Content-Type: application/json" -d '{"session_id": "my-agent"}'

curl -X POST http://localhost:8730/api/v1/sessions/my-agent/memories \
  -H "Content-Type: application/json" \
  -d '{"content": "Error handling uses explicit returns, no panic", "type": "semantic", "importance": 0.8}'

# Semantic search
curl -X POST http://localhost:8730/api/v1/sessions/my-agent/search \
  -H "Content-Type: application/json" \
  -d '{"query": "error handling pattern", "top_k": 5}'
```

---

## ⚙️ DevOps (`:8740` · AIOps Workspace)

All routes prefixed with `/api/v1/devops`.

### Status & Metrics

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Health check |
| `GET` | `/api/v1/devops/status` | Agent status |
| `GET` | `/api/v1/devops/metrics` | Real-time CPU/Memory/Disk/Network |

### Diagnosis & Tools

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/devops/diagnose` | Natural language fault diagnosis |
| `GET` | `/api/v1/devops/tools` | List registered tools |
| `POST` | `/api/v1/devops/tools/execute` | Execute a tool |

### Logs & Fault Memory

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/devops/logs/analyze` | Log anomaly analysis |
| `POST` | `/api/v1/devops/logs/search` | Log search |
| `GET` | `/api/v1/devops/faults` | Fault experience list |
| `POST` | `/api/v1/devops/faults/search` | Semantic fault search |

```bash
# Diagnose
curl -X POST http://localhost:8740/api/v1/devops/diagnose \
  -H "Content-Type: application/json" \
  -d '{"query": "CPU spike in the last hour", "session_id": "ops-1"}'

# Search historical faults
curl -X POST http://localhost:8740/api/v1/devops/faults/search \
  -H "Content-Type: application/json" \
  -d '{"query": "database connection pool exhausted"}'
```

---

## Authentication

All APIs are designed for **local / intranet use** with no built-in authentication. For public exposure, place behind a reverse proxy (Nginx/Caddy) with your own auth layer.

### LLM Key Configuration (3-tier priority)

1. `workspace/llm_config.yaml` — Multi-provider config (recommended, supports DeepSeek/OpenAI/Anthropic/Ollama)
2. `.env` file — `DEEPSEEK_API_KEY`
3. Environment variable — `DEEPSEEK_API_KEY`

**Manage via UI:** Open `/settings/model` in the platform.

## Quick Start

```bash
# Launch all services
cd Noosphere && start.bat

# Or one at a time
cd nebula    && go run ./cmd/nebula-server --port 8730
cd devops    && go run ./cmd/devops-server --port 8740
cd codelens  && python -m src.main serve
cd web       && npm run dev

# Open http://localhost:3000
```
