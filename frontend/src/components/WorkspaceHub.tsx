import { useEffect, useRef, useState } from 'react';
import { FolderOpen, ListTodo, Plus, RefreshCw } from 'lucide-react';
import { api, errMessage, type RunEvent, type WorkspaceRun } from '@/lib/api';
import { isRunActive, RUN_STATUS_LABEL } from '@/lib/task-client';
import { useWorkspaceStore, type WorkspaceInput } from '@/stores/workspace-store';
import { useBackend } from '@/hooks/useBackend';
import { Button } from './ui/button';
import { Input } from './ui/input';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from './ui/dialog';
import './workspace-hub.css';
import { AgentActions } from './AgentActions';
import { AgentResultView } from './AgentTaskRecord';

const KIND_LABEL: Record<string, string> = { chat: '对话', index: '代码索引', diagnosis: '诊断', agent: 'Agent' };
const PHASE_LABEL: Record<string, string> = { ...RUN_STATUS_LABEL, preparing:'准备上下文', waiting:'等待模型', streaming:'正在生成', planning:'分析问题', tool:'工具检查', indexing:'解析项目', indexed:'索引已保存' };

export function WorkspaceHub() {
  const [open, setOpen] = useState(false);
  const activeId = useWorkspaceStore(state => state.activeId);
  const name = useWorkspaceStore(state => state.workspaces.find(item => item.id === state.activeId)?.name ?? '本地工作区');
  return <Dialog open={open} onOpenChange={setOpen}>
    <Button variant="outline" className="ns-workspace-trigger" onClick={() => setOpen(true)} aria-label={`工作区与任务：${name}`} title={`${name} · 工作区与任务`}>
      <FolderOpen size={15} aria-hidden="true" /><span>{name}</span><ListTodo size={14} aria-hidden="true" />
    </Button>
    {open && <DialogContent className="ns-workspace-dialog" aria-describedby="workspace-hub-description">
      <DialogTitle>工作区与任务</DialogTitle>
      <DialogDescription id="workspace-hub-description">每个工作区保存自己的项目目录、默认模型和任务记录。索引与诊断在后台继续运行，切换页面不会取消。</DialogDescription>
      <WorkspacePanel key={activeId} />
    </DialogContent>}
  </Dialog>;
}

