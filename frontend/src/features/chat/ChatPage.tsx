import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ArrowDown, ArrowUpRight, Box, FileCode2, MessageSquare, Network, Plus, RefreshCw } from "lucide-react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { useBackend } from "@/hooks/useBackend";
import { useChatStore, useChatPersistence } from "./chat-store";
import { cancelGeneration, sendChat, useGenerationStore } from "./chat-runtime";
import { SessionSidebar } from "./SessionSidebar";
import { ChatInput, type ChatSubmitOptions } from "./ChatInput";
import { Button } from "@/components/ui/button";
import { LoadingState } from "@/components/ui/feedback";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { useWorkspaceStore } from "@/stores/workspace-store";
import { activeWorkspaceSession, sessionsInWorkspace } from "./chat-backup";
import { consumedModelChatState, modelChatSessionPlan, readModelChatIntent, resolveModelChatIntent } from './model-chat-intent';

const MessageBubble = lazy(() => import("./MessageBubble").then((module) => ({ default: module.MessageBubble })));
const WELCOME_TIPS = [
  { icon: Network, title: "理解项目架构", text: "这个项目的架构是什么样的？", label: "ARCHITECTURE" },
  { icon: RefreshCw, title: "梳理实现逻辑", text: "解释 gateway 模块的重试机制", label: "IMPLEMENTATION" },
  { icon: FileCode2, title: "分析代码流程", text: "帮我分析 main.go 的启动流程", label: "CODE ANALYSIS" },
];
const PHASE_LABEL = { preparing: "检索项目上下文…", waiting: "等待模型响应…", streaming: "正在生成回复…" };

