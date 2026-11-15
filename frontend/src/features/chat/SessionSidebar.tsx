import { memo, useDeferredValue, useEffect, useMemo, useRef, useState } from 'react';
import { ChevronDown, ChevronRight, Folder, MessageSquare, Plus, Search, SquarePen, Trash2, X } from 'lucide-react';
import { useLocation } from 'react-router-dom';
import { cn } from '@/lib/utils';
import { Input } from '@/components/ui/input';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { useChatPersistence, useChatStore } from './chat-store';
import { searchSessions, type SessionSearchResult } from './session-search';
import { useWorkspaceStore } from '@/stores/workspace-store';
import { useUiStore } from '@/stores/ui-store';
import { sessionWorkspaceId } from './chat-backup';
import { sortRecentWorkspaces } from './workspace-recency';
import { useGenerationStore } from './chat-runtime';
import { WorkspaceTasks } from './WorkspaceTasks';
import './project-sidebar.css';

type SelectSession = (sessionId: string, messageId?: string) => void;

function MatchText({ match }: { match: NonNullable<SessionSearchResult['match']> }) {
  return <>{match.snippet.slice(0, match.matchStart)}<mark>{match.snippet.slice(match.matchStart, match.matchEnd)}</mark>{match.snippet.slice(match.matchEnd)}</>;
}

const SessionItem = memo(function SessionItem({ result, active, running, onSelect }: {
  result: SessionSearchResult;
  active: boolean;
  running: boolean;
  onSelect: SelectSession;
}) {
  const { session, match } = result;
  return <div className="ns-project-session group" data-active={active}>
    <button type="button" onClick={() => onSelect(session.id, match?.messageId)}
      aria-label={`打开会话 ${session.title}${match?.messageId ? '，定位匹配消息' : ''}`} title={session.title} aria-current={active ? 'true' : undefined}>
      <MessageSquare size={13} aria-hidden="true" />
      <span>{match?.source === 'title' ? <MatchText match={match} /> : session.title}
        {match?.messageId && <small className="line-clamp-2 break-all text-text-3"><MatchText match={match} /></small>}
      </span>
      {running && <small className="ns-project-run-label">运行中</small>}
    </button>
    <Tooltip><TooltipTrigger asChild><button type="button" className="ns-session-remove" aria-label={`删除会话 ${session.title}`} onClick={() => useChatStore.getState().removeSession(session.id)}><Trash2 size={13} aria-hidden="true" /></button></TooltipTrigger><TooltipContent side="right">删除会话</TooltipContent></Tooltip>
  </div>;
});

