/**
 * AI Gateway API Route — Next.js 内嵌模型路由
 *
 * 替代独立 Gateway 服务。所有前端 LLM 调用通过此路由代理。
 * Go/Python 后端通过各自 SDK 直接调用（不经过此路由）。
 *
 * 端点：
 *   GET  /api/gateway/health
 *   GET  /api/gateway/models
 *   POST /api/gateway/models
 *   DELETE /api/gateway/models/[id]
 *   POST /api/gateway/models/[id]/test
 *   POST /api/gateway/chat
 *   GET  /api/gateway/stats
 *   GET  /api/gateway/logs
 */

import { NextRequest, NextResponse } from "next/server";
import { readFileSync, writeFileSync, existsSync } from "fs";
import { join } from "path";

// ─── Types ───

interface ModelConfig {
  id: string; name: string; provider: string; base_url: string;
  api_key: string; model_name: string; enabled: boolean; is_default: boolean;
  temperature?: number; max_tokens?: number;
}

interface GatewayConfig {
  default_model: string;
  models: ModelConfig[];
}

interface CallLog {
  id: string; model_id: string; provider: string; project: string;
  request_preview: string; response_preview: string;
  tokens_in: number; tokens_out: number; latency_ms: number;
  status: "ok" | "error"; error?: string; timestamp: string;
}

// ─── In-memory state ───

let configCache: GatewayConfig | null = null;
const logStore: CallLog[] = [];
const MAX_LOGS = 500;
const stats: Record<string, { calls: number; tokens: number }> = {};

// ─── Config path ───

function configPath(): string {
  return join(process.cwd(), "..", "workspace", "llm_config.yaml");
}

function loadConfig(): GatewayConfig {
  if (configCache) return configCache;
  const p = configPath();
  if (!existsSync(p)) {
    return { default_model: "", models: [] };
  }
  const raw = readFileSync(p, "utf-8");
  configCache = parseYAML(raw);
  return configCache!;
}

function saveConfig(cfg: GatewayConfig): void {
  configCache = cfg;
  let yaml = `# Noosphere AI Gateway — Model Configuration\n# Managed from /settings/model\n\ndefault_model: ${cfg.default_model}\n\nproviders:\n`;
  for (const m of cfg.models) {
    yaml += `  - name: ${m.id}\n`;
    yaml += `    type: ${m.provider}\n`;
    yaml += `    api_key: "${m.api_key}"\n`;
    yaml += `    base_url: ${m.base_url}\n`;
    yaml += `    model: ${m.model_name}\n`;
    yaml += `    enabled: ${m.enabled}\n`;
    if (m.is_default) yaml += `    is_default: true\n`;
    if (m.temperature) yaml += `    temperature: ${m.temperature}\n`;
    if (m.max_tokens) yaml += `    max_tokens: ${m.max_tokens}\n`;
    yaml += `\n`;
  }
  writeFileSync(configPath(), yaml, "utf-8");
}

// ─── YAML Parser ───

function parseYAML(content: string): GatewayConfig {
  const cfg: GatewayConfig = { default_model: "", models: [] };
  let current: Partial<ModelConfig> | null = null;
  const lines = content.split("\n");

  for (const line of lines) {
    const t = line.trim();
    if (!t || t.startsWith("#")) continue;

    if (t.startsWith("default_model:")) {
      cfg.default_model = t.split(":")[1].trim();
      continue;
    }
    if (t.startsWith("- name:")) {
      if (current?.id) cfg.models.push(current as ModelConfig);
      current = { id: t.split(":")[1].trim(), enabled: true, is_default: false,
        name: "", provider: "deepseek", base_url: "", api_key: "", model_name: "" };
      continue;
    }
    if (current) {
      const [k, ...v] = t.split(":");
      const val = v.join(":").trim().replace(/^"(.*)"$/, "$1").replace(/^'(.*)'$/, "$1");
      switch (k.trim()) {
        case "type": current.provider = val; break;
        case "api_key": current.api_key = val; break;
        case "base_url": current.base_url = val; break;
        case "model": current.model_name = val; break;
        case "enabled": current.enabled = val === "true"; break;
        case "is_default": current.is_default = val === "true"; break;
        case "temperature": current.temperature = parseFloat(val); break;
        case "max_tokens": current.max_tokens = parseInt(val); break;
      }
    }
  }
  if (current?.id) cfg.models.push(current as ModelConfig);
  return cfg;
}

