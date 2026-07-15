import type { Variants } from "framer-motion";

/**
 * 动画系统的统一物理参数。
 * 全站只用这一组曲线，保证动效语言一致：
 * EASE_OUT — 类 easeOutExpo，Apple 式"果断启动、缓慢落定"
 */
export const EASE_OUT = [0.16, 1, 0.3, 1] as const;

export const DUR = {
  fast: 0.5,
  base: 0.9,
  slow: 1.4,
} as const;

/** 上浮 + 模糊消散 —— 全站标准入场 */
export const fadeUp: Variants = {
  hidden: { opacity: 0, y: 48, filter: "blur(12px)" },
  visible: {
    opacity: 1,
    y: 0,
    filter: "blur(0px)",
    transition: { duration: DUR.base, ease: EASE_OUT },
  },
};

/** 纯模糊显影 —— 用于大标题 */
export const blurReveal: Variants = {
  hidden: { opacity: 0, filter: "blur(20px)", scale: 1.04 },
  visible: {
    opacity: 1,
    filter: "blur(0px)",
    scale: 1,
    transition: { duration: DUR.slow, ease: EASE_OUT },
  },
};

/** 子元素级联容器 */
export const stagger = (delay = 0.12, delayChildren = 0): Variants => ({
  hidden: {},
  visible: {
    transition: { staggerChildren: delay, delayChildren },
  },
});

/** 逐字显现 —— Hero 大标题专用 */
export const letter: Variants = {
  hidden: { opacity: 0, y: "0.6em", filter: "blur(8px)" },
  visible: {
    opacity: 1,
    y: 0,
    filter: "blur(0px)",
    transition: { duration: 0.8, ease: EASE_OUT },
  },
};
