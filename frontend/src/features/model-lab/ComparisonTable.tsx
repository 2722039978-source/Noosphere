import type { Report } from '@/lib/model-engineering-types';
import { metricText, percent, type Comparison, type Proportion } from './presentation';

function Rate({ p }: { p: Proportion }) {
  return <><strong>{p.successes}/{p.n} · {percent(p.rate)}</strong><br /><span className="ns-lab-muted">Wilson 95%：{percent(p.lower_95)}–{percent(p.upper_95)} · n={p.n}</span></>;
}
export function ComparisonTable({ report, analysis }: { report: Report; analysis: Comparison | null }) {
  return <>
    <p className="ns-lab-muted">四格顺序：双通过 / Stable 通过且 Candidate 失败（退化）/ Stable 失败且 Candidate 通过（提升）/ 双失败。只有成对完成的独立案例进入四格；重试与回放不扩大 n。</p>
    <div className="ns-lab-table-wrap"><table className="ns-lab-table"><thead><tr><th>类别</th><th>Stable</th><th>Candidate</th><th>四格结果</th><th>成对样本 / 退化率</th></tr></thead><tbody>
      {analysis && <tr><th>全部类别</th><td><Rate p={analysis.arms.stable} /></td><td><Rate p={analysis.arms.candidate} /></td><td>{report.pair_matrix.join(' / ')}</td><td>{report.counts.paired_complete} 对<br /><Rate p={analysis.loss_rate} /></td></tr>}
      {Object.entries(report.categories).map(([name, c]) => { const a = analysis?.categories[name]; return <tr key={name}><th>{name}</th><td>{a ? <Rate p={a.stable} /> : `${c.stable.pass}/${c.stable.scored} 已评分`}</td><td>{a ? <Rate p={a.candidate} /> : `${c.candidate.pass}/${c.candidate.scored} 已评分`}</td><td>{a ? a.pair_matrix.join(' / ') : '历史分析不可用'}</td><td>{a ? <>{a.paired_complete} 对<br /><Rate p={a.loss_rate} /></> : '未知'}</td></tr>; })}
    </tbody></table></div>
    {analysis && <><h3>正式推理性能</h3><p>Candidate / Stable 成对耗时比中位数：{metricText(analysis.performance.paired_median_ratio)}</p><p>可比样本：{analysis.performance.sample_count}/{analysis.performance.planned_pairs} 对 · {analysis.performance.status} · 小于 1 表示候选正式推理更快。</p>
      {analysis.performance.reasons.length > 0 && <ul className="ns-lab-list">{analysis.performance.reasons.map((r, i) => <li key={i}>{r}</li>)}</ul>}
      <p className="ns-lab-muted">正式推理、预热、编译与 WASI 执行分开采集；缺失数据不会补零或当作资源优势。驻留验证不能证明没有外部进程竞争。</p>
      <h3>资源与性能指标覆盖</h3><div className="ns-lab-table-wrap"><table className="ns-lab-table"><thead><tr><th>指标 / 来源见案例明细</th><th>Stable 可用 / 计划</th><th>Candidate 可用 / 计划</th><th>缺失原因</th></tr></thead><tbody>{[...new Set(Object.values(analysis.metric_coverage).flatMap(arm => Object.keys(arm)))].map(name => { const s = analysis.metric_coverage.stable?.[name], c = analysis.metric_coverage.candidate?.[name]; return <tr key={name}><th>{name}<br />{s?.unit || c?.unit}</th><td>{s ? `${s.samples}/${s.planned}` : '未知'}</td><td>{c ? `${c.samples}/${c.planned}` : '未知'}</td><td>{[s, c].flatMap((m, i) => m ? Object.entries(m.missing_reasons).map(([reason, n]) => `${i === 0 ? 'Stable' : 'Candidate'}: ${reason} (${n})`) : []).join('；') || '无记录'}</td></tr>; })}</tbody></table></div>
      <details className="ns-lab-details"><summary>逐例指标、采集来源与质量</summary><pre>{JSON.stringify(analysis.diagnostics, null, 2)}</pre></details>
      <ul className="ns-lab-list">{analysis.limitations.map((l, i) => <li key={i}>{l}</li>)}</ul>
    </>}
  </>;
}
