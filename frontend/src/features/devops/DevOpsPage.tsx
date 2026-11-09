import { useCallback, useEffect, useRef, useState } from "react";
import { DiagnosticAgentPanel } from './DiagnosticAgentPanel';
import {
  Activity,
  Cpu,
  Gauge,
  HardDrive,
  MemoryStick,
  Pause,
  Play,
  RefreshCw,
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
import { api, errMessage, type SystemMetrics } from '@/lib/api';
import './devops-workspace.css';

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
  const { diagnose, modules } = backend;
  const [metrics, setMetrics] = useState<SystemMetrics | null>(() => backend.metrics);
  const [automatic, setAutomatic] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [updatedAt, setUpdatedAt] = useState<string | null>(() => backend.metrics?.timestamp || null);
  const [metricsError, setMetricsError] = useState('');
  const [diagResult, setDiagResult] = useState<DiagResult | null>(null);
  const [diagnosing, setDiagnosing] = useState(false);
  const [diagnosisError, setDiagnosisError] = useState('');
  const alive = useRef(true);
  const inFlight = useRef(false);
  const requestGeneration = useRef(0);
  const automaticRef = useRef(true);

  useEffect(() => {
    alive.current = true;
    return () => { alive.current = false; requestGeneration.current++; };
  }, []);

  const refreshMetrics = useCallback(async () => {
    if (inFlight.current) return;
    const generation = requestGeneration.current;
    inFlight.current = true; setRefreshing(true);
    try {
      const sample = await api.metrics();
      if (!alive.current || generation !== requestGeneration.current) return;
      setMetrics(sample); setUpdatedAt(new Date().toISOString()); setMetricsError('');
    } catch (error) {
      if (alive.current && generation === requestGeneration.current) setMetricsError(errMessage(error));
    } finally {
      inFlight.current = false;
      if (alive.current) setRefreshing(false);
    }
  }, []);

  useEffect(() => {
    if (!automatic) return;
    const initial = window.setTimeout(() => { void refreshMetrics(); }, 0);
    const timer = window.setInterval(() => { if (automaticRef.current && !document.hidden) void refreshMetrics(); }, 5000);
    return () => { window.clearTimeout(initial); window.clearInterval(timer); };
  }, [automatic, refreshMetrics]);

  const toggleSampling = () => {
    const next = !automaticRef.current;
    automaticRef.current = next;
    requestGeneration.current++;
    setAutomatic(next);
  };

  const handleDiagnose = async () => {
    if (diagnosing) return;
    setDiagnosing(true); setDiagnosisError('');
    try {
      const r = (await diagnose("system check")) as DiagResult;
      if (alive.current) setDiagResult(r);
    } catch (error) {
      if (alive.current) setDiagnosisError(errMessage(error));
    } finally {
      if (alive.current) setDiagnosing(false);
    }
  };

  const completeUsage = [metrics?.cpu?.usage_percent, metrics?.memory?.usage_percent, metrics?.disk?.usage_percent].every(isMetric);
  const healthPct = metrics && completeUsage
    ? Math.round(
        metrics.cpu.usage_percent * 0.4 +
        metrics.memory.usage_percent * 0.4 +
        metrics.disk.usage_percent * 0.2
      )
    : null;
  const healthVariant = healthPct == null ? 'secondary' : healthPct < 50 ? "ok" : healthPct < 80 ? "warn" : "err";
  const displayTime = updatedAt && !Number.isNaN(Date.parse(updatedAt)) ? new Date(updatedAt).toLocaleTimeString([], { hour12: false }) : null;

  return (
    <div className="ns-devops-scroll" data-workspace-page="devops">
      <div className="ns-page-wrap ns-devops-page">
        <PageHeader
          icon={<Activity size={20} />}
          accent="var(--acc-devops)"
          title="DevOps"
          subtitle="系统健康中心 · CPU、内存、磁盘、服务与智能诊断"
          actions={
            <>
              <Badge variant={healthVariant} className="text-xs">
                {healthPct != null ? `综合负载 ${healthPct}%` : metrics ? '指标未完整' : "等待指标"}
              </Badge>
              <Button onClick={handleDiagnose} disabled={diagnosing} className="gap-1.5">
                <Stethoscope size={14} /> {diagnosing ? "诊断中…" : "一键诊断"}
              </Button>
            </>
          }
        />

        {/* 系统指标 */}
        <DiagnosticAgentPanel />
        <section className="ns-devops-monitor" aria-label="系统资源采样">
          <div className="ns-sampling-toolbar">
            <div className="ns-sampling-summary"><h2>系统资源</h2><span data-testid="metrics-sampling-state" data-paused={!automatic}>{automatic ? '自动采样 · 每 5 秒' : '自动采样已暂停'}</span><span data-testid="metrics-updated-at">{displayTime ? <>更新于 <time dateTime={updatedAt!}>{displayTime}</time></> : '尚未取得采样'}</span></div>
            <div className="ns-sampling-actions"><Button variant="outline" size="sm" aria-label={automatic ? '暂停自动采样' : '恢复自动采样'} onClick={toggleSampling}>{automatic ? <Pause size={13} /> : <Play size={13} />}{automatic ? '暂停采样' : '恢复采样'}</Button><Button variant="outline" size="sm" aria-label="手动刷新系统指标" disabled={refreshing} onClick={() => void refreshMetrics()}><RefreshCw size={13} />{refreshing ? '刷新中…' : '手动刷新'}</Button></div>
          </div>
          {!automatic && <p className="ns-sampling-note">已停止本页自动采集，当前保留上次样本；可手动刷新。服务健康检查仍继续。</p>}
          {metricsError && <p role="alert" className="ns-form-error">最近一次采样失败{metrics ? '，当前保留上次样本' : ''}：{metricsError}</p>}
        <div className="ns-devops-metrics">
          <MetricCard
            icon={<Cpu size={15} />}
            label="CPU"
            value={percent(metrics?.cpu?.usage_percent)}
            sub={isMetric(metrics?.cpu?.cores) ? `${metrics.cpu.cores} 核` : '核心数未提供'}
            pct={metrics?.cpu?.usage_percent}
          />
          <MetricCard
            icon={<MemoryStick size={15} />}
            label="内存"
            value={percent(metrics?.memory?.usage_percent)}
            sub={isMetric(metrics?.memory?.used_gb) && isMetric(metrics?.memory?.total_gb) ? `${metrics.memory.used_gb.toFixed(1)} / ${metrics.memory.total_gb.toFixed(1)} GB` : '内存容量未提供'}
            pct={metrics?.memory?.usage_percent}
          />
          <MetricCard
            icon={<HardDrive size={15} />}
            label="磁盘"
            value={percent(metrics?.disk?.usage_percent)}
            sub={isMetric(metrics?.disk?.free_gb) ? `剩余 ${Math.round(metrics.disk.free_gb)} GB` : '磁盘容量未提供'}
            pct={metrics?.disk?.usage_percent}
          />
          <MetricCard
            icon={<Gauge size={15} />}
            label="进程数"
            value={isMetric(metrics?.process_count) ? String(metrics.process_count) : "—"}
            sub={isMetric(metrics?.process_count) ? '运行中' : '进程数未提供'}
          />
        </div>
        </section>

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
        {diagnosisError && <p role="alert" className="ns-form-error">{diagnosisError}</p>}
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
    <Surface className="ns-metric-card px-5 py-5">
      <div className="flex items-center gap-2 text-acc-devops">
        {icon}
        <span className="text-xs tracking-[0.04em] text-text-2">{label}</span>
      </div>
      <div className="mt-3 text-[24px] font-semibold leading-tight text-text-1">{value}</div>
      <div className="mt-2 text-xs text-text-3">{sub}</div>
      {isMetric(pct) && <progress className="ns-metric-progress" aria-label={`${label}使用率`} max={100} value={Math.max(0, Math.min(100, pct))} />}
    </Surface>
  );
}

function isMetric(value: unknown): value is number { return typeof value === 'number' && Number.isFinite(value) && value >= 0; }
function percent(value: unknown) { return isMetric(value) ? `${value.toFixed(1)}%` : '—'; }
