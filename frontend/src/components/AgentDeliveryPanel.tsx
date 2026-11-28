import { useEffect, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { api, errMessage, type AgentArtifact, type AgentControl, type RunEvent, type WorkspaceRun } from '@/lib/api';
import { isRunActive } from '@/lib/task-client';
import { Link } from 'react-router-dom';
import { Button } from './ui/button';
import { AgentEffects } from './AgentEffects';

interface Props { run: WorkspaceRun; events: RunEvent[]; controls?: boolean; showAnswer?: boolean }

export function AgentDeliveryPanel({ run, events, controls = false, showAnswer = false }: Props) {
  const { id, workspace_id: ws, status } = run;
  const cache = useQueryClient();
  const key = ['agent-delivery', ws, id];
  const [page, setPage] = useState<{key: string; offset: number}>();
  const pageKey = id + ':' + status;
  const changeOffset = page?.key === pageKey ? page.offset : 0;
  const delivery = useQuery({
    queryKey: [...key, status, changeOffset], queryFn: () => api.agentDelivery(ws, id, changeOffset),
    enabled: status !== 'running' && status !== 'queued', retry: 2,
  });
  const [instruction, setInstruction] = useState('');
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);
  const sending = useRef(false);
  const retryId = useRef<{ body: string; id: string }>();
  const data = delivery.data;
  const approval = data?.approval;
  const partial = data?.publication_state === 'publishing' || data?.publication_state === 'undoing_partial' || data?.publication_state === 'undoing';
  const writeLabel = partial ? (data?.publication_state === 'publishing' ? '部分文件已写入' : '部分文件已恢复') :
    data?.undone ? '已撤销' : data?.published ? '已写入工作区' : '待写入';
  const waiting = status === 'waiting_approval';
  const finalizeUndo = data?.undo_receipt_pending;
  const active = isRunActive(run);
  const finalText = (run.result as { answer?: string } | null)?.answer;
  async function control(action: AgentControl['action'], value?: string, expected?: string) {
    if (sending.current) return;
    sending.current = true;
    setBusy(true);
    setMessage('');
    const body = JSON.stringify([ws, id, action, value, expected]);
    if (retryId.current?.body !== body) retryId.current = { body, id: crypto.randomUUID() };
    try {
      await api.agentControl(ws, id, { action, value, expected, request_id: retryId.current.id });
      retryId.current = undefined;
      if (action === 'steer') setInstruction('');
      setMessage(action === 'steer' ? '指令已登记，将在下一个安全步骤处理。' : '任务状态已更新。');
    } catch (error) { setMessage(errMessage(error)); }
    finally {
      await Promise.all([cache.invalidateQueries({ queryKey: key }),
        cache.invalidateQueries({ queryKey: ['run-observation', ws, id] }),
        cache.invalidateQueries({ queryKey: ['workspace-tasks', ws] })]);
      sending.current = false; setBusy(false);
    }
  }
  const request = [...events].reverse().find(event => event.phase === 'waiting_approval' && (event.data as {digest?: string} | null)?.digest === approval);
  return <section className="my-3 space-y-3 text-sm" aria-label="任务交付">
    {showAnswer && finalText && <p className="whitespace-pre-wrap">{finalText}</p>}
    {controls && active && !data?.history && !data?.undone && <div className="space-y-2">
      {!partial && (status !== 'paused' || data) && <form className="flex flex-wrap gap-2" onSubmit={e => { e.preventDefault(); if (instruction.trim()) void control('steer', instruction.trim()); }}>
        <input aria-label="给当前任务补充指令" className="min-w-44 flex-1 rounded border bg-transparent px-3 py-1"
          placeholder="给这个任务补充要求或纠正方向" value={instruction} maxLength={8000} onChange={e => setInstruction(e.target.value)} />
        <Button type="submit" size="sm" disabled={busy || !instruction.trim()}>发送指令</Button>
      </form>}
      <div className="flex flex-wrap gap-2">
        {status === 'running' && <Button variant="outline" size="sm" disabled={busy} onClick={() => void control('pause')}>暂停</Button>}
        {status === 'paused' && <Button size="sm" disabled={busy} onClick={() => void control('resume')}>从检查点继续</Button>}
        {status === 'budget_exhausted' && <Button size="sm" disabled={busy} onClick={() => void control('extend')}>增加一轮预算</Button>}
        {!partial && (status !== 'paused' || data) && <><Button variant="outline" size="sm" disabled={busy} onClick={() => void control('mode', 'review')}>切换审阅模式</Button>
        <Button variant="outline" size="sm" disabled={busy} onClick={() => void control('mode', 'auto')}>切换自主模式</Button></>}
        <Link to="/settings" className="self-center text-xs text-brand underline">管理 Agent 工具包</Link>
      </div>
    </div>}
    {delivery.isLoading && <p className="text-xs" role="status">正在读取交付记录</p>}
    {delivery.error && <p role="alert">{errMessage(delivery.error)} <Button size="sm" variant="ghost" onClick={() => void delivery.refetch()}>重试读取交付</Button></p>}
    {data && <>
      {(data.change_total ?? data.changes?.length ?? 0) > 0 && <details className="ns-record-detail" open={waiting || undefined}>
        <summary>本轮文件改动 · {data.change_total ?? data.changes.length} 项 · {writeLabel}</summary>
        <ul className="ns-detail-body space-y-1">{data.changes.map(change => <li key={change.path}>
          <details><summary className="break-all">{({ created: '新增', modified: '修改', deleted: '删除' } as Record<string,string>)[change.kind] || change.kind} · {change.path}</summary>
            {change.diff ? <pre className="max-h-72 overflow-auto whitespace-pre-wrap">{change.diff}</pre> :
              <p className="break-all text-xs">修改前 {change.before_hash || '无'}<br/>修改后 {change.after_hash || '无'}</p>}
          </details></li>)}</ul>
        {(changeOffset > 0 || data.next_change_offset != null) && <div className="flex flex-wrap items-center gap-2 py-2 text-xs">
          <Button size="sm" variant="outline" disabled={changeOffset === 0} onClick={() => setPage({ key: pageKey, offset: Math.max(0, changeOffset - 100) })}>上一页</Button>
          <span>{(data.change_offset ?? changeOffset) + 1}–{(data.change_offset ?? changeOffset) + data.changes.length} / {data.change_total}</span>
          <Button size="sm" variant="outline" disabled={data.next_change_offset == null}
            onClick={() => { if (data.next_change_offset != null) setPage({ key: pageKey, offset: data.next_change_offset }); }}>下一页</Button>
        </div>}
      </details>}
      {controls && !data.history && waiting && approval && <div className="space-y-2">
        <p>{request?.message || '请核对本轮文件改动后确认写入工作区。'}</p>
        <Button size="sm" disabled={busy} onClick={() => void control('approve', undefined, approval)}>确认此次操作</Button>{' '}
        <Button size="sm" variant="outline" disabled={busy} onClick={() => void control('reject', undefined, approval)}>拒绝</Button>
      </div>}
      {!!data.conflicts?.length && <details className="space-y-2 text-sm"><summary>运行期间同时修改的文件（{data.conflicts.length}）</summary>
        <p className="text-text-2">草稿已采用更新后的工作区版本。重新合并这些文件后，写回工作区前会再次请你确认。</p>
        {data.conflicts.map(item => <div key={item.path} className="rounded border border-border-subtle p-2"><strong>{item.path}</strong>
          {item.diff && <details><summary>查看原草稿改动</summary><pre className="overflow-auto whitespace-pre-wrap">{item.diff}</pre></details>}</div>)}
      </details>}
      {!!data.effects?.length && <AgentEffects items={data.effects} allowRestore={controls && !!data.can_restore_services} busy={busy} onRestore={(item) => control('restore_service', item.id, item.digest)} />}
      {!!data.artifacts?.length && <div className="space-y-2"><h3 className="font-semibold">交付文件</h3>
        {data.artifacts.map(item => <ArtifactCard key={item.id + item.sha256} item={item} workspace={ws} run={id} />)}
      </div>}
      {controls && !data.history && data.can_undo &&
        <Button variant="outline" size="sm" disabled={busy} onClick={() => void control('undo', undefined, data.digest)}>{finalizeUndo ? '确认撤销结果' : partial ? '撤回已写入的文件' : '撤销本轮全部文件改动'}</Button>}
      {data.history && <p>这是备份恢复的历史记录，可查看和导出交付文件。</p>}
      {data.undone && <p role="status">{finalizeUndo ? '文件已恢复，请确认撤销结果以结束本轮任务。' : '本轮文件改动已撤销。'}</p>}
    </>}
    {message && <p role="status">{message}</p>}
  </section>;
}

