"use client";

import { useEffect, useRef, useState } from "react";
import { useInView } from "framer-motion";
import { formatNumber } from "@/lib/utils";

/**
 * 数字跳动 —— 进入视口后以 easeOutExpo 从 0 滚动至目标值。
 * rAF 驱动，帧间只更新文本节点。
 */
export function CountUp({
  value,
  suffix = "",
  digits = 0,
  duration = 1.8,
  className,
}: {
  value: number;
  suffix?: string;
  digits?: number;
  duration?: number;
  className?: string;
}) {
  const ref = useRef<HTMLSpanElement>(null);
  const inView = useInView(ref, { once: true, margin: "-60px" });
  const [display, setDisplay] = useState("0");

  useEffect(() => {
    if (!inView) return;
    let raf = 0;
    const t0 = performance.now();
    const total = duration * 1000;

    const frame = (now: number) => {
      const p = Math.min((now - t0) / total, 1);
      const eased = 1 - Math.pow(2, -10 * p); // easeOutExpo
      setDisplay(formatNumber(value * eased, digits));
      if (p < 1) raf = requestAnimationFrame(frame);
      else setDisplay(formatNumber(value, digits));
    };
    raf = requestAnimationFrame(frame);
    return () => cancelAnimationFrame(raf);
  }, [inView, value, digits, duration]);

  return (
    <span ref={ref} className={className}>
      {display}
      {suffix}
    </span>
  );
}
