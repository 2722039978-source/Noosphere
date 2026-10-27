import type { WorkspaceRun } from './api';

export const RUN_STATUS_LABEL: Record<string, string> = {
  queued: '等待执行', running: '运行中', completed: '已完成', failed: '失败',
  cancelled: '已取消', interrupted: '已中断',
};

export function isRunActive(run: Pick<WorkspaceRun, 'status'>) {
  return run.status === 'queued' || run.status === 'running';
}

/** The persisted reference also exists for queued/interrupted tasks without a result. */
export function workspaceRunDestination(run: Pick<WorkspaceRun, 'kind' | 'result' | 'reference_id'>): string | null {
  if (run.kind !== 'evaluation' && run.kind !== 'release_simulation') return null;
  const result = run.result && typeof run.result === 'object' && !Array.isArray(run.result) ? run.result as Record<string, unknown> : {};
  const id = run.reference_id || result[run.kind === 'evaluation' ? 'evaluation_id' : 'release_id'];
  if (typeof id !== 'string' || !/^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$/.test(id)) return null;
  return run.kind === 'evaluation' ? `/model-lab?evaluation=${encodeURIComponent(id)}` : `/release-center/${encodeURIComponent(id)}`;
}

function observationAborted() {
  return new DOMException('已停止观察任务；后台任务继续运行。', 'AbortError');
}

function pause(ms: number, signal?: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    if (signal?.aborted) { reject(observationAborted()); return; }
    const stop = () => { clearTimeout(timer); reject(observationAborted()); };
    const timer = setTimeout(() => { signal?.removeEventListener('abort', stop); resolve(); }, ms);
    signal?.addEventListener('abort', stop, { once: true });
  });
}

/** A single observer, never a task owner. Aborting/unmounting must not cancel durable work. */
export async function waitForWorkspaceRun(
  id: string,
  getRun: (id: string) => Promise<WorkspaceRun>,
  options: { signal?: AbortSignal; intervalMs?: number; onUpdate?: (run: WorkspaceRun) => void } = {},
): Promise<WorkspaceRun> {
  let unchanged = 0;
  let previous = '';
  while (true) {
    if (options.signal?.aborted) throw observationAborted();
    const run = await getRun(id);
    if (options.signal?.aborted) throw observationAborted();
    options.onUpdate?.(run);
    if (!isRunActive(run)) {
      if (run.status !== 'completed') throw new Error(run.error || `任务${RUN_STATUS_LABEL[run.status] || run.status}，可在任务中心查看详情。`);
      return run;
    }
    const marker = `${run.status}:${run.phase}:${run.updated_at}`;
    unchanged = marker === previous ? unchanged + 1 : 0;
    previous = marker;
    await pause(Math.min((options.intervalMs ?? 700) * (1 + unchanged), 4000), options.signal);
  }
}
