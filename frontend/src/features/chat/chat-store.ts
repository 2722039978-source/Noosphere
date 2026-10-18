import { create } from "zustand";
import { persist } from "zustand/middleware";
import { createIndexedChatStorage } from "./indexed-chat-storage";
import { parseChatBackup, mergeChatBackup, validateChatSnapshot, sessionWorkspaceId, activeWorkspaceSession } from "./chat-backup";
import { useWorkspaceStore } from "@/stores/workspace-store";
import type { ContextCitation } from "@/lib/api";

export type MessageStatus = "preparing" | "streaming" | "completed" | "cancelled" | "failed" | "interrupted";

export interface ChatMessage {
  id: string;
  role: "user" | "assistant";
  content: string;
  /** 恢复时将未完成生成标记为 interrupted，不自动重新调用模型。 */
  streaming?: boolean;
  status?: MessageStatus;
  error?: string;
  /** RAG 注入的上下文条数 */
  contextCount?: number;
  citations?: ContextCitation[];
  contextWarnings?: string[];
  retrievalMode?: 'lexical' | 'semantic';
  agentMode?: boolean;
  reviewedActions?: boolean;
  agentRunId?: string;
  agentProgress?: string;
  createdAt: number;
}

export interface ChatSession {
  id: string;
  /** Unscoped historic archives belong to the local workspace. */
  workspaceId?: string;
  title: string;
  messages: ChatMessage[];
  modelId: string;
  createdAt: number;
  updatedAt: number;
}

interface ChatState {
  sessions: ChatSession[];
  activeId: string | null;

  newSession: () => string;
  removeSession: (id: string) => void;
  setActive: (id: string) => void;
  setModel: (modelId: string) => void;

  appendMessage: (sessionId: string, msg: ChatMessage) => void;
  appendToken: (sessionId: string, msgId: string, token: string) => void;
  finishMessage: (sessionId: string, msgId: string, error?: string) => void;
  updateMessage: (sessionId: string, msgId: string, patch: Partial<ChatMessage>) => void;
  removeMessageFrom: (sessionId: string, msgId: string) => void;
  clearStreamingFlags: () => void;
}

