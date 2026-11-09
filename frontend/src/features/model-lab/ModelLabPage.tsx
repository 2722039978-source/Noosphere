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
import { FirstEvaluationPath } from './FirstEvaluationPath';
import { initialEvaluationSelection, preflightHelp, startBlocker, type EvaluationMode } from './first-evaluation';
import { useBackend } from '@/hooks/useBackend';
import { datasetWorkflow } from './custom-datasets';
import { PreparationPanel } from './PreparationPanel';
import { preparationCurrent, PREPARATION_LIFETIME, type PreparedSelection } from './preparation';

const DEFAULT: Options = { temperature: 0, seed: 7, num_ctx: 2048, max_output: 1024, deadline_ns: 120e9, parallelism: 1, retry_limit: 0, keep_alive: '0', think: false };
type Detail = ModelEngineeringResponses['get_evaluation'];
export function ModelLabPage() {
  const { modules, checkHealth } = useBackend();
  const workspace = useWorkspaceStore(s => s.activeId), [query, setQuery] = useSearchParams();
  const selectedID = query.get('evaluation') || '';
  const preferredModelID = query.get('model') || '';
  const preferredDatasetID = query.get('dataset') || '';
  const [models, setModels] = useState<ModelRevision[]>([]), [profiles, setProfiles] = useState<InferenceProfile[]>([]), [datasets, setDatasets] = useState<DatasetRevision[]>([]);
  const [history, setHistory] = useState<Evaluation[]>([]), [historyCursor, setHistoryCursor] = useState('');
  const [environment, setEnvironment] = useState<ModelEngineeringResponses['environment'] | null>(null);
  const [stable, setStable] = useState(''), [candidate, setCandidate] = useState(''), [datasetID, setDatasetID] = useState(''), [profileID, setProfileID] = useState('');
  const [options, setOptions] = useState<Options>(DEFAULT), [detail, setDetail] = useState<Detail | null>(null), [caseID, setCaseID] = useState('');
  const [error, setError] = useState<unknown>(), [loading, setLoading] = useState(true), [busy, setBusy] = useState(false), [refresh, setRefresh] = useState(0);
  const [observationError, setObservationError] = useState<unknown>();
  const [failedIntent, setFailedIntent] = useState<ModelEngineeringRequest | null>(null);
  const [mode, setMode] = useState<EvaluationMode>('baseline'), [setupRefresh, setSetupRefresh] = useState(0);
  const preparationWorkspace = useRef<string | null>(null);
  const preparationModel = useRef('');
  const modeRef = useRef(mode); modeRef.current = mode;
  const epoch = useRef(0), mutation = useRef(false);
  const selectedRef = useRef(selectedID); selectedRef.current = selectedID;
  const dataset = datasets.find(d => d.id === datasetID);
  const budget = environment?.preflight.budget;
  const setupError = validateSetup(stable, candidate, datasetID, options, budget, mode);
  const selectedProfile = profiles.find(p => p.id === profileID);
  const profileSaved = selectedProfile && sameOptions(selectedProfile.requested, options);
  const blocker = startBlocker({ setupError, profileSaved: !!profileSaved, preflightReady: !!environment?.preflight.available });
  const [prepared, setPrepared] = useState<PreparedSelection|null>(null);
  const preparationKey=JSON.stringify([workspace,stable,candidate,profileID,datasetID,mode,options,setupRefresh]);
  const preparationReady=preparationCurrent(prepared,preparationKey);
  useEffect(()=>{if(!prepared)return;const timer=setTimeout(()=>setPrepared(null),Math.max(0,PREPARATION_LIFETIME-(Date.now()-prepared.receivedAt)));return()=>clearTimeout(timer)},[prepared]);
  useEffect(() => {
    const reset = preparationWorkspace.current !== workspace;
    const selectedFromLink = !!preferredModelID && preferredModelID !== preparationModel.current;
    preparationModel.current = preferredModelID;
    const generation = ++epoch.current; let disposed = false; setLoading(true); setError(undefined); setEnvironment(null); setFailedIntent(null);
    void checkHealth();
    if (reset) { setBusy(false); mutation.current = false; setStable(''); setCandidate(''); setProfileID(''); setHistory([]); setHistoryCursor(''); setOptions(DEFAULT); setMode('baseline'); modeRef.current = 'baseline'; }
    Promise.allSettled([
      modelEngineering({ operation: 'list_models', limit: 100 }), modelEngineering({ operation: 'list_profiles', limit: 100 }),
      modelEngineering({ operation: 'list_datasets', workspace_id: workspace }), modelEngineering({ operation: 'environment' }),
      modelEngineering({ operation: 'list_evaluations', workspace_id: workspace, limit: 20 }),
    ]).then(([m, p, d, e, h]) => {
      if (disposed || epoch.current !== generation) return;
      // Commit initialization only after a live read. StrictMode discards its
      // first effect, which must not prevent the surviving read applying budgets.
      preparationWorkspace.current = workspace;
      if (m.status === 'fulfilled') {
        const available = m.value.models, selection = initialEvaluationSelection(available, preferredModelID);
        setModels(available);
        setStable(old => !reset && !selectedFromLink && available.some(model => model.id === old) ? old : selection.stable);
        setCandidate(old => !reset && !selectedFromLink && available.some(model => model.id === old) ? old : modeRef.current === 'baseline' ? selection.candidate : available.find(model => model.id !== selection.stable)?.id || '');
      }
      if (p.status === 'fulfilled') { setProfiles(p.value.profiles); }
      if (d.status === 'fulfilled') { const available = d.value.datasets.filter(item => !item.archived); setDatasets(available); setDatasetID(old => preferredDatasetID ? available.find(item => item.id === preferredDatasetID)?.id || '' : !reset && available.some(item => item.id === old) ? old : available[0]?.id || ''); }
      if (e.status === 'fulfilled') {
        setEnvironment(e.value);
        if (reset) { const b = e.value.preflight.budget; setOptions({ ...DEFAULT, num_ctx: Math.min(DEFAULT.num_ctx, b?.max_context ?? 8192), max_output: Math.min(DEFAULT.max_output, b?.max_output ?? 2048), deadline_ns: Math.min(DEFAULT.deadline_ns!, b?.inference_timeout_ns ?? 120e9) }); }
      }
      if (h.status === 'fulfilled') { setHistory(h.value.evaluations); setHistoryCursor(h.value.next_cursor); }
      const failure = [m, p, d, e, h].find(x => x.status === 'rejected'); if (failure?.status === 'rejected') setError(failure.reason);
      setLoading(false);
    });
    return () => { disposed = true; epoch.current++; };
  }, [workspace, setupRefresh, preferredModelID, preferredDatasetID, checkHealth]);
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
  const changeMode = (next: EvaluationMode) => { setMode(next); setCandidate(next === 'baseline' ? stable : models.find(model => model.id !== stable)?.id || ''); };
  return <LabShell title="Model Lab" subtitle="从已接入的本地模型开始 · 检查环境、执行真实任务，再阅读结果与使用模型">
    <FirstEvaluationPath stage={selectedID ? 'running' : 'prepare'} />
    <p className="ns-lab-notice">首次可以只用一个已接入的本地模型建立基线。离开页面后后台任务继续运行，结果会保存；日常会话模型由你在测评后明确选择。</p>
    <LabError error={error} onRetry={failedIntent ? () => void mutate(failedIntent) : undefined} />
    <LabError error={observationError} />
    {failedIntent && <JsonView value={failedIntent} title="待重试的原请求（沿用原参数、任务版本及幂等键）" />}
    {loading ? <LoadingState label="正在读取测评准备状态…" /> : <Surface className="ns-lab-panel"><div className="ns-lab-section"><h2>准备首次测评</h2><Link to="/model-hub">连接与注册模型版本 →</Link></div>
      {!models.length && <p className="ns-lab-notice">先在 Model Hub 打开已接入的本地模型，发现并注册版本，再回到这里。支持原生 Ollama 与本机 llama.cpp；普通远程 API 连接仍可直接用于对话。</p>}
      {preferredModelID && !models.some(model => model.id === preferredModelID) && <p role="alert" className="ns-lab-notice">链接中的版本不在当前已读取的档案中，请明确选择模型版本，或返回 Model Hub 重新注册。</p>}
      {preferredDatasetID && !datasets.some(item => item.id === preferredDatasetID) && <p role="alert" className="ns-lab-notice">链接中的任务版本不可用于当前工作区的新测评，可能已归档或不可读取。请明确选择其他版本，或在任务集档案中恢复。</p>}
      <div className="ns-first-mode" role="group" aria-label="测评方式"><button aria-pressed={mode === 'baseline'} disabled={busy} onClick={() => changeMode('baseline')}>单模型基线<small>用一个模型完成首次测评</small></button><button aria-pressed={mode === 'compare'} disabled={busy} onClick={() => changeMode('compare')}>两个版本比较<small>比较 Stable 与 Candidate</small></button></div>
      <p className="ns-lab-muted">{mode === 'baseline' ? '同一版本在相同配置下执行两轮。Stable / Candidate 分别代表第一轮 / 第二轮，不代表模型升级或跨模型胜出。' : '两个不同的已注册版本使用同一份参数与任务集，查看质量差异和候选退化案例。'}</p>
      <div className="ns-lab-grid"><label className="ns-lab-field">{mode === 'baseline' ? '本次测评模型' : 'Stable 稳定模型'}<select disabled={busy} value={stable} onChange={e => { setStable(e.target.value); if (mode === 'baseline') setCandidate(e.target.value); }}><option value="">选择模型版本</option>{models.map(m => <option key={m.id} value={m.id}>{m.model_name} · {short(m.digest)}</option>)}</select></label>
        {mode === 'compare' && <label className="ns-lab-field">Candidate 候选模型<select disabled={busy} value={candidate} onChange={e => setCandidate(e.target.value)}><option value="">选择模型版本</option>{models.map(m => <option key={m.id} value={m.id}>{m.model_name} · {short(m.digest)}</option>)}</select></label>}
        <label className="ns-lab-field">测评集<select disabled={busy} value={datasetID} onChange={e => { const id = e.target.value; setDatasetID(id); setQuery(old => { const next = new URLSearchParams(old); next.set('dataset', id); return next; }); }}><option value="">选择任务版本</option>{datasets.map(d => <option key={d.id} value={d.id}>{d.title || d.benchmark_id} · {d.version} · {d.cases.length} 例{d.kind === 'workspace_custom' ? ' · 自定义' : ''}</option>)}</select><Link to="/model-lab/datasets">创建、导入与管理自己的任务集 →</Link></label>
        <label className="ns-lab-field">已保存推理配置<select value={profileID} onChange={e => { setProfileID(e.target.value); const p = profiles.find(x => x.id === e.target.value); if (p) setOptions(p.requested); }}><option value="">新建配置</option>{profiles.map(p => <option key={p.id} value={p.id}>{short(p.id)} · ctx {p.requested.num_ctx} / out {p.requested.max_output}</option>)}</select></label>
      </div>
      <details className="ns-lab-details" data-testid="advanced-inference"><summary>高级推理参数</summary><div className="ns-lab-grid ns-lab-options">{([['temperature', '采样温度', options.temperature, 0.1], ['seed', '随机种子', options.seed, 1], ['num_ctx', '上下文预算', options.num_ctx, 1], ['max_output', '输出预算', options.max_output, 1], ['deadline_ns', '推理时限（秒，0=120）', (options.deadline_ns ?? 120e9) / 1e9, 1]] as const).map(([key, label, value, step]) => <label className="ns-lab-field" key={key}>{label}<Input type="number" step={step} value={Number.isFinite(value) ? value : ''} onChange={e => changeOption(key, e.target.value === '' ? NaN : Number(e.target.value) * (key === 'deadline_ns' ? 1e9 : 1))} /></label>)}</div>
      <label className="ns-lab-muted"><input type="checkbox" checked={options.think === true} disabled={busy} onChange={event => setOptions(old => ({ ...old, think: event.target.checked }))} /> 启用思考模式（服务支持时生效；首次默认关闭，为代码输出保留预算）</label></details>
      <p className="ns-lab-muted">{budget ? `服务端预算：上下文 ≤${budget.max_context}、输出 ≤${budget.max_output}、单次推理 ≤${budget.inference_timeout_ns / 1e9} 秒` : '服务端预算未提供；界面暂按全局上限 8192 / 2048 / 120 秒校验，实际配置未确认'}；并发 1，无自动重试。服务端再次校验模型身份、CodeLens 与资源准入，显存未知不代表资源充足。</p>
      <div className="ns-first-checks" aria-label="测评准备检查"><div><strong>模型版本 · {stable && candidate ? '已选择' : '待注册与选择'}</strong><span>{models.find(model => model.id === stable)?.model_name || '返回 Model Hub 发现并注册本地版本'}</span></div><div><strong>本地执行器 · {environment?.preflight.available ? '已就绪' : '待检查'}</strong><span>{environment ? `${environment.environment.os} / ${environment.environment.arch} · ${environment.preflight.code || 'Go / WASI 预检通过'}` : '未取得运行环境'}</span></div><div><strong>推理配置 · {profileSaved ? '已保存' : '待保存'}</strong><span>{profileSaved ? `${options.num_ctx} 上下文 / ${options.max_output} 输出` : '检查参数并点击保存推理配置'}</span></div></div>
      {environment ? <p className={environment.preflight.available ? 'ns-lab-muted' : 'ns-lab-notice'} role="status">运行环境：{environment.environment.os}/{environment.environment.arch} · {environment.environment.cores || '?'} 核 · 显存 {metricText(environment.environment.vram)} · {environment.preflight.available ? '执行器就绪；CodeLens、模型身份与物理资源仍需启动时校验' : `执行器预检未通过：${environment.preflight.code || '执行器未就绪'}。${preflightHelp(environment.preflight.code)}`}</p> : <p className="ns-lab-notice">执行器预检尚不可用。{preflightHelp()}</p>}
      {!environment?.preflight.available && modules.model_lab?.error && <p role="alert" className="ns-lab-notice">Model Lab 启动原因：{modules.model_lab.error}<br />如提示 Go 工具链不可用，请在本机配置的 model_lab.go_binary 填入可信 go.exe 的绝对路径，重启 Noosphere 后重新检查环境。</p>}
      {dataset && <p className="ns-lab-muted">本次包含 {dataset.cases.length} 个独立任务、共 {dataset.cases.length * 2} 次正式推理，串行运行；推理上限约 {Math.ceil(dataset.cases.length * 2 * (options.deadline_ns || 120e9) / 60e9)} 分钟，另有上下文、编译和评分开销。</p>}
      {blocker && <p role="status" className="ns-lab-muted">下一步：{blocker}</p>}
      <PreparationPanel key={preparationKey} request={{operation:'prepare_evaluation',workspace_id:workspace,stable_revision_id:stable,candidate_revision_id:candidate,profile_id:profileID,dataset_id:datasetID,workflow_id:datasetWorkflow(dataset),evaluation_mode:mode}} disabled={busy||!!blocker} onResult={ready=>setPrepared({key:preparationKey,ready,receivedAt:Date.now()})}/>
      {!blocker&&!preparationReady&&<p role="status" className="ns-lab-muted">下一步：点击“检查本次测评准备”；准备结果有效期为两分钟。</p>}
      <div className="ns-lab-actions"><Button variant="outline" disabled={busy || !!validateSetup('a', 'b', 'dataset', options, budget)} onClick={() => void mutate({ operation: 'create_profile', requested: options })}>保存推理配置</Button>
        <Button disabled={busy || !!blocker || !preparationReady} onClick={() => { if(!preparationCurrent(prepared,preparationKey)){setPrepared(null);return;} void mutate({ operation: 'create_evaluation', evaluation_mode: mode, workspace_id: workspace, stable_revision_id: stable, candidate_revision_id: candidate, profile_id: profileID, dataset_id: datasetID, workflow_id: datasetWorkflow(dataset), idempotency_key: crypto.randomUUID() }); }}>{busy ? '正在提交…' : '开始真实离线测评'}</Button>
        <Button variant="ghost" disabled={busy} onClick={() => setSetupRefresh(value => value + 1)}>重新检查环境</Button></div>
      <p className="ns-lab-muted">{profileSaved ? '当前参数已保存，请完成准备检查后开始测评。修改任一参数后需要重新保存。' : '启动前请保存当前参数。保存后配置不可变，两轮执行使用同一份配置。'}</p>
      <JsonView value={{ stable: models.find(m => m.id === stable), candidate: models.find(m => m.id === candidate), requested: options, environment: environment?.environment }} title="核对模型身份、完整参数与硬件环境" />
      {dataset && <JsonView value={dataset} title={`查看测评集 ${dataset.version} · 案例目录、版本与评分器信息（完整断言见任务集档案）`} />}
    </Surface>}
    {selectedID && <Surface className="ns-lab-panel"><div className="ns-lab-section"><h2>测评进度</h2><Button size="sm" variant="outline" onClick={() => setRefresh(v => v + 1)}>刷新任务</Button></div>
      {!detail ? observationError ? <p className="ns-lab-muted">任务状态暂时不可读，正在自动重试；可点击“刷新任务”立即重试。</p> : <LoadingState label="正在读取持久化任务状态…" /> : <><p><strong>{STATUS[detail.evaluation.status] || detail.evaluation.status}</strong> · {detail.evaluation.manifest.evaluation_mode === 'baseline' ? '单模型基线 · ' : ''}{evidenceLabel(detail.evaluation.manifest.source)} · 版本 {detail.evaluation.revision}</p>
        <p className="ns-lab-hash">{detail.evaluation.id} · Run {detail.evaluation.run_id || '正在分派'}</p>
        <p aria-live="polite">已评分 {detail.progress.scored}/{detail.progress.planned} · 通过 {detail.progress.pass} · 质量失败 {detail.progress.fail} · 基础设施失败 {detail.progress.infra} · 中断 {detail.progress.interrupted} · 未知 {detail.progress.unknown}</p>
        <progress className="ns-first-progress" aria-label="测评评分进度" value={detail.progress.scored} max={Math.max(detail.progress.planned, 1)} />
        {active(detail.evaluation.status) && <p className="ns-lab-muted">任务正在后台串行执行。首个案例可能需要准备上下文和模型推理；可以离开页面，通过测评历史重开。</p>}
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
