# Noosphere Mock Data Injector
# 用途: 向运行中的后端 (:8080) 注入多会话 + 多类型记忆, 用于本地验证
#       Nebula 页面会话列表 / 语义搜索 / Chat RAG 的交互效果
# 用法: powershell -File tests\mock-data.ps1   (需后端已启动)
# 日志: 每步输出注入结果; 搜索验证输出混合检索命中情况

$BASE = "http://127.0.0.1:8080"
$ErrorActionPreference = "Stop"

function Post($path, $body) {
  $r = Invoke-RestMethod -Uri ($BASE + $path) -Method Post -Body $body -ContentType "application/json" -TimeoutSec 15
  return $r
}

Write-Host "=== Noosphere Mock Data Injector ===" -ForegroundColor Cyan

# ---- 0. 健康检查 ----
try {
  $h = Invoke-RestMethod -Uri "$BASE/health" -TimeoutSec 5
  Write-Host "[ok] backend online: v$($h.version) (modules: $($h.modules.PSObject.Properties.Name -join ', '))"
} catch {
  Write-Host "[fail] backend offline - start it first: cd backend && go run ./cmd/server" -ForegroundColor Red
  exit 1
}

# ---- 1. 会话 + 记忆定义 ----
# 内容刻意覆盖不同主题关键词 (retry / LSM / HNSW / agent / SSE / WAL),
# 便于验证 BM25 + 向量混合检索对不同 query 的召回差异
$sessions = @(
  @{
    name = "codelens-project"
    memories = @(
      @{ type = "semantic"; importance = 0.9; content = "Project index summary: noosphere backend contains 4 modules. Gateway implements LLM proxy with retry and token stats. Nebula is the memory engine with LSM-Tree storage. DevOps collects Windows metrics via agent loop. CodeLens analyzes code with tree-sitter AST."; tags = @("codelens","project-index"); meta = @{ source = "codelens"; entity_type = "project_summary" } },
      @{ type = "semantic"; importance = 0.8; content = "gateway.go contains HandleChat, HandleChatStream and provider registry. Retry mechanism uses exponential backoff for connection refused, timeout and 429 responses. Max 2 retries per request."; tags = @("codelens","code"); meta = @{ source = "codelens"; file = "internal/gateway/gateway.go" } },
      @{ type = "semantic"; importance = 0.7; content = "Nebula engine pipeline: Ingest -> Extractor -> Embedding -> VectorIndex(HNSW) -> HybridRetrieval(RRF: BM25 + vector similarity). LSM-Tree persists via WAL and SSTable with compaction."; tags = @("codelens","architecture"); meta = @{ source = "codelens"; file = "internal/nebula/engine" } }
    )
  },
  @{
    name = "gateway-debug"
    memories = @(
      @{ type = "episodic"; importance = 0.6; content = "2026-08-18: DeepSeek stream request failed with 429 rate limit. Root cause: concurrent chat requests exceeded provider quota. Fixed by adding semaphore with max 10 concurrent requests."; tags = @("debug","429"); meta = @{ source = "manual"; severity = "high" } },
      @{ type = "procedural"; importance = 0.8; content = "Procedure to test provider connectivity: POST /api/gateway/models/{id}/test. It sends a minimal chat request and reports latency plus token usage. Check models.yaml reload via POST /api/gateway/reload."; tags = @("howto","gateway"); meta = @{ source = "manual" } }
    )
  },
  @{
    name = "arch-design"
    memories = @(
      @{ type = "semantic"; importance = 0.9; content = "Desktop architecture: Tauri v2 shell spawns Go backend as sidecar process on port 8080. React frontend communicates via Tauri IPC invoke commands, Rust layer proxies HTTP to backend. Health monitor restarts backend every 10s check."; tags = @("architecture","tauri"); meta = @{ source = "manual" } },
      @{ type = "working"; importance = 0.4; content = "Pending design: session-level retrieval pushdown for Nebula to fix recall degradation when session count grows. Current global topK*3 then filter loses target session memories."; tags = @("todo","nebula"); meta = @{ source = "manual"; status = "open" } }
    )
  },
  @{
    name = "ops-notes"
    memories = @(
      @{ type = "episodic"; importance = 0.5; content = "Observed CPU spike to 90% during CodeLens indexing of large repos. Python worker tree-sitter parsing is CPU bound. Mitigation: exclude node_modules and target directories from index."; tags = @("ops","codelens"); meta = @{ source = "manual" } },
      @{ type = "procedural"; importance = 0.7; content = "DevOps agent loop: Planner -> Tool Executor -> Observation -> Reflection. Tools include system_info, tasklist, netstat, tail_log, search_log, service_list, service_status. Diagnose endpoint feeds metrics to LLM for analysis."; tags = @("agent","devops"); meta = @{ source = "manual" } }
    )
  }
)

# ---- 2. 注入 ----
$created = @()
foreach ($s in $sessions) {
  $r = Post "/api/v1/sessions" (ConvertTo-Json @{ name = $s.name })
  $sid = $r.session_id
  $created += @{ name = $s.name; id = $sid }
  Write-Host "[session] '$($s.name)' -> $sid"

  foreach ($m in $s.memories) {
    $body = @{
      session_id = $sid
      content    = $m.content
      type       = $m.type
      tags       = $m.tags
      metadata   = $m.meta
    } | ConvertTo-Json -Depth 4
    try {
      Post "/api/v1/sessions/$sid/memories" $body | Out-Null
      Write-Host "  [memory] $($m.type.PadRight(10)) imp=$($m.importance) len=$($m.content.Length)"
    } catch {
      Write-Host "  [memory] FAIL: $_" -ForegroundColor Red
    }
  }
}

# ---- 3. 语义搜索验证 (混合检索召回, 按会话作用域) ----
Write-Host "`n=== Semantic search verification ===" -ForegroundColor Cyan
$sidOf = { param($n) ($created | Where-Object { $_.name -eq $n }).id }

# query -> 目标会话 (验证会话隔离 + 混合召回)
$checks = @(
  @{ session = "codelens-project"; query = "retry mechanism" },
  @{ session = "codelens-project"; query = "HNSW vector index" },
  @{ session = "arch-design";      query = "tauri architecture" },
  @{ session = "gateway-debug";    query = "429 rate limit" }
)

foreach ($c in $checks) {
  $sid = & $sidOf $c.session
  $body = @{ query = $c.query; top_k = 3 } | ConvertTo-Json
  $r = Post "/api/v1/sessions/$sid/search" $body
  Write-Host "`n[query] `"$($c.query)`" @ $($c.session) -> $($r.count) hits"
  foreach ($hit in $r.results) {
    $preview = if ($hit.record.content.Length -gt 60) { $hit.record.content.Substring(0, 60) + "..." } else { $hit.record.content }
    Write-Host ("  score={0:N3}  type={1,-9}  {2}" -f [double]$hit.score, $hit.record.type, $preview)
  }
}

# 跨会话隔离验证: "tauri" 只存在于 arch-design, 在 codelens-project 中应为 0 命中
$clSid = & $sidOf "codelens-project"
$body = @{ query = "tauri sidecar"; top_k = 3 } | ConvertTo-Json
$r = Post "/api/v1/sessions/$clSid/search" $body
Write-Host "`n[isolation] `"tauri sidecar`" @ codelens-project -> $($r.count) hits (expect 0)"

Write-Host "`n=== Done: $($created.Count) sessions injected ===" -ForegroundColor Green
Write-Host "Open the app -> Nebula page to see sessions; try searching 'retry' / 'index' / 'tauri'."
Write-Host "Chat RAG also uses session 'codelens-project' (question about code will auto-inject context)."
