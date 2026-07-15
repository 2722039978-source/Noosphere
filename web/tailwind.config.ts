import type { Config } from "tailwindcss";

/**
 * Noosphere 设计令牌
 * 色彩：Apple 的克制 × 明日方舟终端的冷峻
 */
const config: Config = {
  content: ["./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        void: "#050505", // 主背景 — 近黑
        graphite: "#151515", // 面板底色 — 深灰
        steel: "#1d2126", // 金属灰 — 卡片描边基底
        tech: {
          DEFAULT: "#00A8FF", // 科技蓝 — 品牌强调色（仅核心交互 / 品牌标识 / 关键数据）
          dim: "#0a6fa8",
          glow: "#66cfff",
        },
        frost: "#F5F5F7", // 冷白 — 主文字
        silver: "#9ba1a6", // 银灰 — 次级文字
        // 系统状态 / 能量色 — 常驻但克制，只存在于光效、粒子、数据流、状态指示
        status: {
          ok: "#2EE6A6", // 青绿 — 在线 / 节点同步 / 系统健康 / 扫描线
          warn: "#FFB347", // 琥珀橙 — 计算流动 / 性能峰值 / 能量传输
          info: "#A855F7", // 极光紫 — 神经网络 / 推理链路 / 数据脉冲
          danger: "#FF4D67", // 危险红 — 仅告警 / 异常 / 风险事件
        },
      },
      fontFamily: {
        sans: [
          "var(--font-inter)",
          "var(--font-noto-sc)",
          "SF Pro Display",
          "Helvetica Neue",
          "sans-serif",
        ],
        mono: ["var(--font-jetbrains)", "ui-monospace", "SFMono-Regular", "monospace"],
      },
      letterSpacing: {
        hud: "0.35em", // HUD 标签专用超宽间距
      },
      backgroundImage: {
        // 战术网格 — 明日方舟终端底纹（中性银白，背景不再用蓝）
        "tech-grid":
          "linear-gradient(rgba(245,245,247,0.035) 1px, transparent 1px), linear-gradient(90deg, rgba(245,245,247,0.035) 1px, transparent 1px)",
        // 危险条纹 — 工业警示元素（银灰）
        hazard:
          "repeating-linear-gradient(-45deg, rgba(245,245,247,0.22) 0 8px, transparent 8px 16px)",
      },
      backgroundSize: {
        grid: "56px 56px",
      },
      keyframes: {
        scan: {
          "0%": { transform: "translateY(-100%)" },
          "100%": { transform: "translateY(100%)" },
        },
        "scan-x": {
          "0%": { transform: "translateX(-100%)" },
          "100%": { transform: "translateX(100%)" },
        },
        blink: {
          "0%, 100%": { opacity: "1" },
          "50%": { opacity: "0.25" },
        },
        "pulse-ring": {
          "0%": { transform: "scale(0.85)", opacity: "0.8" },
          "100%": { transform: "scale(1.6)", opacity: "0" },
        },
        marquee: {
          "0%": { transform: "translateX(0)" },
          "100%": { transform: "translateX(-50%)" },
        },
      },
      animation: {
        scan: "scan 3.2s cubic-bezier(0.4, 0, 0.2, 1) infinite",
        "scan-x": "scan-x 2.4s cubic-bezier(0.4, 0, 0.2, 1) infinite",
        blink: "blink 1.6s steps(2, start) infinite",
        "pulse-ring": "pulse-ring 2.8s cubic-bezier(0.16, 1, 0.3, 1) infinite",
        marquee: "marquee 28s linear infinite",
      },
    },
  },
  plugins: [],
};

export default config;
