"use client";

import { GlassCard } from "@/components/ui/GlassCard";
import { CountUp } from "@/components/ui/CountUp";
import { Reveal, RevealItem } from "@/components/ui/Reveal";
import { SectionHeading } from "@/components/ui/SectionHeading";
import { SYSTEM_MODULES } from "@/lib/data";

/** 微型雷达 —— conic 扫描扇面 + 同心圆刻度（青绿扫描波，中心保留品牌蓝点） */
function Radar() {
  return (
    <div className="relative h-12 w-12 shrink-0">
      <div className="absolute inset-0 rounded-full border border-white/15" />
      <div className="absolute inset-[30%] rounded-full border border-white/10" />
      <div
        className="absolute inset-0 rounded-full animate-[spin_4s_linear_infinite]"
        style={{
          background:
            "conic-gradient(from 0deg, rgba(46,230,166,0.32), rgba(46,230,166,0.05) 70deg, transparent 90deg)",
        }}
      />
      <div className="absolute left-1/2 top-1/2 h-1 w-1 -translate-x-1/2 -translate-y-1/2 rounded-full bg-tech shadow-[0_0_6px_rgba(0,168,255,0.6)]" />
    </div>
  );
}

/**
 * 智能系统 —— 四大核心模块。
 * 玻璃卡片 + 3D 倾斜 + 雷达扫描 + 数字跳动。
 */
export function SystemsSection() {
  return (
    <section id="systems" className="relative mx-auto max-w-7xl px-6 py-32 lg:px-10 md:py-44">
      <SectionHeading
        index="SEC.01"
        label="INTELLIGENT SYSTEMS"
        title="四个子系统，一个认知层。"
        subtitle="不是给 AI 更大的记忆，而是给它一个预消化过的思维模型。每个子系统解决一类「理解」问题。"
      />

      <Reveal className="grid gap-5 md:grid-cols-2" amount={0.15}>
        {SYSTEM_MODULES.map((mod) => (
          <RevealItem key={mod.codename}>
            <GlassCard className="h-full p-8 md:p-10">
              {/* 头部：代号 + 雷达 */}
              <div className="flex items-start justify-between">
                <div>
                  <p className="hud-label mb-3">{mod.codename}</p>
                  <h3 className="text-2xl font-extralight tracking-wide text-frost md:text-3xl">
                    {mod.name}
                  </h3>
                </div>
                <Radar />
              </div>

              <p className="mt-6 text-sm font-light leading-relaxed text-silver">
                {mod.desc}
              </p>

              {/* 核心指标 —— 数字跳动 */}
              <div className="mt-8 border-t border-white/[0.06] pt-6">
                <div className="flex items-end gap-2">
                  <CountUp
                    value={mod.stat.value}
                    suffix={mod.stat.suffix}
                    digits={mod.stat.digits ?? 0}
                    className="font-mono text-4xl font-extralight tabular-nums text-frost md:text-5xl"
                  />
                </div>
                <p className="hud-label mt-2 text-silver/50">{mod.stat.label}</p>
              </div>

              {/* 技术标签 */}
              <div className="mt-6 flex flex-wrap gap-2">
                {mod.tags.map((tag) => (
                  <span
                    key={tag}
                    className="rounded border border-white/[0.08] bg-white/[0.03] px-2.5 py-1 font-mono text-[10px] tracking-widest text-silver/70"
                  >
                    {tag}
                  </span>
                ))}
              </div>
            </GlassCard>
          </RevealItem>
        ))}
      </Reveal>
    </section>
  );
}
