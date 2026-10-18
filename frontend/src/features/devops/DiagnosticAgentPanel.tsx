import { useEffect, useRef, useState } from 'react';
import { api, errMessage, type WorkspaceRun } from '@/lib/api';
import { isRunActive, RUN_STATUS_LABEL, waitForWorkspaceRun } from '@/lib/task-client';
import { useWorkspaceStore } from '@/stores/workspace-store';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Surface } from '@/components/ui/surface';

export function DiagnosticAgentPanel() {
  const [issue,setIssue]=useState('');
  const [busy,setBusy]=useState(false);
  const [result,setResult]=useState<Awaited<ReturnType<typeof api.devopsLoop>> | null>(null);
  const [error,setError]=useState('');
  const [task,setTask]=useState<WorkspaceRun | null>(null);
  const [cancelling,setCancelling]=useState(false);
  const activeId=useWorkspaceStore(state=>state.activeId);
  const observation=useRef<AbortController | null>(null);
  const currentWorkspace=useRef(activeId); currentWorkspace.current=activeId;
  useEffect(()=>{
    setBusy(false);setTask(null);setResult(null);setError('');
    return ()=>{observation.current?.abort();observation.current=null;};
  },[activeId]);
  const run=async()=>{
    if(busy)return;
    const workspaceId=activeId;
    const controller=new AbortController();observation.current=controller;setBusy(true);setError('');setResult(null);setTask(null);
    try {
      const submitted=await api.submitWorkspaceRun(workspaceId,'diagnosis',`诊断 · ${issue.trim().slice(0,60)}`,{issue:issue.trim()});
      if(controller.signal.aborted || currentWorkspace.current!==workspaceId)return;
      setTask(submitted);
      const completed=await waitForWorkspaceRun(submitted.id,api.getWorkspaceRun,{signal:controller.signal,onUpdate:setTask});
      setResult(completed.result as Awaited<ReturnType<typeof api.devopsLoop>>);
    }
    catch(error){if(!controller.signal.aborted && currentWorkspace.current===workspaceId)setError(errMessage(error));}
    finally{if(observation.current===controller){observation.current=null;setBusy(false);}}
  };
  const cancel=async()=>{
    if(!task || cancelling)return;
    setCancelling(true);
    try{const cancelled=await api.cancelWorkspaceRun(task.id);if(currentWorkspace.current===activeId)setTask(cancelled);}
    catch(error){if(currentWorkspace.current===activeId)setError(errMessage(error));}
    finally{setCancelling(false);}
  };
  return <Surface className="px-6 py-5">
    <h2 className="text-[15px] font-semibold text-text-1">诊断 Agent</h2>
    <p className="mt-2 text-[13px] text-text-2">描述问题及服务名或日志路径。最多执行 5 步只读检查，完成后显示工具结果与诊断。</p>
    <div className="mt-4 flex flex-wrap gap-3">
      <Input aria-label="诊断问题" value={issue} disabled={busy} maxLength={4000} onChange={e=>setIssue(e.target.value)} placeholder="例如：检查 Spooler 服务的状态" className="min-w-48 flex-1" />
      <Button disabled={busy||!issue.trim()} onClick={()=>void run()}>{busy?'诊断中…':'开始诊断'}</Button>
      {task && isRunActive(task) && <Button variant="outline" disabled={cancelling} onClick={()=>void cancel()}>{cancelling?'取消中…':'取消诊断'}</Button>}
    </div>
    {error && <p role="alert" className="mt-3 text-xs text-err">{error}</p>}
    {busy && <p role="status" className="ns-result-status">{task ? RUN_STATUS_LABEL[task.status] : '正在提交'} · 可切换页面，后台继续诊断；进度与结果保存在顶部“工作区与任务”。</p>}
    {result && <div className="ns-enter mt-4 space-y-3">
      <p className="whitespace-pre-wrap text-sm text-text-1">{result.answer}</p>
      {(result.trace || []).map((step,index)=><details key={index} className="ns-record-detail">
        <summary>{index+1}. {step.action} · {step.result?.success?'完成':'未完成'}</summary>
        <div className="ns-detail-body"><p>{step.thought}</p>
        <pre className="mt-2 max-h-48 overflow-auto whitespace-pre-wrap">{step.result?.output || step.result?.error}</pre>
        </div></details>)}
    </div>}
  </Surface>;
}
