import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ArrowDown, ArrowUpRight } from "lucide-react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { useBackend } from "@/hooks/useBackend";
import { useChatStore, useChatPersistence } from "./chat-store";
import { cancelGeneration, sendChat, useGenerationStore } from "./chat-runtime";
import { ChatInput, type ChatSubmitOptions } from "./ChatInput";
import { Button } from "@/components/ui/button";
import { LoadingState } from "@/components/ui/feedback";
import { useWorkspaceStore } from "@/stores/workspace-store";
import { activeWorkspaceSession, sessionsInWorkspace } from "./chat-backup";
import { consumedModelChatState, modelChatSessionPlan, readModelChatIntent, resolveModelChatIntent } from './model-chat-intent';
import { chatCapabilityBlocker, selectedChatModel } from '@/lib/model-capabilities';
import { generationLabel } from './chat-progress';
import './chat-layout.css';

const MessageBubble = lazy(() => import("./MessageBubble").then((module) => ({ default: module.MessageBubble })));

export function ChatPage() {
  const ready = useChatPersistence((state) => state.ready);
  const { models, modelsReady, modelsError, loadModels } = useBackend();
  const location = useLocation();
  const navigate = useNavigate();
  const allSessions = useChatStore((state) => state.sessions);
  const workspaceId = useWorkspaceStore(state => state.activeId);
  const workspaceReady = useWorkspaceStore(state => state.hydrated);
  const workspaceName = useWorkspaceStore(state => state.workspaces.find(item => item.id === state.activeId)?.name ?? '本地工作区');
  const workspaceModelId = useWorkspaceStore(state => state.workspaces.find(item => item.id === state.activeId)?.default_model_id ?? '');
  const sessions = useMemo(() => sessionsInWorkspace(allSessions, workspaceId), [allSessions, workspaceId]);
  const activeId = useChatStore((state) => state.activeId);
  const setModel = useChatStore((state) => state.setModel);
  const syncModel = useChatStore(state => state.syncModel);
  const modelSelection = useChatStore(state => state.modelSelection);
  const modelSelectionError = useChatStore(state => state.modelSelectionError);
  const [restoredSession, setRestoredSession] = useState<string|null>(null), [restoreAttempt, setRestoreAttempt] = useState(0);
  const pageMounted = useRef(true);
  useEffect(()=>{pageMounted.current=true;return()=>{pageMounted.current=false;};},[]);
  const generation = useGenerationStore((state) => state.active);
  const [capabilityError, setCapabilityError] = useState('');
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
  const session = sessions.find(item => item.id === activeId);
  const chatKey = `${workspaceId}:${session?.id ?? ''}`;
  useEffect(() => { setCapabilityError(''); }, [session?.id, session?.modelId, workspaceModelId, models]);
  const streaming = !!generation && generation.sessionId === session?.id;
  const generatingMessage = streaming ? session?.messages.find(message => message.id === generation.messageId) : undefined;
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
  }, [intent, intentPending, location.pathname, location.search, location.hash, location.state, navigate, workspaceId]);

  useEffect(() => {
    const selection = location.state?.historySelection;
    if (!selection || selection.sessionId !== useChatStore.getState().activeId) {
      setSearchTarget(null);
      nearBottom.current = true;
      return;
    }
    nearBottom.current = false;
    setShowLatest(false);
    setSearchTarget({ sessionId: selection.sessionId, messageId: selection.messageId, request: ++searchRequestRef.current });
  }, [location.key, location.state]);

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
    if (!ready || !workspaceReady || intentPending || hasSearchTarget) return;
    const frame = requestAnimationFrame(() => pageRef.current?.querySelector<HTMLTextAreaElement>('textarea[aria-label="输入问题"]')?.focus({ preventScroll: true }));
    return () => cancelAnimationFrame(frame);
  }, [ready, workspaceReady, session?.id, intentPending, hasSearchTarget]);
  useEffect(() => {
    if (hasSearchTarget) return;
    if (searchTarget) setSearchTarget(null);
    if (nearBottom.current && scrollRef.current) scrollRef.current.scrollTop = session?.messages.length ? scrollRef.current.scrollHeight : 0;
  }, [session?.messages, activeId, hasSearchTarget, searchTarget]);

  const send = useCallback((text: string, options?: Partial<ChatSubmitOptions> & { regenerateFrom?: string }) => {
    if (!ready || !workspaceReady || !usableModels.length || intentPending || modelSelection || (session && restoredSession !== session.id)) return false;
    const state = useChatStore.getState(), workspace = useWorkspaceStore.getState();
    const current = state.sessions.find(item => item.id === state.activeId && (item.workspaceId ?? 'local') === workspaceId);
    // Attachments are read asynchronously; recheck the live binding before any
    // history mutation rather than relying on the pre-read render closure.
    if (state.modelSelection || workspace.activeId !== workspaceId || current?.id !== session?.id) return false;
    const currentDefault = workspace.workspaces.find(item => item.id === workspaceId)?.default_model_id ?? '';
    const blocker = chatCapabilityBlocker(selectedChatModel(models, current?.modelId ?? '', currentDefault), !!options?.agentMode);
    if (blocker) { setCapabilityError(blocker); return false; }
    setCapabilityError('');
    const id = current?.id ?? state.newSession();
    const started = !!id && sendChat(id, text, options);
    if (started) { setSearchTarget(null); nearBottom.current = true; setShowLatest(false); }
    return started;
  }, [session, models, workspaceId, ready, workspaceReady, usableModels.length, intentPending, modelSelection, restoredSession]);
  const lastAsstId = session ? [...session.messages].reverse().find((message) => message.role === "assistant")?.id : undefined;
  const latest = () => {
    setSearchTarget(null);
    nearBottom.current = true;
    setShowLatest(false);
    if (scrollRef.current) scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
  };
  return (
    <div ref={pageRef} className="ns-chat flex h-full min-h-0">
      <div className="ns-chat-column flex min-w-0 flex-1 flex-col">
        <header className="ns-chat-session-header shrink-0 border-b border-border-soft">
          <div className="ns-chat-rail">
            <p className="truncate text-xs leading-5 text-text-3">{workspaceName}</p>
            <h1 className="mt-1 truncate text-text-1" title={session?.title ?? "新对话"}>{session?.title ?? "新对话"}</h1>
          </div>
        </header>
        {modelSelectionError && <div role="alert" className="ns-chat-rail ns-form-error my-2"><span>{modelSelectionError}</span><Button variant="ghost" size="sm" onClick={()=>{if(intentPending&&intentResolution.status==='ready')void acceptModelIntent(intentResolution.model.id);else setRestoreAttempt(value=>value+1);}}>重试读取或切换</Button></div>}
        {modelSelection && <p role="status" className="ns-chat-rail py-2 text-xs text-text-3">正在验证模型连接…</p>}
        {capabilityError && <p role="alert" className="ns-chat-rail ns-form-error my-2">{capabilityError}</p>}
        {intentPending && intentResolution.message && (
          <div role={intentResolution.status === 'loading' ? 'status' : 'alert'} className="ns-chat-rail flex shrink-0 flex-wrap items-center gap-3 border-b border-border-soft py-3 text-xs">
            <span className="min-w-0 flex-1">{intentResolution.message}</span>
            {intentResolution.status !== 'loading' && ready && workspaceReady && usableModels.length > 0 && <select aria-label="选择可用对话模型" value="" onChange={event => acceptModelIntent(event.target.value)} className="border border-border-soft bg-bg-raised px-2 py-1 text-text-1"><option value="" disabled>选择可用模型</option>{usableModels.map(model => <option key={model.id} value={model.id}>{model.name || model.model_name || model.id}</option>)}</select>}
            {intentResolution.status === 'unavailable' && <Button variant="outline" size="sm" onClick={() => { void loadModels(); }}>重试读取模型</Button>}
            {intentResolution.status !== 'loading' && <Link to="/model-hub" className="underline underline-offset-4">管理模型</Link>}
          </div>
        )}
        {otherGeneration && generation && (
          <div role="status" className="ns-chat-rail flex flex-wrap items-center justify-between gap-2 border-b border-border-soft py-2 text-xs">
            <span>「{sessions.find((item) => item.id === generation.sessionId)?.title ?? "其他会话"}」正在生成</span>
            <Button variant="ghost" size="sm" onClick={() => {
              useWorkspaceStore.getState().select(generation.workspaceId ?? 'local');
              selectHistory(generation.sessionId);
            }}>返回生成中的会话 <ArrowUpRight data-icon="inline-end" /></Button>
          </div>
        )}
        {hasSearchTarget && (
          <div className="ns-chat-rail flex shrink-0 items-center justify-between gap-2 border-b border-border-soft py-2"><span role="status" className="text-xs text-text-3">已定位到匹配消息</span><Button variant="ghost" size="sm" onClick={latest}>返回最新消息</Button></div>
        )}
        <div key={`messages:${chatKey}`} ref={scrollRef} className="ns-project-chat-transition relative min-h-0 flex-1 overflow-y-auto" aria-label="对话消息" onScroll={() => {
          const element = scrollRef.current;
          if (!element) return;
          const atBottom = element.scrollHeight - element.scrollTop - element.clientHeight < 100;
          nearBottom.current = atBottom;
          setShowLatest(!atBottom && !hasSearchTarget);
        }}>
          {!session || !session.messages.length ? (
            <div className="ns-chat-rail ns-chat-empty flex min-h-full flex-col justify-center">
              <h2 className="text-text-1">开始对话</h2>
              <p className="mt-2 text-sm leading-6 text-text-2">在下方输入问题，或从侧栏继续已有对话。</p>
              {modelsReady && !models.length && <p className="mt-3 text-sm text-text-2"><Link to="/model-hub" className="underline underline-offset-4">添加模型</Link>后即可发送消息。</p>}
            </div>
          ) : (
            <Suspense fallback={<LoadingState label="正在打开对话…" />}>
              <div className="ns-chat-rail ns-chat-messages" ref={onMessagesMount}>
                {session.messages.map((message) => (
                  <MessageBubble key={message.id} message={message} workspaceId={session.workspaceId ?? 'local'} sessionId={session.id} highlightRequest={hasSearchTarget && searchTarget?.messageId === message.id ? searchTarget.request : undefined} scrollContainer={scrollRef}
                    onRegenerate={message.id === lastAsstId && !generation && models.length ? () => send("", { regenerateFrom: message.id, agentMode: message.agentMode, reviewedActions: message.reviewedActions }) : undefined} />
                ))}
              </div>
            </Suspense>
          )}
        </div>
        {showLatest && <div className="flex justify-center"><Button size="sm" variant="outline" onClick={latest}><ArrowDown data-icon="inline-start" />返回最新消息</Button></div>}
        {streaming && generation && <div role="status" className="ns-chat-rail pt-2 text-xs text-text-3"><span className="mr-2 inline-block size-1.5 bg-brand" />{generationLabel(generatingMessage, generation.phase)}</div>}
        <ChatInput key={`composer:${chatKey}`} models={models} modelId={session?.modelId ?? ""} defaultModelId={workspaceModelId} onModelChange={id => {
          if (intentPending) { void acceptModelIntent(id); return; }
          if (session || useChatStore.getState().newSession()) void setModel(id);
        }} streaming={streaming} busy={otherGeneration || !ready || !workspaceReady || intentPending || !!modelSelection || (!!session && restoredSession !== session.id)} onSend={send} onStop={() => { void cancelGeneration(); }} />
      </div>
    </div>
  );
}
