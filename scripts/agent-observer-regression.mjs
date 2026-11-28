import assert from 'node:assert/strict';
import { test } from 'node:test';
import { setTimeout as wait } from 'node:timers/promises';
import { readRunSnapshot } from '../frontend/src/lib/run-observation.ts';
import { createAgentTransport } from '../frontend/src/features/chat/agent-transport.ts';

const run = { id: 'run-1', workspace_id: 'ws', engine_version: 2, status: 'completed' };

test('cursor pagination drains a terminal run and tolerates duplicates', async () => {
  const history = Array.from({ length: 203 }, (_, i) => ({ run_id: run.id, seq: i + 1 }));
  const source = { getWorkspaceRun: async () => run, getRunEvents: async (_id, cursor) =>
    ({ events: history.filter(e => e.seq >= Math.max(1, cursor)).slice(0, 200) }) };
  const first = await readRunSnapshot(source, 'ws', run.id);
  assert.equal(first.more, true);
  const final = await readRunSnapshot(source, 'ws', run.id, first);
  assert.equal(final.more, false);
  assert.deepEqual(final.events, history);
  await assert.rejects(readRunSnapshot(source, 'wrong', run.id), /工作区/);
});

test('a v2 task detaches after durable registration and never cancels on observer disconnect', async () => {
  const updates = [], events = [];
  const client = { submit: async () => run, get: assert.fail, events: assert.fail, cancel: assert.fail };
  const transport = createAgentTransport(client, { cancelError: assert.fail });
  await transport.start('request', 'ws', 'session', 'model', 'goal', event => events.push(event), patch => updates.push(patch));
  await wait(10);
  assert.equal(updates[0].agentEngine, 2);
  assert.equal(updates[0].agentRunId, run.id);
  assert.equal(events[0].done, true);
  assert.equal(transport.has('request'), false);
});

test('explicit stop during v2 registration cancels the matching run once', async () => {
  const cancelled = [], received = [];
  const client = { submit: async () => run, get: assert.fail, events: assert.fail, cancel: id => cancelled.push(id) };
  const transport = createAgentTransport(client, { cancelError: assert.fail });
  await transport.start('request', 'ws', 'session', 'model', 'goal', event => received.push(event));
  await transport.cancel('request');
  await wait(10);
  assert.deepEqual(cancelled, [run.id]);
  assert.deepEqual(received, []);
});