// ─── Mask API key ───

function maskKey(key: string): string {
  if (!key || key.length < 12) return key ? "***" : "";
  return key.slice(0, 8) + "..." + key.slice(-4);
}

function maskModels(models: ModelConfig[]): ModelConfig[] {
  return models.map(m => ({ ...m, api_key: maskKey(m.api_key) }));
}

// ─── Resolve default model ───

function defaultModel(): ModelConfig | null {
  const cfg = loadConfig();
  const def = cfg.models.find(m => m.id === cfg.default_model && m.enabled);
  if (def) return def;
  return cfg.models.find(m => m.enabled) ?? null;
}

// ─── LLM call ───

async function callLLM(model: ModelConfig, messages: Array<{ role: string; content: string }>, opts?: {
  system_prompt?: string; temperature?: number; max_tokens?: number; stream?: boolean;
}): Promise<{ content: string; tokens_in: number; tokens_out: number; latency_ms: number }> {
  const start = Date.now();

  const msgs: Array<Record<string, string>> = [];
  if (opts?.system_prompt) msgs.push({ role: "system", content: opts.system_prompt });
  for (const m of messages) msgs.push({ role: m.role, content: m.content });

  const body: Record<string, unknown> = {
    model: model.model_name,
    messages: msgs,
    max_tokens: opts?.max_tokens ?? model.max_tokens ?? 4096,
    temperature: opts?.temperature ?? model.temperature ?? 0.1,
    stream: false,
  };

  const url = model.base_url.replace(/\/$/, "") + "/chat/completions";
  const res = await fetch(url, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "Authorization": `Bearer ${model.api_key}`,
    },
    body: JSON.stringify(body),
    signal: AbortSignal.timeout(120_000),
  });

  if (!res.ok) {
    const err = await res.text();
    throw new Error(`Provider returned ${res.status}: ${err.slice(0, 300)}`);
  }

  const data = await res.json();
  const content = data.choices?.[0]?.message?.content ?? "";
  const tokens_in = data.usage?.prompt_tokens ?? 0;
  const tokens_out = data.usage?.completion_tokens ?? 0;

  return {
    content,
    tokens_in,
    tokens_out,
    latency_ms: Date.now() - start,
  };
}

// ─── Route Handler ───

export async function GET(req: NextRequest) {
  const url = new URL(req.url);
  const path = url.pathname.replace("/api/gateway", "").replace(/\/$/, "");
  const parts = path.split("/").filter(Boolean);

  // /api/gateway/health
  if (path === "/health" || path === "") {
    return NextResponse.json({ status: "ok", service: "ai-gateway", embedded: true });
  }

  // /api/gateway/models
  if (path === "/models") {
    const cfg = loadConfig();
    return NextResponse.json({
      models: maskModels(cfg.models),
      default_model: cfg.default_model,
      count: cfg.models.length,
    });
  }

  // /api/gateway/stats
  if (path === "/stats") {
    const cfg = loadConfig();
    const projectStats: Record<string, number> = {};
    for (const log of logStore) {
      projectStats[log.project] = (projectStats[log.project] ?? 0) + 1;
    }
    return NextResponse.json({
      total_models: cfg.models.length,
      total_calls: logStore.length,
      total_tokens: logStore.reduce((s, l) => s + l.tokens_in + l.tokens_out, 0),
      total_logs: logStore.length,
      models: cfg.models.map(m => ({
        id: m.id, name: m.name,
        calls: stats[m.id]?.calls ?? 0,
        tokens: stats[m.id]?.tokens ?? 0,
        status: m.enabled ? "unknown" : "error",
      })),
      by_project: projectStats,
    });
  }

  // /api/gateway/logs
  if (path === "/logs") {
    const project = url.searchParams.get("project");
    const limit = parseInt(url.searchParams.get("limit") ?? "50");
    let logs = logStore;
    if (project) logs = logs.filter(l => l.project === project);
    return NextResponse.json({ logs: logs.slice(-limit).reverse(), count: logs.length });
  }

  return NextResponse.json({ error: "not found" }, { status: 404 });
}

