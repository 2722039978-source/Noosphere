import type { ReportInsights } from './report-insights';

export function CapabilityProfile({ insights, imported = false }: { insights: ReportInsights; imported?: boolean }) {
  return <section className="ns-capability-profile" aria-label={imported ? '源实验包能力画像' : '能力画像（本次任务集）'}>
    <p className="ns-report-eyebrow">CAPABILITY PROFILE / {imported ? '导入证据覆盖' : '本次任务覆盖'}</p><h2>{imported ? '源实验包能力画像' : '能力画像'}</h2>
    <p className="ns-lab-muted">按冻结任务分类核对。分母是该类独立任务数；未评分包含环境失败、取消与未完成，不能当作模型答错。未测能力保持未知，不生成综合分或通用排名。</p>
    <div className="ns-lab-table-wrap"><table className="ns-lab-table"><thead><tr><th>能力 / 任务分类</th><th>{insights.arms.stable.label}</th><th>{insights.arms.candidate.label}</th></tr></thead><tbody>{insights.profile.map(row => <tr key={row.category}><th scope="row">{row.label}<br /><span className="ns-lab-muted">{row.modeLabel}{row.reason ? ` · ${row.reason}` : ''}</span></th>{(['stable', 'candidate'] as const).map(key => <td key={key}>{row.mode === 'manual' ? '需人工评阅 · 无自动分' : row.planned === null ? '未知 · 冻结任务目录不可读取' : row.planned === 0 ? '未知 · 本次未测' : <><strong>通过 {row.arms[key].pass}/{row.planned}</strong><br /><span className="ns-lab-muted">已评分 {row.arms[key].scored}/{row.planned} · 未评分 {row.arms[key].unavailable}<br />其中基础设施失败 {row.arms[key].infra}</span></>}</td>)}</tr>)}</tbody></table></div>
  </section>;
}
