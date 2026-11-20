import { useCallback, useEffect, useRef, useState } from 'react';
import { Bookmark, CodeXml, FlaskConical, FolderOpen, Layers3, Menu, MessageSquare, Settings, Wrench } from 'lucide-react';
import { Link, Outlet, parsePath, useLocation, useNavigate } from 'react-router-dom';
import { isTauri } from '@tauri-apps/api/core';
import { useBackend } from '@/hooks/useBackend';
import { useUiStore, type RouteId } from '@/stores/ui-store';
import { SessionSidebar } from '@/features/chat/SessionSidebar';
import { useChatPersistence, useChatStore } from '@/features/chat/chat-store';
import { useWorkspaceStore } from '@/stores/workspace-store';
import { ProjectPickerDialog } from '@/components/WorkspaceHub';
import StatusBar from '@/components/StatusBar';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { WorkspaceHandoff } from './WorkspaceHandoff';
import { modelSection } from './model-navigation';
import { ModelArchiveBackdrop } from '@/features/model-lab/ModelArchiveBackdrop';
import './workbench.css';

type SectionLink = { label: string; to: string; descendants?: boolean };

const tools: { label: string; icon: typeof Wrench; route: RouteId; links: SectionLink[] }[] = [
  { label: '模型', icon: Layers3, route: 'gateway', links: [
    { label: '本地模型', to: '/model-hub' }, { label: '高级管理', to: '/model-hub/config' },
  ] },
  { label: '测评', icon: FlaskConical, route: 'gateway', links: [
    { label: '模型试聊', to: '/model-check' }, { label: '正式测评', to: '/model-lab' },
    { label: '数据集', to: '/model-lab/datasets' }, { label: '测评报告', to: '/evaluation-reports', descendants: true },
    { label: '发布演练', to: '/release-center', descendants: true },
  ] },
  { label: '代码', icon: CodeXml, route: 'codelens', links: [
    { label: '项目索引', to: '/codelens' }, { label: '搜索代码', to: '/codelens#codelens-search-title' },
    { label: '分析文件', to: '/codelens#file-analysis' },
  ] },
  { label: '记忆', icon: Bookmark, route: 'nebula', links: [
    { label: '已保存的记忆', to: '/nebula' }, { label: '历史记忆', to: '/nebula?view=history' },
  ] },
  { label: '系统', icon: Wrench, route: 'devops', links: [
    { label: '系统资源', to: '/devops' }, { label: '诊断工具', to: '/devops?view=diagnostics' },
  ] },
  { label: '设置', icon: Settings, route: 'settings', links: [
    { label: '外观', to: '/settings' }, { label: '会话备份', to: '/settings#chat-backup' },
    { label: '软件更新', to: '/settings#app-update' }, { label: '应用信息', to: '/settings#app-info' },
  ] },
];

function includesPath(path: string, link: SectionLink) {
  const { pathname } = parsePath(link.to);
  return path === pathname || link.descendants && path.startsWith(pathname + '/');
}

function ToolSidebar({ tool, drawer, onNavigate }: { tool: typeof tools[number]; drawer?: boolean; onNavigate?: () => void }) {
  const { pathname, hash, search } = useLocation();
  const matching = tool.links.filter(link => includesPath(pathname, link));
  const current = matching.find(link => {
    const to = parsePath(link.to);
    return Boolean((to.hash || to.search) && (!to.hash || hash === to.hash) && (!to.search || search === to.search));
  }) ?? matching[0];
  return <aside className={drawer ? 'ns-project-sidebar ns-project-sidebar-drawer h-full flex-col' : 'ns-project-sidebar hidden h-full shrink-0 flex-col lg:flex'} aria-label={tool.label}>
    <div className="ns-sidebar-top"><h2>{tool.label}</h2></div>
    <nav className="ns-sidebar-scroll ns-section-links" aria-label={tool.label + '导航'}>
      {tool.links.map(link => <Link key={link.to} to={link.to} aria-current={link === current ? 'page' : undefined} onClick={onNavigate}>{link.label}</Link>)}
    </nav>
  </aside>;
}