function WorkspacePanel() {
  const { models } = useBackend();
  const activeId = useWorkspaceStore(state => state.activeId);
  const workspaces = useWorkspaceStore(state => state.workspaces);
  const current = workspaces.find(item => item.id === activeId);
  const [draft, setDraft] = useState<WorkspaceInput>(() => ({ id: current?.id, name: current?.name ?? '', project_path: current?.project_path ?? '', default_model_id: current?.default_model_id ?? '' }));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [saved, setSaved] = useState('');
  const [runs, setRuns] = useState<WorkspaceRun[]>([]);
  const [runsError, setRunsError] = useState('');
  const [loading, setLoading] = useState(true);
  const [selectedId, setSelectedId] = useState('');
  const [revision, setRevision] = useState(0);
  const selected = runs.find(run => run.id === selectedId);

  useEffect(() => { void useWorkspaceStore.getState().refresh().catch(error => setError(errMessage(error))); }, []);

  useEffect(() => {
    let disposed = false, inFlight = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let failures = 0;
    const poll = async () => {
      if (disposed || inFlight || document.hidden) return;
      inFlight = true;
      let delay = 7000;
      try {
        const response = await api.listWorkspaceRuns(activeId);
        if (!Array.isArray(response.runs)) throw new Error('任务列表格式异常，请重试。');
        if (disposed) return;
        setRuns(response.runs); setRunsError(''); failures = 0;
        delay = response.runs.some(isRunActive) ? 1500 : 7000;
        setSelectedId(id => response.runs.some(run => run.id === id) ? id : response.runs[0]?.id ?? '');
      } catch (error) {
        if (!disposed) setRunsError(errMessage(error));
        delay = Math.min(2000 * 2 ** failures++, 15000);
      } finally {
        inFlight = false;
        if (!disposed) { setLoading(false); timer = setTimeout(() => void poll(), delay); }
      }
    };
    const resume = () => { clearTimeout(timer); void poll(); };
    void poll(); document.addEventListener('visibilitychange', resume);
    return () => { disposed = true; clearTimeout(timer); document.removeEventListener('visibilitychange', resume); };
  }, [activeId, revision]);

  const save = async (event: React.FormEvent) => {
    event.preventDefault(); if (saving) return;
    setSaving(true); setError(''); setSaved('');
    try {
      const workspace = await useWorkspaceStore.getState().save({ ...draft, name: draft.name.trim(), project_path: draft.project_path.trim() });
      useWorkspaceStore.getState().select(workspace.id);
      setDraft(workspace); setSaved('工作区已保存');
    } catch (error) { setError(errMessage(error)); }
    finally { setSaving(false); }
  };

  return <div className="ns-workspace-body">
    <form onSubmit={event => void save(event)} className="ns-workspace-editor">
      <div className="flex items-center gap-2">
        <label className="min-w-0 flex-1 text-xs text-text-2">当前工作区
          <select aria-label="当前工作区" className="ns-workspace-select mt-1" value={activeId} onChange={event => useWorkspaceStore.getState().select(event.target.value)} disabled={saving}>
            {workspaces.map(workspace => <option key={workspace.id} value={workspace.id}>{workspace.name}</option>)}
          </select>
        </label>
        <Button type="button" variant="outline" className="mt-5" disabled={saving} onClick={() => { setDraft({ name: '', project_path: '', default_model_id: '' }); setError(''); setSaved(''); }}><Plus size={14} />新建</Button>
      </div>
      <p className="text-xs font-medium text-text-2">{draft.id ? '工作区设置' : '创建工作区'}</p>
      <label className="block text-xs text-text-2">名称<Input aria-label="工作区名称" required maxLength={100} value={draft.name} onChange={event => setDraft({ ...draft, name: event.target.value })} className="mt-1" /></label>
      <label className="block text-xs text-text-2">项目目录<Input aria-label="工作区项目目录" value={draft.project_path} onChange={event => setDraft({ ...draft, project_path: event.target.value })} placeholder="输入本机项目绝对路径" className="mt-1" /></label>
      <p className="text-[11px] leading-relaxed text-text-3">可留空用于一般对话和系统诊断。保存时检查目录；更换目录后需要重新索引。</p>
      <label className="block text-xs text-text-2">默认模型
        <select aria-label="工作区默认模型" className="ns-workspace-select mt-1" value={draft.default_model_id} onChange={event => setDraft({ ...draft, default_model_id: event.target.value })}>
          <option value="">使用全局默认模型</option>
          {models.filter(model => model.enabled !== false).map(model => <option key={model.id} value={model.id}>{model.name || model.model_name}</option>)}
          {draft.default_model_id && !models.some(model => model.id === draft.default_model_id && model.enabled !== false) && <option value={draft.default_model_id}>当前模型不可用（{draft.default_model_id}）</option>}
        </select>
      </label>
      <Button type="submit" disabled={saving || !draft.name.trim()}>{saving ? '保存中…' : draft.id ? '保存工作区' : '创建并切换'}</Button>
      {saved && <p role="status" className="text-xs text-ok">{saved}</p>}
      {error && <p role="alert" className="text-xs text-err">{error}</p>}
      {current?.project_id && <p className="break-all text-[11px] text-text-3">当前项目索引已就绪</p>}
    </form>
    <section className="ns-workspace-tasks" aria-label="工作区任务">
      <div className="flex items-center justify-between gap-2"><h3 className="text-sm font-semibold">任务记录</h3><Button variant="ghost" size="icon" aria-label="刷新任务" onClick={() => setRevision(value => value + 1)}><RefreshCw size={14} /></Button></div>
      {runsError && <p role="alert" className="mb-3 text-xs text-err">{runsError}</p>}
      {loading && <p role="status" className="text-xs text-text-3">正在读取任务…</p>}
      {!loading && !runs.length && !runsError && <p className="py-6 text-sm text-text-3">还没有任务。在 AI 对话、CodeLens 或诊断 Agent 中开始工作，进度和结果会显示在这里。</p>}
      <div className="ns-workspace-run-list">{runs.map(run => <button key={run.id} className="ns-workspace-run" aria-pressed={selectedId === run.id} onClick={() => setSelectedId(run.id)}>
        <span className="min-w-0"><strong>{run.title || KIND_LABEL[run.kind] || run.kind}</strong><small>{KIND_LABEL[run.kind] || run.kind} · {new Date(run.created_at).toLocaleString()}</small></span>
        <span className="shrink-0 text-xs" data-status={run.status}>{RUN_STATUS_LABEL[run.status] || run.status}</span>
      </button>)}</div>
      {selected && <RunDetail key={selected.id} initial={selected} onChanged={() => setRevision(value => value + 1)} />}
    </section>
  </div>;
}

