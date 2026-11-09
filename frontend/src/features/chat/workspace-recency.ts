import type { ChatSession } from './chat-store';

export function chooseWorkspaceSession(sessions: ChatSession[], workspaceId: string, rememberedId: string | null, legacyId: string | null) {
  let recent: ChatSession | null = null;
  let legacy: ChatSession | null = null;
  for (const session of sessions) {
    if ((session.workspaceId ?? 'local') !== workspaceId) continue;
    if (session.id === rememberedId) return session;
    if (session.id === legacyId && session.messages.length) legacy = session;
    if (session.messages.length && (!recent || session.updatedAt > recent.updatedAt)) recent = session;
  }
  return legacy ?? recent;
}

export function sortRecentWorkspaces<T extends { id: string; name: string }>(workspaces: T[], sessions: ChatSession[], activeId: string, recentIds: string[]) {
  const rank = new Map([...new Set([activeId, ...recentIds])].map((id, index) => [id, index]));
  const lastMessage = new Map<string, number>();
  for (const session of sessions) {
    if (!session.messages.length) continue;
    const id = session.workspaceId ?? 'local';
    lastMessage.set(id, Math.max(lastMessage.get(id) ?? 0, session.updatedAt));
  }
  return [...workspaces].sort((left, right) =>
    (rank.get(left.id) ?? rank.size) - (rank.get(right.id) ?? rank.size) ||
    (lastMessage.get(right.id) ?? 0) - (lastMessage.get(left.id) ?? 0) ||
    left.name.localeCompare(right.name));
}
