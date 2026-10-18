import { useEffect, useState } from "react";
import { ProjectAgentPanel } from './ProjectAgentPanel';
import { DiagnosticAgentPanel } from './DiagnosticAgentPanel';
import {
  Activity,
  Cpu,
  Gauge,
  HardDrive,
  MemoryStick,
  ShieldCheck,
  Stethoscope,
  TerminalSquare,
} from "lucide-react";
import { useBackend } from "@/hooks/useBackend";
import { Button } from "@/components/ui/button";
import { Surface } from "@/components/ui/surface";
import { PageHeader } from "@/components/ui/page-header";
import { Badge } from "@/components/ui/badge";
import { StatusDot } from "@/components/ui/status-dot";
import { moduleName, moduleState } from "@/components/module-meta";
import { cn } from "@/lib/utils";

interface DiagToolResult {
  success?: boolean;
  output?: string;
  error?: string;
}

interface DiagResult {
  ai_diagnosis?: string;
  tools?: Record<string, DiagToolResult>;
}

export function DevOpsPage() {
  const backend = useBackend();
  const { metrics, loadMetrics, diagnose, modules } = backend;
  const [diagResult, setDiagResult] = useState<DiagResult | null>(null);
  const [diagnosing, setDiagnosing] = useState(false);

  useEffect(() => {
    loadMetrics();
    const t = setInterval(loadMetrics, 5000);
    return () => clearInterval(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleDiagnose = async () => {
    setDiagnosing(true);
    try {
      const r = (await diagnose("system check")) as DiagResult;
      setDiagResult(r);
    } finally {
      setDiagnosing(false);
    }
  };

  const healthPct = metrics
    ? Math.round(
        (metrics.cpu?.usage_percent ?? 0) * 0.4 +
        (metrics.memory?.usage_percent ?? 0) * 0.4 +
        (metrics.disk?.usage_percent ?? 0) * 0.2
      )
    : 0;
  const healthVariant = healthPct < 50 ? "ok" : healthPct < 80 ? "warn" : "err";

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="ns-page-wrap">
        <PageHeader
          icon={<Activity size={20} />}
          accent="var(--acc-devops)"
          title="DevOps"
          subtitle="系统健康中心 · CPU、内存、磁盘、服务与智能诊断"
          actions={
            <>
              <Badge variant={metrics ? healthVariant : "secondary"} className="text-xs">
                {metrics ? `综合负载 ${healthPct}%` : "等待指标"}
              </Badge>
              <Button onClick={handleDiagnose} disabled={diagnosing} className="gap-1.5">
                <Stethoscope size={14} /> {diagnosing ? "诊断中…" : "一键诊断"}
              </Button>
            </>
          }
        />

        {/* 系统指标 */}
        <ProjectAgentPanel />
        <DiagnosticAgentPanel />
        <div className="grid grid-cols-[repeat(auto-fit,minmax(180px,1fr))] gap-4">
          <MetricCard
            icon={<Cpu size={15} />}
            label="CPU"
            value={metrics ? (metrics.cpu?.usage_percent ?? 0).toFixed(1) + "%" : "—"}
            sub={metrics ? `${metrics.cpu?.cores ?? 0} 核` : "加载中"}
            pct={metrics ? metrics.cpu?.usage_percent ?? 0 : 0}
          />
          <MetricCard
            icon={<MemoryStick size={15} />}
            label="内存"
            value={metrics ? (metrics.memory?.usage_percent ?? 0).toFixed(1) + "%" : "—"}
            sub={metrics ? `${(metrics.memory?.used_gb ?? 0).toFixed(1)} / ${(metrics.memory?.total_gb ?? 0).toFixed(1)} GB` : "加载中"}
            pct={metrics ? metrics.memory?.usage_percent ?? 0 : 0}
          />
          <MetricCard
            icon={<HardDrive size={15} />}
            label="磁盘"
            value={metrics ? (metrics.disk?.usage_percent ?? 0).toFixed(1) + "%" : "—"}
            sub={metrics ? `剩余 ${Math.round(metrics.disk?.free_gb ?? 0)} GB` : "加载中"}
            pct={metrics ? metrics.disk?.usage_percent ?? 0 : 0}
          />
          <MetricCard
            icon={<Gauge size={15} />}
            label="进程数"
            value={metrics ? String(metrics.process_count ?? 0) : "—"}
            sub="运行中"
          />
        </div>

        {/* 智能体模块 */}
        <Surface className="px-6 py-5">
          <h2 className="text-[15px] font-semibold text-text-1">智能体模块</h2>
          <div className="mt-4 flex flex-col gap-1">
            {Object.entries(modules).map(([name, m]) => {
              const running = m.state === "running";
              const errored = m.state === "error";
              return (
                <div
                  key={name}
                  className="flex items-center justify-between rounded-none px-3 py-2.5 text-[13px] hover:bg-secondary/40"
                >
                  <span className="flex items-center gap-2.5 text-text-2">
                    <StatusDot status={running ? "ok" : errored ? "err" : "idle"} pulse={running} />
                    {moduleName(name)}
                  </span>
                  <span
                    className={cn(
                      "text-xs",
                      running ? "text-ok" : errored ? "text-err" : "text-text-3"
                    )}
                  >
                    {moduleState(m.state)}
                  </span>
                </div>
              );
            })}
            {Object.keys(modules).length === 0 && (
              <p className="px-3 py-4 text-xs text-text-3">正在加载模块状态…</p>
            )}
          </div>
        </Surface>

        {/* 诊断结果 */}
        {diagResult && (
          <Surface className="ns-enter px-6 py-5">
            <h2 className="text-[15px] font-semibold text-text-1">诊断结果</h2>
            <div className="mt-4 flex flex-col gap-3">
              {diagResult.ai_diagnosis && (
                <div className="rounded-none bg-brand-dim px-4 py-3.5 text-[13.5px] leading-relaxed text-text-1">
                  <div className="mb-1.5 flex items-center gap-2 text-xs text-brand-bright">
                    <ShieldCheck size={13} /> AI 诊断
                  </div>
                  {diagResult.ai_diagnosis}
                </div>
              )}
              {Object.entries(diagResult.tools || {}).map(([name, result]) => (
                <div key={name} className="rounded-none bg-secondary px-4 py-3">
                  <div className="mb-1.5 flex items-center gap-2 text-[13px] text-acc-devops">
                    <TerminalSquare size={13} /> {name}
                    <Badge variant={result.success ? "ok" : "err"} className="ml-auto text-xs">
                      {result.success ? "成功" : "失败"}
                    </Badge>
                  </div>
                  <pre className="max-h-[90px] overflow-auto font-mono text-xs whitespace-pre-wrap text-text-3">
                    {result.output ? String(result.output).slice(0, 400) : result.error || "无输出"}
                  </pre>
                </div>
              ))}
            </div>
          </Surface>
        )}
      </div>
    </div>
  );
}

function MetricCard({
  icon,
  label,
  value,
  sub,
  pct,
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
  sub: string;
  pct?: number;
}) {
  return (
    <Surface className="ns-lift px-5 py-5">
      <div className="flex items-center gap-2 text-acc-devops">
        {icon}
        <span className="text-xs tracking-[0.04em] text-text-2">{label}</span>
      </div>
      <div className="mt-3 text-[24px] font-semibold leading-tight text-text-1">{value}</div>
      <div className="mt-2 text-xs text-text-3">{sub}</div>
      {pct != null && (
        <div className="mt-3 h-1 overflow-hidden rounded-full bg-secondary">
          <div
            className="h-full rounded-full bg-foreground/40 transition-all duration-500"
            style={{ width: `${Math.max(0, Math.min(100, pct))}%` }}
          />
        </div>
      )}
    </Surface>
  );
}
