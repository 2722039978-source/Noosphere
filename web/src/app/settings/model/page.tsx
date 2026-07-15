"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import {
  ArrowLeft, Zap, Trash2, Upload, Globe, BarChart3, Plug2,
  CheckCircle2, XCircle, Clock, Loader2, Settings, Activity, Layers,
} from "lucide-react";
import {
  fetchModels, addModel, deleteModel, testModel,
  fetchStats, fetchLogs, checkGatewayHealth,
} from "@/lib/gateway/types";
import type { ModelInfo, GatewayStats, CallLog } from "@/lib/gateway/types";

const PROVIDERS = [
  { key: "deepseek", label: "DeepSeek", icon: "🧠", baseURL: "https://api.deepseek.com", model: "deepseek-v4-pro" },
  { key: "openai", label: "OpenAI", icon: "⚡", baseURL: "https://api.openai.com/v1", model: "gpt-4o" },
  { key: "anthropic", label: "Claude", icon: "🔮", baseURL: "https://api.anthropic.com", model: "claude-sonnet-5" },
  { key: "gemini", label: "Gemini", icon: "💎", baseURL: "https://generativelanguage.googleapis.com", model: "gemini-2.5-pro" },
  { key: "ollama", label: "Ollama", icon: "🦙", baseURL: "http://localhost:11434/v1", model: "qwen3:latest" },
  { key: "openai_compat", label: "自定义 API", icon: "🔌", baseURL: "", model: "" },
] as const;

const S = {
  page: "min-h-screen bg-[#050505] text-[#F5F5F7] font-sans",
  nav: "sticky top-0 z-50 border-b border-white/[0.06] bg-[#050505]/85 backdrop-blur-2xl",
  navInner: "mx-auto flex h-12 max-w-7xl items-center justify-between px-6 lg:px-8",
  backLink: "flex items-center gap-2 text-[11px] text-[#9BA1A6] hover:text-[#F5F5F7] transition-colors",
  main: "mx-auto max-w-7xl px-6 lg:px-8 py-10",
  header: "mb-8",
  title: "text-3xl font-light tracking-tight",
  subtitle: "mt-2 text-[14px] text-[#9BA1A6] max-w-xl",
  card: "rounded-2xl border border-white/[0.06] bg-white/[0.02] p-6 backdrop-blur-sm",
  cardHover: "hover:border-[#00A8FF]/20 transition-all",
  statGrid: "grid grid-cols-2 lg:grid-cols-4 gap-4 mb-8",
  stat: "rounded-xl border border-white/[0.05] bg-white/[0.015] p-5",
  statNum: "text-2xl font-light tracking-tight text-[#00A8FF]",
  statLabel: "mt-1 text-[11px] text-[#9BA1A6] uppercase tracking-widest",
  tab: "flex gap-1 rounded-xl bg-white/[0.03] p-1 w-fit mb-6",
  tabBtn: "px-4 py-1.5 text-[12px] font-medium rounded-lg transition-colors",
  tabActive: "bg-white/[0.06] text-[#F5F5F7]",
  tabInactive: "text-[#9BA1A6] hover:text-[#F5F5F7]",
  btn: "inline-flex items-center gap-2 rounded-lg px-4 py-2.5 text-[13px] font-medium transition-all",
  btnPrimary: "bg-[#00A8FF] text-black hover:bg-[#33BAFF]",
  btnGhost: "border border-white/[0.08] text-[#9BA1A6] hover:text-white hover:border-white/[0.15] rounded-lg px-3 py-1.5 text-[12px]",
  btnDanger: "border border-red-500/20 text-red-400 hover:bg-red-500/10 rounded-lg px-3 py-1.5 text-[12px]",
  input: "w-full rounded-lg border border-white/[0.08] bg-white/[0.03] px-3.5 py-2.5 text-[14px] text-[#F5F5F7] placeholder:text-[#9BA1A6]/40 outline-none focus:border-[#00A8FF]/40 transition-colors",
  select: "w-full rounded-lg border border-white/[0.08] bg-[#0a0a0a] px-3.5 py-2.5 text-[14px] text-[#F5F5F7] outline-none focus:border-[#00A8FF]/40 transition-colors",
  badge: "inline-flex items-center gap-1 rounded-full px-2.5 py-0.5 text-[10px] font-medium",
  badgeOk: "bg-emerald-500/10 text-emerald-400",
  badgeErr: "bg-red-500/10 text-red-400",
  modal: "fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm",
  modalInner: "w-full max-w-lg rounded-2xl border border-white/[0.08] bg-[#0d0d0d] p-6 shadow-2xl",
  logItem: "flex items-start gap-4 border-b border-white/[0.04] py-3 text-[13px]",
  logProject: "inline-block rounded bg-[#00A8FF]/10 px-2 py-0.5 text-[10px] text-[#00A8FF] font-medium",
} as const;

