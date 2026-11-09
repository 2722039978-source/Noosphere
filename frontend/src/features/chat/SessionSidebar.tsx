import { memo, useDeferredValue, useId, useMemo, useState } from "react";
import { ArrowUpRight, BookOpen, ChevronRight, Folder, Layers3, MessageSquare, Plus, Search, Settings2, SquareCode, Trash2, X } from "lucide-react";
import { Link, NavLink, useLocation } from 'react-router-dom';
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/feedback";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useChatPersistence, useChatStore } from "./chat-store";
import { searchSessions, type SessionSearchResult } from "./session-search";
import { useWorkspaceStore } from "@/stores/workspace-store";
import { sessionWorkspaceId } from './chat-backup';
import { sortRecentWorkspaces } from './workspace-recency';
import { useGenerationStore } from './chat-runtime';
import { WorkspaceHub } from '@/components/WorkspaceHub';
import { modelSection } from '@/app/model-navigation';
import './project-sidebar.css';

type SelectSession = (sessionId: string, messageId?: string) => void;

function MatchText({ match }: { match: NonNullable<SessionSearchResult["match"]> }) {
  return (
    <>
      {match.snippet.slice(0, match.matchStart)}
      <mark className="rounded-sm bg-brand/20 text-inherit">{match.snippet.slice(match.matchStart, match.matchEnd)}</mark>
      {match.snippet.slice(match.matchEnd)}
    </>
  );
}

const SessionItem = memo(function SessionItem({
  result,
  active,
  running,
  onSelect,
  onRemove,
}: {
  result: SessionSearchResult;
  active: boolean;
  running: boolean;
  onSelect: SelectSession;
  onRemove: (id: string) => void;
}) {
  const { session, match } = result;
  const messageMatch = match?.messageId ? match : undefined;
  return (
    <div
      className="ns-project-session group relative" data-active={active}
    >
      <button
        type="button"
        onClick={() => onSelect(session.id, match?.messageId)}
        aria-label={`打开会话 ${session.title}${messageMatch ? "，定位匹配消息" : ""}`}
        title={session.title}
        aria-current={active ? "true" : undefined}
        className="flex min-h-10 w-full min-w-0 items-center gap-2 px-2 py-2 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand/40"
      >
        <MessageSquare
          size={14}
          strokeWidth={1.6}
          className={cn("shrink-0", active ? "text-text-2" : "text-text-3")}
        />
        <span className="flex min-w-0 flex-1 flex-col gap-1 pr-7">
          <span className={cn("truncate text-[13px] leading-5", active ? "font-medium text-text-1" : "text-text-2")}>
            {match?.source === "title" ? <MatchText match={match} /> : session.title}
          </span>
          {running && <small className="ns-project-run-label">运行中</small>}
          {messageMatch && (
            <span className="line-clamp-2 break-all text-xs leading-5 text-text-3">
              {messageMatch.source === "user" ? "你：" : "助手："}<MatchText match={messageMatch} />
            </span>
          )}
        </span>
      </button>
      <Tooltip>
        <TooltipTrigger asChild>
          <button
            type="button"
            onClick={() => onRemove(session.id)}
            aria-label={`删除会话 ${session.title}`}
            className="absolute top-1/2 right-1 flex size-8 -translate-y-1/2 items-center justify-center rounded-[6px] text-text-3 hover:bg-bg-elevated hover:text-err focus-visible:opacity-100 xl:opacity-0 xl:group-hover:opacity-100"
          >
            <Trash2 size={13} />
          </button>
        </TooltipTrigger>
        <TooltipContent>删除会话</TooltipContent>
      </Tooltip>
    </div>
  );
});

