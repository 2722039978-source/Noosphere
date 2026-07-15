/**
 * AI Gateway — shared types and API client
 *
 * Gateway 不再作为独立服务运行，而是作为平台设置模块。
 * 配置存储在 workspace/llm_config.yaml，SDK 直接读取配置并路由调用。
 *
 * 前端通过 Next.js API Route (/api/gateway/*) 调用 LLM。
 * Go/Python 后端通过对应 SDK 直接调用。
 */

export interface ModelInfo {
  id: string;
  name: string;
  provider: string;
  model_name: string;
  base_url: string;
  enabled: boolean;
  is_default: boolean;
  status: "unknown" | "ok" | "error";
  total_calls?: number;
  total_tokens?: number;
  last_used?: string;
}

export interface GatewayStats {
  total_models: number;
  total_calls: number;
  total_tokens: number;
  total_logs: number;
  models: Array<{
    id: string; name: string; calls: number; tokens: number; status: string;
  }>;
  by_project: Record<string, number>;
}

export interface CallLog {
  id: string; model_id: string; provider: string; project: string;
  request_preview: string; response_preview: string;
  tokens_in: number; tokens_out: number; latency_ms: number;
  status: "ok" | "error"; error?: string; timestamp: string;
}

// All LLM calls go through the Next.js API route, not a separate Gateway service
const GATEWAY_PROXY = "/api/gateway";

async function gwFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${GATEWAY_PROXY}${path}`, {
    ...init,
    headers: { "Content-Type": "application/json", ...init?.headers },
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error((err as { error?: string }).error ?? `HTTP ${res.status}`);
  }
  return res.json() as T;
}

export async function fetchModels(): Promise<{ models: ModelInfo[]; default_model: string }> {
  return gwFetch("/models");
}

export async function addModel(model: Record<string, unknown>): Promise<{ added: ModelInfo }> {
  return gwFetch("/models", { method: "POST", body: JSON.stringify(model) });
}

export async function deleteModel(id: string): Promise<{ deleted: string }> {
  return gwFetch(`/models/${id}`, { method: "DELETE" });
}

export async function testModel(id: string): Promise<{
  status: string; model: string; latency_ms: number; error?: string;
}> {
  return gwFetch(`/models/${id}/test`, { method: "POST" });
}

export async function fetchStats(): Promise<GatewayStats> {
  return gwFetch("/stats");
}

export async function fetchLogs(project?: string, limit = 50): Promise<{ logs: CallLog[] }> {
  return gwFetch(`/logs?limit=${limit}${project ? `&project=${project}` : ""}`);
}

export async function chat(req: {
  messages: Array<{ role: string; content: string }>;
  model_id?: string; project?: string; system_prompt?: string;
  temperature?: number; max_tokens?: number;
}): Promise<{ content: string; model: string; tokens_used: number; latency_ms: number }> {
  return gwFetch("/chat", { method: "POST", body: JSON.stringify(req) });
}

// Check if any backend is serving the Gateway API
export async function checkGatewayHealth(): Promise<boolean> {
  try {
    const res = await fetch(`${GATEWAY_PROXY}/health`);
    return res.ok;
  } catch { return false; }
}
