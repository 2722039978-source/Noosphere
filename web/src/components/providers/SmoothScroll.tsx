"use client";

import { useEffect, type ReactNode } from "react";
import Lenis from "lenis";
import { gsap } from "gsap";
import { ScrollTrigger } from "gsap/ScrollTrigger";

gsap.registerPlugin(ScrollTrigger);

/**
 * Lenis 平滑滚动 + GSAP ScrollTrigger 时钟同步。
 * Apple 官网式"重量感"滚动的物理基础：
 * 惯性由 Lenis 提供，滚动驱动动画统一走 ScrollTrigger。
 */
export function SmoothScroll({ children }: { children: ReactNode }) {
  useEffect(() => {
    // 尊重系统减弱动态效果设置
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;

    const lenis = new Lenis({
      duration: 1.15,
      easing: (t) => 1 - Math.pow(1 - t, 4), // easeOutQuart — 真实惯性感
      smoothWheel: true,
      touchMultiplier: 1.6,
    });

    lenis.on("scroll", ScrollTrigger.update);

    // 用 GSAP ticker 驱动 Lenis，保证两套动画共用一个 60FPS 时钟
    const tick = (time: number) => lenis.raf(time * 1000);
    gsap.ticker.add(tick);
    gsap.ticker.lagSmoothing(0);

    return () => {
      gsap.ticker.remove(tick);
      lenis.destroy();
    };
  }, []);

  return <>{children}</>;
}