function RunDetail({ initial, onChanged }: { initial: WorkspaceRun; onChanged: () => void }) {
  const [run, setRun] = useState(initial);
  const [events, setEvents] = useState<RunEvent[]>([]);
  const [error, setError] = useState('');
  const [cancelling, setCancelling] = useState(false);
  const changeRef = useRef(onChanged); changeRef.current = onChanged;
  useEffect(() => {
    let disposed = false, inFlight = false, after = 0, unchanged = 0;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const poll = async () => {
      if (disposed || inFlight || document.hidden) return;
      inFlight = true; let again = true; let delay = 1200;
      try {
        const [latest, response] = await Promise.all([api.getWorkspaceRun(initial.id), api.getRunEvents(initial.id, after)]);
        if (!Array.isArray(response.events)) throw new Error('任务事件格式异常，请重试。');
        if (disposed) return;
        setRun(latest); setError('');
        if (response.events.length) {
          after = Math.max(after, ...response.events.map(event => event.seq));
          setEvents(previous => [...previous, ...response.events].slice(-300)); unchanged = 0;
        } else unchanged++;
        delay = Math.min(1000 + unchanged * 700, 5000);
        again = isRunActive(latest);
      } catch (error) { if (!disposed) setError(errMessage(error)); delay = 5000; }
      finally { inFlight = false; if (!disposed && again) timer = setTimeout(() => void poll(), delay); }
    };
    const resume = () => { clearTimeout(timer); void poll(); };
    void poll(); document.addEventListener('visibilitychange', resume);
    return () => { disposed = true; clearTimeout(timer); document.removeEventListener('visibilitychange', resume); };
  }, [initial.id]);
  const cancel = async () => {
    setCancelling(true); setError('');
    try { setRun(await api.cancelWorkspaceRun(run.id)); changeRef.current(); }
    catch (error) { setError(errMessage(error)); }
    finally { setCancelling(false); }
  };
  return <section className="ns-workspace-detail" aria-label="任务详情">
    <div className="flex flex-wrap items-center justify-between gap-2"><h4 className="min-w-0 break-words text-sm font-semibold">{run.title || KIND_LABEL[run.kind]}</h4>{isRunActive(run) && <Button variant="outline" size="sm" disabled={cancelling} onClick={() => void cancel()}>{cancelling ? '取消中…' : '取消任务'}</Button>}</div>
    <p className="mt-2 text-xs text-text-2">{RUN_STATUS_LABEL[run.status] || run.status}{run.phase && run.phase !== run.status ? ` · ${PHASE_LABEL[run.phase] || run.phase}` : ''}</p>
    {run.status === 'interrupted' && <p className="mt-2 text-xs text-text-2">上次应用退出时任务尚未完成。任务不会自动重放，请按需重新发起。</p>}
    {(error || run.error) && <p role="alert" className="mt-2 text-xs text-err">{error || run.error}</p>}
    {events.length > 0 && <ol className="ns-workspace-events">{events.map(event => <li key={event.seq}><span>{PHASE_LABEL[event.phase] || event.phase}</span><p>{event.message}</p>{event.data != null && <details><summary>查看数据</summary><pre>{displayResult(event.data)}</pre></details>}</li>)}</ol>}
    {run.result != null && <TaskResult run={run} />}
    {run.kind === 'agent' && <AgentActions key={run.id} workspaceId={run.workspace_id} runId={run.id} taskActive={isRunActive(run)} />}
  </section>;
}

function TaskResult({ run }: { run: WorkspaceRun }) {
  if (run.kind === 'agent') return <AgentResultView result={run.result} />;
  const result = run.result as Record<string, unknown>;
  const answer = result && typeof result === 'object' ? result.content ?? result.answer : undefined;
  return <div className="mt-4"><h5 className="text-xs font-medium">任务结果</h5>
    {typeof answer === 'string' ? <p className="ns-workspace-result text-text-1">{displayResult(answer)}</p>
      : run.kind === 'index' && result && typeof result === 'object' ? <p className="ns-workspace-result">已解析 {String(result.total_files ?? 0)} 个文件，保存 {String(result.indexed_chunks ?? 0)} 个代码片段。{result.retrieval_ready === true ? '项目上下文已可用于对话。' : ''}</p>
      : <pre className="ns-workspace-result">{displayResult(result)}</pre>}
    <details className="mt-2 text-xs text-text-3"><summary>完整记录</summary><pre className="ns-workspace-result">{displayResult(result)}</pre></details>
  </div>;
}

function displayResult(result: unknown) {
  const text = typeof result === 'string' ? result : JSON.stringify(result, null, 2);
  return text.length > 120000 ? `${text.slice(0, 120000)}\n…结果较长，当前显示前 120000 个字符。` : text;
}
