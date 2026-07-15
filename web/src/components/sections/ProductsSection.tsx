"use client";

import { useState } from "react";
import dynamic from "next/dynamic";
import { AnimatePresence, motion } from "framer-motion";
import { ArrowUpRight, CheckCircle2, Wifi, WifiOff } from "lucide-react";
import { SectionHeading } from "@/components/ui/SectionHeading";
import { Reveal, RevealItem } from "@/components/ui/Reveal";
import { PRODUCTS } from "@/lib/data";
import { SERVICES } from "@/lib/services";
import { useServiceStatus } from "@/hooks/useServiceStatus";
import { EASE_OUT } from "@/lib/motion";
import { cn } from "@/lib/utils";

const ProductStage = dynamic(() => import("@/components/three/ProductStage"), {
  ssr: false,
  loading: () => (
    <div className="flex h-full items-center justify-center">
      <span className="hud-label animate-blink">LOADING STAGE…</span>
    </div>
  ),
});

/**
 * 产品矩阵 —— Apple 发布会式 3D 展台。
 * 左侧展台可拖拽 / 缩放 / 动态灯光；右侧参数面板随切换淡入。
 * 每个产品显示真实端口与本机在线状态，控制台一键直达。
 */
export function ProductsSection() {
  const [active, setActive] = useState(0);
  const { statuses } = useServiceStatus();
  const product = PRODUCTS[active];
  const service = SERVICES.find((s) => s.id === product.id)!;
  const online = statuses[product.id] === "online";

  return (
    <section id="products" className="relative mx-auto max-w-7xl px-6 py-32 lg:px-10 md:py-44">
      <SectionHeading
        index="SEC.03"
        label="PRODUCT MATRIX"
        title="三件套，各司其职。"
        subtitle="结构、风格、经验 —— 三个维度的项目认知，三个可独立部署的服务。"
      />

      {/* ── 产品切换器 ── */}
      <Reveal>
        <RevealItem>
          <div className="mb-10 flex flex-wrap gap-2">
            {PRODUCTS.map((p, i) => (
              <button
                key={p.id}
                onClick={() => setActive(i)}
                className={cn(
                  "nav-scan rounded-md border px-6 py-2.5 font-mono text-[11px] tracking-[0.25em] transition-all duration-500",
                  i === active
                    ? "border-tech/60 bg-tech/10 text-tech shadow-[0_0_18px_-10px_#00A8FF]"
                    : "border-white/[0.08] text-frost/80 hover:border-white/20 hover:text-tech",
                )}
              >
                {p.name.toUpperCase()}
              </button>
            ))}
          </div>
        </RevealItem>

        <RevealItem>
          <div className="glass hud-corners grid overflow-hidden rounded-xl lg:grid-cols-[1.2fr_1fr]">
            {/* ── 3D 展台 ── */}
            <div className="relative h-[380px] md:h-[520px]">
              <div className="tech-grid-bg absolute inset-0 opacity-50" />
              <ProductStage variant={product.geometry} accent={product.accent} />
              {/* 展台操作提示 */}
              <div className="pointer-events-none absolute bottom-4 left-1/2 -translate-x-1/2">
                <span className="hud-label text-silver/35">DRAG TO ROTATE · SCROLL TO ZOOM</span>
              </div>
              {/* 编号水印 */}
              <span className="pointer-events-none absolute left-6 top-5 font-mono text-[64px] font-thin leading-none text-white/[0.04]">
                0{active + 1}
              </span>
            </div>

            {/* ── 参数面板 ── */}
            <AnimatePresence mode="wait">
              <motion.div
                key={product.id}
                initial={{ opacity: 0, x: 32, filter: "blur(8px)" }}
                animate={{ opacity: 1, x: 0, filter: "blur(0px)" }}
                exit={{ opacity: 0, x: -24, filter: "blur(6px)" }}
                transition={{ duration: 0.6, ease: EASE_OUT }}
                className="flex flex-col justify-between border-t border-white/[0.06] p-8 md:p-10 lg:border-l lg:border-t-0"
              >
                <div>
                  <div className="mb-2 flex items-center justify-between">
                    <p className="hud-label">{service.codename} MODULE</p>
                    {/* 真实在线状态 —— 图标 + 文字 */}
                    <span
                      className={cn(
                        "flex items-center gap-1.5 font-mono text-[10px] tracking-[0.2em]",
                        online ? "text-status-ok" : "text-silver/40",
                      )}
                    >
                      {online ? <Wifi size={11} /> : <WifiOff size={11} />}
                      {online ? "ONLINE" : "STANDBY"}
                    </span>
                  </div>

                  <h3 className="text-4xl font-thin tracking-wide text-frost md:text-5xl">
                    {product.name}
                  </h3>
                  <p className="mt-3 text-base font-light text-frost/85">{product.subtitle}</p>
                  <p className="mt-5 text-sm font-light leading-relaxed text-silver">
                    {product.desc}
                  </p>

                  <ul className="mt-7 space-y-2.5">
                    {product.features.map((f) => (
                      <li key={f} className="flex items-center gap-2.5 text-sm font-light text-silver">
                        <CheckCircle2 size={13} className="shrink-0 text-silver/60" strokeWidth={1.5} />
                        {f}
                      </li>
                    ))}
                  </ul>
                </div>

                <div className="mt-9 flex items-center justify-between border-t border-white/[0.06] pt-6">
                  <div className="font-mono text-[11px] leading-5 text-silver/60">
                    <p>
                      RUNTIME <span className="text-frost">{product.lang}</span>
                    </p>
                    <p>
                      PORT <span className="text-frost">localhost:{product.port}</span>
                    </p>
                  </div>
                  <a
                    href={service.console}
                    target="_blank"
                    rel="noreferrer"
                    className={cn(
                      "group flex items-center gap-2 rounded-md border px-5 py-2.5 font-mono text-[11px] tracking-[0.2em] transition-all duration-500",
                      online
                        ? "border-tech/50 bg-tech/10 text-tech hover:bg-tech/20 hover:shadow-[0_0_20px_-8px_#00A8FF]"
                        : "border-white/[0.1] text-silver hover:border-white/25 hover:text-frost",
                    )}
                  >
                    进入控制台
                    <ArrowUpRight
                      size={13}
                      className="transition-transform duration-500 group-hover:-translate-y-0.5 group-hover:translate-x-0.5"
                    />
                  </a>
                </div>
              </motion.div>
            </AnimatePresence>
          </div>
        </RevealItem>
      </Reveal>
    </section>
  );
}
