import { useCallback, useEffect, useRef, useState } from 'react';
import { Brain, Sparkles, X } from 'lucide-react';
import { Link } from 'react-router-dom';
import { Button } from '@/components/ui/button';
import { api, errMessage, type KnowledgeMemory, type KnowledgeRevisionProposal } from '@/lib/api';
import { useWorkspaceStore } from '@/stores/workspace-store';
import { useChatStore, type ChatMessage } from './chat-store';
import { memoryCandidate, memoryChange } from './conversation-memory';
import './conversation-memory.css';

export { memoryRevisionInstruction } from './conversation-memory';

export interface MemoryRevisionRequest {
  id: string;
  instruction: string;
  memoryId?: string;
}

interface MemoryScope {
  workspaceId: string;
  projectPath: string;
  sessionId?: string;
  modelId?: string;
}

interface Props extends MemoryScope {
  userMessage?: Pick<ChatMessage, 'id' | 'content' | 'sourceProjectPath' | 'memoryCandidateDismissed'>;
  revisionRequest?: MemoryRevisionRequest | null;
  onCloseRevision?: () => void;
}

function currentScope({ workspaceId, projectPath, sessionId }: MemoryScope) {
  const workspace = useWorkspaceStore.getState();
  return workspace.activeId === workspaceId
    && (workspace.workspaces.find(item => item.id === workspaceId)?.project_path ?? '') === projectPath
    && (!sessionId || useChatStore.getState().activeId === sessionId);
}

export function ConversationMemory({ userMessage, revisionRequest, onCloseRevision, ...scope }: Props) {
  if (revisionRequest) {
    return <MemoryRevision key={[scope.workspaceId, scope.projectPath, scope.sessionId, revisionRequest.id].join('|')}
      {...scope} request={revisionRequest} onClose={onCloseRevision} />;
  }
  const candidate = scope.sessionId && userMessage && !userMessage.memoryCandidateDismissed
    && userMessage.sourceProjectPath === scope.projectPath ? memoryCandidate(userMessage.content) : null;
  if (!candidate || !scope.sessionId || !userMessage) return null;
  return <MemoryCandidate key={[scope.workspaceId, scope.projectPath, scope.sessionId, userMessage.id].join('|')}
    {...scope} sessionId={scope.sessionId} messageId={userMessage.id} candidate={candidate} />;
}

function MemoryCandidate({ workspaceId, projectPath, sessionId, messageId, candidate }: MemoryScope & {
  sessionId: string;
  messageId: string;
  candidate: NonNullable<ReturnType<typeof memoryCandidate>>;
}) {
  const [visible, setVisible] = useState(false);
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  useEffect(() => {
    let active = true;
    const show = (memories: KnowledgeMemory[]) => {
      if (!active) return;
      const exists = memories.some(item => item.workspace_id === workspaceId
        && (item.source.project_path ?? '') === projectPath
        && item.source.session_id === sessionId && item.source.message_id === messageId);
      setVisible(!exists);
    };
    void api.listKnowledge(workspaceId).then(result => show(result.memories ?? []), () => show([]));
    return () => { active = false; };
  }, [workspaceId, projectPath, sessionId, messageId]);

  const dismiss = () => {
    if (!currentScope({ workspaceId, projectPath, sessionId })) return;
    useChatStore.getState().updateMessage(sessionId, messageId, { memoryCandidateDismissed: true });
  };
  const save = async () => {
    if (saving) return;
    if (!currentScope({ workspaceId, projectPath, sessionId })) {
      setError('当前工作区或对话已变化，请重新确认。');
      return;
    }
    setSaving(true);
    setError('');
    try {
      const saved = await api.saveKnowledge(workspaceId, {
        content: candidate.content,
        category: candidate.category,
        status: 'confirmed',
        expected_project_path: projectPath,
        source: { kind: 'chat', session_id: sessionId, message_id: messageId, excerpt: candidate.excerpt },
      });
      if (saved.workspace_id !== workspaceId || (saved.source.project_path ?? '') !== projectPath) {
        throw new Error('保存响应与工作区不匹配，请在记忆中查看。');
      }
      setNotice(saved.status === 'confirmed' && saved.content === candidate.content
        ? '已保存到工作区记忆。' : '这条来源已有记忆，可在记忆区查看。');
    } catch (error) {
      setError(errMessage(error));
    } finally {
      setSaving(false);
    }
  };

  if (!visible) return null;
  if (notice) return <p role="status" className="ns-conversation-memory-notice"><Brain aria-hidden="true" />{notice}</p>;
  return <article className="ns-conversation-memory" aria-label="记忆候选">
    <header className="ns-conversation-memory-heading"><Brain aria-hidden="true" /><h3>记忆候选</h3></header>
    <p className="ns-memory-candidate-content">{candidate.content}</p>
    <p className="ns-conversation-memory-hint">确认后可用于当前工作区的对话。</p>
    {error && <p role="alert" className="ns-conversation-memory-error">{error}</p>}
    <div className="ns-conversation-memory-actions">
      <Button type="button" size="sm" onClick={() => void save()} disabled={saving}>{saving ? '保存中…' : '保存记忆'}</Button>
      <Button type="button" size="sm" variant="ghost" onClick={dismiss} disabled={saving}>忽略</Button>
    </div>
  </article>;
}

