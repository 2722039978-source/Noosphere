import { useEffect, useState } from 'react';
import { api, errMessage, type AgentAction } from '@/lib/api';
import { Button } from './ui/button';

const labels: Record<string,string> = {pending:'待确认',applying:'写入中',running:'执行中',applied:'已执行',completed:'测试完成',failed:'执行失败',rejected:'已拒绝',rolled_back:'已恢复',interrupted:'执行中断，需核查',rolling_back:'恢复中'};
export function AgentActions({workspaceId,runId,taskActive=false}:{workspaceId:string;runId:string;taskActive?:boolean}) {
 const [actions,setActions]=useState<AgentAction[]>([]);
 const [error,setError]=useState('');
 const [revision,setRevision]=useState(0);
 useEffect(()=>{
  let disposed=false;let timer:ReturnType<typeof setTimeout>;
  const poll=async()=>{try{
   const response=await api.listAgentActions(workspaceId,runId);if(disposed)return;
   if(!Array.isArray(response.actions)||response.actions.some(a=>a.workspace_id!==workspaceId||a.run_id!==runId))throw new Error('操作记录归属不匹配');
   setActions(response.actions);setError('');
   if(taskActive||response.actions.some(a=>['running','applying','rolling_back'].includes(a.status)))timer=setTimeout(()=>void poll(),1500);
  }catch(e){if(!disposed)setError(errMessage(e))}};
  void poll();return()=>{disposed=true;clearTimeout(timer)};
 },[workspaceId,runId,taskActive,revision]);
 if(!actions.length&&!error)return null;
 return <section className="mt-4 space-y-3 border-t border-border-subtle pt-3" aria-label="Agent 操作审批">
  <h4 className="text-sm font-semibold text-brand">操作预览与执行</h4>
  <p className="text-xs text-text-3">提案不会自动执行。展开核对内容后确认；文件变更和服务状态可按记录恢复。</p>
  {error&&<p role="alert" className="text-xs text-err">{error}<Button variant="outline" onClick={()=>setRevision(x=>x+1)}>重新读取操作</Button></p>}
  {actions.map(a=><ActionCard key={a.id} action={a} refresh={()=>setRevision(x=>x+1)} />)}
 </section>;
}
function ActionCard({action:a,refresh}:{action:AgentAction;refresh:()=>void}) {
 const [busy,setBusy]=useState(false),[error,setError]=useState('');
 const decide=async(decision:string)=>{if(busy)return;setBusy(true);setError('');try{await api.decideAgentAction(a.workspace_id,a.id,decision);refresh()}catch(e){setError(errMessage(e));refresh()}finally{setBusy(false)}};
 return <details className="ns-record-detail">
  <summary>{a.kind==='file'?'文件修改':a.kind==='test'?'项目测试':'服务操作'} · {a.path||a.service} · {labels[a.status]||a.status}</summary>
  <div className="ns-detail-body space-y-3 text-xs">
   <p>{a.reason}</p><p className="break-all">项目：{a.project_path}</p>
   {a.kind==='file'?<><p>{a.existed?'替换现有文件中的匹配文本':'创建新文件'} · 执行与恢复均检查文件版本</p><div className="grid gap-3 lg:grid-cols-2"><div><h5>修改前</h5><pre className="mt-1 max-h-72 overflow-auto whitespace-pre-wrap border-l-2 border-err/50 p-2">{a.existed?a.before:'（文件不存在）'}</pre></div><div><h5>修改后</h5><pre className="mt-1 max-h-72 overflow-auto whitespace-pre-wrap border-l-2 border-ok/50 p-2">{a.after||'（空文件）'}</pre></div></div></>:<><pre className="overflow-auto whitespace-pre-wrap">{a.command}</pre>{a.kind==='test'?<p>测试会执行项目代码，可能产生文件或网络副作用。仅对可信项目确认；停止会终止测试进程，测试副作用不能自动回滚。</p>:<p>原状态：{a.previous} → 目标：{a.target}。可能影响依赖此服务的应用；权限不足会报错，不自动提权。</p>}</>}
   {(a.output||a.error)&&<pre className="max-h-64 overflow-auto whitespace-pre-wrap">{a.output}{a.error?`\n${a.error}`:''}</pre>}
   {error&&<p role="alert" className="text-err">{error}</p>}
   <div className="flex flex-wrap gap-2">
    {a.status==='pending'&&<><Button disabled={busy} onClick={()=>void decide('approve')}>确认执行此操作</Button><Button variant="outline" disabled={busy} onClick={()=>void decide('reject')}>拒绝此操作</Button></>}
    {a.status==='running'&&<Button variant="outline" disabled={busy} onClick={()=>void decide('cancel')}>停止执行</Button>}
    {['applied','interrupted'].includes(a.status)&&a.kind!=='test'&&<Button variant="outline" disabled={busy} onClick={()=>void decide('rollback')}>{a.kind==='file'?'恢复原文件':'恢复原服务状态'}</Button>}
   </div>
  </div>
 </details>;
}
