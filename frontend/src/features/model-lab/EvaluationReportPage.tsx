import { useEffect, useRef, useState } from 'react';
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
import { FailureCasePanel } from './FailureCasePanel';

export function EvaluationReportPage() {
  const { id } = useParams(), workspace = useWorkspaceStore(s => s.activeId), navigate = useNavigate();
  const [data, setData] = useState<ModelEngineeringResponses['get_report'] | null>(null), [history, setHistory] = useState<Evaluation[]>([]);
  const [cursor, setCursor] = useState(''), [error, setError] = useState<unknown>(), [loading, setLoading] = useState(true), [caseID, setCaseID] = useState('');
  const epoch = useRef(0);
  useEffect(() => {
    const generation = ++epoch.current; setLoading(true); setData(null); setHistory([]); setError(undefined); setCaseID(''); setCursor('');
    const run = async () => {
      try {
        if (id) { const r = await modelEngineering({ operation: 'get_report', workspace_id: workspace, id }); if (epoch.current === generation) setData(r); }
        else { const r = await modelEngineering({ operation: 'list_evaluations', workspace_id: workspace, limit: 20 }); if (epoch.current === generation) { setHistory(r.evaluations); setCursor(r.next_cursor); } }
      } catch (e) { if (epoch.current === generation) setError(e); }
      finally { if (epoch.current === generation) setLoading(false); }
    }; void run(); return () => { epoch.current++; };
  }, [workspace, id]);
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
  return <LabShell title="Evaluation Reports" subtitle="冻结的测评结果、失败案例与可审计证据 · 当前资格单独显示，历史报告保持不可变">
    <LabError error={error} />{loading ? <LoadingState label="正在读取持久化报告…" /> : report && data ? <>
      <Surface className="ns-lab-panel"><div className="ns-lab-section"><h2>比较报告</h2><Link to={`/release-center?report=${encodeURIComponent(report.id)}`}>创建模拟发布 →</Link><Link to={`/model-lab?evaluation=${encodeURIComponent(report.evaluation_id)}`}>返回原测评 →</Link></div>
        <p className="ns-lab-notice">{analysis ? evidenceLabel(analysis.source) : '历史分析不可用 · 仅展示已保存的原始结果'}。离线证据不代表真实灰度或生产验证。</p>
        <p>已评分 {report.counts.scored}/{report.counts.planned} · 通过 {report.counts.pass} · 质量失败 {report.counts.fail} · 基础设施失败 {report.counts.infra} · 成对完成 {report.counts.paired_complete}</p>
        <p className="ns-lab-hash">Report {report.id}<br />Manifest {report.manifest_hash}<br />Attempt set {report.attempt_set_hash}<br />Report hash {data.report_hash}</p>
        <p role="status">当前晋级证据资格：{data.promotion_eligibility.eligible ? '服务端判定具备证据资格；仍须发布策略逐条校验' : '不满足'}{data.promotion_eligibility.reasons.length ? ` · ${data.promotion_eligibility.reasons.join('；')}` : ''}</p>
        {!analysis && <p className="ns-lab-muted">此报告缺少受支持的 regression-intelligence-v1 分析。不会重新计算或补造历史区间、性能或资源数据。</p>}
        <ComparisonTable report={report} analysis={analysis} />
        <JsonView value={report} title="原始不可变报告（含版本、选中与被替代尝试）" />
      </Surface>
      <Surface className="ns-lab-panel"><h2>任务与失败案例</h2><p className="ns-lab-muted">基础设施失败与质量失败分开记录；查看任一任务的原始输入、输出、预期结果与评分证据。</p>
        <div className="ns-lab-table-wrap"><table className="ns-lab-table"><thead><tr><th>任务</th><th>模型臂 / 尝试</th><th>状态 / 判定</th><th>证据</th></tr></thead><tbody>{report.selected.map(a => <tr key={a.id}><td>{a.case_id}</td><td>{a.arm} / #{a.attempt_no}</td><td>{a.state} / {a.verdict}</td><td><Button size="sm" variant="ghost" onClick={() => setCaseID(a.case_id)}>查看 {a.case_id}</Button></td></tr>)}</tbody></table></div>
        {caseID && <FailureCasePanel key={`${workspace}/${report.evaluation_id}/${caseID}`} workspace={workspace} evaluationID={report.evaluation_id} caseID={caseID} />}
      </Surface>
    </> : !id ? <Surface className="ns-lab-panel"><h2>历史测评报告</h2>{!history.length ? <EmptyState title="尚无测评报告" description="完成或部分完成的测评会保存报告；运行中任务可在 Model Lab 重开。" action={<Link to="/model-lab">前往 Model Lab</Link>} /> : history.map(e => <article className="ns-lab-record" key={e.id}><div className="ns-lab-section"><h3>{e.manifest.stable.model_name} → {e.manifest.candidate.model_name}</h3><Button size="sm" variant="outline" onClick={() => void openReport(e.id)}>重开报告或任务</Button></div><p>{STATUS[e.status]} · {evidenceLabel(e.manifest.source)} · {e.started_at}</p><p className="ns-lab-hash">{e.id}</p></article>)}
      {cursor && <Button variant="outline" onClick={async () => { const generation = epoch.current; try { const r = await modelEngineering({ operation: 'list_evaluations', workspace_id: workspace, cursor, limit: 20 }); if (epoch.current === generation) { setHistory(old => [...old, ...r.evaluations]); setCursor(r.next_cursor); } } catch (e) { if (epoch.current === generation) setError(e); } }}>加载更早报告</Button>}
    </Surface> : <p>报告不可用。<Link to="/evaluation-reports">返回报告历史</Link></p>}
  </LabShell>;
}
