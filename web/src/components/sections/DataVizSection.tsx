"use client";

import { Wifi, WifiOff, Loader2, Radio, FlaskConical } from "lucide-react";
import { RadialGauge } from "@/components/ui/RadialGauge";
import { TechCurve } from "@/components/ui/TechCurve";
import { Reveal, RevealItem } from "@/components/ui/Reveal";
import { SectionHeading } from "@/components/ui/SectionHeading";
import { GAUGES } from "@/lib/data";
import { SERVICES } from "@/lib/services";
import { useServiceStatus } from "@/hooks/useServiceStatus";
import { useLiveMetrics } from "@/hooks/useLiveMetrics";

/**
 * HUD 数据可视化 —— 战术指挥中心仪表盘。
 * 连接本机三个 Noosphere 服务：
 *  - 服务状态灯（真实健康检查，图标 + 文字，非仅颜色）
 *  - DevOps 在线时，遥测仪表切换为真实 CPU / 内存 / 磁盘
 */
export function DataVizSection() {
  const { statuses } = useServiceStatus();
  const live = useLiveMetrics();

  const statusMeta = {
    online: { icon: Wifi, text: "ONLINE", cls: "text-status-ok" },
    offline: { icon: WifiOff, text: "OFFLINE", cls: "text-silver/40" },
    checking: { icon: Loader2, text: "SCAN", cls: "text-silver/60" },
  } as const;

  // 本机遥测：DevOps 在线 → 真实数据；离线 → 演示数据并明确标注
  const telemetry = live
    ? [
        { label: "处理器负载", en: "CPU LOAD", value: live.cpu, display: "" },
        { label: "内存占用", en: "MEMORY", value: live.memory, display: "" },
        { label: "磁盘占用", en: "DISK", value: live.disk, display: "" },
      ]
    : null;

  return (
    <section className="relative py-32 md:py-44">
      {/* 区块级网格底纹 */}
      <div className="tech-grid-bg pointer-events-none absolute inset-0 opacity-60" />

      <div className="relative mx-auto max-w-7xl px-6 lg:px-10">
        <SectionHeading
          index="SEC.02"
          label="LIVE TELEMETRY"
          title="每一项指标，都在运行中。"
          subtitle="下方面板直连本机 Noosphere 服务。启动 docker compose，仪表盘立即切换为真实遥测。"
        />

        <Reveal amount={0.15}>
          <RevealItem>
            <div className="glass-strong hud-corners overflow-hidden rounded-xl">
              {/* ── 终端标题栏 ── */}
              <div className="flex flex-col gap-3 border-b border-white/[0.06] px-6 py-4 md:flex-row md:items-center md:justify-between">
                <div className="flex items-center gap-3">
                  <span className="flex gap-1.5">
                    <i className="h-2.5 w-2.5 rounded-full bg-status-warn/60" />
                    <i className="h-2.5 w-2.5 rounded-full bg-status-ok/60" />
                    <i className="h-2.5 w-2.5 rounded-full bg-tech/60" />
                  </span>
                  <span className="hud-label">NOOSPHERE TELEMETRY CONSOLE</span>
                </div>

                {/* 服务状态 —— 图标 + 文字（不依赖颜色区分） */}
                <div className="flex flex-wrap items-center gap-4">
                  {SERVICES.map((svc) => {
                    const st = statuses[svc.id] ?? "checking";
                    const meta = statusMeta[st];
                    const Icon = meta.icon;
                    return (
                      <span
                        key={svc.id}
                        className={`flex items-center gap-1.5 font-mono text-[10px] tracking-[0.2em] ${meta.cls}`}
                        title={`${svc.name} · localhost:${svc.port}`}
                      >
                        <Icon size={11} className={st === "checking" ? "animate-spin" : ""} />
                        {svc.name.toUpperCase()}:{meta.text}
                      </span>
                    );
                  })}
                </div>
              </div>

              {/* ── 主面板 ── */}
              <div className="grid gap-10 p-8 md:p-12 lg:grid-cols-[1.05fr_1fr]">
                {/* 左：核心指标仪表阵列 */}
                <div>
                  <p className="hud-label mb-8 flex items-center gap-2">
                    <Radio size={11} />
                    CORE PERFORMANCE INDEX
                  </p>
                  <div className="grid grid-cols-2 gap-x-4 gap-y-10">
                    {GAUGES.map((g) => (
                      <RadialGauge key={g.en} {...g} />
                    ))}
                  </div>
                </div>

                {/* 右：吞吐曲线 + 本机遥测 */}
                <div className="flex flex-col justify-between gap-10">
                  <TechCurve title="AI 计算吞吐" unit="Kt/s" />

                  <div>
                    <p className="hud-label mb-6 flex items-center gap-2">
                      {telemetry ? (
                        <>
                          <Wifi size={11} /> LOCAL TELEMETRY · LIVE
                        </>
                      ) : (
                        <>
                          <FlaskConical size={11} /> LOCAL TELEMETRY · SIMULATED
                        </>
                      )}
                    </p>
                    <div className="grid grid-cols-3 gap-4">
                      {(
                        telemetry ?? [
                          { label: "处理器负载", en: "CPU LOAD", value: 34, display: "34.0%" },
                          { label: "内存占用", en: "MEMORY", value: 61, display: "61.0%" },
                          { label: "磁盘占用", en: "DISK", value: 47, display: "47.0%" },
                        ]
                      ).map((t) => (
                        // 本机遥测 = 系统健康 → 青绿；≥90% 视为异常，短暂转入危险红
                        <RadialGauge
                          key={t.en}
                          {...t}
                          size={96}
                          color={t.value >= 90 ? "#FF4D67" : "#2EE6A6"}
                        />
                      ))}
                    </div>
                  </div>
                </div>
              </div>

              {/* ── 底部状态条 ── */}
              <div className="flex items-center justify-between border-t border-white/[0.06] px-6 py-3 font-mono text-[10px] tracking-[0.25em] text-silver/40">
                <span>UPLINK: api.deepseek.com</span>
                <span className="hidden md:inline">PORTS 8730 · 8740 · 8765</span>
                <span className="flex items-center gap-2">
                  <i className="h-1 w-1 rounded-full bg-status-ok animate-blink" />
                  STREAMING
                </span>
              </div>
            </div>
          </RevealItem>
        </Reveal>
      </div>
    </section>
  );
}