const SessionResults = memo(function SessionResults({ query, statusId, onSelect, onNavigate }: {
  query: string;
  statusId: string;
  onSelect: SelectSession;
  onNavigate?: () => void;
}) {
  const allSessions = useChatStore((s) => s.sessions);
  const workspaces = useWorkspaceStore(s => s.workspaces);
  const workspaceId = useWorkspaceStore(s => s.activeId);
  const recentProjectIds = useWorkspaceStore(s => s.recentProjectIds);
  const activeId = useChatStore((s) => s.activeId);
  const removeSession = useChatStore((s) => s.removeSession);
  const runningSession = useGenerationStore(state => state.active?.sessionId);
  const runningWorkspace = useGenerationStore(state => state.active?.workspaceId);
  const { pathname } = useLocation();
  const [collapsedId, setCollapsedId] = useState<string | null>(null);
  const knownWorkspaces = useMemo(() => new Set(workspaces.map(workspace => workspace.id)), [workspaces]);
  const results = useMemo(() => searchSessions(allSessions.filter(session => knownWorkspaces.has(sessionWorkspaceId(session)) && (session.messages.length || session.id === activeId)), query), [allSessions, knownWorkspaces, query, activeId]);
  const groups = useMemo(() => {
    const byWorkspace = new Map<string, SessionSearchResult[]>();
    for (const result of results) {
      const id = sessionWorkspaceId(result.session);
      const group = byWorkspace.get(id) ?? [];
      if (!byWorkspace.has(id)) byWorkspace.set(id, group);
      group.push(result);
    }
    return sortRecentWorkspaces(workspaces, allSessions, workspaceId, recentProjectIds).map(workspace => ({ workspace, sessions: byWorkspace.get(workspace.id) ?? [] }));
  }, [results, workspaces, allSessions, workspaceId, recentProjectIds]);

  return (
    <>
      <p id={statusId} role="status" className="ns-project-list-heading">{query ? `${results.length} 个匹配会话` : '最近项目'}</p>
      <ScrollArea className="min-h-0 flex-1">
        <div className="ns-project-list flex flex-col">
          {query && !results.length && (
            <EmptyState
              title="没有找到匹配会话"
              description="试试标题或消息中的其他关键词。"
              className="px-3 py-8"
            />
          )}
          {groups.map(({ workspace, sessions }) => {
            if (query && !sessions.length) return null;
            const active = workspace.id === workspaceId;
            const expanded = Boolean(query) || active && collapsedId !== workspace.id;
            return <section key={workspace.id} className="ns-project-group" data-active={active} aria-label={workspace.name}>
              <button type="button" className="ns-project-heading" aria-expanded={expanded} onClick={() => {
                if (active && pathname === '/chat') { setCollapsedId(expanded ? workspace.id : null); return; }
                useWorkspaceStore.getState().select(workspace.id);
                const chat = useChatStore.getState();
                chat.restoreWorkspaceSession(workspace.id);
                setCollapsedId(null);
                onSelect(useChatStore.getState().activeId ?? '');
              }}>
                <ChevronRight size={14} className="ns-project-chevron" aria-hidden="true" /><Folder size={15} aria-hidden="true" />
                <span>{workspace.name}</span>{runningWorkspace === workspace.id && <small className="ns-project-run-label">运行中</small>}
              </button>
              {expanded && <div className="ns-project-group-content">
                {active && !query && <div className="ns-project-tools" aria-label="项目资料">
                  <NavLink to="/codelens" className="ns-project-tool" onClick={onNavigate}><SquareCode size={14} />代码</NavLink>
                  <NavLink to="/nebula" className="ns-project-tool" onClick={onNavigate}><BookOpen size={14} />记忆</NavLink>
                  <WorkspaceHub triggerLabel="任务" />
                </div>}
                {!sessions.length && <p className="ns-project-no-chats">暂无对话</p>}
                {sessions.map(result => <SessionItem key={result.session.id} result={result} active={pathname === '/chat' && active && result.session.id === activeId} running={runningSession === result.session.id} onSelect={(id, messageId) => {
                  useWorkspaceStore.getState().select(workspace.id);
                  setCollapsedId(null);
                  onSelect(id, messageId);
                }} onRemove={removeSession} />)}
              </div>}
            </section>;
          })}
        </div>
      </ScrollArea>
    </>
  );
});

export const SessionSidebar = memo(function SessionSidebar({ className, onSelect, onNavigate }: {
  className?: string;
  onSelect: SelectSession;
  onNavigate?: () => void;
}) {
  const newSession = useChatStore((s) => s.newSession);
  const ready = useChatPersistence(s => s.ready);
  const workspaceReady = useWorkspaceStore(s => s.hydrated);
  const { pathname } = useLocation();
  const [query, setQuery] = useState("");
  const deferredQuery = useDeferredValue(query.trim());
  const inputId = useId();
  const statusId = useId();

  return (
    <aside aria-label="项目与会话" className={cn("ns-project-sidebar h-full shrink-0 flex-col", className)}>
      <Link to="/home" className="ns-project-brand" aria-label="打开动效主页" onClick={onNavigate}>
        <strong>NOOSPHERE</strong><span>动效主页 <ArrowUpRight size={12} aria-hidden="true" /></span>
      </Link>
      <div className="ns-project-actions">
        <WorkspaceHub triggerLabel="打开项目" createProject onProjectSaved={() => { setQuery(''); onSelect(''); }} />
        <Button
          variant="ghost"
          className="ns-project-tool"
          disabled={!ready || !workspaceReady}
          onClick={() => { const id = newSession(); if (id) { setQuery(""); onSelect(id); } }}
        >
          <Plus data-icon="inline-start" aria-hidden="true" />
          新建对话
        </Button>
      </div>
      <div role="search" aria-label="搜索会话历史" className="ns-project-search shrink-0">
        <label htmlFor={inputId} className="sr-only">搜索会话</label>
        <div className="relative">
          <Search className="ns-project-search-icon" size={15} aria-hidden="true" />
          <Input
            id={inputId}
            type="search"
            className="ns-project-search-input [&::-webkit-search-cancel-button]:hidden"
            placeholder="搜索会话标题或消息"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Escape" && query && !event.nativeEvent.isComposing) {
                event.preventDefault();
                event.stopPropagation();
                setQuery("");
              }
            }}
            aria-describedby={statusId}
            autoComplete="off"
          />
          {query && <Button variant="ghost" size="icon-sm" className="absolute inset-y-0 right-1 my-auto" aria-label="清空搜索" onClick={() => setQuery("")}>
            <X aria-hidden="true" />
          </Button>}
        </div>
      </div>
      <div className="flex min-h-0 flex-1 flex-col" aria-busy={query.trim() !== deferredQuery}>
        <SessionResults query={deferredQuery} statusId={statusId} onSelect={onSelect} onNavigate={onNavigate} />
      </div>
      <nav className="ns-project-footer" aria-label="主导航">
        <Link to="/model-hub" className="ns-project-tool" onClick={onNavigate} aria-current={modelSection(pathname) ? 'page' : undefined}><Layers3 size={16} />模型</Link>
        <Link to="/settings" className="ns-project-tool" onClick={onNavigate} aria-current={pathname === '/settings' || pathname === '/devops' ? 'page' : undefined}><Settings2 size={16} />设置</Link>
      </nav>
    </aside>
  );
});
