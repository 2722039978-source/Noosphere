import type { ModelConfig } from '@/hooks/useBackend';
import type { ChatSession } from './chat-store';

export type ModelChatIntent = { key: string; modelId: string; workspaceId: string };
type Resolution = { status: 'none' | 'consumed' | 'loading' | 'unavailable' | 'invalid' | 'disabled' | 'workspace-changed'; message?: string; model?: undefined } | { status: 'ready'; model: ModelConfig; message?: undefined };
type IntentContext = { models: ModelConfig[]; modelsReady: boolean; modelsError: string | null; persistenceReady: boolean; workspaceReady: boolean; workspaceId: string; historyState?: unknown };
type ConsumedIntent = { modelId: string; workspaceId: string; sessionId: string };

export function readModelChatIntent(search: string, key: string, workspaceId: string): ModelChatIntent | null {
  const params = new URLSearchParams(search);
  return params.has('model') ? { key, modelId: params.get('model') ?? '', workspaceId } : null;
}

export function consumedModelChatState(state: unknown, intent: ModelChatIntent, workspaceId: string, sessionId: string) {
  return { ...(state && typeof state === 'object' ? state : {}), modelChatIntent: { modelId: intent.modelId, workspaceId, sessionId } satisfies ConsumedIntent };
}

/** A URL intent is scoped once, then marked in its history entry. It never supplies a fallback. */
export function resolveModelChatIntent(intent: ModelChatIntent | null, context: IntentContext): Resolution {
  if (!intent) return { status: 'none' };
  const history = context.historyState && typeof context.historyState === 'object' ? context.historyState as { modelChatIntent?: ConsumedIntent } : null;
  if (history?.modelChatIntent?.modelId === intent.modelId && history.modelChatIntent.sessionId) return { status: 'consumed' };
  if (intent.workspaceId !== context.workspaceId) return { status: 'workspace-changed', message: '工作区已切换，未将刚才的模型选择应用到其他工作区。请选择本工作区要使用的模型。' };
  if (!context.persistenceReady || !context.workspaceReady) return { status: 'loading', message: '正在恢复本机工作区与会话…' };
  if (!intent.modelId) return { status: 'invalid', message: '链接中没有有效的模型配置，请选择一个可用模型。' };
  if (!context.modelsReady) return context.modelsError
    ? { status: 'unavailable', message: `暂时无法读取模型配置：${context.modelsError}。恢复连接后可重试。` }
    : { status: 'loading', message: '正在读取所选模型配置…' };
  const model = context.models.find(item => item.id === intent.modelId);
  if (!model) return { status: 'invalid', message: `模型配置「${intent.modelId}」不存在或已被删除，请选择一个可用模型。` };
  if (model.enabled === false) return { status: 'disabled', message: `模型「${model.name || model.model_name || model.id}」已停用，请选择一个可用模型，或前往 Gateway 启用。` };
  return { status: 'ready', model };
}

/** Starting from a model preserves conversations with messages and active generations. */
export function modelChatSessionPlan(session: Pick<ChatSession, 'id' | 'workspaceId' | 'messages'> | null | undefined, workspaceId: string, generatingSessionId: string | null): { create: true } | { create: false; sessionId: string } {
  return session && (session.workspaceId ?? 'local') === workspaceId && !session.messages.length && session.id !== generatingSessionId
    ? { create: false, sessionId: session.id }
    : { create: true };
}
