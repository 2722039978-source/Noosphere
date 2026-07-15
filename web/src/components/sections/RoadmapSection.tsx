"use client";

import { useState } from "react";
import { CheckCircle2, Loader2, Circle, Copy, Check, Github } from "lucide-react";
import { SectionHeading } from "@/components/ui/SectionHeading";
import { Reveal, RevealItem } from "@/components/ui/Reveal";
import { GlassCard } from "@/components/ui/GlassCard";
import { ROADMAP } from "@/lib/data";
import { cn } from "@/lib/utils";

const STATUS_META = {
  done: { icon: CheckCircle2, text: "已完成", cls: "text-status-ok" },
  active: { icon: Loader2, text: "进行中", cls: "text-status-warn", spin: true },
  planned: { icon: Circle, text: "规划中", cls: "text-status-info/80" },
} as const;

const INSTALL_CMD = "git clone https://github.com/2722039978-source/noosphere && cd noosphere && docker compose up -d";

/** 未来计划时间线 + 终端式 CTA */
export function RoadmapSection() {
  const [copied, setCopied] = useState(false);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(INSTALL_CMD);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      /* 剪贴板不可用时静默 */
    }
  };

  return (
    <section id="roadmap" className="relative mx-auto max-w-7xl px-6 py-32 lg:px-10 md:py-44">
      <SectionHeading
        index="SEC.05"
        label="ROADMAP"
        title="下一步，认知互联。"
        subtitle="从三个独立服务，到一个互通的认知网络。"
      />

      {/* ── 阶段卡片 ── */}
      <Reveal className="grid gap-5 md:grid-cols-2 xl:grid-cols-4" amount={0.15}>
        {ROADMAP.map((item) => {
          const meta = STATUS_META[item.status];
          const Icon = meta.icon;
          return (
            <RevealItem key={item.phase}>
              <GlassCard className="h-full p-7" maxTilt={5}>
                <div className="flex items-center justify-between">
                  <p className="hud-label">{item.phase}</p>
                  <span className={cn("flex items-center gap-1.5 font-mono text-[10px] tracking-[0.2em]", meta.cls)}>
                    <Icon size={12} className={"spin" in meta && meta.spin ? "animate-spin" : ""} />
                    {meta.text}
                  </span>
                </div>
                <h3 className="mt-4 text-lg font-light tracking-wide text-frost">{item.title}</h3>
                <p className="mt-3 text-[13px] font-light leading-relaxed text-silver">{item.desc}</p>
              </GlassCard>
            </RevealItem>
          );
        })}
      </Reveal>

      {/* ── 终端式 CTA ── */}
      <Reveal className="mt-28 md:mt-36" amount={0.3}>
        <RevealItem>
          <div className="mx-auto max-w-3xl text-center">
            <h2 className="text-4xl font-thin leading-tight tracking-wide text-frost md:text-6xl">
              把认知层，
              <br />
              交给 <span className="text-glow text-tech">Noosphere</span>。
            </h2>
            <p className="mx-auto mt-6 max-w-md text-base font-light leading-relaxed text-silver">
              一条命令，三个服务。开源、本地、即刻运行。
            </p>
          </div>
        </RevealItem>

        <RevealItem className="mx-auto mt-12 max-w-2xl">
          {/* 命令行块 */}
          <div className="glass-strong hud-corners overflow-hidden rounded-xl">
            <div className="flex items-center justify-between border-b border-white/[0.06] px-5 py-3">
              <span className="hud-label">DEPLOY // ONE COMMAND</span>
              <button
                onClick={copy}
                className="flex items-center gap-1.5 font-mono text-[10px] tracking-[0.2em] text-silver transition-colors hover:text-tech"
                aria-label="复制安装命令"
              >
                {copied ? <Check size={12} className="text-status-ok" /> : <Copy size={12} />}
                {copied ? "COPIED" : "COPY"}
              </button>
            </div>
            <div className="overflow-x-auto px-5 py-5">
              <code className="whitespace-nowrap font-mono text-[13px] leading-relaxed text-frost/90">
                <span className="mr-3 select-none text-tech">$</span>
                {INSTALL_CMD}
              </code>
            </div>
          </div>

          <div className="mt-8 flex justify-center">
            <a
              href="https://github.com/2722039978-source"
              target="_blank"
              rel="noreferrer"
              className="nav-scan flex items-center gap-2.5 rounded-md border border-white/[0.12] px-8 py-3.5 text-[13px] font-light tracking-[0.25em] text-frost transition-all duration-500 hover:border-tech/60 hover:text-tech hover:shadow-[0_0_24px_-10px_#00A8FF]"
            >
              <Github size={15} strokeWidth={1.5} />
              GITHUB 开源仓库
            </a>
          </div>
        </RevealItem>
      </Reveal>
    </section>
  );
}
