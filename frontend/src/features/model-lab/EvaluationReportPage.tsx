import { useEffect, useMemo, useRef, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { modelEngineering } from '@/lib/model-engineering-client';
import type { Evaluation, ModelEngineeringResponses } from '@/lib/model-engineering-types';
import { useWorkspaceStore } from '@/stores/workspace-store';
import { Surface } from '@/components/ui/surface';
import { Button } from '@/components/ui/button';
import { EmptyState, LoadingState } from '@/components/ui/feedback';
import { LabError, LabShell, JsonView } from './shared';
import { evidenceLabel, readComparison, STATUS } from './presentation';
import { ComparisonTable } from './ComparisonTable';
import { ReportCasePanel } from './ReportCasePanel';
import { ReportOverview } from './ReportOverview';
import { buildReportInsights, changeLabel, pairedCaseEvidence, verdictLabel } from './report-insights';
import { FirstEvaluationPath } from './FirstEvaluationPath';
import { CapabilityProfile } from './CapabilityProfile';
import { ExperimentReplayPanel } from './ExperimentReplayPanel';
import { collectReportEvidence } from './collect-report-evidence';
import { MeasuredPerformance } from './MeasuredPerformance';
import { buildExperimentBundle } from './experiment-bundle';
import './report-reading.css';

export function EvaluationReportPage() {
  const { id } = useParams(), workspace = useWorkspaceStore(s => s.activeId), navigate = useNavigate();
  const [data, setData] = useState<ModelEngineeringResponses['get_report'] | null>(null), [history, setHistory] = useState<Evaluation[]>([]);
  const [cursor, setCursor] = useState(''), [error, setError] = useState<unknown>(), [loading, setLoading] = useState(true), [caseID, setCaseID] = useState('');
  const [evaluation, setEvaluation] = useState<Evaluation | null>(null), [identityError, setIdentityError] = useState<unknown>();
  const [filter, setFilter] = useState('all'), [refresh, setRefresh] = useState(0);
  const [exportProgress, setExportProgress] = useState<{ completed: number; total: number } | null>(null), [exportNotice, setExportNotice] = useState('');
  const exportController = useRef<AbortController | null>(null);
  const epoch = useRef(0);
  useEffect(() => {
    const generation = ++epoch.current; exportController.current?.abort(); exportController.current = null; setExportProgress(null); setExportNotice(''); setLoading(true); setData(null); setHistory([]); setError(undefined); setCaseID(''); setCursor(''); setEvaluation(null); setIdentityError(undefined); setFilter('all');
    const run = async () => {
      try {
        if (id) {
          const r = await modelEngineering({ operation: 'get_report', workspace_id: workspace, id });
          if (epoch.current !== generation) return;
          setData(r);
          try { const e = await modelEngineering({ operation: 'get_evaluation', workspace_id: workspace, id: r.report.evaluation_id }); if (epoch.current === generation) setEvaluation(e.evaluation); }
          catch (e) { if (epoch.current === generation) setIdentityError(e); }
        }
        else { const r = await modelEngineering({ operation: 'list_evaluations', workspace_id: workspace, limit: 20 }); if (epoch.current === generation) { setHistory(r.evaluations); setCursor(r.next_cursor); } }
      } catch (e) { if (epoch.current === generation) setError(e); }
      finally { if (epoch.current === generation) setLoading(false); }
    }; void run(); return () => { epoch.current++; exportController.current?.abort(); };
  }, [workspace, id, refresh]);
  const openReport = async (evaluationID: string) => {
    const generation = epoch.current; setError(undefined);
    try {
      const e = await modelEngineering({ operation: 'get_evaluation', workspace_id: workspace, id: evaluationID });
      if (epoch.current !== generation) return;
      if (e.latest_report_id) navigate(`/evaluation-reports/${encodeURIComponent(e.latest_report_id)}`);
      else navigate(`/model-lab?evaluation=${encodeURIComponent(evaluationID)}`);
    } catch (e) { if (epoch.current === generation) setError(e); }
  };
  const report = data?.report, analysis = readComparison(report?.analysis);
  const baseline = analysis?.evaluation_mode === 'baseline' || evaluation?.manifest.evaluation_mode === 'baseline';
  const customTasks = !!evaluation && !['local-text-v1', 'codetask-go-v1'].includes(evaluation.manifest.dataset.benchmark_id);
  const textTasks = evaluation?.manifest.workflow_id === 'local.text.v1';
  const insights = useMemo(() => report ? buildReportInsights(report, analysis, evaluation) : null, [report, analysis, evaluation]);
  const cases = insights?.cases.filter(row => filter === 'all' || filter === 'issues' && row.change !== 'both_pass' || filter === 'different' && (row.change === 'improved' || row.change === 'regressed') || row.change === filter) || [];
  const chooseFilter = (value: string) => { setFilter(value); setCaseID(''); };
  const exportReport = async (format: 'html' | 'svg' | 'json' = 'html') => {
    if (!report || !data || !insights || exportController.current) return;
    const controller = new AbortController(), generation = epoch.current;
    exportController.current = controller; setExportProgress({ completed: 0, total: insights.cases.length }); setExportNotice('');
    const current = () => !controller.signal.aborted && epoch.current === generation;
    try {
      const raw = format === 'svg' ? [] : await collectReportEvidence(insights.cases.map(c => c.id), { workspace, evaluationID: report.evaluation_id, reportID: report.id }, modelEngineering, controller.signal, completed => { if (current()) setExportProgress({ completed, total: insights.cases.length }); });
      if (!current()) return;
      const collected = raw.map((item, index) => pairedCaseEvidence(insights.cases[index], item.detail, item.issue));
      const input = { report, reportHash: data.report_hash, evaluation, insights, cases: collected, exportedAt: new Date().toISOString() };
      let content = '', mime = '';
      if (format === 'json') {
        if (!evaluation) throw new Error('冻结的实验清单不可读取，不能导出复跑资料。');
        content = JSON.stringify(await buildExperimentBundle(evaluation, report, data.report_hash, raw, input.exportedAt), null, 2); mime = 'application/json';
      } else {
        const { renderExperimentHTML, renderExperimentSVG } = await import('./report-export');
        content = format === 'svg' ? renderExperimentSVG(input) : renderExperimentHTML(input); mime = format === 'svg' ? 'image/svg+xml' : 'text/html';
      }
      if (!current()) return;
      const url = URL.createObjectURL(new Blob([content], { type: `${mime};charset=utf-8` }));
      const link = document.createElement('a'); link.href = url; link.download = `noosphere-experiment-${report.id.replace(/[^A-Za-z0-9_.-]/g, '_')}.${format}`; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000);
      const missing = collected.filter(c => c.issue || c.stable.notice || c.candidate.notice).length;
      setExportNotice(format === 'svg' ? '已导出分享图卡（SVG），包含任务分类、分母、未评分、条件与报告引用；原始输出见 HTML 或实验包。' : `已导出 ${collected.length} 个案例的${format === 'json' ? '实验包（JSON），含冻结清单、选中尝试、证据文本及内容哈希，可在下方导入核对' : '实验报告，可离线打开和打印'}。${missing ? `其中 ${missing} 个案例的详细证据缺失或不完整，文件中已标明。` : '缺失、脱敏或显示截断处会在文件中标明。'}`);
    } catch (e) { if (current()) setError(e); }
    finally { if (exportController.current === controller) { exportController.current = null; if (epoch.current === generation) setExportProgress(null); } }
  };
  const useModels = evaluation ? baseline || evaluation.manifest.stable.gateway_model_id === evaluation.manifest.candidate.gateway_model_id ? [{ label: '用此模型开始对话', model: evaluation.manifest.stable }] : [{ label: '使用 Stable 模型', model: evaluation.manifest.stable }, { label: '使用 Candidate 模型', model: evaluation.manifest.candidate }] : [];
  return <LabShell title="Evaluation Reports" subtitle="冻结的测评结果、失败案例与可审计证据 · 当前资格单独显示，历史报告保持不可变">
    {!id && <><FirstEvaluationPath stage="report" /><Link className="self-start text-xs text-text-2 underline underline-offset-4" to="/release-center">查看模拟发布记录</Link></>}
    <LabError error={error} onRetry={() => setRefresh(value => value + 1)} />{loading ? <LoadingState label="正在读取持久化报告…" /> : report && data ? <>
      <Surface className="ns-lab-panel"><div className="ns-lab-section"><h2>{baseline ? '单模型基线报告' : '比较报告'}</h2><Link to={`/model-lab?evaluation=${encodeURIComponent(report.evaluation_id)}`}>返回原测评 →</Link></div>
        {insights && <><ReportOverview insights={insights} /><CapabilityProfile insights={insights} /></>}
        {analysis?.source === 'offline_real' && <MeasuredPerformance analysis={analysis} baseline={baseline} planned={evaluation?.manifest.dataset.cases.length ?? Math.floor(report.counts.planned / 2)} />}
        {baseline && <p className="ns-lab-muted">同一版本的两轮结果用于了解任务表现与重复性，不代表相较其他模型胜出。</p>}
        <div className="ns-report-tools"><div className="ns-lab-actions"><Button variant="outline" disabled={!!exportProgress || !insights || !evaluation} onClick={() => void exportReport('html')}>导出实验报告（HTML）</Button><Button variant="outline" disabled={!!exportProgress || !insights || !evaluation} onClick={() => void exportReport('svg')}>导出分享图卡（SVG）</Button><Button variant="outline" disabled={!!exportProgress || !insights || !evaluation} onClick={() => void exportReport('json')}>导出实验包（JSON）</Button></div>{exportProgress && <div className="ns-report-progress" role="status">正在收集案例证据 {exportProgress.completed}/{exportProgress.total}<Button variant="ghost" size="sm" onClick={() => { exportController.current?.abort(); exportController.current = null; setExportProgress(null); setExportNotice('已取消导出，原始报告保持不变。'); }}>取消导出</Button></div>}</div>
        <p className="ns-lab-muted">分享图卡保留未知与样本分母；实验包最多 12 MiB、单条证据最多 65,536 字符，截断与脱敏会明示。分享前请检查任务和模型输出中的个人内容。</p>
        {exportNotice && <p role="status" className="ns-lab-notice">{exportNotice}</p>}
        <details className="ns-report-technical"><summary>核对模型版本、参数与证据</summary>
        <FirstEvaluationPath stage="report" />
        {evaluation && <JsonView value={{ stable: evaluation.manifest.stable, candidate: evaluation.manifest.candidate, profile: evaluation.manifest.profile, environment: evaluation.manifest.environment }} title="冻结的模型身份、推理参数与环境" />}
        {evaluation && <section aria-label="冻结任务集与评分规则" className="ns-lab-record"><h3>冻结任务集与评分规则</h3><p>{evaluation.manifest.dataset.title || evaluation.manifest.dataset.benchmark_id} · {evaluation.manifest.dataset.version} · {evaluation.manifest.dataset.cases.length} 个独立案例</p><p>评分器：{evaluation.manifest.dataset.scorer_version} · {evaluation.manifest.dataset.scoring_rule === 'exact' || evaluation.manifest.dataset.scorer_version.startsWith('host-exact') ? '固定精确匹配；各例全部断言通过才记为通过' : '以冻结的评分器版本与案例证据为准'}。任务集后续修改或归档不会改写本报告。</p><p className="ns-lab-hash">冻结版本 {evaluation.manifest.dataset.id}<br />内容哈希 {evaluation.manifest.dataset.content_hash}{evaluation.manifest.dataset.source_revision_id && <><br />来源版本 {evaluation.manifest.dataset.source_revision_id}<br />来源内容哈希 {evaluation.manifest.dataset.source_content_hash}</>}</p><JsonView value={{ workflow_id: evaluation.manifest.workflow_id, dataset: evaluation.manifest.dataset }} title="核对冻结任务版本、规则与输入证据引用" /></section>}
        </details>
        <div className="ns-first-use">{useModels.filter(item => item.model.gateway_model_id).map(item => <Link key={item.label} to={`/chat?model=${encodeURIComponent(item.model.gateway_model_id)}`}>{item.label} →</Link>)}<Link to="/model-hub">返回模型档案 →</Link></div>
        {useModels.length > 0 && <p className="ns-lab-muted">“使用”会打开所选连接的新对话并校验当前模型，保留已有会话；不会设置工作区或全局默认。测评身份是冻结快照，日常对话使用连接当前提供的版本。</p>}
        {!!identityError && <><LabError error={identityError} /><p className="ns-lab-muted">报告仍可阅读；读取原测评身份失败，暂不提供模型使用快捷入口。</p><Button size="sm" variant="outline" onClick={() => setRefresh(value => value + 1)}>重读报告与模型身份</Button></>}
        <p className="ns-lab-notice">{analysis ? evidenceLabel(analysis.source) : '历史分析不可用 · 仅展示已保存的原始结果'}。离线证据不代表真实灰度或生产验证。</p>
        {!analysis && <p className="ns-lab-muted">此报告缺少受支持的 regression-intelligence-v1 分析。不会重新计算或补造历史区间、性能或资源数据。</p>}
        <details className="ns-report-technical"><summary>查看分类统计、性能与指标缺失原因</summary><ComparisonTable report={report} analysis={analysis} baseline={baseline} /></details>
        <details className="ns-report-technical"><summary>报告哈希、发布资格与原始记录</summary>
          <p className="ns-lab-hash">Report {report.id}<br />Manifest {report.manifest_hash}<br />Attempt set {report.attempt_set_hash}<br />Report hash {data.report_hash}</p>
          <p role="status">当前晋级证据资格：{data.promotion_eligibility.eligible ? '服务端判定具备证据资格；仍须发布策略逐条校验' : '不满足'}{data.promotion_eligibility.reasons.length ? ` · ${data.promotion_eligibility.reasons.join('；')}` : ''}</p>
          <JsonView value={report} title="原始不可变报告（含版本、选中与被替代尝试）" />
        </details>
        {customTasks && <p className="ns-lab-notice">这是你定义的任务回归报告。当前演示发布策略仅适用于内置任务集，自定义任务需要单独设计发布标准；你仍可从上方使用模型。</p>}
        {!baseline && !customTasks && !textTasks && <p className="ns-lab-muted"><Link to={`/release-center?report=${encodeURIComponent(report.id)}`}>创建模拟发布 →</Link> · 模拟发布与日常模型使用分别操作。</p>}
        {textTasks && <p className="ns-lab-muted">文本任务用于本地模型评测与报告；当前 Go 演示发布策略不适用于这组任务。</p>}
      </Surface>
      <Surface className="ns-lab-panel"><div className="ns-lab-section"><h2>任务与失败案例</h2><Button size="sm" variant="outline" aria-pressed={filter === 'issues'} onClick={() => chooseFilter(filter === 'issues' ? 'all' : 'issues')}>{filter === 'issues' ? '显示全部案例' : '只看未通过案例'}</Button></div>
        <p className="ns-lab-muted">按本报告的两边评分逐题核对。基础设施失败与未执行结果不计为候选模型退化。</p>
        <div className="ns-report-filters" role="group" aria-label="任务变化筛选"><Button variant="outline" size="sm" aria-pressed={filter === 'all'} onClick={() => chooseFilter('all')}>全部任务</Button>{baseline ? <Button variant="outline" size="sm" aria-pressed={filter === 'different'} onClick={() => chooseFilter('different')}>两轮结果不同（{(insights?.improvements || 0) + (insights?.regressions || 0)}）</Button> : <><Button variant="outline" size="sm" aria-pressed={filter === 'regressed'} onClick={() => chooseFilter('regressed')}>候选模型变差（{insights?.regressions || 0}）</Button><Button variant="outline" size="sm" aria-pressed={filter === 'improved'} onClick={() => chooseFilter('improved')}>候选模型改善（{insights?.improvements || 0}）</Button></>}<Button variant="outline" size="sm" aria-pressed={filter === 'unavailable'} onClick={() => chooseFilter('unavailable')}>暂无法比较（{insights?.cases.filter(c => c.change === 'unavailable').length || 0}）</Button></div>
        <div className="ns-lab-table-wrap"><table className="ns-lab-table"><thead><tr><th>任务</th><th>{insights?.arms.stable.label}</th><th>{insights?.arms.candidate.label}</th><th>逐题变化</th><th>两边输出</th></tr></thead><tbody>{cases.map(row => <tr key={row.id}><td>{row.id}<br /><span className="ns-lab-muted">{row.category}</span></td><td>{verdictLabel(row.stable?.state, row.stable?.verdict)}</td><td>{verdictLabel(row.candidate?.state, row.candidate?.verdict)}</td><td><span className="ns-report-change" data-change={row.change}>{changeLabel(row.change, baseline)}</span></td><td><Button size="sm" variant="ghost" onClick={() => setCaseID(row.id)}>查看 {row.id}</Button></td></tr>)}</tbody></table></div>
        {!cases.length && <p role="status">{filter === 'regressed' ? '本报告没有候选模型变差的案例。' : filter === 'improved' ? '本报告没有候选模型改善的案例。' : '当前筛选下没有案例。'}</p>}
        {caseID && insights && <ReportCasePanel key={`${workspace}/${report.id}/${caseID}`} workspace={workspace} evaluationID={report.evaluation_id} reportID={report.id} row={insights.cases.find(c => c.id === caseID)!} insights={insights} />}
      </Surface>
    </> : !id ? <Surface className="ns-lab-panel"><h2>历史测评报告</h2>{!history.length ? <EmptyState title="尚无测评报告" description="完成或部分完成的测评会保存报告；运行中任务可在 Model Lab 重开。" action={<Link to="/model-lab">前往 Model Lab</Link>} /> : history.map(e => <article className="ns-lab-record" key={e.id}><div className="ns-lab-section"><h3>{e.manifest.stable.model_name} → {e.manifest.candidate.model_name}</h3><Button size="sm" variant="outline" onClick={() => void openReport(e.id)}>重开报告或任务</Button></div><p>{STATUS[e.status]} · {evidenceLabel(e.manifest.source)} · {e.started_at}</p><p className="ns-lab-hash">{e.id}</p></article>)}
      {cursor && <Button variant="outline" onClick={async () => { const generation = epoch.current; try { const r = await modelEngineering({ operation: 'list_evaluations', workspace_id: workspace, cursor, limit: 20 }); if (epoch.current === generation) { setHistory(old => [...old, ...r.evaluations]); setCursor(r.next_cursor); } } catch (e) { if (epoch.current === generation) setError(e); } }}>加载更早报告</Button>}
    </Surface> : <p>报告不可用。<Link to="/evaluation-reports">返回报告历史</Link></p>}
    <ExperimentReplayPanel key={workspace} workspace={workspace} />
  </LabShell>;
}
