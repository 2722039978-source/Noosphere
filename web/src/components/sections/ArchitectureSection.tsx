"use client";

import { useEffect, useRef } from "react";
import { gsap } from "gsap";
import { ScrollTrigger } from "gsap/ScrollTrigger";
import { SectionHeading } from "@/components/ui/SectionHeading";
import { ARCH_LAYERS } from "@/lib/data";

gsap.registerPlugin(ScrollTrigger);

/**
 * 技术架构 —— GSAP ScrollTrigger 驱动的分层视差。
 * 滚动时三层依次显影（scrub 跟手），中央数据流线随进度生长，
 * 层内编号以不同速率反向移动，形成 Apple 式空间纵深。
 */
export function ArchitectureSection() {
  const wrapRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;

    const ctx = gsap.context(() => {
      // 各层：上浮 + 模糊消散，scrub 绑定滚动进度
      gsap.utils.toArray<HTMLElement>(".arch-layer").forEach((layer) => {
        gsap.fromTo(
          layer,
          { y: 110, opacity: 0, scale: 0.96, filter: "blur(12px)" },
          {
            y: 0,
            opacity: 1,
            scale: 1,
            filter: "blur(0px)",
            ease: "power3.out",
            scrollTrigger: { trigger: layer, start: "top 82%", end: "top 48%", scrub: 1 },
          },
        );
        // 层内巨型编号 —— 更慢的反向视差，制造纵深
        const tier = layer.querySelector(".arch-tier");
        if (tier) {
          gsap.fromTo(
            tier,
            { yPercent: 26 },
            {
              yPercent: -26,
              ease: "none",
              scrollTrigger: { trigger: layer, start: "top bottom", end: "bottom top", scrub: true },
            },
          );
        }
      });

      // 中央数据流线生长
      gsap.fromTo(
        ".arch-line",
        { scaleY: 0 },
        {
          scaleY: 1,
          ease: "none",
          transformOrigin: "top center",
          scrollTrigger: {
            trigger: wrapRef.current,
            start: "top 55%",
            end: "bottom 75%",
            scrub: 1,
          },
        },
      );
    }, wrapRef);

    return () => ctx.revert();
  }, []);

  return (
    <section id="architecture" className="relative mx-auto max-w-7xl px-6 py-32 lg:px-10 md:py-44">
      <SectionHeading
        index="SEC.04"
        label="ARCHITECTURE"
        title="三层认知，逐级压缩。"
        subtitle="从 200K token 的原始代码，到 3K token 的结构化认知 —— 每一层只做一件事，并把它做到极致。"
      />

      <div ref={wrapRef} className="relative">
        {/* 中央数据流线 —— 银白流线，蓝色不再铺陈装饰 */}
        <div className="arch-line absolute left-6 top-0 h-full w-px bg-gradient-to-b from-frost/50 via-white/15 to-transparent md:left-1/2" />

        <div className="space-y-24 md:space-y-36">
          {ARCH_LAYERS.map((layer, i) => (
            <div
              key={layer.tier}
              className={`arch-layer relative flex ${i % 2 ? "md:justify-start" : "md:justify-end"}`}
            >
              {/* 节点 */}
              <span className="absolute left-6 top-3 h-2.5 w-2.5 -translate-x-1/2 rounded-full border border-frost/50 bg-void shadow-[0_0_10px_rgba(245,245,247,0.35)] md:left-1/2" />

              <div className="glass hud-corners relative ml-14 w-full max-w-lg rounded-xl p-8 md:ml-0 md:p-10">
                {/* 巨型编号 —— 视差层 */}
                <span className="arch-tier pointer-events-none absolute -top-9 right-6 select-none font-mono text-[88px] font-thin leading-none text-white/[0.05]">
                  {String(i + 1).padStart(2, "0")}
                </span>
                <p className="hud-label mb-3">{layer.tier}</p>
                <h3 className="text-2xl font-extralight tracking-wide text-frost md:text-3xl">
                  {layer.title}
                </h3>
                <p className="mt-4 text-sm font-light leading-relaxed text-silver">{layer.body}</p>
              </div>
            </div>
          ))}
        </div>
      </div>

      {/* 压缩效果注脚 */}
      <div className="mt-24 flex flex-col items-center gap-2 text-center">
        <p className="font-mono text-sm text-silver/60">
          <span className="text-frost">200,000</span> tokens
          <span className="mx-4 text-silver/50">→</span>
          <span className="text-glow text-tech">3,000</span> tokens
        </p>
        <p className="hud-label text-silver/40">COGNITIVE COMPRESSION PIPELINE</p>
      </div>
    </section>
  );
}
