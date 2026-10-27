import test from 'node:test';
import assert from 'node:assert/strict';
import { loadSource } from './load-source.mjs';

test('engineering tasks reopen their durable reference before any result exists', async () => {
  const { workspaceRunDestination } = await loadSource('../src/lib/task-client.ts');
  assert.equal(workspaceRunDestination({ kind: 'evaluation', reference_id: 'eval-a', result: null }), '/model-lab?evaluation=eval-a');
  assert.equal(workspaceRunDestination({ kind: 'release_simulation', reference_id: 'release-a', result: null }), '/release-center/release-a');
  assert.equal(workspaceRunDestination({ kind: 'evaluation', reference_id: 'durable', result: { evaluation_id: 'other' } }), '/model-lab?evaluation=durable');
});

test('legacy task results remain navigable while unknown kinds and malformed references remain inert', async () => {
  const { workspaceRunDestination } = await loadSource('../src/lib/task-client.ts');
  assert.equal(workspaceRunDestination({ kind: 'evaluation', result: { evaluation_id: 'old-eval' } }), '/model-lab?evaluation=old-eval');
  assert.equal(workspaceRunDestination({ kind: 'release_simulation', result: { release_id: 'old-release' } }), '/release-center/old-release');
  for (const kind of ['chat', 'index', 'agent', 'future-kind']) assert.equal(workspaceRunDestination({ kind, reference_id: 'eval-a' }), null);
  for (const reference_id of ['../foreign', 'javascript:alert(1)', 'a?workspace=other', 'x'.repeat(129)]) assert.equal(workspaceRunDestination({ kind: 'evaluation', reference_id }), null);
  assert.equal(workspaceRunDestination({ kind: 'evaluation', result: null }), null);
});
