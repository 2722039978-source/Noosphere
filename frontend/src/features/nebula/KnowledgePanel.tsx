import { useCallback, useEffect, useRef, useState } from 'react';
import { ArrowUpRight, Plus } from 'lucide-react';
import { Link } from 'react-router-dom';
import { api, errMessage, type KnowledgeMemory, type KnowledgeSource } from '@/lib/api';
import { useWorkspaceStore } from '@/stores/workspace-store';
import { useChatStore } from '@/features/chat/chat-store';
import { sessionWorkspaceId } from '@/features/chat/chat-backup';
import { SourceEvidenceButton } from '@/components/CodeLensEvidence';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Surface } from '@/components/ui/surface';

const categories = { convention: '项目约定', decision: '设计决策', preference: '稳定偏好', solution: '已验证方案' };
const statuses = { proposed: '待确认', confirmed: '已确认', paused: '已暂停', rejected: '已拒绝', superseded: '已被替代' };
const field = 'rounded border border-border-subtle bg-secondary/30 px-3 py-2 text-sm text-text-1';
type KnowledgeInput = Parameters<typeof api.saveKnowledge>[1];

interface Editor {
  mode: 'new' | 'edit' | 'replace';
  current?: KnowledgeMemory;
  content: string;
  category: KnowledgeMemory['category'];
  source: KnowledgeSource;
  expiry: string;
  pending: boolean;
}

const freshEditor = (): Editor => ({
  mode: 'new', content: '', category: 'decision', source: { kind: 'note' }, expiry: '', pending: false,
});

export function KnowledgePanel() {
  const workspaceId = useWorkspaceStore(state => state.activeId);
  const workspace = useWorkspaceStore(state => state.workspaces.find(item => item.id === state.activeId));
  const projectPath = workspace?.project_path ?? '';
  return <WorkspaceKnowledge key={[workspaceId, projectPath].join('|')} workspaceId={workspaceId} projectPath={projectPath}
    name={workspace?.name ?? '本地工作区'} />;
}

