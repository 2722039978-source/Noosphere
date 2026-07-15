"use client";

import { useState } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  motion, useMotionValueEvent, useScroll, AnimatePresence,
} from "framer-motion";
import { Menu, X, Github, Activity, Settings } from "lucide-react";
import { useServiceStatus } from "@/hooks/useServiceStatus";
import { EASE_OUT } from "@/lib/motion";
import { cn } from "@/lib/utils";

/**
 * 全局导航 —— 玻璃拟态 + 滚动感知 + Workspace 切换 + 设置入口
 *
 * 在首页时显示锚点导航（Hero/系统/架构/产品/Roadmap）
 * 在子页面时显示 Workspace 切换 + 返回首页
 */
export function Navbar() {
  const [scrolled, setScrolled] = useState(false);
  const [open, setOpen] = useState(false);
  const { scrollY } = useScroll();
  const { onlineCount } = useServiceStatus();
  const pathname = usePathname();
  const isHome = pathname === "/";

  useMotionValueEvent(scrollY, "change", (v) => setScrolled(v > 40));

  const workspaceLinks = [
    { href: "/codelens", label: "CodeLens" },
    { href: "/nebula", label: "Nebula" },
    { href: "/devops", label: "DevOps" },
  ];

  return (
    <motion.header
      initial={{ y: -80, opacity: 0 }}
      animate={{ y: 0, opacity: 1 }}
      transition={{ duration: 1.1, delay: 2.2, ease: EASE_OUT }}
      className={cn(
        "fixed inset-x-0 top-0 z-50 transition-[background-color,border-color,backdrop-filter] duration-500",
        scrolled || !isHome
          ? "border-b border-white/[0.07] bg-void/60 backdrop-blur-2xl"
          : "border-b border-transparent bg-transparent",
      )}
    >
      <nav className="mx-auto flex h-16 max-w-7xl items-center justify-between px-6 lg:px-10">
        {/* ── Logo ── */}
        <Link href="/" className="group flex items-center gap-3">
          <span className="relative flex h-2.5 w-2.5">
            <span className="absolute h-full w-full rounded-full bg-tech animate-pulse-ring" />
            <span className="relative h-2.5 w-2.5 rounded-full bg-tech shadow-[0_0_12px_#00A8FF]" />
          </span>
          <span className="font-sans text-sm font-light tracking-[0.45em] text-frost">
            NOOSPHERE
          </span>
        </Link>

        {/* ── 桌面端菜单 ── */}
        <ul className="hidden items-center gap-1 md:flex">
          {isHome ? (
            <>
              <li><a href="#dashboard" className="nav-scan rounded-md px-4 py-2 text-[13px] font-light tracking-widest text-frost/90 hover:text-tech">DASHBOARD</a></li>
              <li><a href="#systems" className="nav-scan rounded-md px-4 py-2 text-[13px] font-light tracking-widest text-frost/90 hover:text-tech">SYSTEMS</a></li>
              <li><a href="#products" className="nav-scan rounded-md px-4 py-2 text-[13px] font-light tracking-widest text-frost/90 hover:text-tech">PRODUCTS</a></li>
            </>
          ) : (
            <>
              <li><Link href="/" className="nav-scan rounded-md px-4 py-2 text-[13px] font-light tracking-widest text-frost/90 hover:text-tech">HOME</Link></li>
              {workspaceLinks.map(link => (
                <li key={link.href}>
                  <Link
                    href={link.href}
                    className={cn(
                      "nav-scan rounded-md px-4 py-2 text-[13px] font-light tracking-widest transition-colors",
                      pathname === link.href ? "text-tech" : "text-frost/90 hover:text-tech",
                    )}
                  >
                    {link.label.toUpperCase()}
                  </Link>
                </li>
              ))}
            </>
          )}
        </ul>

        {/* ── 右侧 ── */}
        <div className="hidden items-center gap-5 md:flex">
          <div className="hud-label flex items-center gap-2" title="本机 Noosphere 服务在线数">
            <Activity size={12} className={onlineCount > 0 ? "text-status-ok" : "text-silver/50"} />
            <span className={onlineCount > 0 ? "text-status-ok" : "text-silver/50"}>
              SYS {onlineCount}/4
            </span>
          </div>
          <Link
            href="/settings/model"
            className={cn(
              "rounded-md px-3 py-1.5 text-[12px] font-light tracking-widest transition-colors",
              pathname === "/settings/model"
                ? "bg-white/[0.06] text-tech"
                : "text-frost/90 hover:text-tech",
            )}
            title="AI Gateway 模型设置"
          >
            <Settings size={14} className="inline mr-1" /> GATEWAY
          </Link>
          <a
            href="https://github.com/2722039978-source"
            target="_blank" rel="noreferrer" aria-label="GitHub"
            className="text-frost/90 transition-colors duration-300 hover:text-tech"
          >
            <Github size={17} strokeWidth={1.5} />
          </a>
        </div>

        {/* ── 移动端开关 ── */}
        <button className="text-frost md:hidden" onClick={() => setOpen(v => !v)} aria-label={open ? "关闭菜单" : "打开菜单"}>
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
              {isHome ? (
                <>
                  {[{ href: "#dashboard", label: "Dashboard" }, { href: "#systems", label: "Systems" }, { href: "#products", label: "Products" }].map((link, i) => (
                    <motion.li key={link.href} initial={{ x: -24, opacity: 0 }} animate={{ x: 0, opacity: 1 }} transition={{ delay: 0.05 * i, duration: 0.4, ease: EASE_OUT }}>
                      <a href={link.href} onClick={() => setOpen(false)} className="block py-2.5 text-sm font-light tracking-widest text-frost/90 hover:text-tech">{link.label}</a>
                    </motion.li>
                  ))}
                </>
              ) : (
                <>
                  {[{ href: "/", label: "Home" }, ...workspaceLinks.map(w => ({ href: w.href, label: w.label })), { href: "/settings/model", label: "AI Gateway" }].map((link, i) => (
                    <motion.li key={link.href} initial={{ x: -24, opacity: 0 }} animate={{ x: 0, opacity: 1 }} transition={{ delay: 0.05 * i, duration: 0.4, ease: EASE_OUT }}>
                      <Link href={link.href} onClick={() => setOpen(false)} className="block py-2.5 text-sm font-light tracking-widest text-frost/90 hover:text-tech">{link.label}</Link>
                    </motion.li>
                  ))}
                </>
              )}
            </ul>
          </motion.div>
        )}
      </AnimatePresence>
    </motion.header>
  );
}
