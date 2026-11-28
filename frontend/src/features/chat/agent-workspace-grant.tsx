import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, errMessage, type AgentAuthorization } from '@/lib/api';
import { useWorkspaceStore } from '@/stores/workspace-store';
import { Button } from '@/components/ui/button';

export function useAgentWorkspaceGrant(workspaceId: string, enabled: boolean, review: boolean) {
  const [longTask, setLongTask] = useState(false);
  const workspace = useWorkspaceStore(s => s.workspaces.find(item => item.id === workspaceId));
  const binding = workspace?.project_path ? workspace.project_binding_id : undefined;
  const key = ['agent-grant', workspaceId, binding];
  const cache = useQueryClient();
  const grant = useQuery({
    queryKey: key, enabled: enabled && !!binding,
    queryFn: () => api.agentGrant(workspaceId), retry: false,
  });
  const approve = useMutation({
    mutationFn: () => api.agentGrant(workspaceId, binding),
    onSuccess: result => cache.setQueryData(key, result),
  });
  const authorized = !!binding && grant.data?.binding_id === binding;
  const authorization: AgentAuthorization | undefined = authorized
    ? { engine_version: 2, mode: review ? 'review' : 'auto', authorized_binding_id: binding,
        ...(longTask ? { budget: { model_calls: 96, tool_calls: 320, active_seconds: 3600 } } : {}) } : undefined;
  return { binding, authorization, longTask, setLongTask, path: workspace?.project_path, pending: approve.isPending || grant.isLoading,
    error: grant.error || approve.error, retry: () => grant.refetch(), approve: () => approve.mutate(), grantFailed: grant.isError };
}

export function AgentGrantPrompt({ grant }: { grant: ReturnType<typeof useAgentWorkspaceGrant> }) {
  if (!grant.binding) return null;
  if (grant.authorization) return <label className="flex flex-wrap items-center gap-2 px-3 py-1 text-xs text-text-2">
    任务预算<select aria-label="任务预算" className="rounded border border-border-subtle bg-bg-raised" value={grant.longTask ? 'long' : 'normal'}
      onChange={event => grant.setLongTask(event.target.value === 'long')}>
      <option value="normal">标准 · 48 次模型请求 / 160 次工具 / 30 分钟</option>
      <option value="long">长任务 · 96 次模型请求 / 320 次工具 / 60 分钟</option>
    </select></label>;
  return <div className="flex flex-wrap items-center gap-2 px-3 py-2 text-xs text-text-2">
    <span className="break-all">允许 Agent 在 {grant.path} 内生成、修改和验证文件。</span>
    {grant.grantFailed
      ? <Button size="sm" variant="outline" onClick={grant.retry}>重试读取授权</Button>
      : <Button size="sm" variant="outline" disabled={grant.pending} onClick={grant.approve}>授权此工作区</Button>}
    {!!grant.error && <span role="alert">{errMessage(grant.error)}</span>}
  </div>;
}