export const SessionSidebar = memo(function SessionSidebar({ className, onSelect, onNavigate }: {
  className?: string;
  onSelect: SelectSession;
  onNavigate?: () => void;
}) {
  const ready = useChatPersistence(state => state.ready);
  const workspaceReady = useWorkspaceStore(state => state.hydrated);
  const allSessions = useChatStore(state => state.sessions);
  const activeId = useChatStore(state => state.activeId);
  const workspaces = useWorkspaceStore(state => state.workspaces);
  const workspaceId = useWorkspaceStore(state => state.activeId);
  const recentIds = useWorkspaceStore(state => state.recentProjectIds);
  const runningSession = useGenerationStore(state => state.active?.sessionId);
  const runningWorkspace = useGenerationStore(state => state.active?.workspaceId);
  const { pathname, search } = useLocation();
  const selectedChat = pathname === '/chat' && !new URLSearchParams(search).has('task');
  const [searchOpen, setSearchOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [earlierOpen, setEarlierOpen] = useState(false);
  const [projectsOpen, setProjectsOpen] = useState(true);
  const deferredQuery = useDeferredValue(query.trim());
  const searchRef = useRef<HTMLInputElement>(null);
  useEffect(() => { if (searchOpen) searchRef.current?.focus(); }, [searchOpen]);
  const projects = useMemo(() => sortRecentWorkspaces(workspaces, allSessions, workspaceId, recentIds), [workspaces, allSessions, workspaceId, recentIds]);
  const results = useMemo(() => {
    const ids = new Set(workspaces.map(workspace => workspace.id));
    const sessions = allSessions.filter(session => {
      const id = sessionWorkspaceId(session);
      return deferredQuery ? ids.has(id) && session.messages.length : id === workspaceId && (session.messages.length || session.id === activeId);
    });
    return searchSessions(sessions, deferredQuery);
  }, [allSessions, workspaces, workspaceId, activeId, deferredQuery]);
  const recentCutoff = Date.now() - 7 * 24 * 60 * 60 * 1000;
  const recent = deferredQuery ? results : results.filter(({ session }) => session.updatedAt >= recentCutoff);
  const earlier = deferredQuery ? [] : results.filter(({ session }) => session.updatedAt < recentCutoff);

  function closeSearch() { setQuery(''); setSearchOpen(false); }
  function choose(id: string, sessionId: string, messageId?: string) {
    useWorkspaceStore.getState().select(id);
    if (sessionId) useChatStore.getState().setActive(sessionId);
    else useChatStore.getState().restoreWorkspaceSession(id);
    onSelect(useChatStore.getState().activeId ?? '', messageId);
  }
  function row(result: SessionSearchResult) {
    const { session } = result;
    return <SessionItem key={session.id} result={result} active={selectedChat && session.id === activeId} running={runningSession === session.id}
      onSelect={(id, messageId) => choose(sessionWorkspaceId(session), id, messageId)} />;
  }

  return <aside className={cn('ns-project-sidebar h-full shrink-0 flex-col', className)} aria-label="项目与会话">
    <div className="ns-sidebar-top">
      <h2>对话</h2>
      <Tooltip><TooltipTrigger asChild><button type="button" aria-label="搜索会话" aria-expanded={searchOpen} className="ns-sidebar-icon" onClick={() => searchOpen ? closeSearch() : setSearchOpen(true)}>{searchOpen ? <X aria-hidden="true" /> : <Search aria-hidden="true" />}</button></TooltipTrigger><TooltipContent>搜索会话</TooltipContent></Tooltip>
      <Tooltip><TooltipTrigger asChild><button type="button" aria-label="新建对话" className="ns-sidebar-icon" disabled={!ready || !workspaceReady} onClick={() => {
        const id = useChatStore.getState().newSession();
        if (id) { closeSearch(); onSelect(id); }
      }}><SquarePen aria-hidden="true" /></button></TooltipTrigger><TooltipContent>新建对话</TooltipContent></Tooltip>
    </div>
    {searchOpen && <div className="ns-project-search" role="search" aria-label="会话历史">
      <Input ref={searchRef} type="search" aria-label="搜索会话标题或消息" placeholder="搜索标题或消息" value={query} autoComplete="off" onChange={event => setQuery(event.target.value)} onKeyDown={event => {
        if (event.key === 'Escape' && !event.nativeEvent.isComposing) { event.preventDefault(); event.stopPropagation(); closeSearch(); }
      }} />
    </div>}
    <div className="ns-sidebar-scroll" aria-busy={query.trim() !== deferredQuery}>
      <section aria-label="对话历史">
        <p className="ns-conversation-list-label" role={deferredQuery ? 'status' : undefined}>{deferredQuery ? `${results.length} 个匹配会话` : recent.length ? '最近 7 天' : '当前对话'}</p>
        {!results.length && !deferredQuery && <div className="ns-session-placeholder"><MessageSquare size={13} aria-hidden="true" />新对话</div>}
        {recent.map(row)}
        {earlier.length > 0 && <><button className="ns-sidebar-fold" type="button" aria-expanded={earlierOpen} onClick={() => setEarlierOpen(value => !value)}><span>更早</span>{earlierOpen ? <ChevronDown size={14} /> : <ChevronRight size={14} />}</button>{earlierOpen && earlier.map(row)}</>}
      </section>
      {!deferredQuery && <>
        <WorkspaceTasks key={workspaceId} workspaceId={workspaceId} onNavigate={onNavigate} />
        <section aria-label="项目">
          <button className="ns-sidebar-fold ns-sidebar-project-heading" type="button" aria-expanded={projectsOpen} onClick={() => setProjectsOpen(value => !value)}><span>项目</span>{projectsOpen ? <ChevronDown size={14} /> : <ChevronRight size={14} />}</button>
          {projectsOpen && <div className="ns-sidebar-projects">
            {projects.map(workspace => <button key={workspace.id} type="button" className="ns-sidebar-project" title={workspace.project_path || workspace.name} aria-current={workspace.id === workspaceId ? 'true' : undefined} aria-label={workspace.name} onClick={() => choose(workspace.id, '')}>
              <Folder size={15} aria-hidden="true" /><span>{workspace.name}</span>{runningWorkspace === workspace.id && <small className="ns-project-run-label">运行中</small>}
            </button>)}
            <button type="button" className="ns-sidebar-project ns-sidebar-add-project" disabled={!workspaceReady} onClick={() => { onNavigate?.(); useUiStore.getState().setProjectPickerOpen(true); }}><Plus size={16} aria-hidden="true" />添加项目</button>
          </div>}
        </section>
      </>}
    </div>
  </aside>;
});