const uid = () => `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;

export const useChatPersistence = create<{ error: string | null; ready: boolean }>(() => ({ error: null, ready: false }));
const chatStorage = createIndexedChatStorage<Pick<ChatState, "sessions" | "activeId">>({
  factory: () => indexedDB,
  legacy: () => localStorage,
  validate: validateChatSnapshot,
  onError: (error) => useChatPersistence.setState({ error: error ? "会话存储未完成，原始历史已保留。请检查存储空间或备份后重启。" : null }),
});
export const flushChatPersistence = () => chatStorage.flush();
export const isChatPersistenceDirty = () => chatStorage.isDirty();

export async function importChatBackup(raw: string) {
  if (!useChatPersistence.getState().ready) throw new Error('请等待会话加载完成。');
  const incoming = parseChatBackup(raw);
  const existing = new Set(useChatStore.getState().sessions.map(session => session.id));
  const foreignIds = new Set(incoming.sessions.filter(session => !existing.has(session.id)).map(sessionWorkspaceId).filter(id => id !== 'local'));
  const restored = new Map<string, string>();
  if (foreignIds.size) {
    await useWorkspaceStore.getState().refresh();
    const known = new Set(useWorkspaceStore.getState().workspaces.map(workspace => workspace.id));
    for (const id of foreignIds) {
      if (known.has(id)) continue;
      // A different installation has no trustworthy project/model binding for
      // this ID. Restore an empty workspace rather than hiding or mixing chats.
      const workspace = await useWorkspaceStore.getState().save({ name: `导入工作区 ${id.slice(0, 12)}`, project_path: '', default_model_id: '' });
      restored.set(id, workspace.id);
    }
  }
  const normalized = { ...incoming, sessions: incoming.sessions.map(session => ({
    ...session,
    ...(restored.has(sessionWorkspaceId(session)) ? {workspaceId: restored.get(sessionWorkspaceId(session)), modelId: ''} : {}),
    messages: session.messages.map(message => message.streaming ? {...message, streaming: false, status: 'interrupted' as const, error: '导入的未完成回复已中断'} : message),
  })) };
  // Read current state after the asynchronous recovery: navigating/receiving a
  // reply while importing must not be overwritten or marked interrupted.
  useChatStore.setState(mergeChatBackup(useChatStore.getState(), normalized));
  if (!await flushChatPersistence()) throw new Error('导入尚未保存，请重试保存。');
  return { recoveredWorkspaces: restored.size };
}

export function exportChatBackup() {
  if (!useChatPersistence.getState().ready) throw new Error('请等待会话加载完成。');
  const { sessions, activeId } = useChatStore.getState();
  return JSON.stringify({ format:'noosphere-chat-backup',version:1,state:{sessions,activeId} },null,2);
}

export const useChatStore = create<ChatState>()(
  persist(
    (set, get) => ({
      sessions: [],
      activeId: null,

      newSession: () => {
        if (!useChatPersistence.getState().ready) return '';
        const id = uid();
        const s: ChatSession = {
          id,
          workspaceId: useWorkspaceStore.getState().activeId,
          title: "新会话",
          messages: [],
          modelId: "",
          createdAt: Date.now(),
          updatedAt: Date.now(),
        };
        set((st) => ({ sessions: [s, ...st.sessions], activeId: id }));
        flushChatPersistence();
        return id;
      },

      removeSession: (id) => {
        set((st) => {
          const sessions = st.sessions.filter((s) => s.id !== id);
          const activeId =
            st.activeId === id ? (activeWorkspaceSession(sessions, null, useWorkspaceStore.getState().activeId)?.id ?? null) : st.activeId;
          return { sessions, activeId };
        });
        flushChatPersistence();
      },

      setActive: (activeId) => {
        const session = get().sessions.find(item => item.id === activeId);
        if (session && sessionWorkspaceId(session) === useWorkspaceStore.getState().activeId) set({ activeId });
      },

      setModel: (modelId) => {
        const { activeId } = get();
        if (!activeId) return;
        const session = get().sessions.find(item => item.id === activeId);
        if (!session || sessionWorkspaceId(session) !== useWorkspaceStore.getState().activeId) return;
        set((st) => ({
          sessions: st.sessions.map((s) =>
            s.id === activeId ? { ...s, modelId, updatedAt: Date.now() } : s
          ),
        }));
      },

      appendMessage: (sessionId, msg) => {
        set((st) => ({
          sessions: st.sessions.map((s) =>
            s.id === sessionId
              ? {
                  ...s,
                  title:
                    s.title === "新会话" && msg.role === "user"
                      ? msg.content.slice(0, 24) || "新会话"
                      : s.title,
                  messages: [...s.messages, msg],
                  updatedAt: Date.now(),
                }
              : s
          ),
        }));
        flushChatPersistence();
      },

      updateMessage: (sessionId, msgId, patch) => set((st) => ({
        sessions: st.sessions.map((s) => s.id === sessionId ? {
          ...s,
          messages: s.messages.map((m) => m.id === msgId ? { ...m, ...patch, id: m.id, role: m.role } : m),
        } : s),
      })),

      appendToken: (sessionId, msgId, token) =>
        set((st) => ({
          sessions: st.sessions.map((s) =>
            s.id === sessionId
              ? {
                  ...s,
                  messages: s.messages.map((m) =>
                    m.id === msgId ? { ...m, content: m.content + token } : m
                  ),
                  updatedAt: Date.now(),
                }
              : s
          ),
        })),

      finishMessage: (sessionId, msgId, error) => {
        set((st) => ({
          sessions: st.sessions.map((s) =>
            s.id === sessionId
              ? {
                  ...s,
                  messages: s.messages.map((m) =>
                    m.id === msgId
                      ? { ...m, streaming: false, status: error ? "failed" : "completed", error }
                      : m
                  ),
                }
              : s
          ),
        }));
        flushChatPersistence();
      },

      removeMessageFrom: (sessionId, msgId) =>
        set((st) => ({
          sessions: st.sessions.map((s) => {
            if (s.id !== sessionId) return s;
            const idx = s.messages.findIndex((m) => m.id === msgId);
            return idx < 0 ? s : { ...s, messages: s.messages.slice(0, idx) };
          }),
        })),

      clearStreamingFlags: () =>
        set((st) => ({
          sessions: st.sessions.map((s) => ({
            ...s,
            messages: s.messages.map((m) =>
              m.streaming ? { ...m, streaming: false, status: "interrupted", error: "上次生成已中断" } : m
            ),
          })),
        })),
    }),
    {
      name: "noosphere-chat-v1",
      storage: chatStorage,
      partialize: ({ sessions, activeId }) => ({ sessions, activeId }),
      onRehydrateStorage: () => (state, error) => { useChatPersistence.setState({ready:!error}); if (!error) state?.clearStreamingFlags(); },
    }
  )
);

export const newMsgId = uid;
