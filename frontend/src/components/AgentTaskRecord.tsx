import { useEffect, useState } from 'react';
import { api, errMessage, type RunEvent, type WorkspaceRun } from '@/lib/api';
import { isRunActive, RUN_STATUS_LABEL } from '@/lib/task-client';
import { AgentActions } from './AgentActions';
import { Button } from './ui/button';

interface Evidence { id: string; kind: string; title: string; file?: string; start_line?: number; end_line?: number; source_hash?: string; memory_id?: string; revision?: number; excerpt: string; observed_at: string }
interface Step { number: number; tool: string; duration_ms: number; result: { success: boolean; output?: string; error?: string; evidence?: Evidence[] } }
interface Result { answer?: string; steps?: Step[]; evidence?: Evidence[]; model_calls?: number; tool_calls?: number; stop_reason?: string; model_id?: string }

export function AgentResultView({ result, events = [], showAnswer = true }: { result: unknown; events?: RunEvent[]; showAnswer?: boolean }) {
  const data = (result ?? {}) as Result;
  const steps = data.steps ?? events.filter(e => e.phase === 'tool_completed').map(e => e.data as Step).filter(s => s?.result);
  const evidence = data.evidence ?? steps.flatMap(s => s.result.evidence ?? []);
  return <div className="mt-3 space-y-3 text-xs text-text-2" data-testid="agent-evidence">
    {showAnswer && data.answer && <div><h4 className="font-semibold text-brand">模型结论</h4><p className="mt-2 whitespace-pre-wrap leading-7 text-sm text-text-1">{data.answer}</p></div>}
    <p>已执行 {data.tool_calls ?? steps.length} 次工具调用{data.model_calls != null ? ` · ${data.model_calls} 次模型调用` : ''}{data.model_id ? ` · ${data.model_id}` : ''}</p>
    {steps.map(step => <details className="ns-record-detail" key={step.number}><summary>{step.number}. {step.tool} · {step.result.success ? '完成' : '失败'} · {step.duration_ms} ms</summary><pre className="ns-detail-body max-h-48 overflow-auto whitespace-pre-wrap">{step.result.output || step.result.error || '结果见下方证据'}</pre></details>)}
    <h4 className="font-semibold">工具采集的证据 · {evidence.length} 条</h4>
    {evidence.map(e => <details className="ns-record-detail" key={e.id}><summary>[{e.id}] {e.file || e.title}{e.start_line ? `:${e.start_line}–${e.end_line}` : ''}</summary><div className="ns-detail-body space-y-2"><p>{e.kind} · {e.observed_at}</p>{e.source_hash && <p className="break-all font-mono">SHA-256: {e.source_hash}</p>}{e.memory_id && <p>记忆 {e.memory_id} · 版本 {e.revision}</p>}<pre className="max-h-64 overflow-auto whitespace-pre-wrap">{e.excerpt}</pre></div></details>)}
    {!evidence.length && <p>尚无工具证据。模型回答本身不构成已执行操作的证明。</p>}
    {data.stop_reason && data.stop_reason !== 'completed' && <p>停止原因：{data.stop_reason}</p>}
  </div>;
}

export function AgentTaskRecord({ runId, workspaceId, expanded = false, showAnswer = false }: { runId: string; workspaceId: string; expanded?: boolean; showAnswer?: boolean }) {
  const [open, setOpen] = useState(expanded);
  const [run, setRun] = useState<WorkspaceRun | null>(null);
  const [events, setEvents] = useState<RunEvent[]>([]);
  const [error, setError] = useState('');
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    if (!open) return;
    let disposed = false, after = 0;
    let timer: ReturnType<typeof setTimeout>;
    setRun(null); setEvents([]); setError('');
    const poll = async () => {
      try {
        const [next, response] = await Promise.all([api.getWorkspaceRun(runId), api.getRunEvents(runId, after)]);
        if (disposed) return;
        if (next.workspace_id !== workspaceId || next.kind !== 'agent' || !Array.isArray(response.events)) throw new Error('Agent 任务归属不匹配');
        setRun(next); setEvents(old => [...old, ...response.events]);
        after = Math.max(after, ...response.events.map(e => e.seq));
        if (isRunActive(next)) timer = setTimeout(() => void poll(), 1400);
      } catch (e) { if (!disposed) setError(errMessage(e)); }
    };
    void poll();
    return () => { disposed = true; clearTimeout(timer); };
  }, [open, runId, workspaceId, revision]);
  return <details open={open} onToggle={e => setOpen(e.currentTarget.open)} className="ns-record-detail mt-3">
    <summary>Agent 执行记录{run ? ` · ${RUN_STATUS_LABEL[run.status]}` : ''}</summary>
    {open && <div className="ns-detail-body">
      {error && <p role="alert">{error} <Button variant="outline" onClick={() => setRevision(r => r + 1)}>重新读取</Button></p>}
      {!run && !error && <p role="status">正在读取已保存的任务…</p>}
      {run && <><p role="status">{events[events.length - 1]?.message || RUN_STATUS_LABEL[run.status]}</p>
        {isRunActive(run) && <Button variant="outline" className="mt-2" onClick={() => { void api.cancelWorkspaceRun(run.id).then(() => setRevision(r => r + 1), e => setError(errMessage(e))); }}>取消 Agent 任务</Button>}
        {run.error && <p role="alert" className="mt-2 text-err">{run.error}</p>}
        <AgentResultView result={run.result} events={events} showAnswer={showAnswer} />
        <AgentActions key={runId} workspaceId={workspaceId} runId={runId} taskActive={isRunActive(run)} />
      </>}
    </div>}
  </details>;
}
