"use client";

import { useRef, type ReactNode } from "react";
import { motion, useMotionValue, useSpring, useMotionTemplate } from "framer-motion";
import { cn } from "@/lib/utils";

/**
 * 3D 倾斜玻璃卡片 —— 鼠标驱动的透视倾斜 + 跟随光斑反射。
 * 纯 transform / opacity 驱动，全程 GPU 合成。
 */
export function GlassCard({
  children,
  className,
  maxTilt = 7,
}: {
  children: ReactNode;
  className?: string;
  maxTilt?: number;
}) {
  const ref = useRef<HTMLDivElement>(null);

  const rx = useMotionValue(0);
  const ry = useMotionValue(0);
  const glareX = useMotionValue(50);
  const glareY = useMotionValue(50);
  const glareO = useMotionValue(0);

  // 弹簧平滑 —— 让倾斜带真实惯性，而非线性跟随
  const srx = useSpring(rx, { stiffness: 180, damping: 22 });
  const sry = useSpring(ry, { stiffness: 180, damping: 22 });
  const sgx = useSpring(glareX, { stiffness: 120, damping: 24 });
  const sgy = useSpring(glareY, { stiffness: 120, damping: 24 });
  const sgo = useSpring(glareO, { stiffness: 100, damping: 26 });

  const glare = useMotionTemplate`radial-gradient(480px circle at ${sgx}% ${sgy}%, rgba(255,255,255,0.12), rgba(255,255,255,0.04) 40%, transparent 65%)`;

  const onMove = (e: React.PointerEvent) => {
    const el = ref.current;
    if (!el || e.pointerType !== "mouse") return;
    const rect = el.getBoundingClientRect();
    const px = (e.clientX - rect.left) / rect.width; // 0..1
    const py = (e.clientY - rect.top) / rect.height;
    ry.set((px - 0.5) * 2 * maxTilt);
    rx.set(-(py - 0.5) * 2 * maxTilt);
    glareX.set(px * 100);
    glareY.set(py * 100);
    glareO.set(1);
  };

  const onLeave = () => {
    rx.set(0);
    ry.set(0);
    glareO.set(0);
  };

  return (
    <div style={{ perspective: 1100 }}>
      <motion.div
        ref={ref}
        onPointerMove={onMove}
        onPointerLeave={onLeave}
        style={{ rotateX: srx, rotateY: sry, transformStyle: "preserve-3d", willChange: "transform" }}
        className={cn("glass hud-corners group relative overflow-hidden rounded-xl", className)}
      >
        {/* 跟随鼠标的玻璃反射层 */}
        <motion.div
          aria-hidden
          className="pointer-events-none absolute inset-0 z-10"
          style={{ background: glare, opacity: sgo }}
        />
        {children}
      </motion.div>
    </div>
  );
}
