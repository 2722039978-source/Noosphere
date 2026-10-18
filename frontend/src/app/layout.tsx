import { useEffect, useState } from "react";
import { ArrowUpRight, Menu } from "lucide-react";
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

const ROUTE_BY_PATH: Record<string, RouteId> = {
  "/chat": "assistant", "/home": "home", "/gateway": "gateway", "/nebula": "nebula",
  "/codelens": "codelens", "/devops": "devops", "/settings": "settings",
};

export function AppLayout() {
  const backend = useBackend();
  const location = useLocation();
  const setRoute = useUiStore((s) => s.setRoute);
  const [mobileNavOpen, setMobileNavOpen] = useState(false);
  const active = NAV_ITEMS.find((item) => item.to === location.pathname) ?? NAV_ITEMS[1];
  const module = WORKSPACE_MODULES.find((item) => item.to === location.pathname);

  useEffect(() => {
    const route = ROUTE_BY_PATH[location.pathname];
    if (route) setRoute(route);
  }, [location.pathname, setRoute]);

  return (
    <div className="ns-app-shell flex min-h-0 flex-1 flex-col overflow-hidden bg-bg-app text-text-1" style={{ '--module-accent': module?.accent ?? '#9b754c' } as React.CSSProperties}>
      <a href="#workspace-main" className="ns-skip-link" onClick={(event) => { event.preventDefault(); document.getElementById("workspace-main")?.focus(); }}>跳到主要内容</a>
      <WorkspaceHandoff />
      <header className="ns-masthead">
        <Link to="/home" className="ns-wordmark" aria-label="Noosphere 工作台"><strong>NOOSPHERE</strong><span>SYNTHESIZE INFORMATION</span></Link>
        <div className="ns-masthead-section"><span>PERSONAL INTELLIGENCE SYSTEM</span><b>ANALYSIS <strong>OS</strong></b></div>
        <WorkspaceSignal />
        <WorkspaceHub />
        <div className="ns-masthead-current"><span>FILE / NS–0{active.number}</span><span>{active.english} <ArrowUpRight size={13} aria-hidden="true" /></span></div>
        <Button variant="ghost" size="icon" className="ns-mobile-menu lg:hidden" aria-label="打开主导航" onClick={() => setMobileNavOpen(true)}><Menu aria-hidden="true" /></Button>
      </header>
      <div className="flex min-h-0 flex-1">
        <Sidebar className="hidden lg:flex" />
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
  );
}
