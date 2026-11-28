import { useEffect, useRef, useState } from 'react';
import { api, errMessage } from '@/lib/api';
import { useAgentWorkspaceGrant, AgentGrantPrompt } from '@/features/chat/agent-workspace-grant';
import { useWorkspaceStore } from '@/stores/workspace-store';
import { AgentTaskRecord } from '@/components/AgentTaskRecord';
import { Surface } from '@/components/ui/surface';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';

export function ProjectAgentPanel() {
  const workspaceId = useWorkspaceStore(s => s.activeId);
  return <ProjectAgentForm key={workspaceId} workspaceId={workspaceId} />;
}
function ProjectAgentForm({ workspaceId }: { workspaceId: string }) {
  const [goal, setGoal] = useState('');
  const [reviewedActions,setReviewedActions] = useState(false);
  const grant = useAgentWorkspaceGrant(workspaceId, true, reviewedActions);
  const needsGrant = !!grant.binding && !grant.authorization;
  const [busy, setBusy] = useState(false);
  const [runId, setRunId] = useState('');
  const [error, setError] = useState('');
  const mounted = useRef(true);
  const submitting = useRef(false);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const submit = async () => {
    if (submitting.current || needsGrant || !goal.trim()) return;
    submitting.current = true; setBusy(true); setError('');
    try {
      const run = await api.submitWorkspaceRun(workspaceId, 'agent', `Agent · ${goal.slice(0, 60)}`, { goal: goal.trim(), ...(grant.authorization ? { ...grant.authorization, request_id: crypto.randomUUID() } : { reviewed_actions: reviewedActions }) });
      if (mounted.current) setRunId(run.id);
    } catch (e) { if (mounted.current) setError(errMessage(e)); }
    finally { submitting.current = false; if (mounted.current) setBusy(false); }
  };
  return <Surface className="px-6 py-5">
    <h2 className="text-[15px] font-semibold text-text-1">{grant.binding ? '项目 Agent' : reviewedActions ? '项目 Agent · 操作审批' : '项目只读 Agent'}</h2>
    <p className="mt-2 text-[13px] leading-6 text-text-2">{grant.binding ? 'Agent 会执行任务、检查结果，并在任务记录中交付文件。' : '根据目标搜索代码、读取项目文件、检索记忆及检查系统状态。'}</p>
    <AgentGrantPrompt grant={grant} />
    <label className="mt-3 flex items-center gap-2 text-xs text-text-2"><input type="checkbox" checked={reviewedActions} onChange={e=>setReviewedActions(e.target.checked)} /> {grant.binding ? '审阅模式（交付前确认修改）' : '允许操作提案（执行前确认）'}</label>
    <div className="mt-4 flex flex-wrap gap-3"><Input aria-label="Agent 任务目标" value={goal} maxLength={8000} onChange={e => setGoal(e.target.value)} placeholder="例如：查找项目的启动入口，并用文件证据说明调用过程" className="min-w-48 flex-1" /><Button disabled={needsGrant || busy || !goal.trim()} onClick={() => void submit()}>{busy ? '提交中…' : grant.binding ? '开始任务' : reviewedActions ? '生成操作提案' : '执行只读任务'}</Button></div>
    <p className="mt-2 text-xs text-text-3">切换页面后后台继续执行；历史任务与取消入口保存在顶部“工作区与任务”。</p>
    {error && <p role="alert" className="mt-3 text-xs text-err">{error}</p>}
    {runId && <AgentTaskRecord key={runId} runId={runId} workspaceId={workspaceId} expanded showAnswer />}
  </Surface>;
}
