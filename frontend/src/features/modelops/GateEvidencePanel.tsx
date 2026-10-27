import { Link } from 'react-router-dom';
import type { GateDecision } from '@/lib/model-engineering-types';
export function GateEvidencePanel({ decision }: {decision:GateDecision}) {
 return <section aria-label="发布规则证据"><h3>服务端规则判定：{decision.allowed?'允许此操作':'不满足'}</h3><p className="ns-lab-hash">Policy {decision.policy_hash}<br/>Evidence {decision.evidence_hash}</p>
 {decision.reasons?.length>0&&<ul className="ns-lab-list">{decision.reasons.map((r,i)=><li key={i}>{r}</li>)}</ul>}
 {!decision.rules?.length?<p className="ns-lab-muted">此操作未返回逐条质量门槛；仍由服务端校验状态、版本、身份及权限。</p>:<div className="ns-lab-table-wrap"><table className="ns-lab-table"><thead><tr><th>规则 / 状态</th><th>观测 / 要求</th><th>独立样本 / 窗口</th><th>原因与证据引用</th></tr></thead><tbody>{decision.rules.map(g=><tr key={g.id}><th>{g.id}<br/>{g.status}</th><td>{String(g.observed??'未知')} / {String(g.required??'未知')} {g.unit}</td><td>{g.sample_count} 例<br/>{(g.observation_window_ns/1e9).toFixed(1)} 秒（策略窗口）</td><td>{g.reason}<details><summary>证据引用</summary><ul>{(g.evidence_ids||[]).map(id=><li className="ns-lab-hash" key={id}>{id}</li>)}</ul></details></td></tr>)}</tbody></table></div>}
 <p className="ns-lab-muted">虚拟时间、回放次数和模拟会话不会扩大独立案例数，也不能证明真实用户流量的质量。</p><Link to="/evaluation-reports">查看离线报告与失败案例 →</Link></section>;
}
