"use client";

import { useEffect, useRef, useState } from "react";
import { useInView } from "framer-motion";

/**
 * 环形仪表 —— 单值单色，中心数值用文本色而非系列色。
 * 默认品牌蓝（关键数据锚点）；可传入能量色表达系统状态
 * （如遥测面板的健康绿，超阈值时切换危险红）。
 * 进入视口后以 easeOutExpo 扫描至目标值；数值与圆弧同步跳动。
 */
export function RadialGauge({
  value,
  display,
  label,
  en,
  size = 128,
  color = "#00A8FF",
}: {
  /** 0–100 */
  value: number;
  /** 中心显示文本（如 "99.6%"） */
  display: string;
  label: string;
  en: string;
  size?: number;
  /** 数值弧颜色（能量/状态色），默认品牌蓝 */
  color?: string;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const inView = useInView(ref, { once: true, margin: "-40px" });
  const [progress, setProgress] = useState(0);
  const progressRef = useRef(0);

  useEffect(() => {
    if (!inView) return;
    let raf = 0;
    const t0 = performance.now();
    const from = progressRef.current; // 实时数据更新时从当前值平滑过渡，而非归零重扫
    const frame = (now: number) => {
      const p = Math.min((now - t0) / 1600, 1);
      const eased = 1 - Math.pow(2, -10 * p);
      const next = from + (value - from) * eased;
      progressRef.current = next;
      setProgress(next);
      if (p < 1) raf = requestAnimationFrame(frame);
    };
    raf = requestAnimationFrame(frame);
    return () => cancelAnimationFrame(raf);
  }, [inView, value]);

  const stroke = 3.5;
  const r = (size - stroke * 2) / 2;
  const c = 2 * Math.PI * r;
  const offset = c * (1 - progress / 100);

  return (
    <div ref={ref} className="flex flex-col items-center gap-3">
      <div className="relative" style={{ width: size, height: size }}>
        <svg width={size} height={size} className="-rotate-90">
          {/* 轨道 —— 隐性网格级的存在感 */}
          <circle
            cx={size / 2}
            cy={size / 2}
            r={r}
            fill="none"
            stroke="rgba(255,255,255,0.07)"
            strokeWidth={stroke}
          />
          {/* 数值弧 —— 颜色即状态，辉光收敛 */}
          <circle
            cx={size / 2}
            cy={size / 2}
            r={r}
            fill="none"
            stroke={color}
            strokeWidth={stroke}
            strokeLinecap="round"
            strokeDasharray={c}
            strokeDashoffset={offset}
            style={{
              filter: `drop-shadow(0 0 4px ${color}4d)`,
              transition: "stroke 0.6s ease",
            }}
          />
        </svg>
        {/* 中心读数 —— 文本色，不用系列色 */}
        <div className="absolute inset-0 flex flex-col items-center justify-center">
          <span className="font-mono text-xl font-light tabular-nums text-frost">
            {display === "" ? `${progress.toFixed(1)}%` : display}
          </span>
        </div>
        {/* 刻度装饰 */}
        <div className="pointer-events-none absolute inset-[-6px] rounded-full border border-dashed border-white/[0.06]" />
      </div>
      <div className="text-center">
        <p className="text-[13px] font-light tracking-widest text-silver">{label}</p>
        <p className="hud-label mt-1 text-silver/40">{en}</p>
      </div>
    </div>
  );
}
