import { memo, useDeferredValue, useId, useMemo, useState } from "react";
import { MessageSquare, Plus, Search, Trash2, X } from "lucide-react";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/feedback";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useChatStore } from "./chat-store";
import { searchSessions, type SessionSearchResult } from "./session-search";
import { useWorkspaceStore } from "@/stores/workspace-store";
import { sessionsInWorkspace } from "./chat-backup";

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
  onSelect,
  onRemove,
}: {
  result: SessionSearchResult;
  active: boolean;
  onSelect: SelectSession;
  onRemove: (id: string) => void;
}) {
  const { session, match } = result;
  const messageMatch = match?.messageId ? match : undefined;
  return (
    <div
      className={cn(
        "group relative border-b border-border-soft transition-colors",
        active
          ? "bg-brand-dim text-text-1"
          : "hover:bg-secondary/50"
      )}
    >
      <button
        type="button"
        onClick={() => onSelect(session.id, match?.messageId)}
        aria-label={`打开会话 ${session.title}${messageMatch ? "，定位匹配消息" : ""}`}
        aria-current={active ? "true" : undefined}
        className="flex min-h-[56px] w-full min-w-0 items-center gap-3 px-3 py-3 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand/40"
      >
        <MessageSquare
          size={14}
          strokeWidth={1.6}
          className={cn("shrink-0", active ? "text-text-2" : "text-text-3")}
        />
        <span className="flex min-w-0 flex-1 flex-col gap-1 pr-7">
          <span className={cn("truncate text-[13px] font-normal leading-6", active ? "font-medium text-text-1" : "text-text-2")}>
            {match?.source === "title" ? <MatchText match={match} /> : session.title}
          </span>
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

// Separate the list from the input so urgent keystrokes can render without rescanning history.
const SessionResults = memo(function SessionResults({ query, statusId, onSelect }: {
  query: string;
  statusId: string;
  onSelect: SelectSession;
}) {
  const allSessions = useChatStore((s) => s.sessions);
  const workspaceId = useWorkspaceStore(s => s.activeId);
  const sessions = useMemo(() => sessionsInWorkspace(allSessions, workspaceId), [allSessions, workspaceId]);
  const activeId = useChatStore((s) => s.activeId);
  const removeSession = useChatStore((s) => s.removeSession);
  const results = useMemo(() => searchSessions(sessions, query), [sessions, query]);

  return (
    <>
      <p id={statusId} role="status" className="px-3 pb-2 text-xs text-text-3">
        {query ? `${results.length} 个匹配会话` : `${sessions.length} 个会话 · 最近更新`}
      </p>
      <ScrollArea className="min-h-0 flex-1 pr-2 pl-[9px]">
        <div className="flex flex-col gap-1 pb-3">
          {results.length === 0 && (
            <EmptyState
              title={query ? "没有找到匹配会话" : "暂无会话"}
              description={query ? "试试标题或消息中的其他关键词。" : "点击上方新建，开始一次对话。"}
              className="px-3 py-8"
            />
          )}
          {results.map((result) => (
            <SessionItem
              key={result.session.id}
              result={result}
              active={result.session.id === activeId}
              onSelect={onSelect}
              onRemove={removeSession}
            />
          ))}
        </div>
      </ScrollArea>
    </>
  );
});

export const SessionSidebar = memo(function SessionSidebar({ className, onSelect }: {
  className?: string;
  onSelect: SelectSession;
}) {
  const newSession = useChatStore((s) => s.newSession);
  const [query, setQuery] = useState("");
  const deferredQuery = useDeferredValue(query.trim());
  const inputId = useId();
  const statusId = useId();

  return (
    <aside aria-label="会话历史" className={cn("flex h-full w-[264px] shrink-0 flex-col border-r border-border-soft bg-bg-raised/25", className)}>
      <div className="flex h-[74px] shrink-0 items-center border-b border-border-subtle px-3">
        <Button
          variant="ghost"
          className="h-11 w-full justify-start gap-3 rounded-[8px] px-2 text-[16px] font-medium text-text-1"
          onClick={() => { setQuery(""); onSelect(newSession()); }}
        >
          <Plus data-icon="inline-start" aria-hidden="true" />
          新会话
        </Button>
      </div>
      <div role="search" aria-label="搜索会话历史" className="flex shrink-0 flex-col gap-2 px-3 py-3">
        <label htmlFor={inputId} className="flex items-center gap-2 text-xs text-text-3">
          <Search size={14} aria-hidden="true" /> 搜索会话
        </label>
        <div className="flex items-center gap-1">
          <Input
            id={inputId}
            type="search"
            className="[&::-webkit-search-cancel-button]:hidden"
            placeholder="搜索标题或消息…"
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
          <Button variant="ghost" size="icon-sm" aria-label="清空搜索" disabled={!query} onClick={() => setQuery("")}>
            <X aria-hidden="true" />
          </Button>
        </div>
      </div>
      <div className="flex min-h-0 flex-1 flex-col" aria-busy={query.trim() !== deferredQuery}>
        <SessionResults query={deferredQuery} statusId={statusId} onSelect={onSelect} />
      </div>
    </aside>
  );
});
