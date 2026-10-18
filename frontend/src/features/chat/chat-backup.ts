import type { ChatSession } from './chat-store';

export interface ChatSnapshot { sessions: ChatSession[]; activeId: string | null }

export const sessionWorkspaceId = (session: ChatSession) => session.workspaceId ?? 'local';
export const sessionsInWorkspace = (sessions: ChatSession[], workspaceId: string) => sessions.filter(session => sessionWorkspaceId(session) === workspaceId);
export function activeWorkspaceSession(sessions: ChatSession[], activeId: string | null, workspaceId: string) {
  const scoped = sessionsInWorkspace(sessions, workspaceId);
  return scoped.find(session => session.id === activeId) ?? scoped[0] ?? null;
}

export function parseChatBackup(raw: string): ChatSnapshot {
  const envelope = JSON.parse(raw);
  if (envelope.format && (envelope.format !== 'noosphere-chat-backup' || envelope.version !== 1)) throw new Error('不支持此备份版本。');
  return validateChatSnapshot(envelope.state);
}

// Validate structure without serializing or imposing a lifetime size cap;
// exported archives must remain importable as they accumulate conversations.
export function validateChatSnapshot(value: unknown): ChatSnapshot {
  const state = value as ChatSnapshot;
  if (!state || !Array.isArray(state.sessions) || (state.activeId !== null && typeof state.activeId !== 'string')) throw new Error('会话备份格式无效。');
  const ids = new Set<string>();
  for (const session of state.sessions) {
    if (session.workspaceId !== undefined && (typeof session.workspaceId !== 'string' || !/^[A-Za-z0-9_-]{1,128}$/.test(session.workspaceId))) throw new Error('会话工作区标识无效。');
    if (!session || typeof session.id !== 'string' || !session.id || ids.has(session.id) || typeof session.title !== 'string' || typeof session.modelId !== 'string' || !Number.isFinite(session.createdAt) || !Number.isFinite(session.updatedAt) || !Array.isArray(session.messages)) throw new Error('会话数据不完整或 ID 重复。');
    ids.add(session.id);
    const messages = new Set<string>();
    for (const message of session.messages) {
      if (!message || typeof message.id !== 'string' || !message.id || messages.has(message.id) || !['user','assistant'].includes(message.role) || typeof message.content !== 'string' || !Number.isFinite(message.createdAt)) throw new Error('消息数据无效。');
      if (message.retrievalMode !== undefined && !['lexical', 'semantic'].includes(message.retrievalMode)) throw new Error('检索模式无效。');
      if (message.contextWarnings !== undefined && (!Array.isArray(message.contextWarnings) || message.contextWarnings.length > 8 || message.contextWarnings.some(w => typeof w !== 'string' || w.length > 8192))) throw new Error('引用提示格式无效。');
      if (message.citations !== undefined && (!Array.isArray(message.citations) || message.citations.length > 8 || message.citations.some(c => {
        if (!c || !['code', 'memory'].includes(c.kind)) return true;
        for (const key of ['id', 'title', 'excerpt', 'workspace_id'] as const) if (typeof c[key] !== 'string' || c[key].length > 20000) return true;
        for (const key of ['file', 'source_hash', 'symbol', 'memory_id', 'project_id'] as const) if (c[key] !== undefined && (typeof c[key] !== 'string' || c[key]!.length > 20000)) return true;
        for (const key of ['start_line', 'end_line', 'revision'] as const) if (c[key] !== undefined && (!Number.isSafeInteger(c[key]) || c[key]! < 0)) return true;
        return false;
      }))) throw new Error('引用快照格式无效。');
      messages.add(message.id);
      if (message.agentRunId !== undefined && (typeof message.agentRunId !== 'string' || !/^[A-Za-z0-9_-]{1,128}$/.test(message.agentRunId))) throw new Error('Agent 任务标识无效。');
      if (message.reviewedActions !== undefined && typeof message.reviewedActions !== 'boolean') throw new Error('操作模式格式无效。');
      if (message.agentMode !== undefined && typeof message.agentMode !== 'boolean') throw new Error('Agent 模式格式无效。');
      if (message.agentProgress !== undefined && (typeof message.agentProgress !== 'string' || message.agentProgress.length > 8192)) throw new Error('Agent 进度格式无效。');
    }
  }
  return { sessions: state.sessions, activeId: state.activeId !== null && ids.has(state.activeId) ? state.activeId : state.sessions[0]?.id ?? null };
}

export function mergeChatBackup(current: ChatSnapshot, incoming: ChatSnapshot): ChatSnapshot {
  const known = new Set(current.sessions.map((session) => session.id));
  // Existing IDs win: imports never silently replace a local conversation.
  return { sessions: [...current.sessions, ...incoming.sessions.filter((session) => !known.has(session.id))], activeId: current.activeId ?? incoming.activeId };
}
