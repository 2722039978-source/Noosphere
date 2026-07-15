"use client";

import { WorkspaceLayout } from "@/components/workspace/WorkspaceLayout";

/**
 * CodeLens Workspace — AI DevOps Agent
 *
 * 功能：代码分析 / CI/CD 辅助 / 自动排障 / 日志分析 / 服务监控 / 自动化运维建议
 * 后端：Python FastAPI  :8765
 *
 * 内嵌原有 CodeLens 仪表盘以保持完整功能和 UI。
 */
export default function CodeLensPage() {
  return (
    <WorkspaceLayout title="CodeLens · AI DevOps" port={8765}>
      {/* Quick info bar */}
      <div className="mb-4 rounded-xl border border-white/[0.06] bg-white/[0.02] p-4 backdrop-blur-sm">
        <div className="flex flex-wrap items-center gap-4 text-[12px] text-[#9BA1A6]">
          <span className="flex items-center gap-1.5">
            <span className="h-2 w-2 rounded-full bg-[#00A8FF]" />
            CodeLens v1.0 — Python · Tree-sitter · NetworkX · ChromaDB
          </span>
          <span>端口 :8765</span>
          <a href="http://localhost:8765/docs" target="_blank" rel="noreferrer"
             className="text-[#00A8FF] hover:underline">API 文档 ↗</a>
          <a href="http://localhost:8765" target="_blank" rel="noreferrer"
             className="text-[#00A8FF] hover:underline">独立控制台 ↗</a>
        </div>
      </div>

      {/* Embedded original dashboard */}
      <div className="rounded-2xl border border-white/[0.06] overflow-hidden" style={{ height: "calc(100vh - 200px)" }}>
        <iframe
          src="http://localhost:8765"
          style={{ width: "100%", height: "100%", border: "none" }}
          title="CodeLens Dashboard"
          sandbox="allow-scripts allow-same-origin allow-forms allow-popups"
        />
      </div>
    </WorkspaceLayout>
  );
}
