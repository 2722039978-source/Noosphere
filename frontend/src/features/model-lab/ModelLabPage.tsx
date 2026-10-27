import { useEffect, useRef, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { modelEngineering, waitForModelEngineeringRun } from '@/lib/model-engineering-client';
import type { DatasetRevision, Evaluation, InferenceProfile, ModelEngineeringRequest, ModelEngineeringResponses, ModelRevision, Options } from '@/lib/model-engineering-types';
import { useWorkspaceStore } from '@/stores/workspace-store';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Surface } from '@/components/ui/surface';
import { EmptyState, LoadingState } from '@/components/ui/feedback';
import { LabError, LabShell, JsonView } from './shared';
import { active, evidenceLabel, metricText, sameOptions, short, STATUS, validateSetup } from './presentation';
import { FailureCasePanel } from './FailureCasePanel';
import { HardwarePanel } from './HardwarePanel';

const DEFAULT: Options = { temperature: 0, seed: 7, num_ctx: 4096, max_output: 1024, deadline_ns: 120e9, parallelism: 1, retry_limit: 0, keep_alive: '0' };
type Detail = ModelEngineeringResponses['get_evaluation'];
export function ModelLabPage() {
  const workspace = useWorkspaceStore(s => s.activeId), [query, setQuery] = useSearchParams();
  const selectedID = query.get('evaluation') || '';
  const [models, setModels] = useState<ModelRevision[]>([]), [profiles, setProfiles] = useState<InferenceProfile[]>([]), [datasets, setDatasets] = useState<DatasetRevision[]>([]);
  const [history, setHistory] = useState<Evaluation[]>([]), [historyCursor, setHistoryCursor] = useState('');
  const [environment, setEnvironment] = useState<ModelEngineeringResponses['environment'] | null>(null);
  const [stable, setStable] = useState(''), [candidate, setCandidate] = useState(''), [datasetID, setDatasetID] = useState(''), [profileID, setProfileID] = useState('');
  const [options, setOptions] = useState<Options>(DEFAULT), [detail, setDetail] = useState<Detail | null>(null), [caseID, setCaseID] = useState('');
  const [error, setError] = useState<unknown>(), [loading, setLoading] = useState(true), [busy, setBusy] = useState(false), [refresh, setRefresh] = useState(0);
  const [observationError, setObservationError] = useState<unknown>();
  const [failedIntent, setFailedIntent] = useState<ModelEngineeringRequest | null>(null);
  const epoch = useRef(0), mutation = useRef(false);
  const selectedRef = useRef(selectedID); selectedRef.current = selectedID;
  const dataset = datasets.find(d => d.id === datasetID);
  const budget = environment?.preflight.budget;
  const setupError = validateSetup(stable, candidate, datasetID, options, budget);
  const selectedProfile = profiles.find(p => p.id === profileID);
  const profileSaved = selectedProfile && sameOptions(selectedProfile.requested, options);
  useEffect(() => {
    const generation = ++epoch.current; let disposed = false; setLoading(true); setError(undefined); setEnvironment(null); setDetail(null); setCaseID(''); setFailedIntent(null); setBusy(false); mutation.current = false;
    setStable(''); setCandidate(''); setProfileID(''); setHistory([]); setHistoryCursor('');
    Promise.allSettled([
      modelEngineering({ operation: 'list_models', limit: 100 }), modelEngineering({ operation: 'list_profiles', limit: 100 }),
      modelEngineering({ operation: 'list_datasets' }), modelEngineering({ operation: 'environment' }),
      modelEngineering({ operation: 'list_evaluations', workspace_id: workspace, limit: 20 }),
    ]).then(([m, p, d, e, h]) => {
      if (disposed || epoch.current !== generation) return;
      if (m.status === 'fulfilled') { setModels(m.value.models); setStable(m.value.models[0]?.id || ''); setCandidate(m.value.models[1]?.id || ''); }
      if (p.status === 'fulfilled') { setProfiles(p.value.profiles); if (p.value.profiles[0]) { setProfileID(p.value.profiles[0].id); setOptions(p.value.profiles[0].requested); } }
      if (d.status === 'fulfilled') { setDatasets(d.value.datasets); setDatasetID(d.value.datasets[0]?.id || ''); }
      if (e.status === 'fulfilled') {
        setEnvironment(e.value);
        if (p.status !== 'fulfilled' || !p.value.profiles.length) { const b = e.value.preflight.budget; setOptions({ ...DEFAULT, num_ctx: Math.min(DEFAULT.num_ctx, b?.max_context ?? 8192), max_output: Math.min(DEFAULT.max_output, b?.max_output ?? 2048), deadline_ns: Math.min(DEFAULT.deadline_ns!, b?.inference_timeout_ns ?? 120e9) }); }
      }
      if (h.status === 'fulfilled') { setHistory(h.value.evaluations); setHistoryCursor(h.value.next_cursor); }
      const failure = [m, p, d, e, h].find(x => x.status === 'rejected'); if (failure?.status === 'rejected') setError(failure.reason);
      setLoading(false);
    });
    return () => { disposed = true; epoch.current++; };
  }, [workspace]);
  // WorkspaceRun observation is cancellable; it never owns the evaluation. Navigation
  // only aborts observation. All controls below send explicit persisted commands.
  useEffect(() => {
    setDetail(null); setCaseID(''); setObservationError(undefined);
    if (!selectedID) return;
    const controller = new AbortController(); let inFlight: Promise<Detail | null> | null = null;
    let timer: ReturnType<typeof setTimeout> | undefined, runObserved = false, failures = 0;
    setObservationError(undefined);
    const update = (): Promise<Detail | null> => {
      if (controller.signal.aborted) return Promise.resolve(null);
      if (inFlight) return inFlight;
      inFlight = modelEngineering({ operation: 'get_evaluation', workspace_id: workspace, id: selectedID, limit: 100 }).then(r => {
        if (!controller.signal.aborted) { setDetail(r); setObservationError(undefined); setHistory(old => [r.evaluation, ...old.filter(e => e.id !== r.evaluation.id)]); }
        return r;
      }).catch(e => { if (!controller.signal.aborted) setObservationError(e); return null; }).finally(() => { inFlight = null; });
      return inFlight;
    };
    const observe = async () => {
      let r = await update(); if (controller.signal.aborted) return;
      if (r && !active(r.evaluation.status)) return;
      if (r?.evaluation.run_id && !runObserved) {
        runObserved = true;
        try { await waitForModelEngineeringRun(r.evaluation.run_id, { signal: controller.signal, intervalMs: 1200, onUpdate: () => { void update(); } }); }
        catch { /* A transient run read or terminal WorkspaceRun does not terminate domain observation. */ }
        if (controller.signal.aborted) return;
        // A terminal run callback may be sharing an older in-flight domain read.
        // Drain that read, then fetch again so a late running snapshot cannot strand UI.
        await update(); r = await update();
      }
      if (!controller.signal.aborted && (!r || active(r.evaluation.status))) {
        failures = r ? 0 : failures + 1;
        timer = setTimeout(() => void observe(), Math.min(1200 * (1 + failures), 6000));
      }
    };
    void observe(); return () => { controller.abort(); clearTimeout(timer); };
  }, [selectedID, workspace, refresh]);
  const mutate = async (request: ModelEngineeringRequest) => {
    if (mutation.current) return; mutation.current = true; const generation = epoch.current, selection = selectedRef.current;
    setBusy(true); setError(undefined); setFailedIntent(null);
    try {
      const r = await modelEngineering(request);
      if (generation !== epoch.current || (request.operation !== 'create_profile' && selection !== selectedRef.current)) return;
      if ('evaluation_id' in r) { setQuery({ evaluation: r.evaluation_id }); setRefresh(v => v + 1); }
      else if (request.operation === 'create_profile') { const p = r as InferenceProfile; setProfiles(old => [p, ...old.filter(x => x.id !== p.id)]); setProfileID(p.id); setOptions(p.requested); }
    } catch (e) { if (generation === epoch.current && selection === selectedRef.current) { setError(e); if (request.operation !== 'create_profile') setFailedIntent(request); } }
    finally { if (generation === epoch.current) { mutation.current = false; setBusy(false); } }
  };
  const command = (command: 'cancel' | 'resume') => detail && mutate({ operation: 'evaluation_command', workspace_id: workspace, id: detail.evaluation.id, command, expected_revision: detail.evaluation.revision, idempotency_key: crypto.randomUUID() });
  const changeOption = (key: keyof Options, value: number) => setOptions(old => ({ ...old, [key]: value }));
  return <LabShell title="Model Lab" subtitle="在同一 CodeLens 编程任务集上比较模型 · 本地串行执行，评分与证据可复查">
    <p className="ns-lab-notice">离开页面后后台任务继续运行。真实离线测评与模拟发布是不同证据；离线通过不会自动改变日常模型或绕过工具审批。</p>
    <LabError error={error} onRetry={failedIntent ? () => void mutate(failedIntent) : undefined} />
    <LabError error={observationError} />
    {failedIntent && <JsonView value={failedIntent} title="待重试的原请求（沿用原参数、任务版本及幂等键）" />}
    {loading ? <LoadingState label="正在读取测评准备状态…" /> : <Surface className="ns-lab-panel"><div className="ns-lab-section"><h2>准备成对测评</h2><Link to="/model-hub">管理模型版本 →</Link></div>
      {models.length < 2 && <p className="ns-lab-notice">需要两个已注册的原生 Ollama 模型版本。OpenAI 兼容连接可用于 Gateway；原生测评适配尚未提供。</p>}
      <div className="ns-lab-grid"><label className="ns-lab-field">Stable 稳定模型<select value={stable} onChange={e => setStable(e.target.value)}><option value="">选择模型版本</option>{models.map(m => <option key={m.id} value={m.id}>{m.model_name} · {short(m.digest)}</option>)}</select></label>
        <label className="ns-lab-field">Candidate 候选模型<select value={candidate} onChange={e => setCandidate(e.target.value)}><option value="">选择模型版本</option>{models.map(m => <option key={m.id} value={m.id}>{m.model_name} · {short(m.digest)}</option>)}</select></label>
        <label className="ns-lab-field">测评集<select value={datasetID} onChange={e => setDatasetID(e.target.value)}>{datasets.map(d => <option key={d.id} value={d.id}>{d.benchmark_id} · {d.version} · {d.cases.length} 例</option>)}</select></label>
        <label className="ns-lab-field">已保存推理配置<select value={profileID} onChange={e => { setProfileID(e.target.value); const p = profiles.find(x => x.id === e.target.value); if (p) setOptions(p.requested); }}><option value="">新建配置</option>{profiles.map(p => <option key={p.id} value={p.id}>{short(p.id)} · ctx {p.requested.num_ctx} / out {p.requested.max_output}</option>)}</select></label>
      </div>
      <div className="ns-lab-grid ns-lab-options">{([['temperature', '采样温度', options.temperature, 0.1], ['seed', '随机种子', options.seed, 1], ['num_ctx', '上下文预算', options.num_ctx, 1], ['max_output', '输出预算', options.max_output, 1], ['deadline_ns', '推理时限（秒，0=120）', (options.deadline_ns ?? 120e9) / 1e9, 1]] as const).map(([key, label, value, step]) => <label className="ns-lab-field" key={key}>{label}<Input type="number" step={step} value={Number.isFinite(value) ? value : ''} onChange={e => changeOption(key, e.target.value === '' ? NaN : Number(e.target.value) * (key === 'deadline_ns' ? 1e9 : 1))} /></label>)}</div>
      <p className="ns-lab-muted">{budget ? `服务端预算：上下文 ≤${budget.max_context}、输出 ≤${budget.max_output}、单次推理 ≤${budget.inference_timeout_ns / 1e9} 秒` : '服务端预算未提供；界面暂按全局上限 8192 / 2048 / 120 秒校验，实际配置未确认'}；并发 1，无自动重试。服务端再次校验模型身份、CodeLens 与资源准入，显存未知不代表资源充足。</p>
      {environment ? <p className={environment.preflight.available ? 'ns-lab-muted' : 'ns-lab-notice'} role="status">运行环境：{environment.environment.os}/{environment.environment.arch} · {environment.environment.cores || '?'} 核 · 显存 {metricText(environment.environment.vram)} · {environment.preflight.available ? '执行器就绪；CodeLens、模型身份与物理资源仍需启动时校验' : `执行器预检未通过：${environment.preflight.code || '执行器未就绪'}`}</p> : <p className="ns-lab-notice">执行器预检尚不可用，请检查后端与 CodeLens 服务并刷新准备状态。</p>}
      {setupError && <p role="status" className="ns-lab-muted">{setupError}</p>}
      <div className="ns-lab-actions"><Button variant="outline" disabled={busy || !!validateSetup('a', 'b', 'dataset', options, budget)} onClick={() => void mutate({ operation: 'create_profile', requested: options })}>保存推理配置</Button>
        <Button disabled={busy || !!setupError || !profileSaved || !environment?.preflight.available} onClick={() => void mutate({ operation: 'create_evaluation', workspace_id: workspace, stable_revision_id: stable, candidate_revision_id: candidate, profile_id: profileID, dataset_id: datasetID, workflow_id: 'codelens.codetask.go.v1', idempotency_key: crypto.randomUUID() })}>{busy ? '正在提交…' : '开始真实离线测评'}</Button>
        <Button variant="ghost" disabled={busy} onClick={() => window.location.reload()}>刷新准备状态</Button></div>
      {!profileSaved && <p className="ns-lab-muted">启动前请保存当前参数。保存后配置不可变，两个模型使用同一份配置。</p>}
      <JsonView value={{ stable: models.find(m => m.id === stable), candidate: models.find(m => m.id === candidate), requested: options, environment: environment?.environment }} title="核对模型身份、完整参数与硬件环境" />
      {dataset && <JsonView value={dataset} title={`查看测评集 ${dataset.version} · 输入证据引用、预期结果与版本`} />}
    </Surface>}
    {selectedID && <Surface className="ns-lab-panel"><div className="ns-lab-section"><h2>测评进度</h2><Button size="sm" variant="outline" onClick={() => setRefresh(v => v + 1)}>刷新任务</Button></div>
      {!detail ? <LoadingState label="正在读取持久化任务状态…" /> : <><p><strong>{STATUS[detail.evaluation.status] || detail.evaluation.status}</strong> · {evidenceLabel(detail.evaluation.manifest.source)} · 版本 {detail.evaluation.revision}</p>
        <p className="ns-lab-hash">{detail.evaluation.id} · Run {detail.evaluation.run_id || '正在分派'}</p>
        <p aria-live="polite">已评分 {detail.progress.scored}/{detail.progress.planned} · 通过 {detail.progress.pass} · 质量失败 {detail.progress.fail} · 基础设施失败 {detail.progress.infra} · 中断 {detail.progress.interrupted} · 未知 {detail.progress.unknown}</p>
        <progress aria-label="测评评分进度" value={detail.progress.scored} max={Math.max(detail.progress.planned, 1)} />
        <div className="ns-lab-actions">{active(detail.evaluation.status) ? <Button variant="outline" disabled={busy || detail.evaluation.cancel_requested} onClick={() => void command('cancel')}>{detail.evaluation.cancel_requested ? '已请求取消，等待安全检查点' : '取消测评'}</Button> : ['interrupted', 'cancelled', 'incomplete', 'failed'].includes(detail.evaluation.status) && <Button variant="outline" disabled={busy} onClick={() => void command('resume')}>校验原身份后继续</Button>}
          {detail.latest_report_id && <Link to={`/evaluation-reports/${encodeURIComponent(detail.latest_report_id)}`}>查看持久化比较报告 →</Link>}</div>
        <JsonView value={detail.evaluation.manifest} title="核对冻结的输入上下文、参数及权限清单" />
        <div className="ns-lab-table-wrap"><table className="ns-lab-table"><thead><tr><th>任务 / 模型臂</th><th>尝试</th><th>状态与结果</th><th>证据</th></tr></thead><tbody>{detail.attempts.map(a => <tr key={a.id}><td>{a.case_id}<br />{a.arm}</td><td>#{a.attempt_no}</td><td>{a.state} · {a.verdict}<br />{a.error_code}</td><td><Button size="sm" variant="ghost" onClick={() => setCaseID(a.case_id)}>查看 {a.case_id}</Button></td></tr>)}</tbody></table></div>
        {detail.next_cursor && <Button size="sm" variant="outline" onClick={async () => { const generation = epoch.current; const id = selectedID; try { const r = await modelEngineering({ operation: 'get_evaluation', workspace_id: workspace, id, cursor: detail.next_cursor, limit: 100 }); if (epoch.current === generation) setDetail(old => old?.evaluation.id === id ? { ...r, attempts: [...old.attempts, ...r.attempts] } : old); } catch (e) { if (epoch.current === generation) setError(e); } }}>加载更多尝试</Button>}
        {caseID && <FailureCasePanel key={`${workspace}/${selectedID}/${caseID}`} workspace={workspace} evaluationID={selectedID} caseID={caseID} revision={detail.evaluation.revision} canRetry={!active(detail.evaluation.status)} busy={busy} onRetry={arm => void mutate({ operation: 'evaluation_command', workspace_id: workspace, id: selectedID, case_id: caseID, arm, command: 'retry_case', expected_revision: detail.evaluation.revision, idempotency_key: crypto.randomUUID() })} />}
      </>}
    </Surface>}
    <HardwarePanel environment={environment?.environment} load={false} />
    <Surface className="ns-lab-panel"><div className="ns-lab-section"><h2>测评历史 · 当前工作区</h2><Link to="/evaluation-reports">Evaluation Reports →</Link></div>
      {!history.length ? <EmptyState title="尚无持久化测评" description="新建任务后，重开页面仍可查看状态与历史证据。" /> : history.map(e => <article className="ns-lab-record" key={e.id}><div className="ns-lab-section"><h3>{e.manifest.stable.model_name} → {e.manifest.candidate.model_name}</h3><Button variant="outline" size="sm" onClick={() => { setQuery({ evaluation: e.id }); setRefresh(v => v + 1); }}>重开测评</Button></div><p>{STATUS[e.status]} · {evidenceLabel(e.manifest.source)} · {e.started_at}</p><p className="ns-lab-hash">{e.id}</p></article>)}
      {historyCursor && <Button variant="outline" disabled={busy} onClick={async () => { const generation = epoch.current; try { const r = await modelEngineering({ operation: 'list_evaluations', workspace_id: workspace, cursor: historyCursor, limit: 20 }); if (epoch.current === generation) { setHistory(old => [...old, ...r.evaluations]); setHistoryCursor(r.next_cursor); } } catch (e) { if (epoch.current === generation) setError(e); } }}>加载更早记录</Button>}
    </Surface>
  </LabShell>;
}
