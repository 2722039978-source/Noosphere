import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { ChevronRight, ListTodo, Plus } from 'lucide-react';
import { Link, useLocation } from 'react-router-dom';
import { api, errMessage } from '@/lib/api';
import { isRunActive, RUN_STATUS_LABEL, TASK_KIND_LABEL, taskRecordPath } from '@/lib/task-client';
import { useChatPersistence, useChatStore } from './chat-store';
import { useGenerationStore } from './chat-runtime';

export function WorkspaceTasks({ workspaceId, onNavigate }: { workspaceId: string; onNavigate?: () => void }) {
  const ready = useChatPersistence(state => state.ready);
  const generation = useGenerationStore(state => state.active?.workspaceId === workspaceId);
  const { pathname, search } = useLocation();
  const task = pathname === '/chat' ? new URLSearchParams(search).get('task') : null;
  const [open, setOpen] = useState(true);
  const [all, setAll] = useState(false);
  const query = useQuery({
    queryKey: ['workspace-tasks', workspaceId],
    queryFn: async () => {
      const { runs } = await api.listWorkspaceRuns(workspaceId, true);
      if (!Array.isArray(runs)) throw new Error('无法读取任务记录');
      return runs.filter(run => run.workspace_id === workspaceId && run.kind !== 'chat');
    },
    staleTime: 1200,
    refetchInterval: query => generation || query.state.data?.some(isRunActive) ? 1400 : open ? 6500 : false,
    enabled: open,
  });
  const runs = query.data ?? [];
  const active = runs.filter(isRunActive).length;
  return <section className="ns-sidebar-tasks" aria-label="任务">
    <div className="ns-sidebar-task-heading">
      <button type="button" aria-expanded={open} onClick={() => setOpen(value => !value)}><ChevronRight size={14} aria-hidden="true" />任务{active > 0 && <span className="ns-project-run-label">{active} 运行中</span>}</button>
      {ready && <Link to="/chat?task=new" aria-label="新建任务" title="新建任务" onClick={() => { useChatStore.getState().newSession(); onNavigate?.(); }}><Plus size={17} aria-hidden="true" /></Link>}
    </div>
    {open && <div className="ns-sidebar-task-list">
      {query.isError && <button type="button" onClick={() => void query.refetch()} className="ns-sidebar-task-more" title={errMessage(query.error)}>重试读取任务</button>}
      {runs.slice(0, all ? runs.length : 5).map(run => <Link className="ns-sidebar-task" key={run.id} to={taskRecordPath(run.id)} onClick={onNavigate} aria-current={task === run.id ? 'page' : undefined} aria-label={`打开任务 ${run.title || TASK_KIND_LABEL[run.kind] || run.kind}`}>
        <ListTodo size={14} aria-hidden="true" /><span title={run.title}>{run.title || TASK_KIND_LABEL[run.kind] || run.kind}</span><small data-active={isRunActive(run)}>{RUN_STATUS_LABEL[run.status] ?? run.status}</small>
      </Link>)}
      {runs.length > 5 && <button type="button" className="ns-sidebar-task-more" onClick={() => setAll(value => !value)}>{all ? '收起' : `查看全部（${runs.length}）`}</button>}
    </div>}
  </section>;
}