function MemoryRevision({ workspaceId, projectPath, sessionId, modelId, request, onClose }: MemoryScope & {
  request: MemoryRevisionRequest;
  onClose?: () => void;
}) {
  const [items, setItems] = useState<KnowledgeMemory[]>([]);
  const [targetId, setTargetId] = useState(request.memoryId ?? '');
  const [instruction, setInstruction] = useState(request.instruction);
  const [proposal, setProposal] = useState<KnowledgeRevisionProposal | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<'rewrite' | 'save' | ''>('');
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);
  const editor = useRef<HTMLElement>(null);
  const version = useRef(0);
  const inFlight = useRef(false);
  const target = items.find(item => item.id === targetId) ?? (!targetId && items.length === 1 ? items[0] : undefined);

  const load = useCallback(async () => {
    const currentVersion = ++version.current;
    setLoading(true);
    setProposal(null);
    setError('');
    try {
      const result = await api.listKnowledge(workspaceId);
      if (version.current !== currentVersion) return;
      const available = (result.memories ?? []).filter(item => item.workspace_id === workspaceId
        && (item.source.project_path ?? '') === projectPath
        && (item.status === 'confirmed' || item.status === 'paused' || item.status === 'proposed'));
      setItems(available);
      setTargetId(id => available.some(item => item.id === id) ? id : '');
    } catch (error) {
      if (version.current === currentVersion) setError(errMessage(error));
    } finally {
      if (version.current === currentVersion) setLoading(false);
    }
  }, [workspaceId, projectPath]);

  useEffect(() => {
    void load();
    const element = editor.current;
    element?.focus({ preventScroll: true });
    element?.scrollIntoView({ block: 'nearest' });
    return () => { version.current++; };
  }, [load]);

  async function rewrite() {
    if (inFlight.current || !target || !instruction.trim()) return;
    if (!currentScope({ workspaceId, projectPath, sessionId })) {
      setError('当前工作区或对话已变化，请重新打开改写。');
      return;
    }
    const currentVersion = version.current;
    inFlight.current = true;
    setBusy('rewrite');
    setError('');
    setProposal(null);
    try {
      const result = await api.suggestKnowledgeRevision(workspaceId, target.id, {
        revision: target.revision, expected_project_path: projectPath, instruction: instruction.trim(),
        session_id: sessionId, model_id: modelId,
      });
      if (version.current !== currentVersion) return;
      if (result.before.workspace_id !== workspaceId || result.before.id !== target.id
        || result.before.revision !== target.revision || (result.before.source.project_path ?? '') !== projectPath
        || !result.after?.trim()) throw new Error('记忆版本已变化，请重新读取。');
      setProposal(result);
    } catch (error) {
      if (version.current === currentVersion) setError(errMessage(error));
    } finally {
      inFlight.current = false;
      if (version.current === currentVersion) setBusy('');
    }
  }

  async function confirm() {
    if (inFlight.current || !proposal || proposal.before.content === proposal.after) return;
    if (!currentScope({ workspaceId, projectPath, sessionId })) {
      setError('当前工作区或对话已变化，请重新打开改写。');
      return;
    }
    const currentVersion = version.current;
    inFlight.current = true;
    setBusy('save');
    setError('');
    try {
      await api.saveKnowledge(workspaceId, {
        ...proposal.before,
        content: proposal.after,
        expected_project_path: projectPath,
      });
      if (version.current === currentVersion) setSaved(true);
    } catch (error) {
      if (version.current === currentVersion) setError(errMessage(error));
    } finally {
      inFlight.current = false;
      if (version.current === currentVersion) setBusy('');
    }
  }

  const change = proposal && memoryChange(proposal.before.content, proposal.after);
  return <article ref={editor} tabIndex={-1} className="ns-conversation-memory" aria-label="改写工作区记忆" aria-busy={loading || !!busy}>
    <header className="ns-conversation-memory-heading">
      <Sparkles aria-hidden="true" /><h3>改写记忆</h3>
      {onClose && <Button variant="ghost" size="icon-sm" aria-label="关闭记忆改写" className="ml-auto" onClick={onClose} disabled={busy === 'save'}><X aria-hidden="true" /></Button>}
    </header>
    {saved ? <p role="status" className="ns-conversation-memory-hint">记忆已更新。</p> : <>
      {loading && <p role="status" className="ns-conversation-memory-hint">正在读取记忆…</p>}
      {!loading && !error && items.length === 0 && <p className="ns-conversation-memory-hint">当前工作区还没有可改写的记忆。<Link to="/nebula" className="underline underline-offset-4">查看记忆</Link></p>}
      {items.length > 0 && <>
        <label className="ns-memory-revision-field">要改写的记忆
          <select value={target?.id ?? ''} disabled={!!busy || loading} onChange={event => { setTargetId(event.target.value); setProposal(null); setError(''); }}>
            {!target && <option value="" disabled>选择一条记忆</option>}
            {items.map(item => <option key={item.id} value={item.id}>{item.content.replace(/\s+/g, ' ').slice(0, 96)}</option>)}
          </select>
        </label>
        <label className="ns-memory-revision-field">修改要求
          <textarea rows={2} maxLength={1000} value={instruction} disabled={!!busy || loading}
            placeholder="例如把时区约定改为北京时间"
            onChange={event => { setInstruction(event.target.value); setProposal(null); setError(''); }} />
        </label>
        {change && <div className="ns-memory-diff" aria-label="记忆改写对比">
          <div><h4>现在的记忆</h4><p>{change.prefix}{change.before && <del>{change.before}</del>}{change.suffix}</p></div>
          <div><h4>模型建议</h4><p>{change.prefix}{change.after && <ins>{change.after}</ins>}{change.suffix}</p></div>
        </div>}
        {proposal && proposal.before.content === proposal.after && <p role="status" className="ns-conversation-memory-hint">建议与原文一致。</p>}
        {!proposal && <p className="ns-conversation-memory-hint">当前模型会收到选中的记忆和修改要求。</p>}
        <div className="ns-conversation-memory-actions">
          {change && (change.before || change.after)
            ? <Button size="sm" onClick={() => void confirm()} disabled={!!busy}>{busy === 'save' ? '保存中…' : '确认更新'}</Button>
            : <Button size="sm" onClick={() => void rewrite()} disabled={!!busy || loading || !target || !instruction.trim()}>{busy === 'rewrite' ? '正在改写…' : '让模型提出改写'}</Button>}
          {proposal && <Button size="sm" variant="ghost" disabled={!!busy} onClick={() => setProposal(null)}>放弃建议</Button>}
        </div>
      </>}
      {error && <div className="ns-conversation-memory-actions">
        <p role="alert" className="ns-conversation-memory-error">{error}</p>
        <Button size="sm" variant="ghost" disabled={!!busy || loading} onClick={() => void load()}>重新读取记忆</Button>
      </div>}
    </>}
  </article>;
}
