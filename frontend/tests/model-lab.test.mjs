import test from 'node:test';
import assert from 'node:assert/strict';
import { loadSource } from './load-source.mjs';

test('unknown metrics never become zero and measured zero remains visible with provenance', async () => {
  const { metricText } = await loadSource('../src/features/model-lab/presentation.ts');
  assert.equal(metricText({ value: null, unit: 'bytes', source: 'unavailable', quality: 'unknown', missing_reason: 'GPU unavailable' }), '未知 · GPU unavailable');
  assert.equal(metricText({ value: 0, unit: 'percent', source: 'host', quality: 'observed' }), '0 percent · host / observed');
});

test('legacy and unsupported analysis remain unavailable; malformed v1 cannot crash report rendering', async () => {
  const { readComparison } = await loadSource('../src/features/model-lab/presentation.ts');
  for (const value of [undefined, null, {}, { version: 'future' }, { version: 'regression-intelligence-v1', arms: {} }]) assert.equal(readComparison(value), null);
});

test('preflight enforces distinct revisions and bounded local inference parameters', async () => {
  const { validateSetup } = await loadSource('../src/features/model-lab/presentation.ts');
  const options = { temperature: 0, seed: 7, num_ctx: 4096, max_output: 1024, deadline_ns: 120e9, parallelism: 1, retry_limit: 0, keep_alive: '0' };
  assert.equal(validateSetup('a', 'b', 'dataset', options), '');
  assert.match(validateSetup('a', 'a', 'dataset', options), /不同/);
  assert.match(validateSetup('a', 'b', 'dataset', { ...options, num_ctx: 8193 }), /8192/);
  assert.match(validateSetup('a', 'b', 'dataset', { ...options, deadline_ns: 121e9 }), /120/);
  assert.match(validateSetup('a', 'b', 'dataset', { ...options, max_output: NaN }), /2048/);
  assert.match(validateSetup('a', 'b', 'dataset', options, { max_context: 2048, max_output: 1024, inference_timeout_ns: 90e9 }), /2048/);
  assert.match(validateSetup('a', 'b', 'dataset', { ...options, seed: -1 }), /非负/);
  assert.equal(validateSetup('a', 'b', 'dataset', { ...options, deadline_ns: undefined }), '');
  assert.equal(validateSetup('a', 'b', 'dataset', { ...options, deadline_ns: 0 }), '');
  assert.match(validateSetup('a', 'b', 'dataset', { ...options, deadline_ns: undefined }, { max_context: 8192, max_output: 2048, inference_timeout_ns: 90e9 }), /90/);
  assert.match(validateSetup('a', 'b', 'dataset', { ...options, parallelism: 2 }), /并发 1/);
});

test('saved profile comparison ignores transport object key ordering', async () => {
  const { sameOptions } = await loadSource('../src/features/model-lab/presentation.ts');
  assert.equal(sameOptions({ seed: 7, temperature: 0 }, { temperature: 0, seed: 7 }), true);
  assert.equal(sameOptions({ seed: 7 }, { seed: 8 }), false);
});
