import { useState } from 'react';
import { ArrowLeft, ArrowUpRight, RotateCcw } from 'lucide-react';
import { Link, useNavigate } from 'react-router-dom';
import { useRunRecord } from '@/hooks/useRunRecord';
import { AgentDeliveryPanel } from '@/components/AgentDeliveryPanel';
import { AgentActions } from '@/components/AgentActions';
import { AgentResultView } from '@/components/AgentTaskRecord';
import { AgentRunTimeline } from '@/components/AgentRunTimeline';
import { Button } from '@/components/ui/button';
import { useBackend } from '@/hooks/useBackend';
import { api, errMessage, type WorkspaceRun } from '@/lib/api';
import { chatCapabilityBlocker, selectedChatModel } from '@/lib/model-capabilities';
import { isRunActive, RUN_STATUS_LABEL, TASK_KIND_LABEL, workspaceRunDestination } from '@/lib/task-client';
import { useWorkspaceStore } from '@/stores/workspace-store';
import { newMsgId, useChatPersistence, useChatStore } from './chat-store';
import { useGenerationStore } from './chat-runtime';

function taskResult(run: WorkspaceRun) {
  const result = run.result && typeof run.result === 'object' ? run.result as Record<string, unknown> : {};
  const text = result.answer ?? result.content;
  return typeof text === 'string' ? text : '';
}

