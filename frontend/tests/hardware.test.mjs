import test from 'node:test';
import assert from 'node:assert/strict';
import { loadSource } from './load-source.mjs';

test('GPU capacities distinguish missing and invalid data from an actual 8 GiB adapter', async () => {
  const { hardwareCapacity } = await loadSource('../src/features/model-lab/hardware-presentation.ts');
  for (const value of [null, undefined, -1, 0, NaN]) assert.equal(hardwareCapacity(value), '未知');
  assert.equal(hardwareCapacity(8589934592), '8.00 GiB');
});

test('legacy, future or incomplete inventories cannot masquerade as a detected device', async () => {
  const { readHardware } = await loadSource('../src/features/model-lab/hardware-presentation.ts');
  for (const value of [undefined, null, {}, { schema_version: 2 }, { schema_version: 1, status: 'complete' }]) assert.equal(readHardware(value), null);
});
