"use client";

import { useEffect } from "react";
import { motion, useMotionValue, useSpring } from "framer-motion";

/**
 * 光标追光 —— 一团柔和的冷白辉光以弹簧物理跟随鼠标，
 * 为玻璃面板提供"环境光源"，营造材质反射感（禁止大面积蓝色发光）。
 * 纯 transform 驱动，GPU 合成层，不触发重排。
 */
export function CursorGlow() {
  const x = useMotionValue(-600);
  const y = useMotionValue(-600);
  const sx = useSpring(x, { stiffness: 60, damping: 20, mass: 0.8 });
  const sy = useSpring(y, { stiffness: 60, damping: 20, mass: 0.8 });

  useEffect(() => {
    // 触屏设备与减弱动效场景不渲染
    if (window.matchMedia("(pointer: coarse)").matches) return;
    const move = (e: PointerEvent) => {
      x.set(e.clientX - 300);
      y.set(e.clientY - 300);
    };
    window.addEventListener("pointermove", move, { passive: true });
    return () => window.removeEventListener("pointermove", move);
  }, [x, y]);

  return (
    <motion.div
      aria-hidden
      className="pointer-events-none fixed left-0 top-0 z-[5] h-[600px] w-[600px] rounded-full opacity-[0.09]"
      style={{
        x: sx,
        y: sy,
        background:
          "radial-gradient(circle at center, #F5F5F7 0%, rgba(245,245,247,0.25) 35%, transparent 70%)",
        willChange: "transform",
      }}
    />
  );
}
