"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { Activity, Cpu, Zap, BarChart3, Layers, ArrowRight, CheckCircle2, XCircle, Loader2 } from "lucide-react";
import { useServiceStatus } from "@/hooks/useServiceStatus";
import { fetchStats, fetchModels, checkGatewayHealth, type GatewayStats, type ModelInfo } from "@/lib/gateway/types";

/** 平台总览面板 — 状态 / 模型 / Token / 快捷入口 */
export function DashboardOverview() {
  const { statuses, onlineCount } = useServiceStatus();
  const [gwOnline, setGwOnline] = useState(false);
  const [stats, setStats] = useState<GatewayStats | null>(null);
  const [models, setModels] = useState<ModelInfo[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    (async () => {
      const online = await checkGatewayHealth();
      setGwOnline(online);
      if (online) {
        const [s, m] = await Promise.all([fetchStats(), fetchModels()]);
        setStats(s); setModels(m.models ?? []);
      }
      setLoading(false);
    })();
    const t = setInterval(async () => {
      const online = await checkGatewayHealth();
      setGwOnline(online);
      if (online) {
        const [s, m] = await Promise.all([fetchStats(), fetchModels()]);
        setStats(s); setModels(m.models ?? []);
      }
    }, 15000);
    return () => clearInterval(t);
  }, []);

  const defaultModel = models.find(m => m.is_default) ?? models[0];

  return (
    <section id="dashboard" className="relative mx-auto max-w-7xl px-6 lg:px-8 py-16">
      {/* Section header */}
      <div className="mb-10">
        <p className="text-[11px] font-medium uppercase tracking-[0.3em] text-[#00A8FF]">PLATFORM OVERVIEW</p>
        <h2 className="mt-2 text-3xl font-light tracking-tight text-[#F5F5F7]">Noosphere Dashboard</h2>
        <p className="mt-2 text-[14px] text-[#9BA1A6] max-w-xl">平台状态、模型接入、Token 用量一目了然。</p>
      </div>

      {/* Stat cards */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
        {[
          { label: "服务在线", value: `${onlineCount}/4`, icon: Activity, color: "text-emerald-400" },
          { label: "已接入模型", value: String(models.filter(m => m.enabled).length), icon: Cpu, color: "text-[#00A8FF]" },
          { label: "总调用次数", value: stats?.total_calls?.toLocaleString() ?? "--", icon: Zap, color: "text-amber-400" },
          { label: "Token 用量", value: stats?.total_tokens?.toLocaleString() ?? "--", icon: BarChart3, color: "text-violet-400" },
        ].map(s => (
          <div key={s.label} className="rounded-xl border border-white/[0.06] bg-white/[0.02] p-5 backdrop-blur-sm">
            <div className="flex items-center justify-between mb-1">
              <span className="text-[11px] uppercase tracking-widest text-[#9BA1A6]">{s.label}</span>
              <s.icon size={16} className={s.color} />
            </div>
            <div className="text-2xl font-light text-[#F5F5F7]">{loading ? <Loader2 size={20} className="animate-spin text-[#9BA1A6]" /> : s.value}</div>
          </div>
        ))}
      </div>

      {/* Current model + quick entry */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Model status */}
        <div className="lg:col-span-1 rounded-2xl border border-white/[0.06] bg-white/[0.02] p-6 backdrop-blur-sm">
          <div className="flex items-center gap-2 mb-4">
            <Layers size={16} className="text-[#00A8FF]" />
            <span className="text-[12px] font-medium uppercase tracking-widest text-[#9BA1A6]">当前模型</span>
          </div>
          {defaultModel ? (
            <div>
              <div className="flex items-center gap-2">
                <span className="text-lg font-light text-[#F5F5F7]">{defaultModel.name}</span>
                <span className="rounded-full bg-emerald-500/10 px-2 py-0.5 text-[10px] text-emerald-400">
                  {defaultModel.provider}
                </span>
              </div>
              <div className="mt-1 text-[12px] text-[#9BA1A6]">{defaultModel.model_name}</div>
              <Link
                href="/settings/model"
                className="inline-flex items-center gap-1.5 mt-4 text-[12px] text-[#00A8FF] hover:text-[#33BAFF] transition-colors"
              >
                管理模型 <ArrowRight size={12} />
              </Link>
            </div>
          ) : (
            <div>
              <p className="text-[14px] text-[#9BA1A6]">尚未配置模型</p>
              <Link
                href="/settings/model"
                className="inline-flex items-center gap-1.5 mt-3 text-[12px] text-[#00A8FF] hover:text-[#33BAFF]"
              >
                前往配置 <ArrowRight size={12} />
              </Link>
            </div>
          )}
        </div>

        {/* Workspace cards */}
        <div className="lg:col-span-2 grid grid-cols-1 sm:grid-cols-3 gap-4">
          {[
            { name: "CodeLens", href: "/codelens", desc: "AI DevOps — 代码分析 · CI/CD · 自动排障 · 日志分析 · 服务监控", icon: "🔍", status: statuses.codelens },
            { name: "Nebula Agent", href: "/nebula", desc: "通用 AI Agent — 多步骤任务规划 · Tool Calling · Agent Workflow · 自动执行", icon: "☁️", status: statuses.nebula },
            { name: "DevOps Agent", href: "/devops", desc: "运维智能体 — 指标采集 · 故障诊断 · 工具调度 · 经验记忆", icon: "⚙️", status: statuses.devops },
          ].map(ws => (
            <Link
              key={ws.href}
              href={ws.href}
              className="group rounded-2xl border border-white/[0.06] bg-white/[0.02] p-6 backdrop-blur-sm hover:border-[#00A8FF]/30 hover:bg-white/[0.04] transition-all duration-300"
            >
              <div className="flex items-center justify-between mb-3">
                <span className="text-2xl">{ws.icon}</span>
                {ws.status === "online" ? (
                  <CheckCircle2 size={14} className="text-emerald-400" />
                ) : ws.status === "checking" ? (
                  <Loader2 size={14} className="animate-spin text-[#9BA1A6]" />
                ) : (
                  <XCircle size={14} className="text-red-400" />
                )}
              </div>
              <h3 className="text-[15px] font-medium text-[#F5F5F7] group-hover:text-[#00A8FF] transition-colors">{ws.name}</h3>
              <p className="mt-1.5 text-[12px] text-[#9BA1A6] leading-relaxed">{ws.desc}</p>
              <div className="flex items-center gap-1 mt-3 text-[11px] text-[#00A8FF] opacity-0 group-hover:opacity-100 transition-opacity">
                进入工作区 <ArrowRight size={11} />
              </div>
            </Link>
          ))}
        </div>
      </div>
    </section>
  );
}
