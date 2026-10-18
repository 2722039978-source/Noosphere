import { useEffect, useRef, useState } from 'react';
import { api, errMessage, type KnowledgeMemory, type KnowledgeSource } from '@/lib/api';
import { useWorkspaceStore } from '@/stores/workspace-store';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Surface } from '@/components/ui/surface';

const categories = { convention: '项目约定', decision: '设计决策', preference: '稳定偏好', solution: '已验证方案' };
const statuses = { proposed: '待确认', confirmed: '已确认', rejected: '已拒绝', superseded: '已被替代' };
const field = 'rounded border border-border-subtle bg-secondary/30 px-3 py-2 text-sm text-text-1';

export function KnowledgePanel() {
  const workspaceId = useWorkspaceStore(s => s.activeId);
  return <WorkspaceKnowledge key={workspaceId} workspaceId={workspaceId} />;
}

function WorkspaceKnowledge({ workspaceId }: { workspaceId: string }) {
  const [items, setItems] = useState<KnowledgeMemory[]>([]);
  const [content, setContent] = useState('');
  const [category, setCategory] = useState<KnowledgeMemory['category']>('decision');
  const [editing, setEditing] = useState<KnowledgeMemory | null>(null);
  const [supersedes, setSupersedes] = useState('');
  const [supersedesRevision, setSupersedesRevision] = useState(0);
  const [expiry, setExpiry] = useState('');
  const [source, setSource] = useState<KnowledgeSource>({ kind: 'note' });
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);
  const [filter, setFilter] = useState('');
  const alive = useRef(true);
  async function load() {
    try { const result = await api.listKnowledge(workspaceId); if (alive.current) setItems(result.memories ?? []); }
    catch (e) { if (alive.current) setError(errMessage(e)); }
    finally { if (alive.current) setLoading(false); }
  }
  useEffect(() => { alive.current = true; void load(); return () => { alive.current = false; }; }, [workspaceId]);
  function reset() { setEditing(null); setSupersedes(''); setContent(''); setExpiry(''); setSource({ kind: 'note' }); }
  async function save(memory: Partial<KnowledgeMemory> & Pick<KnowledgeMemory, 'content' | 'category' | 'source'>) {
    if (busy) return;
    setBusy(true); setError(''); setNotice('');
    try {
      await api.saveKnowledge(workspaceId, memory);
      if (!alive.current) return;
      reset(); setNotice('已保存到当前工作区。'); await load();
    } catch (e) { if (alive.current) setError(errMessage(e)); }
    finally { if (alive.current) setBusy(false); }
  }
  function edit(item: KnowledgeMemory, replace = false) {
    setEditing(replace ? null : item); setSupersedes(replace ? item.id : ''); setSupersedesRevision(replace ? item.revision : 0);
    setContent(item.content); setCategory(item.category); setSource(replace ? { kind: 'note' } : item.source);
    setExpiry(item.valid_until ? new Date(item.valid_until).toISOString().slice(0, 16) : ''); setError(''); setNotice('');
  }
  return <Surface className="mb-6 p-5" aria-label="工作区长期记忆">
    <div className="mb-2 flex flex-wrap items-center justify-between gap-2"><h2 className="text-base font-semibold">项目长期记忆</h2><Button size="sm" variant="outline" disabled={busy} onClick={() => { setError(''); void load(); }}>刷新记忆</Button></div>
    <p className="mb-4 text-xs leading-6 text-text-3">整理当前工作区的约定、决策和已验证方案。待确认的内容不会用于回答；来源变化、过期或被替代后自动排除。下方历史记忆会话独立保留。</p>
    <form className="grid gap-3" onSubmit={event => { event.preventDefault(); void save({ ...editing, content, category, source, status: supersedes ? 'confirmed' : 'proposed', supersedes: supersedes || editing?.supersedes, supersedes_revision: supersedes ? supersedesRevision : editing?.supersedes_revision, valid_until: expiry ? new Date(expiry + ':00Z').toISOString() : '' }); }}>
      <div className="flex flex-wrap gap-3">
        <label className="grid gap-1 text-xs text-text-3">分类<select aria-label="记忆分类" className={field} value={category} onChange={e => setCategory(e.target.value as KnowledgeMemory['category'])}>{Object.entries(categories).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label>
        <label className="grid gap-1 text-xs text-text-3">到期时间（UTC，可留空）<input aria-label="记忆到期时间" type="datetime-local" className={field} value={expiry} onChange={e => setExpiry(e.target.value)} /></label>
        {!editing && <label className="grid gap-1 text-xs text-text-3">来源<select aria-label="记忆来源类型" className={field} value={source.kind} onChange={e => setSource(e.target.value === 'file' ? { kind: 'file', file: '', start_line: 1, end_line: 1 } : { kind: 'note' })}><option value="note">手动整理</option><option value="file">项目文件</option></select></label>}
      </div>
      {source.kind === 'file' && <div className="flex flex-wrap gap-2"><Input aria-label="来源相对路径" placeholder="项目内路径，例如 src/main.go" value={source.file || ''} disabled={!!editing} onChange={e => setSource({ ...source, file: e.target.value })} /><input className={field} aria-label="来源起始行" type="number" min="1" value={source.start_line} onChange={e => setSource({ ...source, start_line: Number(e.target.value) })} /><input className={field} aria-label="来源结束行" type="number" min="1" value={source.end_line} onChange={e => setSource({ ...source, end_line: Number(e.target.value) })} /></div>}
      <label className="grid gap-1 text-xs text-text-3">{supersedes ? '替代旧记录：保存后确认新记录并停用旧记录' : editing ? '编辑记录：保存后需要重新确认' : '整理内容'}<textarea aria-label="长期记忆内容" className={`${field} min-h-24 resize-y leading-6`} maxLength={5000} required value={content} onChange={e => setContent(e.target.value)} placeholder="例如：配置保存使用原子替换，以避免中断导致文件损坏。" /></label>
      <div className="flex gap-2"><Button type="submit" size="sm" disabled={busy || !content.trim()}>{supersedes ? '确认并替代旧记录' : '保存为待确认'}</Button>{(editing || supersedes) && <Button type="button" size="sm" variant="outline" onClick={reset}>取消编辑</Button>}</div>
    </form>
    {error && <p role="alert" className="mt-3 text-sm text-red-600">{error}</p>}
    {notice && <p role="status" className="mt-3 text-xs text-text-3">{notice}</p>}
    <div className="mt-5 border-t border-border-subtle pt-4"><Input aria-label="筛选长期记忆" placeholder="筛选已整理的记忆…" value={filter} onChange={e => setFilter(e.target.value)} /></div>
    {loading ? <p className="py-4 text-xs text-text-3">正在读取记忆…</p> : !items.length && <p className="py-4 text-xs text-text-3">当前工作区还没有整理记忆。也可以从 AI 回答中保存待确认记录。</p>}
    <div className="mt-3 grid gap-3">{items.filter(item => item.content.toLowerCase().includes(filter.toLowerCase())).map(item => <article key={item.id} className="border border-border-subtle p-3" data-memory-id={item.id}>
      <div className="flex flex-wrap gap-3 text-xs text-text-3"><span>{categories[item.category]}</span><strong>{statuses[item.status]}</strong><span>版本 {item.revision}</span>{item.inactive_reason && !['proposed', 'rejected', 'superseded'].includes(item.inactive_reason) && <span>{item.inactive_reason}</span>}</div>
      <p className="my-2 whitespace-pre-wrap break-words text-sm leading-6">{item.content}</p>
      <p className="break-all text-xs text-text-3">{item.source.kind === 'file' ? `${item.source.file} · L${item.source.start_line}–${item.source.end_line} · ${item.source.source_hash?.slice(0, 12)}` : item.source.kind === 'chat' ? `会话 ${item.source.session_id} / 消息 ${item.source.message_id}（需人工核实）` : '手动整理'}{item.valid_until && ` · 到期 ${new Date(item.valid_until).toLocaleString()}`}{item.superseded_by && ` · 替代记录 ${item.superseded_by}`}</p>
      {item.status !== 'superseded' && <div className="mt-3 flex flex-wrap gap-2">
        {item.status !== 'confirmed' && <Button size="sm" variant="outline" disabled={busy} onClick={() => void save({ ...item, status: 'confirmed' })}>确认采用</Button>}
        {item.status !== 'rejected' && <Button size="sm" variant="outline" disabled={busy} onClick={() => void save({ ...item, status: 'rejected' })}>拒绝采用</Button>}
        <Button size="sm" variant="outline" disabled={busy} onClick={() => edit(item)}>编辑内容</Button>
        <Button size="sm" variant="outline" disabled={busy} onClick={() => edit(item, true)}>用新记录替代</Button>
      </div>}
    </article>)}</div>
  </Surface>;
}
