import type { WorkspaceRun, RunEvent as TaskEvent } from '@/lib/api';
import type { RunEvent } from './stream-runner';

interface Client {
  submit(workspaceId: string, goal: string, modelId: string, sessionId: string, reviewedActions?: boolean): Promise<WorkspaceRun>;
  get(id: string): Promise<WorkspaceRun>;
  events(id: string, after: number): Promise<{ events: TaskEvent[] }>;
  cancel(id: string): Promise<unknown>;
}
interface Patch { agentRunId?: string; agentProgress?: string }
interface Entry { runId: string; stopped: boolean; timer?: ReturnType<typeof setTimeout> }

/** Acknowledges after durable registration; observation never delays cancellation. */
export function createAgentTransport(client: Client, callbacks: { update?(id: string, patch: Patch): void; cancelError(id: string, error: unknown): void }, interval = 850) {
  const entries = new Map<string, Entry>();
  async function cancel(id: string) {
    const entry = entries.get(id); if (!entry) return;
    entry.stopped = true; clearTimeout(entry.timer);
    await client.cancel(entry.runId); entries.delete(id);
  }
  return {
    has: (id: string) => entries.has(id), cancel,
    async start(id: string, workspaceId: string, sessionId: string, modelId: string, goal: string, receive: (event: RunEvent) => void, onUpdate: (patch: Patch) => void = patch => callbacks.update?.(id, patch), reviewedActions = false) {
      const run = await client.submit(workspaceId, goal, modelId, sessionId, reviewedActions);
      const entry: Entry = { runId: run.id, stopped: false }; entries.set(id, entry);
      onUpdate({ agentRunId: run.id, agentProgress: 'Agent 已提交，正在等待执行。' });
      let after = 0;
      const observe = async () => {
        if (entry.stopped) return;
        try {
          const [latest, events] = await Promise.all([client.get(run.id), client.events(run.id, after)]);
          if (entry.stopped) return;
          if (latest.workspace_id !== workspaceId || !Array.isArray(events.events)) throw new Error('Agent 任务归属或事件格式不匹配');
          if (events.events.length) {
            after = Math.max(after, ...events.events.map(event => event.seq));
            onUpdate({ agentProgress: events.events[events.events.length - 1]?.message || latest.phase });
          }
          if (latest.status !== 'queued' && latest.status !== 'running') {
            entries.delete(id); entry.stopped = true;
            const result = latest.result as { answer?: unknown } | null;
            const answer = typeof result?.answer === 'string' ? result.answer : undefined;
            receive({ request_id: id, done: true, status: latest.status === 'completed' ? 'completed' : latest.status === 'cancelled' ? 'cancelled' : 'failed', content: answer,
              error: latest.status === 'failed' || latest.status === 'interrupted' ? latest.error || 'Agent 执行中断，可查看任务记录。' : undefined });
            return;
          }
          entry.timer = setTimeout(() => void observe(), interval);
        } catch (error) {
          if (entry.stopped) return;
          try { await cancel(id); } catch (cancelError) { callbacks.cancelError(id, cancelError); }
          receive({ request_id: id, done: true, status: 'failed', error: `Agent 状态读取失败：${error instanceof Error ? error.message : String(error)}` });
        }
      };
      // Registration is the ACK boundary consumed by the common chat runner.
      entry.timer = setTimeout(() => void observe(), 0);
    },
  };
}