function WorkspaceKnowledge({ workspaceId, projectPath, name }: { workspaceId: string; projectPath: string; name: string }) {
  const [items, setItems] = useState<KnowledgeMemory[]>([]);
  const [editor, setEditor] = useState<Editor | null>(null);
  const [deleting, setDeleting] = useState<KnowledgeMemory | null>(null);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);
  const [filter, setFilter] = useState('');
  const [categoryFilter, setCategoryFilter] = useState('all');
  const [statusFilter, setStatusFilter] = useState('all');
  const alive = useRef(true);
  const mutating = useRef(false);
  const loadGeneration = useRef(0);
  const title = useRef<HTMLHeadingElement>(null);
  const removeButton = useRef<HTMLButtonElement | null>(null);

  const load = useCallback(async () => {
    const generation = ++loadGeneration.current;
    setLoading(true);
    setError('');
    try {
      const result = await api.listKnowledge(workspaceId);
      if (alive.current && generation === loadGeneration.current) {
        setItems((result.memories ?? []).filter(item => item.workspace_id === workspaceId && (item.source.project_path ?? '') === projectPath));
      }
    } catch (error) {
      if (alive.current && generation === loadGeneration.current) setError(errMessage(error));
    } finally {
      if (alive.current && generation === loadGeneration.current) setLoading(false);
    }
  }, [workspaceId, projectPath]);
  useEffect(() => {
    alive.current = true;
    void load();
    return () => { alive.current = false; loadGeneration.current++; };
  }, [load]);

  async function change(operation: () => Promise<void>, feedback: string) {
    if (mutating.current) return;
    const state = useWorkspaceStore.getState();
    if (state.activeId !== workspaceId || (state.workspaces.find(ws => ws.id === workspaceId)?.project_path ?? '') !== projectPath) {
      setError('当前工作区或项目已变化，请重新打开记忆。');
      return;
    }
    mutating.current = true;
    setBusy(true);
    setError('');
    setNotice('');
    try {
      await operation();
      if (!alive.current) return;
      setNotice(feedback);
      await load();
    } catch (error) {
      if (alive.current) setError(errMessage(error));
    } finally {
      mutating.current = false;
      if (alive.current) setBusy(false);
    }
  }

  function save(input: KnowledgeInput, feedback: string, closeEditor = false) {
    void change(async () => {
      await api.saveKnowledge(workspaceId, { ...input, expected_project_path: projectPath });
      if (alive.current && closeEditor) setEditor(null);
    }, feedback);
  }

  function edit(item: KnowledgeMemory, mode: 'edit' | 'replace') {
    setEditor({
      mode, current: item, content: item.content, category: item.category,
      source: mode === 'replace' ? { kind: 'note' } : item.source,
      expiry: item.valid_until ? new Date(item.valid_until).toISOString().slice(0, 16) : '',
      pending: item.status === 'proposed',
    });
    setError('');
    setNotice('');
  }

  function saveEditor() {
    if (!editor) return;
    const { current, mode, content, category, source, expiry, pending } = editor;
    const until = expiry ? Date.parse(expiry + ':00Z') : 0;
    if (!Number.isFinite(until)) {
      setError('请输入有效的到期时间。');
      return;
    }
    const currentStatus = current?.status === 'rejected' ? 'proposed' : current?.status;
    const status = mode === 'replace' ? 'confirmed' : mode === 'edit' ? currentStatus : pending ? 'proposed' : 'confirmed';
    save({
      ...(mode === 'edit' ? current : {}),
      content, category, source, status,
      ...(mode === 'replace' ? { supersedes: current?.id, supersedes_revision: current?.revision } : {}),
      valid_until: expiry ? new Date(until).toISOString() : '',
    }, status === 'proposed' ? '已保存为待确认。' : '记忆已保存。', true);
  }

  function remove(item: KnowledgeMemory) {
    void change(async () => {
      const result = await api.deleteKnowledge(workspaceId, item.id, {
        revision: item.revision, expected_project_path: projectPath,
      });
      if (!result.deleted) throw new Error('删除未确认，请重新读取记忆。');
      const { source } = item;
      if (source.kind === 'chat' && source.session_id && source.message_id) {
        const store = useChatStore.getState();
        const session = store.sessions.find(entry => entry.id === source.session_id && sessionWorkspaceId(entry) === workspaceId);
        if (session?.messages.some(message => message.id === source.message_id && message.role === 'user' && message.sourceProjectPath === projectPath)) {
          store.updateMessage(session.id, source.message_id, { memoryCandidateDismissed: true });
        }
      }
      if (alive.current) {
        setDeleting(null);
        setItems(current => current.filter(memory => memory.id !== item.id));
        setEditor(current => current?.current?.id === item.id ? null : current);
      }
    }, '记忆已删除。');
  }

  const filtered = Boolean(filter.trim()) || categoryFilter !== 'all' || statusFilter !== 'all';
  const visibleItems = filtered ? items.filter(item =>
    item.content.toLocaleLowerCase().includes(filter.trim().toLocaleLowerCase())
    && (categoryFilter === 'all' || item.category === categoryFilter)
    && (statusFilter === 'all' || item.status === statusFilter)) : items;
  const updateEditor = (changes: Partial<Editor>) => setEditor(current => current ? { ...current, ...changes } : null);

  return <Surface className="ns-knowledge-panel" aria-label="工作区长期记忆" aria-busy={busy || loading}>
    <div className="flex flex-wrap items-center justify-between gap-2">
      <h2 ref={title} tabIndex={-1} className="min-w-0 truncate text-base font-semibold" title={name}>{name}</h2>
      <Button size="sm" variant="outline" disabled={busy || !!editor} onClick={() => { setEditor(freshEditor()); setError(''); setNotice(''); }}><Plus aria-hidden="true" />新建记忆</Button>
    </div>
    {editor && <form className="ns-memory-editor-wrap mt-4 border border-border-subtle p-4" onSubmit={event => { event.preventDefault(); saveEditor(); }}>
      <fieldset disabled={busy} className="ns-memory-editor grid min-w-0 gap-3">
        <legend className="sr-only">{editor.mode === 'replace' ? '替代记忆' : editor.mode === 'edit' ? '编辑记忆' : '新建记忆'}</legend>
        <label className="grid gap-1 text-xs text-text-2">记忆内容
          <textarea key={editor.current?.id ?? 'new'} name="memory-content" aria-label="长期记忆内容" className={field} maxLength={5000} required autoFocus rows={3}
            value={editor.content} onChange={event => updateEditor({ content: event.target.value })}
            placeholder="例如本项目统一使用 UTC 时间。" />
        </label>
        <details className="ns-memory-advanced">
          <summary>高级选项</summary>
          <div className="ns-memory-editor-fields mt-2 flex flex-wrap gap-3">
            <label className="grid gap-1 text-xs text-text-3">分类
              <select aria-label="记忆分类" className={field} value={editor.category} onChange={event => updateEditor({ category: event.target.value as KnowledgeMemory['category'] })}>
                {Object.entries(categories).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
              </select>
            </label>
            <label className="grid gap-1 text-xs text-text-3">到期时间（UTC）
              <input aria-label="记忆到期时间" type="datetime-local" max="9999-12-31T23:59" className={field} value={editor.expiry} onChange={event => updateEditor({ expiry: event.target.value })} />
            </label>
            {editor.mode !== 'edit' && <label className="grid gap-1 text-xs text-text-3">来源
              <select aria-label="记忆来源类型" className={field} value={editor.source.kind} onChange={event => updateEditor({ source: event.target.value === 'file' ? { kind: 'file', file: '', start_line: 1, end_line: 1 } : { kind: 'note' } })}>
                <option value="note">手动记录</option><option value="file" disabled={!projectPath}>项目文件</option>
              </select>
            </label>}
          </div>
          {editor.source.kind === 'file' && <div className="ns-memory-file-source mt-2 flex flex-wrap gap-2">
            <Input aria-label="来源相对路径" placeholder="例如 src/main.go" value={editor.source.file ?? ''} disabled={editor.mode === 'edit'}
              onChange={event => updateEditor({ source: { ...editor.source, file: event.target.value } })} />
            <input className={field} aria-label="来源起始行" type="number" min="1" value={editor.source.start_line} onChange={event => updateEditor({ source: { ...editor.source, start_line: Number(event.target.value) } })} />
            <input className={field} aria-label="来源结束行" type="number" min="1" value={editor.source.end_line} onChange={event => updateEditor({ source: { ...editor.source, end_line: Number(event.target.value) } })} />
          </div>}
          {editor.mode === 'new' && <label className="mt-2 flex items-center gap-2 text-xs text-text-2">
            <input type="checkbox" checked={editor.pending} onChange={event => updateEditor({ pending: event.target.checked })} />保存为待确认
          </label>}
        </details>
        {editor.mode === 'replace' && <p className="text-xs text-text-2">新记忆确认后，旧记录停止用于回答。</p>}
        <div className="flex flex-wrap gap-2">
          <Button type="submit" size="sm" disabled={busy || !editor.content.trim()}>{busy ? '保存中…' : editor.mode === 'replace' ? '确认并替代旧记录' : editor.mode === 'edit' ? '保存修改' : editor.pending ? '保存为待确认' : '保存记忆'}</Button>
          <Button type="button" size="sm" variant="ghost" onClick={() => setEditor(null)}>取消</Button>
        </div>
      </fieldset>
    </form>}
    {error && !deleting && <div className="mt-3 flex flex-wrap items-center gap-2">
      <p role="alert" className="text-sm text-err">{error}</p>
      <Button variant="outline" size="sm" onClick={() => void load()} disabled={busy || loading}>重新读取</Button>
    </div>}
    {notice && <p role="status" className="mt-3 text-xs text-text-2">{notice}</p>}
    {(items.length > 1 || filtered) && <div className="ns-memory-filters mt-4 border-t border-border-subtle pt-4">
      <Input aria-label="筛选长期记忆" placeholder="搜索记忆…" value={filter} onChange={event => setFilter(event.target.value)} />
      <details className="ns-memory-advanced">
        <summary>筛选选项</summary>
        <div className="flex flex-wrap gap-2">
          <select aria-label="筛选记忆分类" className={field} value={categoryFilter} onChange={event => setCategoryFilter(event.target.value)}>
            <option value="all">全部分类</option>{Object.entries(categories).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
          </select>
          <select aria-label="筛选记忆状态" className={field} value={statusFilter} onChange={event => setStatusFilter(event.target.value)}>
            <option value="all">全部状态</option>{Object.entries(statuses).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
          </select>
        </div>
      </details>
      {filtered && <Button size="sm" variant="ghost" onClick={() => { setFilter(''); setCategoryFilter('all'); setStatusFilter('all'); }}>清除筛选</Button>}
    </div>}
    {!loading && filtered && <p role="status" className="ns-memory-result-count">显示 {visibleItems.length} / {items.length} 条记录</p>}
    {loading ? <p role="status" className="py-4 text-xs text-text-3">正在读取记忆…</p> : !error && !items.length && <p className="py-5 text-sm text-text-2">在<Link to="/chat" className="underline underline-offset-4">对话</Link>中说「请记住……」，即可预览和保存记忆。</p>}
    {!loading && items.length > 0 && !visibleItems.length && <p className="py-4 text-sm text-text-2">没有匹配的记忆。</p>}
    <div className="mt-3 grid gap-3">{visibleItems.map(item => {
      const inactiveReason = item.status === 'confirmed' ? item.inactive_reason : '';
      const liveRecord = item.status !== 'superseded' && item.status !== 'rejected';
      return <article key={item.id} className="ns-knowledge-record border border-border-subtle p-4" data-memory-id={item.id}>
        <div className="flex flex-wrap gap-2 text-xs text-text-3">
          <span>{categories[item.category]}</span><strong className={item.status === 'proposed' ? 'text-warn' : 'text-text-2'}>{statuses[item.status]}</strong>
          {inactiveReason && <span>{inactiveReason}</span>}
        </div>
        <p className="my-2 whitespace-pre-wrap break-words text-sm leading-6">{item.content}</p>
        <MemorySource item={item} workspaceId={workspaceId} projectPath={projectPath} />
        <div className="mt-2 flex flex-wrap items-center gap-1">
          {item.status === 'proposed' && <Button size="sm" variant="outline" disabled={busy} onClick={() => save({ ...item, status: 'confirmed' }, '记忆已确认。')}>确认采用</Button>}
          {liveRecord && <Button size="sm" variant="ghost" asChild><Link to="/chat" state={{ memoryId: item.id, memoryWorkspaceId: workspaceId }}>在对话中改写<ArrowUpRight aria-hidden="true" /></Link></Button>}
          <details className="ns-memory-advanced">
            <summary>更多操作</summary>
            <div className="flex flex-wrap gap-1">
              {item.status !== 'superseded' && <Button size="sm" variant="ghost" disabled={busy} onClick={() => edit(item, 'edit')}>编辑内容</Button>}
              {item.status === 'confirmed' && <Button size="sm" variant="ghost" disabled={busy} onClick={() => save({ ...item, status: 'paused' }, '记忆已暂停。')}>暂停使用</Button>}
              {item.status === 'paused' && <Button size="sm" variant="ghost" disabled={busy} onClick={() => save({ ...item, status: 'confirmed' }, '记忆已恢复。')}>恢复使用</Button>}
              {item.status === 'proposed' && <Button size="sm" variant="ghost" disabled={busy} onClick={() => save({ ...item, status: 'rejected' }, '已拒绝候选。')}>拒绝采用</Button>}
              {item.status === 'rejected' && <Button size="sm" variant="ghost" disabled={busy} onClick={() => save({ ...item, status: 'confirmed' }, '记忆已确认。')}>确认采用</Button>}
              {item.status !== 'superseded' && <Button size="sm" variant="ghost" disabled={busy} onClick={() => edit(item, 'replace')}>用新记录替代</Button>}
              <Button size="sm" variant="ghost" className="text-err" disabled={busy} onClick={event => { removeButton.current = event.currentTarget; setError(''); setDeleting(item); }}>删除</Button>
            </div>
          </details>
        </div>
      </article>;
    })}</div>
    <Dialog open={!!deleting} onOpenChange={open => { if (!open && !busy) { setDeleting(null); setError(''); } }}>
      {deleting && <DialogContent onCloseAutoFocus={event => {
        event.preventDefault();
        const trigger = removeButton.current;
        if (trigger?.isConnected && !trigger.disabled) trigger.focus();
        else title.current?.focus();
      }}>
        <DialogTitle>删除这条记忆？</DialogTitle>
        <DialogDescription>删除后，当前工作区的后续对话将无法引用这条记忆。</DialogDescription>
        <p className="max-h-32 overflow-y-auto whitespace-pre-wrap break-words text-sm text-text-1">{deleting.content}</p>
        {error && <p role="alert" className="text-sm text-err">{error}</p>}
        <DialogFooter>
          <Button variant="outline" size="sm" onClick={() => { setDeleting(null); setError(''); }} disabled={busy}>取消</Button>
          <Button variant="destructive" size="sm" disabled={busy} onClick={() => remove(deleting)}>{busy ? '删除中…' : '确认删除'}</Button>
        </DialogFooter>
      </DialogContent>}
    </Dialog>
  </Surface>;
}

function MemorySource({ item, workspaceId, projectPath }: { item: KnowledgeMemory; workspaceId: string; projectPath: string }) {
  const { source } = item;
  const original = useChatStore(state => {
    if (source.kind !== 'chat') return undefined;
    const session = state.sessions.find(chat => chat.id === source.session_id);
    if (!session || sessionWorkspaceId(session) !== workspaceId) return undefined;
    const message = session.messages.find(entry => entry.id === source.message_id);
    if (!message || (message.sourceProjectPath !== undefined && message.sourceProjectPath !== projectPath)) return undefined;
    return message;
  });
  return <details className="ns-memory-advanced">
    <summary>查看来源</summary>
    <div className="mt-1 grid gap-1 text-xs leading-relaxed text-text-2">
      {source.kind === 'chat' && <>
        {(source.excerpt || original?.content) && <blockquote aria-label="原始对话内容" className="max-h-32 overflow-y-auto whitespace-pre-wrap break-words border-l border-border-strong pl-3">{source.excerpt || original?.content}</blockquote>}
        {original ? <Link className="w-fit underline underline-offset-4" to="/chat" state={{ memorySource: { workspaceId, sessionId: source.session_id, messageId: source.message_id } }}>回到原对话</Link>
          : <span>原消息已不在本机对话中。</span>}
      </>}
      {source.kind === 'file' && source.file && source.start_line && source.end_line && <>
        <p className="break-all">{source.file} · L{source.start_line}–{source.end_line}{source.source_hash && ' · ' + source.source_hash.slice(0, 12)}</p>
        <SourceEvidenceButton workspaceId={workspaceId} label="打开来源文件" location={{ file: source.file, start_line: source.start_line, end_line: source.end_line, source_hash: source.source_hash }} />
      </>}
      {source.kind === 'note' && <span>手动记录</span>}
      <span>版本 {item.revision}{item.valid_until && ' · 到期 ' + new Date(item.valid_until).toLocaleString()}</span>
    </div>
  </details>;
}