export async function POST(req: NextRequest) {
  const url = new URL(req.url);
  const path = url.pathname.replace("/api/gateway", "").replace(/\/$/, "");
  const parts = path.split("/").filter(Boolean);

  // /api/gateway/models — add model
  if (path === "/models") {
    const body = await req.json();
    const cfg = loadConfig();
    const idx = cfg.models.findIndex(m => m.id === body.id);
    const model: ModelConfig = {
      id: body.id || `model-${Date.now()}`,
      name: body.name || body.id,
      provider: body.provider || "deepseek",
      base_url: body.base_url || "",
      api_key: body.api_key || "",
      model_name: body.model_name || "",
      enabled: body.enabled !== false,
      is_default: body.is_default === true,
    };
    if (idx >= 0) cfg.models[idx] = model;
    else cfg.models.push(model);
    if (model.is_default) cfg.default_model = model.id;
    saveConfig(cfg);
    return NextResponse.json({ added: { ...model, api_key: maskKey(model.api_key) }, status: "ok" }, { status: 201 });
  }

  // /api/gateway/models/[id]/test
  if (parts.length >= 2 && parts[0] === "models" && parts[parts.length - 1] === "test") {
    const modelId = parts[1];
    const cfg = loadConfig();
    const model = cfg.models.find(m => m.id === modelId);
    if (!model) return NextResponse.json({ error: "model not found" }, { status: 404 });

    try {
      const resp = await callLLM(model, [{ role: "user", content: "Say 'OK' and nothing else." }], { max_tokens: 10 });
      return NextResponse.json({
        status: "ok", model: model.model_name, provider: model.provider,
        latency_ms: resp.latency_ms, tokens: resp.tokens_in + resp.tokens_out,
        response: resp.content.slice(0, 100),
      });
    } catch (e: unknown) {
      return NextResponse.json({
        status: "error", model: model.model_name,
        latency_ms: 0, error: (e as Error).message,
      });
    }
  }

  // /api/gateway/chat
  if (path === "/chat") {
    const body = await req.json();
    const cfg = loadConfig();
    const model = body.model_id
      ? cfg.models.find((m: ModelConfig) => m.id === body.model_id)
      : defaultModel();
    if (!model) return NextResponse.json({ error: "no model configured" }, { status: 400 });

    try {
      const resp = await callLLM(model, body.messages ?? [], {
        system_prompt: body.system_prompt,
        temperature: body.temperature,
        max_tokens: body.max_tokens,
      });

      // Record log
      const log: CallLog = {
        id: `log-${Date.now()}`,
        model_id: model.id, provider: model.provider,
        project: body.project ?? "web",
        request_preview: (body.messages?.slice(-1)?.[0]?.content ?? "").slice(0, 200),
        response_preview: resp.content.slice(0, 200),
        tokens_in: resp.tokens_in, tokens_out: resp.tokens_out,
        latency_ms: resp.latency_ms, status: "ok",
        timestamp: new Date().toISOString(),
      };
      logStore.push(log);
      if (logStore.length > MAX_LOGS) logStore.shift();
      if (!stats[model.id]) stats[model.id] = { calls: 0, tokens: 0 };
      stats[model.id].calls++;
      stats[model.id].tokens += resp.tokens_in + resp.tokens_out;

      return NextResponse.json({
        id: log.id, model: model.model_name, provider: model.provider,
        content: resp.content, tokens_used: resp.tokens_in + resp.tokens_out,
        latency_ms: resp.latency_ms, timestamp: log.timestamp, project: body.project,
      });
    } catch (e: unknown) {
      return NextResponse.json({ error: (e as Error).message }, { status: 502 });
    }
  }

  return NextResponse.json({ error: "not found" }, { status: 404 });
}

export async function DELETE(req: NextRequest) {
  const url = new URL(req.url);
  const path = url.pathname.replace("/api/gateway", "").replace(/\/$/, "");
  const parts = path.split("/").filter(Boolean);

  // /api/gateway/models/[id]
  if (parts.length === 2 && parts[0] === "models") {
    const modelId = parts[1];
    const cfg = loadConfig();
    cfg.models = cfg.models.filter(m => m.id !== modelId);
    if (cfg.default_model === modelId) cfg.default_model = cfg.models[0]?.id ?? "";
    saveConfig(cfg);
    return NextResponse.json({ deleted: modelId, status: "ok" });
  }

  return NextResponse.json({ error: "not found" }, { status: 404 });
}
