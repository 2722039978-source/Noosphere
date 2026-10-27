import { ArrowUpRight, Home, MessageSquare, SquareCode, Box, ChartNoAxesCombined, Settings, Grid2X2 } from "lucide-react";
import { Link, useLocation } from "react-router-dom";
import { cn } from "@/lib/utils";
import { WORKSPACE_MODULES } from './modules';
import { buildPrimaryNavigation, primaryNavigationPath } from './model-navigation';

export const NAV_ITEMS = buildPrimaryNavigation(WORKSPACE_MODULES);
const NAV_ICONS = [Home, MessageSquare, SquareCode, Box, ChartNoAxesCombined, Box, Grid2X2, Settings];

export function Sidebar({ className, onNavigate }: { className?: string; onNavigate?: () => void }) {
  const location = useLocation();
  return (
    <aside className={cn("ns-sidebar", className)}>
      <Link to="/home" className="ns-sidebar-brand" aria-label="Noosphere 工作台" onClick={onNavigate}><strong>NOOSPHERE</strong><small>SYNTHESIZE INFORMATION</small><span>ANALYSIS <b>OS</b></span></Link>
      <div className="ns-sidebar-heading"><span>SYSTEM INDEX</span><span>{String(NAV_ITEMS.length).padStart(2, '0')}</span></div>
      <nav aria-label="主导航" className="ns-index-nav">
        {NAV_ITEMS.map((item, index) => { const Icon = NAV_ICONS[index] ?? Box; return (
          <Link key={item.to} to={item.to} onClick={onNavigate} aria-current={primaryNavigationPath(location.pathname) === item.to ? 'page' : undefined}
            className={cn("ns-index-item", primaryNavigationPath(location.pathname) === item.to && "is-active")}>
            <span className="ns-index-number">{item.number}</span>
            <Icon size={21} className="ns-index-icon" aria-hidden="true" />
            <span className="min-w-0"><span className="ns-index-label">{item.label}</span><span className="ns-index-english">{item.english}</span></span>
            <ArrowUpRight size={13} className="ns-index-arrow" aria-hidden="true" />
          </Link>
        ); })}
      </nav>
      <div className="ns-sidebar-foot"><span>PERSONAL<br />INTELLIGENCE<br />LOCAL WORKSPACE /<br />V.05</span></div>
    </aside>
  );
}
