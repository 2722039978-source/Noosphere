import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

const moduleURL = source => `data:text/javascript;base64,${Buffer.from(ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
}).outputText).toString('base64')}`;
const identityURL = moduleURL(await readFile(new URL('../src/features/model-lab/model-identity.ts', import.meta.url), 'utf8'));
const source = await readFile(new URL('../src/features/model-lab/hub-presentation.ts', import.meta.url), 'utf8');
const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } }).outputText
  .replace(/from ['"]\.\/model-identity['"]/g, `from ${JSON.stringify(identityURL)}`);
const { primaryHubModel, modelDeployment, filterHubModels, matchingRevision, modelChatDestination } = await import(`data:text/javascript;base64,${Buffer.from(compiled).toString('base64')}`);
const models = [
  { id: 'disabled', name: 'A', enabled: false, is_default: true, base_url: 'http://localhost:11434', provider: 'ollama', model_name: 'local-a' },
  { id: 'global', name: 'B', enabled: true, is_default: true, base_url: 'https://api.example.com', provider: 'openai_compatible', model_name: 'remote-b' },
  { id: 'local /模型', name: 'C', enabled: true, base_url: 'http://127.0.0.1:8080/v1', provider: 'openai_compatible', model_name: 'bonsai' },
];
test('featured model honors enabled workspace default, then global default, then enabled order', () => {
  assert.equal(primaryHubModel(models, 'local /模型').id, 'local /模型');
  assert.equal(primaryHubModel(models, 'disabled').id, 'global');
  assert.equal(primaryHubModel(models.map(m => ({ ...m, is_default: false })), 'removed').id, 'global');
  assert.equal(primaryHubModel([models[0]], ''), undefined);
});
test('deployment derives from loopback endpoint rather than the configured protocol', () => {
  assert.equal(modelDeployment(models[2]), 'local');
  assert.equal(modelDeployment({ ...models[0], base_url: 'http://192.168.1.2:11434' }), 'api');
  assert.equal(modelDeployment({ ...models[2], base_url: 'bad url' }), 'unknown');
});
test('search and deployment filters compose without mutating original model order', () => {
  assert.deepEqual(filterHubModels(models, 'local', 'BONSAI', 'name').map(m => m.id), ['local /模型']);
  assert.deepEqual(filterHubModels(models, 'api', '', 'name').map(m => m.id), ['global']);
  assert.equal(models[0].id, 'disabled');
});
test('revision metadata must match Gateway ID, provider and configured model identity', () => {
  const revisions = [
    { gateway_model_id: 'global', provider: 'openai', model_name: 'remote-b', id: 'wrong-provider' },
    { gateway_model_id: 'global', provider: 'openai_compatible', model_name: 'old', id: 'wrong-name' },
    { gateway_model_id: 'global', provider: 'openai_compatible', model_name: 'remote-b', id: 'correct' },
  ];
  assert.equal(matchingRevision(models[1], revisions).id, 'correct');
  assert.equal(matchingRevision(models[1], [revisions[0]]), undefined);
  assert.equal(matchingRevision(models[2], revisions), undefined);
});
test('chat handoff encodes exact Gateway ID without changing backend defaults', () => {
  assert.equal(modelChatDestination(models[2]), '/chat?model=local%20%2F%E6%A8%A1%E5%9E%8B');
});