function ArtifactCard({ item, workspace, run }: { item: AgentArtifact; workspace: string; run: string }) {
  const [preview, setPreview] = useState<{ url?: string; text?: string }>();
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);
  const mounted = useRef(true);
  const url = useRef('');
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; if (url.current) URL.revokeObjectURL(url.current); };
  }, []);
  async function loadPreview() {
    if (preview) { setPreview(undefined); if (url.current) URL.revokeObjectURL(url.current); url.current = ''; return; }
    setBusy(true); setMessage('');
    try {
      const bytes = new Uint8Array(await api.agentPreview(workspace, run, item.id));
      if (!mounted.current) return;
      const png = [137,80,78,71,13,10,26,10].every((b,i) => bytes[i] === b);
      const jpeg = bytes[0] === 255 && bytes[1] === 216 && bytes[2] === 255;
      if (png || jpeg) {
        url.current = URL.createObjectURL(new Blob([bytes], { type: png ? 'image/png' : 'image/jpeg' }));
        setPreview({ url: url.current });
      } else {
        const text = new TextDecoder('utf-8', { fatal: true }).decode(bytes);
        if (text.includes('\0')) throw new Error('此产物请导出后打开。');
        setPreview({ text });
      }
    } catch (e) { if (mounted.current) setMessage(errMessage(e)); }
    finally { if (mounted.current) setBusy(false); }
  }
  async function save() {
    setBusy(true); setMessage('');
    try { if (await api.exportAgentArtifact(workspace, run, item) && mounted.current) setMessage('文件已导出，SHA-256 校验通过。'); }
    catch (e) { if (mounted.current) setMessage(errMessage(e)); }
    finally { if (mounted.current) setBusy(false); }
  }
  return <article className="rounded border border-border-subtle p-3">
    <h4 className="break-all font-semibold">{item.name}</h4>
    <p className="break-all text-xs text-text-2">{item.path} · {item.mime} · {item.size.toLocaleString()} 字节</p>
    <details className="my-1 text-xs"><summary>文件校验</summary><p className="break-all">SHA-256 {item.sha256}</p>
      {item.checks?.map(check => <p key={check}>{check}</p>)}</details>
    <div className="flex gap-2">
      {item.preview && <Button size="sm" variant="outline" disabled={busy} onClick={() => void loadPreview()}>{preview ? '收起预览' : '预览'}</Button>}
      <Button size="sm" variant="outline" disabled={busy} onClick={() => void save()}>导出到文件夹</Button>
    </div>
    {preview?.url && <img className="mt-2 max-h-96 max-w-full object-contain" src={preview.url} alt={item.name} />}
    {preview?.text !== undefined && <pre className="mt-2 max-h-72 overflow-auto whitespace-pre-wrap">{preview.text}</pre>}
    {message && <p role="status" className="mt-2 text-xs">{message}</p>}
  </article>;
}