export default function SettingsModelPage() {
  const [models, setModels] = useState<ModelInfo[]>([]);
  const [stats, setStats] = useState<GatewayStats | null>(null);
  const [logs, setLogs] = useState<CallLog[]>([]);
  const [gwOnline, setGwOnline] = useState(false);
  const [tab, setTab] = useState<"models" | "logs" | "docs">("models");
  const [testing, setTesting] = useState<string | null>(null);
  const [showAdd, setShowAdd] = useState(false);
  const [addForm, setAddForm] = useState({
    id: "", name: "", provider: "deepseek", base_url: "https://api.deepseek.com",
    api_key: "", model_name: "deepseek-v4-pro", is_default: false,
  });

  const loadAll = async () => {
    const online = await checkGatewayHealth(); setGwOnline(online);
    if (!online) return;
    try {
      const [mRes, sRes, lRes] = await Promise.all([fetchModels(), fetchStats(), fetchLogs()]);
      setModels(mRes.models ?? []); setStats(sRes); setLogs(lRes.logs ?? []);
    } catch { /* silent */ }
  };

  useEffect(() => { loadAll(); const t = setInterval(loadAll, 10000); return () => clearInterval(t); }, []);

  const handleAdd = async () => {
    if (!addForm.name || !addForm.api_key) return;
    await addModel(addForm as Record<string, unknown>);
    setShowAdd(false);
    setAddForm({ id: "", name: "", provider: "deepseek", base_url: "https://api.deepseek.com", api_key: "", model_name: "deepseek-v4-pro", is_default: false });
    loadAll();
  };

  const handleDelete = async (id: string) => { await deleteModel(id); loadAll(); };
  const handleTest = async (id: string) => { setTesting(id); await testModel(id); setTesting(null); loadAll(); };

  return (
    <div className={S.page}>
      {/* Sub-nav */}
      <nav className={S.nav}>
        <div className={S.navInner}>
          <Link href="/" className={S.backLink}><ArrowLeft size={14} /> NOOSPHERE</Link>
          <div className="flex items-center gap-2 text-[12px] text-[#9BA1A6]">
            <Settings size={14} /> 系统设置 · AI Gateway
            <span className="ml-2 flex items-center gap-1 text-[10px]">
              <span className={`h-1.5 w-1.5 rounded-full ${gwOnline ? "bg-emerald-400" : "bg-red-400"}`} />
              {gwOnline ? "已配置" : "未配置"}
            </span>
          </div>
        </div>
      </nav>

      <main className={S.main}>
        <div className={S.header}>
          <h1 className={S.title}>AI Gateway <span className="text-[11px] text-[#9BA1A6] ml-2">模型管理 · 系统设置</span></h1>
          <p className={S.subtitle}>统一管理所有 Workspace 使用的大语言模型。添加 API Key 后，CodeLens / Nebula / DevOps 自动通过 Gateway 调用。</p>
        </div>

        <div className={S.statGrid}>
          {[
            [stats?.total_models ?? models.length, "已接入模型"],
            [stats?.total_calls ?? 0, "总调用次数"],
            [stats?.total_tokens?.toLocaleString() ?? 0, "Token 用量"],
            [models.filter(m => m.enabled).length, "已启用"],
          ].map(([v, l]) => (
            <div key={l as string} className={S.stat}><div className={S.statNum}>{String(v)}</div><div className={S.statLabel}>{l as string}</div></div>
          ))}
        </div>

        <div className="flex items-center justify-between mb-4">
          <div className={S.tab}>
            {(["models", "logs", "docs"] as const).map(t => (
              <button key={t} className={tab === t ? `${S.tabBtn} ${S.tabActive}` : `${S.tabBtn} ${S.tabInactive}`}
                onClick={() => setTab(t)}>
                {t === "models" ? "🔑 模型列表" : t === "logs" ? "📋 调用日志" : "📄 API 文档导入"}
              </button>
            ))}
          </div>
          <button className={S.btn + " " + S.btnPrimary} onClick={() => setShowAdd(true)}>+ 添加模型</button>
        </div>

        {tab === "models" && (
          <div className="space-y-3">
            {models.length === 0 && <div className={S.card}><p className="text-center text-[#9BA1A6] py-10">尚未配置模型。点击「+ 添加模型」接入你的 LLM API。</p></div>}
            {models.map(m => (
              <div key={m.id} className={`${S.card} flex items-center gap-5 !p-5`}>
                <span className="text-2xl">{PROVIDERS.find(p => p.key === m.provider)?.icon ?? "🤖"}</span>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="text-[15px] font-medium">{m.name}</span>
                    {m.is_default && <span className="rounded bg-[#00A8FF]/10 px-2 py-0.5 text-[10px] text-[#00A8FF]">默认</span>}
                    <span className={m.status === "ok" ? S.badge + " " + S.badgeOk : S.badge + " " + S.badgeErr}>
                      {m.status === "ok" ? <CheckCircle2 size={11} /> : <XCircle size={11} />}
                      {m.status === "ok" ? "OK" : "--"}
                    </span>
                  </div>
                  <div className="mt-1 text-[11px] text-[#9BA1A6]">{m.provider} · {m.model_name}</div>
                </div>
                <button className={S.btnGhost} onClick={() => handleTest(m.id)} disabled={testing === m.id}>
                  {testing === m.id ? <Loader2 size={13} className="animate-spin" /> : <Zap size={13} />} 测试
                </button>
                <button className={S.btnDanger} onClick={() => handleDelete(m.id)}><Trash2 size={13} /></button>
              </div>
            ))}
          </div>
        )}

        {tab === "logs" && (
          <div className={S.card}>
            <h3 className="text-[12px] text-[#9BA1A6] uppercase tracking-widest mb-3">最近调用</h3>
            {logs.length === 0 && <p className="text-center text-[#9BA1A6] py-8">暂无调用记录</p>}
            {logs.map(l => (
              <div key={l.id} className={S.logItem}>
                <span className={l.status === "ok" ? "text-emerald-400" : "text-red-400"}>
                  {l.status === "ok" ? <CheckCircle2 size={14} /> : <XCircle size={14} />}
                </span>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className={S.logProject}>{l.project || "web"}</span>
                    <span className="text-[11px] text-[#9BA1A6]">{l.provider} · {l.model_id}</span>
                    <span className="text-[11px] text-[#9BA1A6] ml-auto">{l.latency_ms.toFixed(0)}ms</span>
                  </div>
                  <div className="mt-0.5 text-[12px] text-[#F5F5F7]/70 truncate">{l.request_preview}</div>
                </div>
              </div>
            ))}
          </div>
        )}

        {tab === "docs" && (
          <div className={S.card}>
            <h3 className="text-[12px] text-[#9BA1A6] uppercase tracking-widest mb-3">
              <Upload size={14} className="inline mr-2" />导入 API 文档
            </h3>
            <p className="text-[13px] text-[#9BA1A6] mb-4">上传 Swagger JSON / OpenAPI YAML / Markdown 接口文档，自动解析接口地址和参数结构。</p>
            <textarea className={`${S.input} !h-28 resize-none font-mono text-[12px]`} id="docContent" placeholder="粘贴 JSON 或 Markdown 文档..." />
            <div className="flex gap-2 mt-3">
              <select className={S.select} id="docFormat"><option value="openapi">OpenAPI / Swagger</option><option value="markdown">Markdown</option></select>
              <button className={S.btn + " " + S.btnPrimary} onClick={async () => {
                const c = (document.getElementById("docContent") as HTMLTextAreaElement)?.value;
                const f = (document.getElementById("docFormat") as HTMLSelectElement)?.value;
                if (!c) return;
                await fetch("/api/gateway/docs/import", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ content: c, format: f }) });
                loadAll();
              }}><Upload size={14} /> 导入</button>
            </div>
          </div>
        )}
      </main>

      {showAdd && (
        <div className={S.modal} onClick={e => { if (e.target === e.currentTarget) setShowAdd(false); }}>
          <div className={S.modalInner}>
            <h2 className="text-lg font-light mb-5">接入新模型</h2>
            <div className="space-y-4">
              <div className="grid grid-cols-2 gap-3">
                <div><label className="text-[11px] text-[#9BA1A6] uppercase tracking-widest">名称</label><input className={S.input} placeholder="My Model" value={addForm.name} onChange={e => setAddForm({ ...addForm, name: e.target.value, id: e.target.value.toLowerCase().replace(/\s+/g, "-") })} /></div>
                <div><label className="text-[11px] text-[#9BA1A6] uppercase tracking-widest">提供商</label><select className={S.select} value={addForm.provider} onChange={e => {
                  const p = PROVIDERS.find(p => p.key === e.target.value);
                  setAddForm({ ...addForm, provider: e.target.value, base_url: p?.baseURL ?? "", model_name: p?.model ?? "" });
                }}>{PROVIDERS.map(p => <option key={p.key} value={p.key}>{p.icon} {p.label}</option>)}</select></div>
              </div>
              <div><label className="text-[11px] text-[#9BA1A6] uppercase tracking-widest">API Key</label><input className={S.input} type="password" placeholder="sk-..." value={addForm.api_key} onChange={e => setAddForm({ ...addForm, api_key: e.target.value })} /></div>
              <div className="grid grid-cols-2 gap-3">
                <div><label className="text-[11px] text-[#9BA1A6] uppercase tracking-widest">Base URL</label><input className={S.input} value={addForm.base_url} onChange={e => setAddForm({ ...addForm, base_url: e.target.value })} /></div>
                <div><label className="text-[11px] text-[#9BA1A6] uppercase tracking-widest">Model Name</label><input className={S.input} value={addForm.model_name} onChange={e => setAddForm({ ...addForm, model_name: e.target.value })} /></div>
              </div>
              <label className="flex items-center gap-2 text-[12px] text-[#9BA1A6] cursor-pointer"><input type="checkbox" checked={addForm.is_default} onChange={e => setAddForm({ ...addForm, is_default: e.target.checked })} />设为默认模型</label>
            </div>
            <div className="flex justify-end gap-3 mt-6">
              <button className={S.btnGhost} onClick={() => setShowAdd(false)}>取消</button>
              <button className={S.btn + " " + S.btnPrimary} onClick={handleAdd}>添加</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
