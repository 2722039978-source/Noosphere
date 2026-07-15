"use client";

import { useState } from "react";
import {
  motion,
  useMotionValueEvent,
  useScroll,
  AnimatePresence,
} from "framer-motion";
import { Menu, X, Github, Activity } from "lucide-react";
import { NAV_LINKS } from "@/lib/data";
import { useServiceStatus } from "@/hooks/useServiceStatus";
import { EASE_OUT } from "@/lib/motion";
import { cn } from "@/lib/utils";

/**
 * 顶部导航 —— 玻璃拟态 + 滚动感知。
 * 顶部时几乎透明融入 Hero；滚动后浮现玻璃底与描边。
 */
export function Navbar() {
  const [scrolled, setScrolled] = useState(false);
  const [open, setOpen] = useState(false);
  const { scrollY } = useScroll();
  const { onlineCount } = useServiceStatus();

  useMotionValueEvent(scrollY, "change", (v) => setScrolled(v > 40));

  return (
    <motion.header
      initial={{ y: -80, opacity: 0 }}
      animate={{ y: 0, opacity: 1 }}
      transition={{ duration: 1.1, delay: 2.2, ease: EASE_OUT }}
      className={cn(
        "fixed inset-x-0 top-0 z-50 transition-[background-color,border-color,backdrop-filter] duration-500",
        scrolled
          ? "border-b border-white/[0.07] bg-void/60 backdrop-blur-2xl"
          : "border-b border-transparent bg-transparent",
      )}
    >
      <nav className="mx-auto flex h-16 max-w-7xl items-center justify-between px-6 lg:px-10">
        {/* ── Logo ── */}
        <a href="#hero" className="group flex items-center gap-3">
          <span className="relative flex h-2.5 w-2.5">
            <span className="absolute h-full w-full rounded-full bg-tech animate-pulse-ring" />
            <span className="relative h-2.5 w-2.5 rounded-full bg-tech shadow-[0_0_12px_#00A8FF]" />
          </span>
          <span className="font-sans text-sm font-light tracking-[0.45em] text-frost">
            NOOSPHERE
          </span>
        </a>

        {/* ── 桌面端菜单 ── */}
        <ul className="hidden items-center gap-1 md:flex">
          {NAV_LINKS.map((link) => (
            <li key={link.href}>
              <a
                href={link.href}
                className="nav-scan relative rounded-md px-4 py-2 text-[13px] font-light tracking-widest text-frost/90 transition-colors duration-300 hover:text-tech"
              >
                {link.label}
              </a>
            </li>
          ))}
        </ul>

        {/* ── 右侧：系统状态 + GitHub ── */}
        <div className="hidden items-center gap-5 md:flex">
          <div className="hud-label flex items-center gap-2" title="本机 Noosphere 服务在线数">
            <Activity size={12} className={onlineCount > 0 ? "text-status-ok" : "text-silver/50"} />
            <span className={onlineCount > 0 ? "text-status-ok" : "text-silver/50"}>
              SYS {onlineCount}/3
            </span>
          </div>
          <a
            href="https://github.com/2722039978-source"
            target="_blank"
            rel="noreferrer"
            aria-label="GitHub"
            className="text-frost/90 transition-colors duration-300 hover:text-tech"
          >
            <Github size={17} strokeWidth={1.5} />
          </a>
        </div>

        {/* ── 移动端开关 ── */}
        <button
          className="text-frost md:hidden"
          onClick={() => setOpen((v) => !v)}
          aria-label={open ? "关闭菜单" : "打开菜单"}
        >
          {open ? <X size={20} strokeWidth={1.5} /> : <Menu size={20} strokeWidth={1.5} />}
        </button>
      </nav>

      {/* ── 移动端抽屉 ── */}
      <AnimatePresence>
        {open && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: "auto", opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.45, ease: EASE_OUT }}
            className="overflow-hidden border-b border-white/[0.07] bg-void/90 backdrop-blur-2xl md:hidden"
          >
            <ul className="space-y-1 px-6 py-4">
              {NAV_LINKS.map((link, i) => (
                <motion.li
                  key={link.href}
                  initial={{ x: -24, opacity: 0 }}
                  animate={{ x: 0, opacity: 1 }}
                  transition={{ delay: 0.05 * i, duration: 0.4, ease: EASE_OUT }}
                >
                  <a
                    href={link.href}
                    onClick={() => setOpen(false)}
                    className="block py-2.5 text-sm font-light tracking-widest text-frost/90 hover:text-tech"
                  >
                    {link.label}
                  </a>
                </motion.li>
              ))}
            </ul>
          </motion.div>
        )}
      </AnimatePresence>
    </motion.header>
  );
}
