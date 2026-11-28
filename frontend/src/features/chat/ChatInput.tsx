import { useEffect, useRef, useState } from "react";
import { AtSign, Paperclip, Plus, Send, Square, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { ModelConfig } from "@/hooks/useBackend";
import { agentCapability, chatCapabilityBlocker, imageInputBlocker } from "@/lib/model-capabilities";
import { LocalModelPicker } from '@/features/gateway/LocalModelPicker';
import { isLocalModel, type LocalModelChoice } from '@/features/gateway/local-model-match';
import { AgentGrantPrompt, useAgentWorkspaceGrant } from './agent-workspace-grant';
import type { AgentAuthorization, ChatAttachment, ContextCitation } from '@/lib/api';
import { useWorkspaceStore } from '@/stores/workspace-store';
import { useUiStore } from '@/stores/ui-store';
import { DropdownMenu, DropdownMenuCheckboxItem, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu';
import { useEvidenceDraft } from '@/features/codelens/evidence-draft';
import { useChatAttachments } from './chat-attachments';
import { attachmentMaxMiB } from './chat-backup';
import { useChatStore } from './chat-store';

interface Props {
  modelChoices: LocalModelChoice[];
  selectedModel?: ModelConfig;
  attachmentSessionId?: string;
  onModelChange: (choice: LocalModelChoice) => void;
  onConfigureModel: () => void;
  modelBusy?: boolean;
  initialAgentMode?: boolean;
  initialText?: string;
  lifecycleState?: 'starting' | 'offline' | 'preparing' | 'waiting' | 'streaming' | 'settled' | 'idle';
  lifecycleLabel?: string;
  streaming: boolean;
  busy?: boolean;
  blocked?: boolean;
  onSend: (text: string, options: ChatSubmitOptions) => boolean;
  onStop: () => void;
}

export interface ChatSubmitOptions {
  agentMode: boolean;
  reviewedActions?: boolean;
  authorization?: AgentAuthorization;
  attachments: ChatAttachment[];
  artifact?: ContextCitation;
}

/** 概念稿输入框: 紧凑移动布局、宽阔桌面布局，底部附件/模式 + 右侧发送 */
export function ChatInput({ modelChoices, selectedModel, attachmentSessionId = '', onModelChange, onConfigureModel, modelBusy, initialAgentMode = false, initialText = '', lifecycleState = 'idle', lifecycleLabel = '', streaming, busy, blocked, onSend, onStop }: Props) {
  const [text, setText] = useState(initialText);
  const [agentMode, setAgentMode] = useState(initialAgentMode);
  const [reviewedActions, setReviewedActions] = useState(false);
  const [dragging, setDragging] = useState(false);
  const workspaceId = useWorkspaceStore(state => state.activeId);
  const grant = useAgentWorkspaceGrant(workspaceId, agentMode, reviewedActions);
  const needsGrant = agentMode && !!grant.binding && !grant.authorization;
  const workspaceName = useWorkspaceStore(state => state.workspaces.find(workspace => workspace.id === state.activeId)?.name ?? '项目');
  const workspaceReady = useWorkspaceStore(state => state.hydrated);
  const projectPath = useWorkspaceStore(state => state.workspaces.find(workspace => workspace.id === state.activeId)?.project_path);
  const artifact = useEvidenceDraft(state => state.artifact?.citation.workspace_id === workspaceId && state.artifact.projectPath === projectPath ? state.artifact : null);
  const attachmentDraft = useChatAttachments(workspaceId, attachmentSessionId, projectPath ?? '');
  const [reuseMessageId, setReuseMessageId] = useState<string>();
  const previousFiles = useChatStore(state => {
    const messages = state.sessions.find(session => session.id === attachmentSessionId && (session.workspaceId ?? 'local') === workspaceId)?.messages;
    if (!messages) return undefined;
    for (let i = messages.length - 1; i >= 0; i--) {
      const message = messages[i];
      if (message.role === 'user' && message.attachments?.length && message.sourceProjectPath === (projectPath ?? '')) return message;
    }
  });
  const reuse = previousFiles && previousFiles.id === reuseMessageId && !attachmentDraft.files.length;
  const [compact, setCompact] = useState(() => window.matchMedia("(max-width: 639px)").matches);
  const ref = useRef<HTMLTextAreaElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const agentSupport = agentCapability(selectedModel);
  const selectedFiles = reuse ? previousFiles.attachments ?? [] : attachmentDraft.files.map(entry => entry.file);
  const hasImages = selectedFiles.some(file => /\.(png|jpe?g)$/i.test(file.name));
  const liveChoice = hasImages && selectedModel ? modelChoices.find(item => item.config?.id === selectedModel.id) : undefined;
  const visionWarning = hasImages && (imageInputBlocker(selectedModel, agentMode) ||
    (liveChoice?.engine?.id === 'custom_llama' && liveChoice.model?.vision !== true ? '当前运行时尚未加载可用的视觉投影器。' : ''));
  const capabilityBlocked = chatCapabilityBlocker(selectedModel, agentMode) || visionWarning;
  const capabilityReason = capabilityBlocked || (agentMode ? agentSupport.reason : '');

  useEffect(() => {
    setAgentMode(initialAgentMode);
    setReviewedActions(false);
  }, [initialAgentMode]);

  useEffect(() => {
    const query = window.matchMedia("(max-width: 639px)");
    const update = (event: MediaQueryListEvent) => setCompact(event.matches);
    setCompact(query.matches);
    query.addEventListener("change", update);
    return () => query.removeEventListener("change", update);
  }, []);

  const resize = () => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 168)}px`;
  };

  const submit = () => {
    const t = text.trim();
    if (!t || needsGrant || streaming || busy || blocked || capabilityBlocked || attachmentDraft.blocked) return;
    const workspace = useWorkspaceStore.getState();
    if (workspace.activeId !== workspaceId || workspace.workspaces.find(item => item.id === workspaceId)?.project_path !== projectPath ||
        (artifact && useEvidenceDraft.getState().artifact !== artifact)) {
      attachmentDraft.show('项目或附件已变化，请确认后重新发送。');
      return;
    }
    if (onSend(t, { agentMode, authorization: agentMode ? grant.authorization : undefined, reviewedActions: agentMode && reviewedActions, attachments: reuse ? previousFiles.attachments ?? [] : attachmentDraft.attachments, artifact: artifact?.citation })) {
      if (artifact) useEvidenceDraft.getState().remove(artifact);
      setText('');
      setReuseMessageId(undefined);
      attachmentDraft.markSent();
      requestAnimationFrame(() => { if (ref.current) ref.current.style.height = 'auto'; });
    }
  };

  const canSend = !needsGrant && !!text.trim() && !capabilityBlocked && !streaming && !busy && !blocked && !attachmentDraft.blocked;
  const canAttach = !!attachmentSessionId && !streaming && !busy;
  const remoteModel = selectedModel && !isLocalModel(selectedModel);

  return (
    <div className="ns-chat-composer shrink-0" data-lifecycle={lifecycleState}>
      <div className="ns-chat-rail">
        <div
          className={`ns-chat-input-frame ${dragging ? 'is-dragging' : ''} ${agentMode ? 'is-agent' : ''}`}
          data-lifecycle={lifecycleState}
          onDragOver={event => {
            if (!event.dataTransfer.types.includes('Files')) return;
            event.preventDefault();
            event.dataTransfer.dropEffect = canAttach ? 'copy' : 'none';
            setDragging(canAttach);
          }}
          onDragLeave={event => {
            if (!(event.relatedTarget instanceof Node) || !event.currentTarget.contains(event.relatedTarget)) setDragging(false);
          }}
          onDrop={event => {
            if (!event.dataTransfer.types.includes('Files')) return;
            event.preventDefault();
            setDragging(false);
            if (canAttach) attachmentDraft.add(Array.from(event.dataTransfer.files));
          }}>
          <div className="ns-composer-signal-line" data-lifecycle={lifecycleState} aria-hidden="true"><i /></div>
          {(streaming || lifecycleState === 'settled') && lifecycleLabel && (
            <div role="status" className="ns-composer-telemetry" data-lifecycle={lifecycleState}>
              <span className="ns-mini-orbit" data-mode={lifecycleState === 'streaming' ? 'streaming' : lifecycleState === 'settled' ? 'idle' : 'active'} aria-hidden="true"><i /><b /></span>
              <span className="truncate">{lifecycleLabel}</span>
            </div>
          )}
          {dragging && <p className="ns-composer-drop-hint">拖放到此处添加附件</p>}
          {attachmentDraft.error && <p role="alert" className="px-4 pt-3 text-xs text-err">{attachmentDraft.error}</p>}
          {previousFiles && !attachmentDraft.files.length && !streaming && <div className="ns-composer-reuse flex flex-wrap items-center gap-x-2 px-4 pt-2.5 text-xs text-text-3">
            <button type="button" className="underline underline-offset-4 hover:text-text-1" aria-pressed={!!reuse} onClick={() => setReuseMessageId(reuse ? undefined : previousFiles.id)}>
              {reuse ? '移除上次附件' : '使用上次附件'}
            </button>
            {reuse && <span className="max-w-full truncate">{previousFiles.attachments?.map(file => file.name).join('、')} · 本次发送会引用所选附件</span>}
          </div>}
          {selectedModel && (capabilityBlocked || agentMode && !agentSupport.known) && <p role="status" className="px-4 pt-3 text-xs text-text-2" data-testid="chat-capability-note">{capabilityReason}</p>}
          {attachmentDraft.files.length > 0 || artifact ? (
            <div className="ns-composer-attachments flex flex-wrap gap-2 border-b border-border-subtle px-4 py-2.5" data-testid="chat-attachments">
              {artifact && <span className="ns-attachment-chip">
                <span className="max-w-52 truncate">REA · {artifact.citation.title}</span>
                <button type="button" aria-label="移除 REA 记录" onClick={() => useEvidenceDraft.getState().remove(artifact)}><X aria-hidden="true" className="size-3" /></button>
              </span>}
              {attachmentDraft.files.map(({ file, id, state }) => {
                const processing = state.phase === 'reading' || state.phase === 'processing';
                return <div key={id} className="ns-attachment-chip" data-phase={state.phase}>
                  <Paperclip aria-hidden="true" className="size-3.5 text-brand" />
                  <span className="max-w-48 truncate" title={file.name}>{file.name}</span>
                  {state.phase === 'reading' && <progress className="ns-attachment-progress" aria-label={`读取 ${file.name}`} value={state.progress} max={100} />}
                  {state.phase === 'processing' && <span role="status" className="text-text-3">正在处理附件…</span>}
                  {state.phase === 'ready' && <span role="status" className="text-ok">可以发送</span>}
                  {state.phase === 'failed' && <>
                    <span role="alert" className="text-err">{state.message}</span>
                    {state.retry && <button type="button" onClick={() => attachmentDraft.retry(id)} className="underline underline-offset-4" aria-label={`重试附件 ${file.name}`}>重试</button>}
                  </>}
                  <button
                    type="button"
                    aria-label={`${processing ? '取消' : '移除'}附件 ${file.name}`}
                    className="ns-attachment-remove"
                    onClick={() => attachmentDraft.remove(id)}
                  >
                    <X aria-hidden="true" className="size-3" />
                  </button>
                </div>;
              })}
              {attachmentDraft.attachments.length > 0 && !remoteModel && <p className="w-full px-0.5 text-[11.5px] text-text-3">{hasImages ? '图像会随消息发送给所选模型。' : '相关文字节选会随消息发送给所选模型。'}</p>}
            </div>
          ) : null}
          {agentMode && <AgentGrantPrompt grant={grant} />}
          <textarea
            aria-label="输入问题"
            ref={ref}
            value={text}
            rows={1}
            placeholder={agentMode ? '描述你希望完成的任务…' : compact ? '输入问题…' : '输入问题，或从下方选择功能…'}
            className="ns-chat-input block w-full resize-none bg-transparent text-text-1 placeholder:text-text-3 outline-none"
            onChange={(e) => {
              setText(e.target.value);
              resize();
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
                e.preventDefault();
                void submit();
              }
            }}
          />

          {remoteModel && (!!text.trim() || !!attachmentDraft.attachments.length || reuse || artifact) &&
            <p className="px-4 pb-1 text-xs text-text-3">对话、检索到的项目依据和所选附件内容会随请求发送到当前 API。</p>}

          <input
            ref={fileRef}
            type="file"
            accept=".txt,.log,.md,.markdown,.rst,.adoc,.json,.jsonl,.yaml,.yml,.toml,.ini,.cfg,.conf,.properties,.xml,.html,.htm,.css,.scss,.csv,.tsv,.go,.mod,.sum,.py,.pyw,.rs,.js,.jsx,.mjs,.cjs,.ts,.tsx,.vue,.svelte,.astro,.java,.kt,.kts,.cs,.fs,.vb,.c,.h,.cpp,.hpp,.cc,.swift,.rb,.php,.sh,.bash,.zsh,.ps1,.psm1,.bat,.cmd,.sql,.lua,.r,.dart,.gd,.tf,.gitignore,.editorconfig,.gitattributes,.pdf,.png,.jpg,.jpeg"
            aria-label="对话附件"
            multiple
            className="sr-only"
            tabIndex={-1}
            onChange={event => {
              if (canAttach) attachmentDraft.add(Array.from(event.currentTarget.files ?? []));
              event.currentTarget.value = '';
            }}
          />

          <div className="ns-chat-input-tools flex flex-wrap items-center justify-between gap-x-2 gap-y-2">
            <div className="flex min-w-0 flex-wrap items-center gap-1">
              <DropdownMenu>
                <DropdownMenuTrigger asChild><Button type="button" variant="ghost" size="icon" className="ns-composer-icon" aria-label="对话功能" title="对话功能"><Plus aria-hidden="true" /></Button></DropdownMenuTrigger>
                <DropdownMenuContent align="start" side="top">
                  <DropdownMenuItem disabled={streaming || !agentMode && (agentSupport.blocked || !selectedModel || selectedModel.enabled === false)} onSelect={() => setAgentMode(value => !value)}>{agentMode ? '切回普通对话' : '转为任务'}</DropdownMenuItem>
                  {agentMode && <DropdownMenuCheckboxItem checked={reviewedActions} disabled={streaming} onCheckedChange={setReviewedActions}>{grant.binding ? '审阅模式' : '操作提案'}</DropdownMenuCheckboxItem>}
                </DropdownMenuContent>
              </DropdownMenu>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label="选择附件"
                title={`文本、代码、含文字的 PDF（最多 ${attachmentMaxMiB} MB），或 JPEG、PNG（最多 8 MB）`}
                disabled={!canAttach}
                className="ns-composer-icon"
                onClick={() => fileRef.current?.click()}
              >
                <Paperclip data-icon="inline-start" aria-hidden="true" />
              </Button>
              <Button type="button" variant="ghost" size="sm" aria-label="对话项目" title={workspaceName} className="ns-composer-project" disabled={!workspaceReady || streaming} onClick={() => useUiStore.getState().setProjectPickerOpen(true)}><AtSign aria-hidden="true" /><span className="truncate">{projectPath ? workspaceName : '项目'}</span></Button>
              {agentMode && <span className="ns-composer-mode" role="status">{grant.binding ? (reviewedActions ? '任务 · 审阅' : '任务 · 自主执行') : (reviewedActions ? '任务 · 操作待审批' : '任务 · 只读')}</span>}
            </div>
            <div className="ns-composer-model flex min-w-0 items-center justify-end gap-2">
              <LocalModelPicker choices={modelChoices} current={selectedModel} disabled={streaming || modelBusy} label="选择对话模型"
                className="ns-composer-model-trigger h-9 flex-[0_1_180px] px-2.5" onSelect={onModelChange} onBrowse={onConfigureModel} />
              {streaming ? (
                <Button
                  size="icon"
                  variant="secondary"
                  className="ns-composer-send shrink-0"
                  onClick={onStop}
                  aria-label="停止生成"
                >
                  <Square aria-hidden="true" />
                </Button>
              ) : (
                <Button
                  size="icon"
                  className="ns-composer-send shrink-0"
                  onClick={() => void submit()}
                  disabled={!canSend}
                  aria-label="发送"
                >
                  <Send aria-hidden="true" />
                </Button>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
