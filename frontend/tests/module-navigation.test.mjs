import test from 'node:test';
import assert from 'node:assert/strict';
import { loadSource } from './load-source.mjs';

test('module search finds Chinese descriptions and English identities without inventing records', async () => {
  const { WORKSPACE_MODULES, searchModules } = await loadSource('../src/app/modules.ts');
  assert.equal(WORKSPACE_MODULES.length, 5);
  assert.equal(new Set(WORKSPACE_MODULES.map(x => x.to)).size, 5);
  assert.equal(searchModules('  CODElens ')[0].to, '/codelens');
  assert.equal(searchModules('记忆档案')[0].to, '/nebula');
  assert.equal(searchModules('不存在的功能').length, 0);
  assert.equal(searchModules('').length, 5);
});

test('archive navigation wraps both ways and never selects settings as a business module', async () => {
  const { nextModuleIndex, WORKSPACE_MODULES } = await loadSource('../src/app/modules.ts');
  assert.equal(nextModuleIndex(0, -1), 4);
  assert.equal(nextModuleIndex(4, 1), 0);
  assert.equal(nextModuleIndex(0, 11), 1);
  assert.ok(WORKSPACE_MODULES.every(x => x.to !== '/settings'));
  assert.equal(WORKSPACE_MODULES[0].to, '/chat');
});
