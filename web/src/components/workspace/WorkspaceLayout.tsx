"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { Activity, ArrowLeft } from "lucide-react";
import { useServiceStatus } from "@/hooks/useServiceStatus";

/** 各 Workspace 页面的共享布局组件 */
export function WorkspaceLayout({ title, port, children }: {
  title: string; port: number; children: React.ReactNode;
}) {
  const pathname = usePathname();
  const { statuses, onlineCount } = useServiceStatus();

  // Determine which workspace is active
  const isActive = (href: string) => pathname === href;

  return (
    <div className="min-h-screen bg-[#050505]">
      {/* Workspace nav */}
      <header className="sticky top-0 z-50 border-b border-white/[0.06] bg-[#050505]/85 backdrop-blur-2xl">
        <div className="mx-auto flex h-12 max-w-7xl items-center justify-between px-6 lg:px-8">
          <div className="flex items-center gap-4">
            <Link href="/" className="flex items-center gap-2 text-[11px] text-[#9BA1A6] hover:text-[#F5F5F7] transition-colors">
              <ArrowLeft size={14} />
              NOOSPHERE
            </Link>
            <span className="text-[#9BA1A6]/30">|</span>
            <span className="text-[13px] font-medium text-[#F5F5F7]">{title}</span>
            <span className="text-[10px] text-[#9BA1A6]">:{port}</span>
          </div>

          <nav className="flex items-center gap-1">
            {[
              { href: "/codelens", label: "CodeLens" },
              { href: "/nebula", label: "Nebula" },
              { href: "/devops", label: "DevOps" },
            ].map(link => (
              <Link
                key={link.href}
                href={link.href}
                className={`px-3 py-1.5 text-[11px] font-medium rounded-md transition-colors ${
                  isActive(link.href)
                    ? "bg-white/[0.06] text-[#00A8FF]"
                    : "text-[#9BA1A6] hover:text-[#F5F5F7]"
                }`}
              >
                {link.label}
              </Link>
            ))}
            <span className="mx-2 text-[#9BA1A6]/20">|</span>
            <div className="flex items-center gap-1.5 text-[10px] text-[#9BA1A6]">
              <Activity size={10} className={onlineCount > 0 ? "text-emerald-400" : "text-[#9BA1A6]"} />
              SYS {onlineCount}/4
            </div>
          </nav>
        </div>
      </header>

      {/* Embedded dashboard */}
      <main className="mx-auto max-w-7xl px-6 lg:px-8 py-6">
        {children}
      </main>
    </div>
  );
}
