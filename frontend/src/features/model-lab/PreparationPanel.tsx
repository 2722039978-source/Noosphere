import { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { modelEngineering } from '@/lib/model-engineering-client';
import type { EvaluationPreparation, ModelEngineeringRequest } from '@/lib/model-engineering-types';
import { Button } from '@/components/ui/button';
import { LabError } from './shared';

const actions: Record<string,{to:string;label:string}>={ 'model-hub':{to:'/model-hub',label:'管理模型版本'},datasets:{to:'/model-lab/datasets',label:'管理任务集'},codelens:{to:'/codelens',label:'检查 CodeLens'},devops:{to:'/devops',label:'查看系统资源'},settings:{to:'/settings',label:'查看设置'} };
export function PreparationPanel({ request, disabled, onResult }: {request: Extract<ModelEngineeringRequest,{operation:'prepare_evaluation'}>; disabled:boolean;onResult:(ready:boolean)=>void}) {
 const mounted=useRef(true),[busy,setBusy]=useState(false),[result,setResult]=useState<EvaluationPreparation|null>(null),[error,setError]=useState<unknown>();
 useEffect(()=>{mounted.current=true;return()=>{mounted.current=false}},[]);
 const check=async()=>{
  if(busy||disabled)return;setBusy(true);setError(undefined);setResult(null);onResult(false);
  try{const value=await modelEngineering(request);if(!mounted.current)return;setResult(value);onResult(value.ready&&value.checks.length>0&&value.checks.every(c=>c.ready));}
  catch(e){if(mounted.current)setError(e)}finally{if(mounted.current)setBusy(false)}
 };
 return <section className="ns-lab-record" aria-label="开始前完整检查"><div className="ns-lab-section"><h3>开始前完整检查</h3><Button variant="outline" disabled={disabled||busy} onClick={()=>void check()}>{busy?'正在核对准备条件…':'检查本次测评准备'}</Button></div>
 <p className="ns-lab-muted">选择模型、任务集并保存参数后，核对当前准备条件。检查不发送推理请求；修改选择后需要重新检查，开始执行时仍会再次校验。</p>
 <LabError error={error}/>
 {result&&<><p role="status">{result.ready?'准备检查通过':'还有准备条件需要处理'} · {new Date(result.checked_at).toLocaleTimeString()}</p><ul className="ns-lab-list">{result.checks.map(c=><li key={c.id}><strong>{c.ready?'✓':'待处理'} {c.label}</strong>：{c.detail}{c.code && <small>（{c.code}）</small>} {!c.ready&&c.action&&actions[c.action]&&<Link to={actions[c.action].to}>{actions[c.action].label} →</Link>}{!c.ready&&c.action==='parameters'&&<span>请展开高级推理参数，调整后重新保存。</span>}</li>)}</ul></>}
 </section>;
}
