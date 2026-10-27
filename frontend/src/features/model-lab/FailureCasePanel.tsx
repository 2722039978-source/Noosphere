import { useEffect, useRef, useState } from 'react';
import { modelEngineering } from '@/lib/model-engineering-client';
import type { ModelEngineeringResponses } from '@/lib/model-engineering-types';
import { Button } from '@/components/ui/button';
import { LoadingState } from '@/components/ui/feedback';
import { LabError, JsonView } from './shared';
import { metricText } from './presentation';

export function FailureCasePanel({ workspace, evaluationID, caseID, revision, canRetry = false, busy = false, onRetry }: {
  workspace: string; evaluationID: string; caseID: string; revision?: number; canRetry?: boolean; busy?: boolean; onRetry?: (arm: 'stable' | 'candidate') => void;
}) {
  const [data, setData] = useState<ModelEngineeringResponses['get_case'] | null>(null), [error, setError] = useState<unknown>();
  const epoch = useRef(0);
  useEffect(() => {
    let disposed = false; epoch.current++; setData(null); setError(undefined);
    modelEngineering({ operation: 'get_case', workspace_id: workspace, evaluation_id: evaluationID, case_id: caseID, limit: 100 }).then(r => { if (!disposed) setData(r); }).catch(e => { if (!disposed) setError(e); });
    return () => { disposed = true; epoch.current++; };
  }, [workspace, evaluationID, caseID, revision]);
  return <section aria-label={`任务证据 ${caseID}`} className="ns-lab-case"><h3>任务证据 · {caseID}</h3><LabError error={error} />
    {!data && !error ? <LoadingState label="正在读取案例与证据…" /> : data && <><p>{data.case.category} · 仅查看，不执行输出中的代码或工具。</p>
      <JsonView value={data.case} title="任务定义与预期结果" />
      {data.attempts.map(a => <article key={a.id} className="ns-lab-record"><h4>{a.arm} · 尝试 #{a.attempt_no} · {a.state} / {a.verdict}</h4>
        {a.error_code && <p className="ns-lab-notice">{a.error_code} · {a.result.error_stage} · {a.result.error_reason}</p>}
        <p className="ns-lab-muted">正式推理：{metricText(a.metrics.wall_ns)} · 预热验证：{a.metrics.warm_verified ? '已验证' : '未验证'}</p>
        <JsonView value={{ expected: a.result.expected, actual: a.result.actual, difference: a.result.difference, inference_completed: a.result.inference_completed, truncated: a.result.truncated, scorer_version: a.result.scorer_version }} title="实际输出、预期与差异" />
        <JsonView value={a} title="完整尝试、资源指标与证据引用" />
      </article>)}
      <h4>输入、输出与评分证据</h4>{data.artifacts.map(a => <details key={a.id} className="ns-lab-details"><summary>{a.id} {a.missing ? '· 缺失' : ''} {a.truncated ? '· 显示已截断' : ''} {a.redacted ? '· 私密协议字段已脱敏' : ''}</summary><pre>{a.content ?? '证据内容不可用，请检查本地存储。'}</pre></details>)}
      {data.next_cursor && <Button size="sm" variant="outline" disabled={busy} onClick={async () => { const generation = epoch.current; try { const r = await modelEngineering({ operation: 'get_case', workspace_id: workspace, evaluation_id: evaluationID, case_id: caseID, cursor: data.next_cursor, limit: 100 }); if (epoch.current === generation) setData(old => old ? { ...r, attempts: [...old.attempts, ...r.attempts], artifacts: [...new Map([...old.artifacts, ...r.artifacts].map(a => [a.id, a])).values()] } : r); } catch (e) { if (epoch.current === generation) setError(e); } }}>加载更早尝试</Button>}
      {canRetry && onRetry && <div className="ns-lab-actions"><Button size="sm" variant="outline" disabled={busy} onClick={() => onRetry('stable')}>重试 Stable 此例</Button><Button size="sm" variant="outline" disabled={busy} onClick={() => onRetry('candidate')}>重试 Candidate 此例</Button><p className="ns-lab-muted">显式重试替换该例最新尝试，不会增加独立样本数；服务端重新校验身份与任务版本。</p></div>}
    </>}
  </section>;
}