export function TaskConversation({ runId }: { runId: string }) {
  const navigate = useNavigate();
  const { models, modelsReady } = useBackend();
  const workspace = useWorkspaceStore(state => state.workspaces.find(item => item.id === state.activeId));
  const workspaceReady = useWorkspaceStore(state => state.hydrated);
  const workspaceId = workspace?.id ?? '';
  const ready = useChatPersistence(state => state.ready);
  const generation = useGenerationStore(state => state.active);
  const query = useRunRecord(workspaceId, runId, workspaceReady);
  const record = query.data;
  const [error, setError] = useState('');
  const refresh = () => { setError(''); void query.refetch(); };
  const [stopping, setStopping] = useState(false);
  const [switching, setSwitching] = useState(false);
  const run = record?.run;
  const events = record?.events ?? [];
  const session = useChatStore(state => state.sessions.find(item => item.id === run?.session_id && (item.workspaceId ?? 'local') === workspaceId));
  const active = Boolean(run && isRunActive(run));
  const liveConversation = active && generation?.sessionId === session?.id && generation?.workspaceId === workspaceId;
  const originAssistant = session?.messages.findIndex(message => message.agentRunId === runId) ?? -1;
  const question = originAssistant > 0 ? session?.messages[originAssistant - 1] : undefined;

  const showRun = run?.workspace_id === workspaceId ? run : undefined;
  const goal = showRun?.goal || (question?.role === 'user' ? question.content : '') || showRun?.title || '';
  const answer = showRun ? taskResult(showRun) : '';
  const currentModel = selectedChatModel(models, session?.modelId ?? '', workspace?.default_model_id);
  const originalModel = showRun?.model_id ? models.find(model => model.id === showRun.model_id) : undefined;
  const targetModel = session || !showRun?.model_id ? currentModel : originalModel;
  const pathChanged = Boolean(showRun?.project && showRun.project.path !== workspace?.project_path);
  const indexChanged = Boolean(showRun?.project && showRun.project.id !== workspace?.project_id);
  const originalUnavailable = Boolean(showRun?.model_id && (!originalModel || originalModel.enabled === false));
  const destination = showRun ? workspaceRunDestination(showRun) : null;

  async function followUp() {
    if (!showRun || pathChanged || switching || generation || useWorkspaceStore.getState().activeId !== workspaceId) return;
    const chat = useChatStore.getState();
    if (session) {
      chat.setActive(session.id);
      const messageId = session.messages[originAssistant]?.id;
      navigate('/chat', { state: messageId ? { historySelection: { sessionId: session.id, messageId } } : null });
      return;
    }
    if (!targetModel || chatCapabilityBlocker(targetModel, false)) return;
    const id = chat.newSession();
    if (!id) return;
    setSwitching(true);
    setError('');
    if (!await useChatStore.getState().setModel(targetModel.id)) {
      if (useChatStore.getState().activeId === id) setError(useChatStore.getState().modelSelectionError ?? '无法恢复任务模型');
      setSwitching(false);
      return;
    }
    setSwitching(false);
    if (useWorkspaceStore.getState().activeId !== workspaceId || useChatStore.getState().activeId !== id) return;
    chat.appendMessage(id, { id: newMsgId(), role: 'user', content: goal.slice(0, 3000), createdAt: Date.now() });
    if (showRun.kind === 'agent') {
      const previousTask = `历史任务状态：${RUN_STATUS_LABEL[showRun.status]}。${showRun.error ? `\n运行记录：${showRun.error.slice(0, 1000)}` : ''}`;
      const conclusion = answer ? `\n\n模型结论：\n${answer.slice(0, 5000)}` : '';
      chat.appendMessage(id, { id: newMsgId(), role: 'assistant', content: previousTask + conclusion, agentRunId: showRun.id, createdAt: Date.now() });
    } else if (answer) {
      chat.appendMessage(id, { id: newMsgId(), role: 'assistant', content: answer.slice(0, 5000), createdAt: Date.now() });
    }
    navigate('/chat');
  }

  return <div className="ns-chat flex min-h-0 flex-1">
    <div className="ns-chat-column flex min-w-0 flex-1 flex-col">
      <header className="ns-chat-session-header shrink-0 border-b border-border-soft"><div className="ns-chat-rail flex items-center gap-3">
        <Button asChild variant="ghost" size="icon" aria-label="返回对话"><Link to="/chat"><ArrowLeft aria-hidden="true" /></Link></Button>
        <div className="min-w-0 flex-1"><p className="truncate text-xs text-text-3">{workspace?.name} · 任务记录</p><h1 className="truncate">{showRun?.title || '任务记录'}</h1></div>
        {showRun && <span role="status" className="ns-task-state" data-active={active}>{RUN_STATUS_LABEL[showRun.status]}</span>}
      </div></header>
      <div className="min-h-0 flex-1 overflow-y-auto" aria-label="任务对话">
        <div className="ns-chat-rail ns-chat-messages">
          {(error || query.error) && <div role="alert" className="ns-form-error mb-3">{error || errMessage(query.error)} <Button size="sm" variant="ghost" onClick={refresh}>重试读取</Button></div>}
          {!showRun && !error && !query.error && <p role="status" className="text-sm text-text-2">正在读取任务…</p>}
          {showRun && <>
            <article className="ns-chat-message"><h2 className="ns-chat-message-label">任务目标</h2><p className="ns-message-body whitespace-pre-wrap">{goal}</p></article>
            <article className="ns-chat-message"><h2 className="ns-chat-message-label">{active ? '执行进度' : showRun.kind === 'agent' && answer ? '模型结论' : '任务状态'}</h2>
              {answer && <p className="ns-message-body whitespace-pre-wrap">{answer}</p>}
              {showRun.error && <p role="alert" className="text-sm text-err">{showRun.error}</p>}
              {!answer && !showRun.error && <p className="text-sm text-text-2">{[...events].reverse().find(event => event.message)?.message || RUN_STATUS_LABEL[showRun.status]}</p>}
              {destination && <Link to={destination} className="mt-3 inline-flex items-center gap-1 text-sm text-brand">打开产物 <ArrowUpRight size={14} /></Link>}
              {active && <Button variant="outline" size="sm" className="mt-3" disabled={stopping} onClick={() => {
                setStopping(true);
                void api.cancelWorkspaceRun(showRun.id).then(refresh, problem => setError(errMessage(problem))).finally(() => setStopping(false));
              }}>取消任务</Button>}
            </article>
            {showRun.engine_version === 2 && <AgentDeliveryPanel run={showRun} events={events} controls />}
            {showRun.kind === 'agent' && showRun.engine_version !== 2 && <div className="pb-3"><AgentActions key={runId} workspaceId={workspaceId} runId={runId} taskActive={active} /></div>}
            {(events.length > 0 || showRun.result != null) && <details className="ns-record-detail"><summary>过程与产物</summary><div className="ns-detail-body">
              {showRun.kind === 'agent' && showRun.engine_version !== 2 && <AgentResultView result={showRun.result} events={events} showAnswer={false} workspaceId={workspaceId} />}
              <AgentRunTimeline events={events} />
              {showRun.result != null && showRun.kind !== 'agent' && <pre className="max-h-52 overflow-auto whitespace-pre-wrap break-all text-xs">{JSON.stringify(showRun.result, null, 2)}</pre>}
            </div></details>}
            <details className="ns-record-detail mt-2"><summary>项目与模型</summary><dl className="ns-detail-body space-y-2 break-all text-xs text-text-2">
              <div><dt>任务类型</dt><dd>{TASK_KIND_LABEL[showRun.kind] ?? showRun.kind}</dd></div>
              <div><dt>运行时项目</dt><dd>{showRun.project ? showRun.project.path || '无项目文件夹' : '旧记录未保存项目快照'}</dd></div>
              <div><dt>索引版本</dt><dd>{showRun.project?.id || '无可用快照'}</dd></div>
              <div><dt>模型</dt><dd>{originalModel?.name || showRun.model_id || '无'}{originalUnavailable ? ' · 当前不可用' : ''}</dd></div>
            </dl></details>
            {!active && <div className="mt-5 space-y-3">
              <p className="text-xs text-text-2">{pathChanged ? '项目目录已变化。此处保留原始记录，继续前请恢复原项目目录。' : session ? '继续使用原对话。文件内容以对话中的附件快照及当前项目版本为准。' : '继续时带入任务目标与回答摘要。附件及审批需重新确认。'}</p>
              {indexChanged && !pathChanged && <p className="text-xs text-warn">项目索引版本已变化。</p>}
              {!showRun.project && <p className="text-xs text-warn">旧记录缺少项目版本信息，继续时使用当前项目。</p>}
              {modelsReady && (originalUnavailable || session && showRun.model_id && targetModel?.id !== showRun.model_id) && <p className="text-xs text-warn">原任务模型{originalUnavailable ? '当前不可用' : '与当前会话配置不同'}。{targetModel && !chatCapabilityBlocker(targetModel, false) ? `将使用 ${targetModel.name || targetModel.model_name}。` : '可在打开的对话中选择模型。'}</p>}
              <div className="flex flex-wrap gap-2">
                <Button disabled={!ready || pathChanged || !!generation || switching || !session && !!chatCapabilityBlocker(targetModel, false)} onClick={() => void followUp()}>{switching ? '正在连接…' : '继续对话'}</Button>
                {showRun.kind === 'agent' && <Button variant="outline" disabled={!ready || pathChanged || !!generation} onClick={() => {
                  const id = useChatStore.getState().newSession();
                  if (id) navigate('/chat?task=new', { state: { taskGoal: goal, taskSessionId: id } });
                }}><RotateCcw size={14} />调整后重新发起</Button>}
              </div>
            </div>}
            {liveConversation && <Button asChild variant="ghost" size="sm" className="mt-3"><Link to="/chat" onClick={() => { if (session) useChatStore.getState().setActive(session.id); }}>返回运行中的对话</Link></Button>}
          </>}
        </div>
      </div>
    </div>
  </div>;
}
