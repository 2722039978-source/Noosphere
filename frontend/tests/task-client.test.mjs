import test from 'node:test';
import assert from 'node:assert/strict';
import { loadSource } from './load-source.mjs';
const { waitForWorkspaceRun, isRunActive } = await loadSource('../src/lib/task-client.ts');

test('queued jobs are observed serially until their durable completed result arrives', async () => {
  const states = ['queued', 'running', 'completed'];
  let calls = 0, pending = 0, peak = 0;
  const run = await waitForWorkspaceRun('run-a', async id => {
    assert.equal(id, 'run-a');
    pending++; peak = Math.max(peak, pending);
    await new Promise(resolve => setTimeout(resolve, 2));
    pending--;
    return { id, status: states[calls++], result: { project_id: 'project-a' } };
  }, { intervalMs: 1 });
  assert.equal(run.result.project_id, 'project-a');
  assert.equal(calls, 3);
  assert.equal(peak, 1);
});

test('failed, cancelled and interrupted jobs never resolve as a successful result', async () => {
  for (const status of ['failed', 'cancelled', 'interrupted']) {
    await assert.rejects(waitForWorkspaceRun('task', async () => ({ status, error: 'backend detail' })), /backend detail/);
    assert.equal(isRunActive({ status }), false);
  }
});

test('aborting observation promptly stops polling without cancelling the backend job', async () => {
  const controller = new AbortController();
  let polls = 0;
  const promise = waitForWorkspaceRun('task', async () => {
    polls++;
    return { status: 'running' };
  }, { signal: controller.signal, intervalMs: 10000 });
  await new Promise(resolve => setTimeout(resolve, 2));
  controller.abort();
  await assert.rejects(promise, error => error.name === 'AbortError');
  assert.equal(polls, 1);
});
