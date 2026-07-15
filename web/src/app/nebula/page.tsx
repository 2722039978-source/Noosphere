"use client";

import { WorkspaceLayout } from "@/components/workspace/WorkspaceLayout";

/**
 * Nebula Agent Workspace — 通用 AI Agent
 *
 * 功能：多步骤任务规划 · Tool Calling · Agent Workflow · 自动任务执行 · 多模型协作
 * 后端：Go  :8730 · 内嵌 LSM-Tree 记忆引擎 + HNSW 向量检索
 */
export default function NebulaPage() {
  return (
    <WorkspaceLayout title="Nebula Agent · 通用 AI Agent" port={8730}>
      <div className="mb-4 rounded-xl border border-white/[0.06] bg-white/[0.02] p-4 backdrop-blur-sm">
        <div className="flex flex-wrap items-center gap-4 text-[12px] text-[#9BA1A6]">
          <span className="flex items-center gap-1.5">
            <span className="h-2 w-2 rounded-full bg-violet-400" />
            Nebula v1.0 — Go · LSM-Tree · HNSW · RRF Hybrid Retrieval
          </span>
          <span>端口 :8730</span>
          <a href="http://localhost:8730" target="_blank" rel="noreferrer"
             className="text-[#00A8FF] hover:underline">独立控制台 ↗</a>
        </div>
      </div>

      <div className="rounded-2xl border border-white/[0.06] overflow-hidden" style={{ height: "calc(100vh - 200px)" }}>
        <iframe
          src="http://localhost:8730"
          style={{ width: "100%", height: "100%", border: "none" }}
          title="Nebula Agent Dashboard"
          sandbox="allow-scripts allow-same-origin allow-forms allow-popups"
        />
      </div>
    </WorkspaceLayout>
  );
}
