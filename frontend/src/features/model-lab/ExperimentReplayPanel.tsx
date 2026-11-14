import { useEffect, useMemo, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { modelEngineering } from '@/lib/model-engineering-client';
import type { EvaluationPreparation, ModelEngineeringRequest, ModelEngineeringResponses } from '@/lib/model-engineering-types';
import { Button } from '@/components/ui/button';
import { Surface } from '@/components/ui/surface';
import { LabError, JsonView } from './shared';
import { buildReportInsights, pairedCaseEvidence } from './report-insights';
import { CapabilityProfile } from './CapabilityProfile';
import { assessReplay, EXPERIMENT_MAX_BYTES, parseExperimentBundle, type ExperimentBundle, type ReplayPlan } from './experiment-bundle';
import { PREPARATION_LIFETIME } from './preparation';

async function localInventory(workspace: string) {
  const modelPages = async () => {
    const models: ModelEngineeringResponses['list_models']['models'] = []; let cursor = ''; const seen = new Set<string>();
    do { const page = await modelEngineering({ operation: 'list_models', cursor: cursor || undefined, limit: 100 }); models.push(...page.models); cursor = page.next_cursor; if (cursor && (seen.has(cursor) || seen.size >= 20)) throw new Error('模型目录分页未完成，请在 Model Hub 检查后重试。'); seen.add(cursor); } while (cursor);
    return models;
  };
  const profilePages = async () => {
    const profiles: ModelEngineeringResponses['list_profiles']['profiles'] = []; let cursor = ''; const seen = new Set<string>();
    do { const page = await modelEngineering({ operation: 'list_profiles', cursor: cursor || undefined, limit: 100 }); profiles.push(...page.profiles); cursor = page.next_cursor; if (cursor && (seen.has(cursor) || seen.size >= 20)) throw new Error('配置目录分页未完成，请在 Model Lab 检查后重试。'); seen.add(cursor); } while (cursor);
    return profiles;
  };
  const [models, profiles, { datasets }] = await Promise.all([modelPages(), profilePages(), modelEngineering({ operation: 'list_datasets', workspace_id: workspace })]);
  return { models, profiles, datasets };
}

export function ExperimentReplayPanel({ workspace }: { workspace: string }) {
  const [bundle, setBundle] = useState<ExperimentBundle | null>(null), [issues, setIssues] = useState<string[]>([]), [busy, setBusy] = useState(false), [error, setError] = useState<unknown>();
  const [plan, setPlan] = useState<ReplayPlan | null>(null), [preparation, setPreparation] = useState<EvaluationPreparation | null>(null), [checked, setChecked] = useState(false), [expires, setExpires] = useState(0);
  const [caseID, setCaseID] = useState(''), [started, setStarted] = useState('');
  const generation = useRef(0), submitting = useRef(false), intent = useRef<Extract<ModelEngineeringRequest, { operation: 'create_evaluation' }> | null>(null);
  useEffect(() => { return () => { generation.current++; }; }, []);
  useEffect(() => { if (!expires) return; const timer = setTimeout(() => { setExpires(0); setChecked(false); }, Math.max(0, expires - Date.now())); return () => clearTimeout(timer); }, [expires]);
  const insights = useMemo(() => bundle ? buildReportInsights(bundle.payload.report, null, bundle.payload.evaluation) : null, [bundle]);
  const row = insights?.cases.find(c => c.id === caseID), collected = bundle?.payload.evidence.find(c => c.id === caseID);
  const evidence = row ? pairedCaseEvidence(row, collected?.detail || null, collected?.issue) : null;
  const readFile = async (file?: File) => {
    if (!file || submitting.current) return;
    const current = ++generation.current; setBundle(null); setIssues([]); setError(undefined); setPlan(null); setPreparation(null); setExpires(0); setChecked(false); setStarted(''); setCaseID(''); intent.current = null;
    if (file.size > EXPERIMENT_MAX_BYTES) { setIssues(['文件超过 12 MiB 限制。']); return; }
    setBusy(true);
    try { const result = await parseExperimentBundle(await file.text()); if (current === generation.current) { setIssues(result.issues); setBundle(result.bundle || null); } }
    catch (e) { if (current === generation.current) setError(e); }
    finally { if (current === generation.current) setBusy(false); }
  };
  const prepare = async () => {
    if (!bundle || !insights?.baseline || busy) return;
    const current = generation.current; setBusy(true); setError(undefined); setPlan(null); setPreparation(null); setExpires(0); setChecked(false); intent.current = null;
    try {
      const inventory = await localInventory(workspace); if (current !== generation.current) return;
      const next = assessReplay(bundle, inventory.models, inventory.profiles, inventory.datasets); setPlan(next);
      if (!next.ready || !next.stable || !next.candidate || !next.dataset || !next.profile) return;
      const fields = { workspace_id: workspace, evaluation_mode: next.mode, stable_revision_id: next.stable.id, candidate_revision_id: next.candidate.id, profile_id: next.profile.id, dataset_id: next.dataset.id, workflow_id: next.workflow };
      const result = await modelEngineering({ operation: 'prepare_evaluation', ...fields }); if (current !== generation.current) return;
      setPreparation(result);
      if (result.ready && result.checks.length > 0 && result.checks.every(c => c.ready)) { setExpires(Date.now() + PREPARATION_LIFETIME); intent.current = { operation: 'create_evaluation', ...fields, idempotency_key: crypto.randomUUID() }; }
    } catch (e) { if (current === generation.current) setError(e); }
    finally { if (current === generation.current) setBusy(false); }
  };
  const start = async () => {
    if (!checked || !intent.current || !plan?.ready || submitting.current || Date.now() >= expires) return;
    submitting.current = true; setBusy(true); setError(undefined); const current = generation.current;
    try { const result = await modelEngineering(intent.current); if (current === generation.current) { setStarted(result.evaluation_id); setExpires(0); setChecked(false); } }
    catch (e) { if (current === generation.current) setError(e); }
    finally { submitting.current = false; if (current === generation.current) setBusy(false); }
  };
  return <Surface className="ns-lab-panel ns-experiment-replay"><details><summary>导入实验包与本机复跑</summary>
    <p className="ns-lab-muted">导入时核对文件格式、大小、哈希和报告证据。单模型内置任务可匹配本机版本与参数并准备复跑。历史双模型实验包可阅读。</p>
    <label className="ns-lab-field">导入实验包（JSON）<input aria-label="导入实验包（JSON）" type="file" accept=".json,application/json" disabled={busy} onChange={event => { const file = event.target.files?.[0]; event.target.value = ''; void readFile(file); }} /></label>
    <LabError error={error} />{issues.length > 0 && <ul role="alert" className="ns-lab-notice">{issues.map(issue => <li key={issue}>{issue}</li>)}</ul>}
    {busy && <p role="status">正在校验实验与本机准备条件…</p>}
    {bundle && insights && <><p role="status" className="ns-lab-notice">实验包完整性校验通过 · {bundle.payload.evaluation.manifest.source === 'offline_real' ? '源包标记为真实离线测评（作者身份未认证）' : '流程验证数据，非真实模型测评'}。哈希只能检查内容是否改动，不能证明来源或结论真实。</p>
      <p>{bundle.payload.evaluation.manifest.dataset.benchmark_id} · {bundle.payload.evaluation.manifest.dataset.version} · {insights.baseline ? '同一模型两轮基线' : '两个版本比较'}<br /><span className="ns-lab-muted">{insights.arms.stable.modelName} / {insights.arms.candidate.modelName} · 导出于 {bundle.exported_at}</span></p>
      <CapabilityProfile insights={insights} imported />
      <details className="ns-report-technical"><summary>核对源包参数、环境与完整性边界</summary><JsonView value={{ profile: bundle.payload.evaluation.manifest.profile, environment: bundle.payload.evaluation.manifest.environment, manifest_hash: bundle.payload.report.manifest_hash, report_hash: bundle.payload.report_hash, export_hash: bundle.integrity.payload_sha256, omissions: bundle.payload.omissions }} title="源实验条件与证据引用" /></details>
      <label className="ns-lab-field">只读比较源包两边输出<select aria-label="只读比较源包两边输出" value={caseID} onChange={event => setCaseID(event.target.value)}><option value="">选择案例查看已保存证据</option>{insights.cases.map(c => <option key={c.id} value={c.id}>{c.id} · {c.category}</option>)}</select></label>
      {evidence && <section aria-label="导入案例只读证据"><p>{evidence.task}</p>{evidence.issue && <p className="ns-lab-notice">{evidence.issue}</p>}<div className="ns-report-pair">{(['stable', 'candidate'] as const).map(key => <article key={key} className="ns-report-output"><h4>{insights.arms[key].label} · {evidence[key].verdictText || '未知'}</h4><p>{evidence[key].notice}</p><pre>{evidence[key].output || '无可读取输出'}</pre>{evidence[key].score && <details><summary>评分证据</summary><pre>{evidence[key].score}</pre></details>}</article>)}</div></section>}
      {!started && insights.baseline && <Button variant="outline" disabled={busy} onClick={() => void prepare()}>校验并准备本机复跑</Button>}
      {plan && <section aria-label="本机复跑条件核对" className="ns-lab-record"><h3>本机复跑条件核对</h3>{plan.reasons.length > 0 && <><p className="ns-lab-notice">当前不能完整复跑；上方仍可只读比较源包证据。</p><ul>{plan.reasons.map(reason => <li key={reason}>{reason}</li>)}</ul><p><Link to="/model-hub">注册模型 →</Link> · <Link to="/model-lab">核对并保存推理配置 →</Link></p></>}
        {plan.ready && <><p>将使用本机 {plan.stable?.model_name} / {plan.candidate?.model_name}，执行 {plan.dataset?.cases.length} 个独立任务、两轮共 {(plan.dataset?.cases.length || 0) * 2} 次正式推理。</p><JsonView value={{ stable: plan.stable, candidate: plan.candidate, profile: plan.profile, dataset_id: plan.dataset?.id, source_content_hash: plan.dataset?.content_hash, workflow: plan.workflow }} title="核对实际使用的本机版本与完整参数" /></>}
        <ul className="ns-lab-muted">{plan.notices.map(notice => <li key={notice}>{notice}</li>)}</ul>
        {preparation && <><p role="status">{preparation.ready ? '服务端准备检查通过' : '服务端准备检查未通过'}</p><ul>{preparation.checks.map(c => <li key={c.id}>{c.ready ? '✓' : '待处理'} {c.label}：{c.detail}</li>)}</ul></>}
        {!!expires && !started && <label className="ns-replay-confirm"><input type="checkbox" checked={checked} disabled={busy} onChange={event => setChecked(event.target.checked)} /> 已核对本机模型、参数和任务集</label>}
        {plan.ready && !started && <><Button disabled={busy || !checked || !expires} onClick={() => void start()}>确认开始本机复跑</Button>{!expires && preparation?.ready && <p role="status" className="ns-lab-muted">准备结果已过期，请重新校验；每次检查有效两分钟。</p>}</>}
      </section>}
      {started && <p role="status" className="ns-lab-notice">已创建本机复跑，后台任务继续运行。<Link to={`/model-lab?evaluation=${encodeURIComponent(started)}`}>查看复跑进度与新报告 →</Link> 源实验包保持独立。</p>}
    </>}
  </details></Surface>;
}