export function ChatPage() {
  const ready = useChatPersistence((state) => state.ready);
  const { models, modelsReady, modelsError, loadModels } = useBackend();
  const location = useLocation();
  const navigate = useNavigate();
  const allSessions = useChatStore((state) => state.sessions);
  const workspaceId = useWorkspaceStore(state => state.activeId);
  const workspaceReady = useWorkspaceStore(state => state.hydrated);
  const workspaceModelId = useWorkspaceStore(state => state.workspaces.find(item => item.id === state.activeId)?.default_model_id ?? '');
  const sessions = useMemo(() => sessionsInWorkspace(allSessions, workspaceId), [allSessions, workspaceId]);
  const activeId = useChatStore((state) => state.activeId);
  const newSession = useChatStore((state) => state.newSession);
  const setModel = useChatStore((state) => state.setModel);
  const syncModel = useChatStore(state => state.syncModel);
  const modelSelection = useChatStore(state => state.modelSelection);
  const modelSelectionError = useChatStore(state => state.modelSelectionError);
  const [restoredSession, setRestoredSession] = useState<string|null>(null), [restoreAttempt, setRestoreAttempt] = useState(0);
  const pageMounted = useRef(true);
  useEffect(()=>{pageMounted.current=true;return()=>{pageMounted.current=false;};},[]);
  const generation = useGenerationStore((state) => state.active);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [searchTarget, setSearchTarget] = useState<{ sessionId: string; messageId: string; request: number } | null>(null);
  const searchRequestRef = useRef(0);
  const scrollRef = useRef<HTMLDivElement>(null);
  const nearBottom = useRef(true);
  const [showLatest, setShowLatest] = useState(false);
  const pageRef = useRef<HTMLDivElement>(null);
  const intentEntry = `${location.key}:${location.search}`;
  // Capture the originating workspace once; a late metadata response cannot cross a workspace switch.
  const intentRef = useRef({ entry: intentEntry, intent: readModelChatIntent(location.search, location.key, workspaceId) });
  if (intentRef.current.entry !== intentEntry) intentRef.current = { entry: intentEntry, intent: readModelChatIntent(location.search, location.key, workspaceId) };
  const intent = intentRef.current.intent;
  const handledIntents = useRef(new Set<string>());
  const intentResolution = resolveModelChatIntent(intent, { models, modelsReady, modelsError, persistenceReady: ready, workspaceReady, workspaceId, historyState: location.state });
  const intentPending = !!intent && intentResolution.status !== 'consumed';
  const usableModels = useMemo(() => models.filter(model => model.enabled !== false), [models]);
  const session = activeWorkspaceSession(sessions, activeId, workspaceId);
  const streaming = !!generation && generation.sessionId === session?.id;
  const otherGeneration = !!generation && !streaming;
  useEffect(()=>{
    if(!ready||!session||streaming||modelSelection||intentPending)return;
    let current=true;setRestoredSession(null);
    void syncModel(session.id).then(ok=>{if(current&&ok)setRestoredSession(session.id);});
    return()=>{current=false;};
  },[ready,session?.id,streaming,modelSelection,intentPending,syncModel,restoreAttempt,models]);
  const hasSearchTarget = searchTarget?.sessionId === session?.id && !!session?.messages.some((message) => message.id === searchTarget?.messageId);
  const onMessagesMount = useCallback((node: HTMLDivElement | null) => {
    if (node && nearBottom.current && !hasSearchTarget && scrollRef.current) scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
  }, [hasSearchTarget]);

  const selectHistory = useCallback((sessionId: string, messageId?: string) => {
    useChatStore.getState().setActive(sessionId);
    // Choosing history explicitly cancels a pending model start instead of changing that conversation.
    if (intent && intentPending && sessionId) navigate({ pathname: location.pathname, search: location.search, hash: location.hash }, {
      replace: true, state: consumedModelChatState(location.state, intent, workspaceId, sessionId),
    });
    nearBottom.current = !messageId;
    setShowLatest(false);
    setSearchTarget(messageId ? { sessionId, messageId, request: ++searchRequestRef.current } : null);
    setHistoryOpen(false);
  }, [intent, intentPending, location.pathname, location.search, location.hash, location.state, navigate, workspaceId]);

  const acceptModelIntent = useCallback(async (modelId: string) => {
    if (!intent || !ready || !workspaceReady || useWorkspaceStore.getState().activeId !== workspaceId) return;
    if (!models.some(model => model.id === modelId && model.enabled !== false)) return;
    const state = useChatStore.getState();
    if (state.modelSelection) return;
    const current = activeWorkspaceSession(state.sessions, state.activeId, workspaceId);
    const plan = modelChatSessionPlan(current, workspaceId, useGenerationStore.getState().active?.sessionId ?? null);
    const sessionId = plan.create ? state.newSession() : plan.sessionId;
    if (!sessionId) return;
    handledIntents.current.add(intentEntry);
    useChatStore.getState().setActive(sessionId);
    if (!await useChatStore.getState().setModel(modelId)) return;
    if (!pageMounted.current || intentRef.current.entry !== intentEntry || useChatStore.getState().activeId !== sessionId || useWorkspaceStore.getState().activeId !== workspaceId) return;
    setSearchTarget(null);
    nearBottom.current = true;
    setShowLatest(false);
    navigate({ pathname: location.pathname, search: location.search, hash: location.hash }, {
      replace: true, state: consumedModelChatState(location.state, intent, workspaceId, sessionId),
    });
  }, [intent, intentEntry, ready, workspaceReady, workspaceId, models, navigate, location.pathname, location.search, location.hash, location.state]);

  useEffect(() => {
    if (!modelSelection && intent && intentResolution.status === 'ready' && !handledIntents.current.has(intentEntry)) void acceptModelIntent(intentResolution.model.id);
  }, [modelSelection, intent, intentEntry, intentResolution.status, intentResolution.model?.id, acceptModelIntent]);
  useEffect(() => {
    if (!ready || intentPending) return;
    const state = useChatStore.getState();
    const scoped = activeWorkspaceSession(state.sessions, state.activeId, workspaceId);
    if (!scoped) state.newSession();
    else if (scoped.id !== state.activeId) state.setActive(scoped.id);
  }, [activeId, ready, workspaceId, intentPending]);
  useEffect(() => {
    if (!ready || !session || intentPending || hasSearchTarget) return;
    const frame = requestAnimationFrame(() => pageRef.current?.querySelector<HTMLTextAreaElement>('textarea[aria-label="输入问题"]')?.focus({ preventScroll: true }));
    return () => cancelAnimationFrame(frame);
  }, [ready, session?.id, intentPending, hasSearchTarget]);
  useEffect(() => {
    if (hasSearchTarget) return;
    if (searchTarget) setSearchTarget(null);
    if (nearBottom.current && scrollRef.current) scrollRef.current.scrollTop = session?.messages.length ? scrollRef.current.scrollHeight : 0;
  }, [session?.messages, activeId, hasSearchTarget, searchTarget]);

  const send = useCallback((text: string, options?: Partial<ChatSubmitOptions> & { regenerateFrom?: string }) => {
    if (!session || !usableModels.length || intentPending || modelSelection || restoredSession !== session.id) return false;
    const started = sendChat(session.id, text, options);
    if (started) { setSearchTarget(null); nearBottom.current = true; setShowLatest(false); }
    return started;
  }, [session?.id, usableModels.length, intentPending, modelSelection, restoredSession]);
  const lastAsstId = session ? [...session.messages].reverse().find((message) => message.role === "assistant")?.id : undefined;
  const latest = () => {
    setSearchTarget(null);
    nearBottom.current = true;
    setShowLatest(false);
    if (scrollRef.current) scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
  };
  const number = Math.max(1, sessions.findIndex((item) => item.id === activeId) + 1).toString().padStart(3, "0");

  return (
    <div ref={pageRef} className="ns-chat flex h-full min-h-0">
      <SessionSidebar className="hidden xl:flex" onSelect={selectHistory} />
      <Dialog open={historyOpen} onOpenChange={setHistoryOpen}>
        <DialogContent
          className="ns-nav-dialog top-0 left-0 h-dvh max-w-[320px] translate-x-0 translate-y-0 gap-0 rounded-none border-r border-border-soft bg-bg-raised p-0 xl:hidden"
          onEscapeKeyDown={(event) => {
            const target = event.target;
            if (event.isComposing || (target instanceof HTMLInputElement && target.type === "search" && target.value)) event.preventDefault();
          }}
          onCloseAutoFocus={(event) => { if (hasSearchTarget) event.preventDefault(); }}
        >
          <DialogTitle className="sr-only">会话历史</DialogTitle>
          <DialogDescription className="sr-only">搜索本机保存的标题和消息，选择结果打开会话。</DialogDescription>
          <SessionSidebar className="w-full border-r-0 [&>div:first-child]:pr-12" onSelect={selectHistory} />
        </DialogContent>
      </Dialog>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="ns-chat-session-header flex min-h-[76px] shrink-0 items-center gap-3 border-b border-border-soft px-5 py-3 lg:px-8">
          <Button type="button" variant="ghost" size="icon" className="xl:hidden" aria-label="打开会话历史" onClick={() => setHistoryOpen(true)}><MessageSquare aria-hidden="true" /></Button>
          <div className="min-w-0 flex-1">
            <p className="ns-kicker text-[10px] tracking-[0.14em] text-text-3">CONVERSATION / {number}</p>
            <h1 className="mt-1 truncate text-base font-semibold text-text-1">{session?.title ?? "新会话"}</h1>
          </div>
          <div className="hidden items-center gap-2 text-xs text-text-2 md:flex"><Box size={15} aria-hidden="true" />{intentPending ? (models.find(model => model.id === intent?.modelId)?.name || intent?.modelId || '未指定模型') : session?.modelId ? `当前会话 · ${models.find(model=>model.id===session.modelId)?.name || '模型已删除或不可用'}` : workspaceModelId ? `工作区默认 · ${models.find(model=>model.id===workspaceModelId)?.name || '模型不可用'}` : `全局默认 · ${models.find(model=>model.is_default&&model.enabled!==false)?.name || models.find(model=>model.enabled!==false)?.name || '未配置模型'}`}</div>
          <Button variant="outline" size="sm" className="xl:hidden" aria-label="新会话" onClick={() => selectHistory(newSession())}><Plus data-icon="inline-start" aria-hidden="true" /><span className="hidden sm:inline">新会话</span></Button>
        </header>
        {modelSelectionError && <div role="alert" className="ns-form-error mx-5 my-2"><span>{modelSelectionError}</span><Button variant="ghost" size="sm" onClick={()=>{if(intentPending&&intentResolution.status==='ready')void acceptModelIntent(intentResolution.model.id);else setRestoreAttempt(value=>value+1);}}>重试读取或切换</Button></div>}
        {modelSelection && <p role="status" className="px-5 py-2 text-xs text-text-3">正在验证模型连接…</p>}
        {intentPending && intentResolution.message && (
          <div role={intentResolution.status === 'loading' ? 'status' : 'alert'} className="flex shrink-0 flex-wrap items-center gap-3 border-b border-border-soft px-5 py-3 text-xs">
            <span className="flex-1">{intentResolution.message}</span>
            {intentResolution.status !== 'loading' && ready && workspaceReady && usableModels.length > 0 && <select aria-label="选择可用对话模型" value="" onChange={event => acceptModelIntent(event.target.value)} className="border border-border-soft bg-bg-raised px-2 py-1 text-text-1"><option value="" disabled>选择可用模型</option>{usableModels.map(model => <option key={model.id} value={model.id}>{model.name || model.model_name || model.id}</option>)}</select>}
            {intentResolution.status === 'unavailable' && <Button variant="outline" size="sm" onClick={() => { void loadModels(); }}>重试读取模型</Button>}
            {intentResolution.status !== 'loading' && <Link to="/gateway" className="underline underline-offset-4">管理模型</Link>}
          </div>
        )}
        {otherGeneration && generation && (
          <div role="status" className="flex flex-wrap items-center justify-between gap-2 border-b border-border-soft bg-brand-dim px-5 py-2 text-xs">
            <span>「{sessions.find((item) => item.id === generation.sessionId)?.title ?? "其他会话"}」正在生成</span>
            <Button variant="ghost" size="sm" onClick={() => {
              useWorkspaceStore.getState().select(generation.workspaceId ?? 'local');
              selectHistory(generation.sessionId);
            }}>返回生成中的会话 <ArrowUpRight data-icon="inline-end" /></Button>
          </div>
        )}
        {hasSearchTarget && (
          <div className="flex shrink-0 items-center justify-between gap-2 border-b border-border-soft px-5 py-2"><span role="status" className="text-xs text-text-3">已定位到匹配消息</span><Button variant="ghost" size="sm" onClick={latest}>返回最新消息</Button></div>
        )}
        <div ref={scrollRef} className="relative min-h-0 flex-1 overflow-y-auto" aria-label="对话消息" onScroll={() => {
          const element = scrollRef.current;
          if (!element) return;
          const atBottom = element.scrollHeight - element.scrollTop - element.clientHeight < 100;
          nearBottom.current = atBottom;
          setShowLatest(!atBottom && !hasSearchTarget);
        }}>
          {!session || !session.messages.length ? (
            <div className="ns-chat-empty mx-auto flex min-h-full w-full max-w-4xl flex-col justify-center px-6 py-10 sm:px-10 lg:px-14">
              <p className="ns-kicker mb-5 text-[10px] tracking-[0.22em] text-text-3">NOOSPHERE / INTELLIGENCE TERMINAL</p>
              <h2 className="text-[38px] font-light leading-[1.08] tracking-[-0.04em] text-text-1 sm:text-[56px]">从一个问题，<br /><strong className="font-semibold">连接你的知识。</strong></h2>
              <div className="mb-7 mt-6 h-px w-14 bg-brand" />
              <p className="max-w-lg text-[13px] leading-7 text-text-2">在这里讨论代码、梳理思路。CodeLens 与 Nebula 将为对话提供项目上下文。</p>
              <div className="mt-8 grid border-t border-border-soft">
                {WELCOME_TIPS.map(({ icon: Icon, text, title, label }, index) => (
                  <button type="button" key={text} disabled={!!generation || !usableModels.length || intentPending} onClick={() => send(text)} className="group flex min-h-16 items-center gap-4 border-b border-border-soft py-3 text-left transition-colors hover:bg-secondary/50 focus-visible:outline-2 focus-visible:outline-brand disabled:cursor-not-allowed disabled:opacity-55">
                    <span className="font-mono text-[10px] text-text-3">0{index + 1}</span><Icon size={17} strokeWidth={1.5} className="text-text-2" aria-hidden="true" /><span className="flex-1 text-sm font-medium">{title}</span><span className="hidden text-[9px] tracking-widest text-text-3 sm:block">{label}</span><ArrowUpRight size={16} aria-hidden="true" />
                  </button>
                ))}
              </div>
              {!models.length && <p className="mt-5 text-xs leading-6 text-text-3">开始之前，前往 <Link to="/gateway" className="text-text-1 underline underline-offset-4">Gateway 配置模型</Link>。</p>}
            </div>
          ) : (
            <Suspense fallback={<LoadingState label="正在打开对话…" />}>
              <div className="mx-auto max-w-5xl py-5" ref={onMessagesMount}>
                {session.messages.map((message) => (
                  <MessageBubble key={message.id} message={message} workspaceId={session.workspaceId ?? 'local'} sessionId={session.id} highlightRequest={hasSearchTarget && searchTarget?.messageId === message.id ? searchTarget.request : undefined} scrollContainer={scrollRef}
                    onRegenerate={message.id === lastAsstId && !generation && models.length ? () => send("", { regenerateFrom: message.id, agentMode: message.agentMode, reviewedActions: message.reviewedActions }) : undefined} />
                ))}
              </div>
            </Suspense>
          )}
        </div>
        {showLatest && <div className="flex justify-center"><Button size="sm" variant="outline" onClick={latest}><ArrowDown data-icon="inline-start" />返回最新消息</Button></div>}
        {streaming && generation && <div role="status" className="px-6 pt-2 text-xs text-text-3"><span className="mr-2 inline-block size-1.5 bg-brand" />{PHASE_LABEL[generation.phase]}</div>}
        <ChatInput key={`${workspaceId}:${session?.id ?? ''}`} models={models} modelId={session?.modelId ?? ""} defaultModelId={workspaceModelId} onModelChange={id => { if (intentPending) void acceptModelIntent(id); else void setModel(id); }} streaming={streaming} busy={otherGeneration || !ready || !session || intentPending || !!modelSelection || restoredSession !== session.id} onSend={send} onStop={() => { void cancelGeneration(); }} />
      </div>
    </div>
  );
}
