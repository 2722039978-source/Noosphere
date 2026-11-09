import { useCallback, useEffect, useState } from "react";
import { ArrowUpRight, Menu } from "lucide-react";
import { Outlet, useLocation, useNavigate } from "react-router-dom";
import { useBackend } from "@/hooks/useBackend";
import { useUiStore, type RouteId } from "@/stores/ui-store";
import { SessionSidebar } from '@/features/chat/SessionSidebar';
import { useChatPersistence, useChatStore } from '@/features/chat/chat-store';
import { useWorkspaceStore } from '@/stores/workspace-store';
import StatusBar from "@/components/StatusBar";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { WORKSPACE_MODULES } from './modules';
import { WorkspaceSignal } from '@/components/WorkspaceSignal';
import { WorkspaceHandoff } from './WorkspaceHandoff';
import { modelSection } from './model-navigation';
import { ModelArchiveBackdrop } from '@/features/model-lab/ModelArchiveBackdrop';
import { WorkspaceBackdrop } from '@/components/WorkspaceBackdrop';

const ROUTE_BY_PATH: Record<string, RouteId> = {
  "/": "home", "/chat": "assistant", "/home": "home", "/gateway": "gateway", "/nebula": "nebula",
  "/codelens": "codelens", "/devops": "devops", "/settings": "settings",
};

export function AppLayout() {
  const backend = useBackend();
  const location = useLocation();
  const navigate = useNavigate();
  const setRoute = useUiStore((s) => s.setRoute);
  const [mobileNavOpen, setMobileNavOpen] = useState(false);
  const workspaceId = useWorkspaceStore(state => state.activeId);
  const workspaceReady = useWorkspaceStore(state => state.hydrated);
  const chatReady = useChatPersistence(state => state.ready);
  const chat = location.pathname === '/chat';
  const section = modelSection(location.pathname);
  const module = WORKSPACE_MODULES.find((item) => item.to === location.pathname);

  useEffect(() => {
    if (workspaceReady && chatReady) useChatStore.getState().restoreWorkspaceSession(workspaceId);
  }, [workspaceId, workspaceReady, chatReady]);

  const selectSession = useCallback((sessionId: string, messageId?: string) => {
    if (sessionId) useChatStore.getState().setActive(sessionId);
    setMobileNavOpen(false);
    navigate('/chat', { replace: chat, state: { historySelection: messageId ? { sessionId, messageId } : null } });
  }, [navigate, chat]);

  useEffect(() => {
    const route = ROUTE_BY_PATH[location.pathname];
    if (route) setRoute(route);
  }, [location.pathname, setRoute]);

  return (
    <div className="ns-app-shell ns-reference-shell flex min-h-0 flex-1 overflow-hidden bg-bg-app text-text-1" data-model-archive-active={Boolean(section)} data-workspace-art-active={!section && !chat} data-project-chat={chat} data-workspace-module={module?.id ?? 'settings'} style={{ '--module-accent': module?.accent ?? '#9b754c' } as React.CSSProperties}>
      <a href="#workspace-main" className="ns-skip-link" onClick={(event) => { event.preventDefault(); document.getElementById("workspace-main")?.focus(); }}>跳到主要内容</a>
      <WorkspaceHandoff />
      <SessionSidebar className="hidden lg:flex" onSelect={selectSession} />
      <div className="ns-workspace-column" data-model-archive-active={Boolean(section)} data-model-section={section?.id}>
      <WorkspaceBackdrop active={!section && !chat} />
      <ModelArchiveBackdrop active={Boolean(section)} />
      {chat ? <div className="ns-project-mobile-header flex shrink-0 items-center gap-2 px-3 lg:hidden"><Button variant="ghost" size="icon" aria-label="打开项目侧栏" onClick={() => setMobileNavOpen(true)}><Menu aria-hidden="true" /></Button><span className="text-xs font-semibold">NOOSPHERE</span></div> : <header className="ns-masthead">
        <WorkspaceSignal />
        <div className="ns-masthead-current"><span>FILE / NS–{section ? '007' : (module?.number ?? '06').padStart(3, '0')}</span><span>{section?.label.toUpperCase() || module?.english || 'PREFERENCES'} <ArrowUpRight size={13} aria-hidden="true" /></span></div>
        <Button variant="ghost" size="icon" className="ns-mobile-menu lg:hidden" aria-label="打开项目侧栏" onClick={() => setMobileNavOpen(true)}><Menu aria-hidden="true" /></Button>
      </header>}
      <div className="ns-workspace-content flex min-h-0 flex-1">
        <Dialog open={mobileNavOpen} onOpenChange={setMobileNavOpen}>
          <DialogContent className="ns-nav-dialog top-0 left-0 h-dvh max-w-[300px] translate-x-0 translate-y-0 gap-0 rounded-none border-r border-border-soft bg-bg-app p-0 shadow-pop lg:hidden">
            <DialogTitle className="sr-only">项目与会话</DialogTitle>
            <DialogDescription className="sr-only">打开项目，继续或搜索已保存的对话。</DialogDescription>
            <SessionSidebar className="ns-project-sidebar-drawer" onSelect={selectSession} onNavigate={() => setMobileNavOpen(false)} />
          </DialogContent>
        </Dialog>
        <main id="workspace-main" tabIndex={-1} className="relative flex min-w-0 flex-1 flex-col outline-none">
          <div key={location.pathname} className="ns-page ns-project-page flex min-h-0 flex-1 flex-col"><Outlet /></div>
        </main>
      </div>
      <StatusBar backend={backend} />
      </div>
    </div>
  );
}
