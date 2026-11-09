import { useEffect, useState } from 'react';
import { modelEngineering } from '@/lib/model-engineering-client';
import type { ModelEngineeringResponses } from '@/lib/model-engineering-types';
import { LoadingState } from '@/components/ui/feedback';
import { Button } from '@/components/ui/button';
import { LabError, JsonView } from './shared';
import { pairedCaseEvidence, verdictLabel, type ReportCase, type ReportInsights } from './report-insights';

export function ReportCasePanel({ workspace, evaluationID, reportID, row, insights }: { workspace: string; evaluationID: string; reportID: string; row: ReportCase; insights: ReportInsights }) {
  const [data, setData] = useState<ModelEngineeringResponses['get_case'] | null>(null), [error, setError] = useState<unknown>(), [refresh, setRefresh] = useState(0);
  useEffect(() => {
    let disposed = false; setData(null); setError(undefined);
    modelEngineering({ operation: 'get_case', workspace_id: workspace, evaluation_id: evaluationID, case_id: row.id, report_id: reportID, limit: 100 }).then(r => { if (!disposed) setData(r); }).catch(e => { if (!disposed) setError(e); });
    return () => { disposed = true; };
  }, [workspace, evaluationID, reportID, row.id, refresh]);
  const pair = pairedCaseEvidence(row, data);
  return <section className="ns-report-case" aria-label={`并排查看 ${row.id}`}><div className="ns-lab-section"><h3>两边输出 · {row.id}</h3><Button size="sm" variant="ghost" onClick={() => setRefresh(v => v + 1)}>重读本报告证据</Button></div><p className="ns-lab-muted">仅展示这份报告选中的尝试。后续重试不会替换这里的输出；模型代码作为文本阅读。</p>
    <LabError error={error} onRetry={() => setRefresh(v => v + 1)} />
    {!data && !error ? <LoadingState label="正在读取本报告的两边输出…" /> : <>
      {pair.task && <p>{pair.task}</p>}
      {pair.issue && <p className="ns-lab-notice">{pair.issue}</p>}
      <details className="ns-lab-details"><summary>本题输入与预期结果</summary><pre>{pair.expected || '预期结果记录暂不可读取。'}</pre></details>
      <div className="ns-report-pair">{(['stable', 'candidate'] as const).map(key => { const a = pair[key]; return <article key={key} className="ns-report-output"><p className="ns-report-eyebrow">{insights.arms[key].label}</p><h4>{insights.arms[key].modelName}</h4><p className={a.verdict === 'pass' ? 'ns-report-pass' : 'ns-lab-muted'}>{verdictLabel(a.state, a.verdict)}</p>{a.scoreSummary && <p>{a.scoreSummary}</p>}{a.notice && <p className="ns-lab-notice">{a.notice}</p>}<h5>原始模型输出</h5><pre tabIndex={0}>{a.output || '无可读取的原始模型输出。'}</pre><p className="ns-lab-hash">报告选中尝试 #{a.attemptNo ?? '—'} · {a.attemptID || '无记录'}</p>{a.score && <details className="ns-lab-details"><summary>逐输入评分明细</summary><pre>{a.score}</pre></details>}</article>; })}</div>
      {data && <JsonView value={data} title="核对冻结尝试与原始证据引用" />}
    </>}
  </section>;
}