export function AppLayout() {
  const backend = useBackend();
  const location = useLocation();
  const navigate = useNavigate();
  const setRoute = useUiStore(state => state.setRoute);
  const projectOpen = useUiStore(state => state.projectPickerOpen);
  const setProjectOpen = useUiStore(state => state.setProjectPickerOpen);
  const [mobileNavOpen, setMobileNavOpen] = useState(false);
  const mobileTrigger = useRef<HTMLButtonElement>(null);
  const workspaceId = useWorkspaceStore(state => state.activeId);
  const workspaceReady = useWorkspaceStore(state => state.hydrated);
  const chatReady = useChatPersistence(state => state.ready);
  const chat = location.pathname === '/chat';
  const tool = tools.find(item => item.links.some(link => includesPath(location.pathname, link)));
  const panelName = chat ? '项目与会话' : tool?.label ?? '导航';
  const section = modelSection(location.pathname);
  const archive = Boolean(section) && location.pathname !== '/model-hub';

  useEffect(() => {
    if (!workspaceReady || !isTauri()) return;
    function openProject(event: KeyboardEvent) {
      if (event.defaultPrevented || event.isComposing || event.repeat ||
        event.altKey || event.shiftKey || event.ctrlKey === event.metaKey ||
        event.key.toLowerCase() !== 'o') return;
      event.preventDefault();
      setProjectOpen(true);
    }
    window.addEventListener('keydown', openProject);
    return () => window.removeEventListener('keydown', openProject);
  }, [workspaceReady, setProjectOpen]);

  useEffect(() => {
    if (workspaceReady && chatReady) useChatStore.getState().restoreWorkspaceSession(workspaceId);
  }, [workspaceId, workspaceReady, chatReady]);

  const selectSession = useCallback((sessionId: string, messageId?: string) => {
    if (sessionId) useChatStore.getState().setActive(sessionId);
    setMobileNavOpen(false);
    navigate('/chat', { replace: chat, state: { historySelection: messageId ? { sessionId, messageId } : null } });
  }, [navigate, chat]);

  useEffect(() => {
    const route = chat ? 'assistant' : tool?.route;
    if (route) setRoute(route);
    setMobileNavOpen(false);
  }, [chat, tool, setRoute]);

  return <div className="ns-app-shell ns-reference-shell ns-workbench flex min-h-0 flex-1 overflow-hidden" data-model-archive-active={archive} data-project-chat={chat}>
    <a href="#workspace-main" className="ns-skip-link" onClick={event => { event.preventDefault(); document.getElementById('workspace-main')?.focus(); }}>跳到主要内容</a>
    <WorkspaceHandoff />
    <nav className="ns-tool-rail" aria-label="主导航">
      <Tooltip><TooltipTrigger asChild><Link to="/chat" className="ns-rail-button" aria-label="对话" aria-current={chat ? 'page' : undefined}><MessageSquare aria-hidden="true" /></Link></TooltipTrigger><TooltipContent side="right">对话</TooltipContent></Tooltip>
      <Tooltip><TooltipTrigger asChild><button type="button" className="ns-rail-button" aria-label="打开项目" aria-haspopup="dialog" aria-expanded={projectOpen} disabled={!workspaceReady} onClick={() => setProjectOpen(true)}><FolderOpen aria-hidden="true" /></button></TooltipTrigger><TooltipContent side="right">打开项目</TooltipContent></Tooltip>
      {tools.map(item => <Tooltip key={item.label}>
        <TooltipTrigger asChild><Link className="ns-rail-button" aria-label={item.label} aria-current={item === tool ? 'page' : undefined} to={item.links[0].to}><item.icon aria-hidden="true" /></Link></TooltipTrigger>
        <TooltipContent side="right">{item.label}</TooltipContent>
      </Tooltip>)}
    </nav>
    <div className="ns-workbench-frame flex min-w-0 flex-1">
      {chat ? <SessionSidebar className="hidden lg:flex" onSelect={selectSession} /> : tool && <ToolSidebar tool={tool} />}
      <div className="ns-workspace-column" data-model-archive-active={archive} data-model-section={section?.id}>
        <ModelArchiveBackdrop active={archive} />
        {(chat || tool) && <div className="ns-project-mobile-header flex shrink-0 items-center gap-2 px-3 lg:hidden">
          <Button ref={mobileTrigger} variant="ghost" size="icon" aria-label={chat ? '打开项目侧栏' : '打开' + panelName + '侧栏'} onClick={() => setMobileNavOpen(true)}><Menu aria-hidden="true" /></Button>
          <span className="text-xs font-medium text-text-2">{chat ? '对话' : panelName}</span>
        </div>}
        <div className="ns-workspace-content flex min-h-0 flex-1">
          <Dialog open={mobileNavOpen} onOpenChange={setMobileNavOpen}>
            <DialogContent className="ns-nav-dialog top-0 left-0 h-dvh max-w-[300px] translate-x-0 translate-y-0 gap-0 rounded-none border-r border-border-soft bg-bg-app p-0 shadow-pop sm:max-w-[300px] lg:hidden"
              onOpenAutoFocus={event => { event.preventDefault(); if (event.target instanceof HTMLElement) event.target.focus(); }}
              onCloseAutoFocus={event => { if (mobileTrigger.current?.offsetParent) { event.preventDefault(); mobileTrigger.current.focus(); } }}>
              <DialogTitle className="sr-only">{panelName}</DialogTitle>
              <DialogDescription className="sr-only">{chat ? '打开项目、对话或任务。' : '选择' + panelName + '页面。'}</DialogDescription>
              {chat ? <SessionSidebar className="ns-project-sidebar-drawer" onSelect={selectSession} onNavigate={() => setMobileNavOpen(false)} />
                : tool && <ToolSidebar drawer tool={tool} onNavigate={() => setMobileNavOpen(false)} />}
            </DialogContent>
          </Dialog>
          <main id="workspace-main" tabIndex={-1} className="relative flex min-w-0 flex-1 flex-col outline-none">
            <div key={location.pathname} className="ns-page ns-project-page flex min-h-0 flex-1 flex-col"><Outlet /></div>
          </main>
        </div>
        {backend.health === 'online' && !backend.backendError ? <span role="status" className="sr-only">后端在线</span> : <StatusBar backend={backend} />}
      </div>
    </div>
    <ProjectPickerDialog open={projectOpen} onOpenChange={setProjectOpen} onProjectSaved={() => selectSession(useChatStore.getState().activeId ?? '')} />
  </div>;
}
