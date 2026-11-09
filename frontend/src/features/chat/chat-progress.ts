import type { ChatMessage } from './chat-store';
import type { RunPhase } from './stream-runner';

const PHASE_LABEL: Record<RunPhase, string> = {
  preparing: '检索项目上下文…',
  waiting: '等待模型响应…',
  streaming: '正在生成回复…',
};

export function generationLabel(message: Pick<ChatMessage, 'agentMode' | 'agentProgress'> | undefined, phase: RunPhase): string {
  if (message?.agentMode) return message.agentProgress || '正在准备 Agent 任务…';
  return PHASE_LABEL[phase];
}
