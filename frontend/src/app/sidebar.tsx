import { ArrowUpRight } from "lucide-react";
import { NavLink } from "react-router-dom";
import { cn } from "@/lib/utils";
import { WORKSPACE_MODULES } from './modules';

export const NAV_ITEMS = [
  { to: "/home", label: "工作台", english: "OVERVIEW", number: "00", end: true },
  ...WORKSPACE_MODULES.map((item) => ({ to: item.to as string, label: item.name as string, english: item.english as string, number: item.number as string, end: true })),
  { to: "/settings", label: "设置", english: "PREFERENCES", number: "06", end: true },
];

export function Sidebar({ className, onNavigate }: { className?: string; onNavigate?: () => void }) {
  return (
    <aside className={cn("ns-sidebar", className)}>
      <div className="ns-sidebar-heading"><span>SYSTEM INDEX</span><span>07</span></div>
      <nav aria-label="主导航" className="ns-index-nav">
        {NAV_ITEMS.map((item) => (
          <NavLink key={item.to} to={item.to} end={item.end} onClick={onNavigate}
            className={({ isActive }) => cn("ns-index-item", isActive && "is-active")}>
            <span className="ns-index-number">{item.number}</span>
            <span className="min-w-0"><span className="ns-index-label">{item.label}</span><span className="ns-index-english">{item.english}</span></span>
            <ArrowUpRight size={13} className="ns-index-arrow" aria-hidden="true" />
          </NavLink>
        ))}
      </nav>
      <div className="ns-sidebar-foot"><span className="ns-sidebar-bars" aria-hidden="true" /><span>PERSONAL INTELLIGENCE<br />LOCAL WORKSPACE / V.05</span></div>
    </aside>
  );
}
