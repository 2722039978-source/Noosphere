import { useEffect, useState } from "react";
import { ArrowUpRight, Menu, Search, Settings2 } from "lucide-react";
import { Link, Outlet, useLocation } from "react-router-dom";
import { useBackend } from "@/hooks/useBackend";
import { useUiStore, type RouteId } from "@/stores/ui-store";
import { NAV_ITEMS, Sidebar } from "./sidebar";
import StatusBar from "@/components/StatusBar";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { WORKSPACE_MODULES } from './modules';
import { WorkspaceSignal } from '@/components/WorkspaceSignal';
import { WorkspaceHub } from '@/components/WorkspaceHub';
import { WorkspaceHandoff } from './WorkspaceHandoff';
import { primaryNavigationPath, modelSection } from './model-navigation';
import { ModelArchiveBackdrop } from '@/features/model-lab/ModelArchiveBackdrop';

const ROUTE_BY_PATH: Record<string, RouteId> = {
  "/": "home", "/chat": "assistant", "/home": "home", "/gateway": "gateway", "/nebula": "nebula",
  "/codelens": "codelens", "/devops": "devops", "/settings": "settings",
};

export function AppLayout() {
  const backend = useBackend();
  const location = useLocation();
  const setRoute = useUiStore((s) => s.setRoute);
  const [mobileNavOpen, setMobileNavOpen] = useState(false);
  const active = NAV_ITEMS.find(item => item.to === primaryNavigationPath(location.pathname)) ?? NAV_ITEMS[1];
  const section = modelSection(location.pathname);
  const module = WORKSPACE_MODULES.find((item) => item.to === location.pathname);
  const isModelHub = ['/', '/home', '/model-hub'].includes(location.pathname);

  useEffect(() => {
    const route = ROUTE_BY_PATH[location.pathname];
    if (route) setRoute(route);
  }, [location.pathname, setRoute]);

  return (
    <div className="ns-app-shell ns-reference-shell flex min-h-0 flex-1 overflow-hidden bg-bg-app text-text-1" data-model-archive-active={Boolean(section)} style={{ '--module-accent': module?.accent ?? '#9b754c' } as React.CSSProperties}>
      <a href="#workspace-main" className="ns-skip-link" onClick={(event) => { event.preventDefault(); document.getElementById("workspace-main")?.focus(); }}>跳到主要内容</a>
      <WorkspaceHandoff />
      <Sidebar className="hidden lg:flex" />
      <div className="ns-workspace-column" data-model-archive-active={Boolean(section)} data-model-section={section?.id}>
      <ModelArchiveBackdrop active={Boolean(section)} />
      <header className="ns-masthead">
        <WorkspaceSignal />
        <div className="ns-masthead-tools"><WorkspaceHub />
        <Link to="/model-hub#hub-model-search" className="ns-header-search" onClick={event => { const input = document.getElementById('hub-model-search'); if (input) { event.preventDefault(); input.focus(); } }}><Search size={18} aria-hidden="true" /><span>搜索模型…</span><kbd>/</kbd></Link>
        <Link to="/chat" className="ns-header-chat">AI 对话</Link>
        <Link to="/settings" aria-label="打开设置" className="ns-header-settings"><Settings2 size={17} aria-hidden="true" /></Link></div>
        <div className="ns-masthead-current"><span>FILE / NS–{section ? '007' : active.number.padStart(3, '0')}</span><span>{section?.label.toUpperCase() || (isModelHub ? 'MODEL HUB' : active.english)} <ArrowUpRight size={13} aria-hidden="true" /></span></div>
        <Button variant="ghost" size="icon" className="ns-mobile-menu lg:hidden" aria-label="打开主导航" onClick={() => setMobileNavOpen(true)}><Menu aria-hidden="true" /></Button>
      </header>
      <div className="ns-workspace-content flex min-h-0 flex-1">
        <Dialog open={mobileNavOpen} onOpenChange={setMobileNavOpen}>
          <DialogContent aria-describedby={undefined} className="ns-nav-dialog top-0 left-0 h-dvh max-w-[300px] translate-x-0 translate-y-0 gap-0 rounded-none border-r border-border-soft bg-bg-app p-0 shadow-pop lg:hidden">
            <DialogTitle className="sr-only">主导航</DialogTitle>
            <Sidebar className="w-full border-r-0" onNavigate={() => setMobileNavOpen(false)} />
          </DialogContent>
        </Dialog>
        <main id="workspace-main" tabIndex={-1} className="relative flex min-w-0 flex-1 flex-col outline-none">
          <div key={location.pathname} className="ns-page flex min-h-0 flex-1 flex-col"><Outlet /></div>
        </main>
      </div>
      <StatusBar backend={backend} />
      </div>
    </div>
  );
}
