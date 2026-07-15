"use client";

import { useRef } from "react";
import dynamic from "next/dynamic";
import { motion, useScroll, useTransform } from "framer-motion";
import { ChevronDown } from "lucide-react";
import { EASE_OUT, letter, stagger } from "@/lib/motion";
import { TICKER_ITEMS } from "@/lib/data";

// Three.js 场景仅客户端加载，SSR 输出纯黑底避免闪烁
const ParticleField = dynamic(() => import("./ParticleField"), {
  ssr: false,
  loading: () => <div className="absolute inset-0 bg-void" />,
});

const TITLE = "Beyond Intelligence";

/** 数据流跑马灯的能量色循环 —— 数据即能量，低透明度克制呈现 */
const TICKER_ACCENTS = [
  "text-tech/40",
  "text-status-info/40",
  "text-status-warn/40",
  "text-status-ok/40",
] as const;

/**
 * 首屏 —— 电影级开场时序：
 * 0.0s 背景展开 → 0.6s HUD 标签 → 1.0s 主标题逐字显影
 * → 2.0s 中文副标题 → 2.4s CTA → 2.6s 数据流跑马灯
 * （导航栏在 layout 中于 2.2s 落下，与此处时序咬合）
 */
export function Hero() {
  const ref = useRef<HTMLElement>(null);

  // 滚动视差：内容上移消隐，场景缓慢放大退后 —— Apple 式离场
  const { scrollYProgress } = useScroll({
    target: ref,
    offset: ["start start", "end start"],
  });
  const contentY = useTransform(scrollYProgress, [0, 1], [0, -180]);
  const contentOpacity = useTransform(scrollYProgress, [0, 0.65], [1, 0]);
  const sceneScale = useTransform(scrollYProgress, [0, 1], [1, 1.14]);
  const sceneOpacity = useTransform(scrollYProgress, [0, 0.9], [1, 0.15]);

  return (
    <section ref={ref} id="hero" className="relative h-[100svh] overflow-hidden">
      {/* ── 三维场景层 ── */}
      <motion.div
        style={{ scale: sceneScale, opacity: sceneOpacity, willChange: "transform" }}
        className="absolute inset-0"
      >
        <motion.div
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ duration: 2.4, ease: "easeOut" }}
          className="absolute inset-0"
        >
          <ParticleField />
        </motion.div>
      </motion.div>

      {/* ── 氛围层：网格 + 晕影 + 底部渐隐 ── */}
      <div className="tech-grid-bg pointer-events-none absolute inset-0" />
      <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(ellipse_100%_70%_at_50%_45%,transparent_40%,#050505_100%)]" />
      <div className="pointer-events-none absolute inset-x-0 bottom-0 h-40 bg-gradient-to-t from-void to-transparent" />

      {/* ── 内容层 ── */}
      <motion.div
        style={{ y: contentY, opacity: contentOpacity, willChange: "transform" }}
        className="relative z-10 flex h-full flex-col items-center justify-center px-6 text-center"
      >
        {/* HUD 战术标签 */}
        <motion.div
          initial={{ opacity: 0, y: 16 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ delay: 0.6, duration: 0.9, ease: EASE_OUT }}
          className="hud-label mb-8 flex items-center gap-3"
        >
          <span className="inline-block h-1 w-1 rounded-full bg-tech animate-blink" />
          PROJECT COGNITIVE INFRASTRUCTURE
          <span className="text-silver/50">//</span>
          <span className="text-silver/70">TERMINAL v0.1.0</span>
        </motion.div>

        {/* 主标题 —— 逐字显影 */}
        <motion.h1
          variants={stagger(0.045, 1.0)}
          initial="hidden"
          animate="visible"
          aria-label={TITLE}
          className="text-glow select-none text-[11vw] font-thin leading-none tracking-[0.04em] text-frost md:text-[9vw] lg:text-[7.5rem]"
        >
          {TITLE.split(" ").map((word, wi, words) => (
            <span key={wi} className="inline-block whitespace-nowrap" aria-hidden>
              {word.split("").map((ch, i) => (
                <motion.span key={i} variants={letter} className="inline-block">
                  {ch}
                </motion.span>
              ))}
              {wi < words.length - 1 && <span className="inline-block w-[0.32em]" />}
            </span>
          ))}
        </motion.h1>

        {/* 中文副标题 */}
        <motion.p
          initial={{ opacity: 0, y: 24, filter: "blur(10px)" }}
          animate={{ opacity: 1, y: 0, filter: "blur(0px)" }}
          transition={{ delay: 2.0, duration: 1.1, ease: EASE_OUT }}
          className="mt-8 text-lg font-thin tracking-[0.5em] text-silver md:text-2xl"
        >
          重新定义项目认知基础设施
        </motion.p>

        {/* CTA */}
        <motion.div
          initial={{ opacity: 0, y: 24 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ delay: 2.4, duration: 1.0, ease: EASE_OUT }}
          className="mt-14 flex flex-col items-center gap-4 sm:flex-row"
        >
          <a
            href="#systems"
            className="nav-scan hud-corners group rounded-md border border-tech/40 bg-tech/10 px-10 py-3.5 text-[13px] font-light tracking-[0.3em] text-tech backdrop-blur-md transition-all duration-500 hover:border-tech/80 hover:bg-tech/20 hover:shadow-[0_0_28px_-10px_#00A8FF]"
          >
            进入终端
          </a>
          <a
            href="#products"
            className="rounded-md px-10 py-3.5 text-[13px] font-light tracking-[0.3em] text-silver transition-colors duration-500 hover:text-frost"
          >
            探索产品矩阵 →
          </a>
        </motion.div>
      </motion.div>

      {/* ── 底部数据流跑马灯 ── */}
      <motion.div
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        transition={{ delay: 2.6, duration: 1.2 }}
        className="absolute inset-x-0 bottom-0 z-10"
      >
        <div className="flex items-center gap-6 overflow-hidden border-t border-white/[0.05] bg-void/40 py-3 backdrop-blur-sm">
          <div className="flex min-w-max animate-marquee gap-16">
            {[...TICKER_ITEMS, ...TICKER_ITEMS].map((item, i) => (
              <span key={i} className="font-mono text-[10px] tracking-[0.3em] text-silver/45">
                <span className={`mr-3 ${TICKER_ACCENTS[i % TICKER_ACCENTS.length]}`}>▸</span>
                {item}
              </span>
            ))}
          </div>
        </div>

        {/* 滚动指示 */}
        <div className="pointer-events-none absolute -top-20 left-1/2 flex -translate-x-1/2 flex-col items-center gap-2">
          <span className="hud-label text-silver/40">SCROLL</span>
          <div className="relative h-10 w-px overflow-hidden bg-white/10">
            {/* 扫描线 —— 青绿同步色 */}
            <div className="absolute h-4 w-px animate-scan bg-status-ok/70" />
          </div>
          <ChevronDown size={12} className="text-silver/50" strokeWidth={1.5} />
        </div>
      </motion.div>
    </section>
  );
}
