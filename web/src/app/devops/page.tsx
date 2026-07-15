"use client";

import { WorkspaceLayout } from "@/components/workspace/WorkspaceLayout";

/**
 * DevOps Workspace — 运维智能体
 *
 * 功能：系统指标采集 · 故障诊断 · 工具调度 · 日志分析 · 故障经验记忆
 * 后端：Go  :8740 · 内嵌 Nebula 记忆引擎
 */
export default function DevOpsPage() {
  return (
    <WorkspaceLayout title="DevOps Agent · 运维智能体" port={8740}>
      <div className="mb-4 rounded-xl border border-white/[0.06] bg-white/[0.02] p-4 backdrop-blur-sm">
        <div className="flex flex-wrap items-center gap-4 text-[12px] text-[#9BA1A6]">
          <span className="flex items-center gap-1.5">
            <span className="h-2 w-2 rounded-full bg-emerald-400" />
            DevOps v1.0 — Go · Metrics Collector · Fault Store · Tool Registry
          </span>
          <span>端口 :8740</span>
          <a href="http://localhost:8740/web/" target="_blank" rel="noreferrer"
             className="text-[#00A8FF] hover:underline">独立控制台 ↗</a>
        </div>
      </div>

      <div className="rounded-2xl border border-white/[0.06] overflow-hidden" style={{ height: "calc(100vh - 200px)" }}>
        <iframe
          src="http://localhost:8740/web/"
          style={{ width: "100%", height: "100%", border: "none" }}
          title="DevOps Agent Dashboard"
          sandbox="allow-scripts allow-same-origin allow-forms allow-popups"
        />
      </div>
    </WorkspaceLayout>
  );
}
