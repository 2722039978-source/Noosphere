import type { RunEvent, WorkspaceRun } from './api';

interface RunSource {
  getWorkspaceRun(id: string): Promise<WorkspaceRun>;
  getRunEvents(id: string, after: number): Promise<{ events: RunEvent[] }>;
}
export interface RunSnapshot { run: WorkspaceRun; events: RunEvent[]; cursor: number; more: boolean }

export async function readRunSnapshot(source: RunSource, workspace: string, id: string, prior?: RunSnapshot): Promise<RunSnapshot> {
  const run = await source.getWorkspaceRun(id);
  if (run.workspace_id !== workspace || run.id !== id) throw new Error('任务归属与当前工作区不符');
  const previous = prior?.run.id === id && prior.run.workspace_id === workspace ? prior : undefined;
  const cursor = previous?.cursor ?? 0;
  const { events } = await source.getRunEvents(id, cursor);
  if (!Array.isArray(events) || events.some(e => e.run_id !== id || !Number.isSafeInteger(e.seq))) throw new Error('任务事件格式异常');
  let last = cursor;
  const added = events.filter(e => {
    if (e.seq <= last) return false;
    last = e.seq;
    return true;
  });
  return { run, events: added.length ? [...(previous?.events ?? []), ...added] : previous?.events ?? [], cursor: last,
    more: events.length === 200 && last > cursor };
}
