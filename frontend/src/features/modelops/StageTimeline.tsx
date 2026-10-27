import type { Release, ReleaseState } from '@/lib/model-engineering-types';
import { STAGES } from './presentation';
export function StageTimeline({ release }: { release:Release }) {
 const index=STAGES.indexOf((release.state==='PAUSED'?release.paused_from:release.state) as typeof STAGES[number]);
 return <><ol className="ns-release-stages" aria-label="模拟灰度阶段">{STAGES.map((state,i)=><li key={state} aria-current={release.state===state?'step':undefined} data-done={index>=i}><span>{i+1}</span><strong>{state}</strong><small>{state==='PROMOTED'?'100% 模拟分配':state.startsWith('CANARY_')?`${state.slice(7)}% 模拟分配`:state==='SHADOW'?'候选输出不服务用户':'准备与离线证据'}</small></li>)}</ol>
 {!STAGES.includes(release.state as ReleaseState & typeof STAGES[number])&&<p className="ns-lab-notice">当前状态：{release.state}{release.paused_from?` · 暂停前 ${release.paused_from}`:''}。{release.state==='ROLLED_BACK'?'候选新请求分配已关闭，保留原稳定基线与审计。':''}</p>}</>;
}
