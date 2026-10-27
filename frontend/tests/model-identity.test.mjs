import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

async function loadSource(path) {
  const source = await readFile(new URL(path, import.meta.url), 'utf8');
  const compiled = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
  }).outputText;
  return import(`data:text/javascript;base64,${Buffer.from(compiled).toString('base64')}`);
}

const { resolveModelIdentity } = await loadSource('../src/features/model-lab/model-identity.ts');
const { modelStatus } = await loadSource('../src/features/model-lab/model-status.ts');

const model = {
  id: 'gateway-a',
  name: 'My private model',
  provider: 'openai_compatible',
  model_name: 'bonsai',
  base_url: 'http://127.0.0.1:8080/v1',
  api_key: '',
  context_length: 8192,
  enabled: true,
};

test('explicit vendor outranks configured family, provider metadata and served model series', () => {
  const identity = resolveModelIdentity({
    ...model, vendor: ' Deep_Seek ', model_family: 'Qwen3', model_name: 'gpt-4.1',
    metadata: { vendor: 'Anthropic', family: 'Llama' },
  });
  assert.equal(identity.brand, 'deepseek');
  assert.equal(identity.vendor, 'Deep_Seek');
  assert.equal(identity.icon, '/model-brands/deepseek-color.svg');
});

test('configured model family takes precedence over conflicting discovered metadata', () => {
  const identity = resolveModelIdentity({
    ...model, model_family: 'Qwen3', model_name: 'claude-sonnet-4',
    metadata: { vendor: 'Meta', family: 'Llama' },
  });
  assert.equal(identity.brand, 'qwen');
  assert.equal(identity.vendor, 'Qwen');
  assert.equal(identity.family, 'Qwen3');
  assert.equal(identity.icon, '/model-brands/qwen-color.svg');
});

test('structured vendor identifies a custom served ID without trusting its display alias', () => {
  const identity = resolveModelIdentity({
    ...model, name: 'OpenAI GPT latest', metadata: { vendor: 'Alibaba', family: 'Qwen3' },
  });
  assert.equal(identity.brand, 'qwen');
  assert.equal(identity.vendor, 'Alibaba');
  assert.equal(identity.family, 'Qwen3');
  assert.equal(identity.provider, 'OpenAI-compatible');
});

test('supplied discovery metadata can identify a model independently of saved metadata', () => {
  const identity = resolveModelIdentity(
    { ...model, name: 'Qwen', metadata: { vendor: 'Meta' } },
    { manufacturer: 'Anthropic', family: 'Claude' },
  );
  assert.equal(identity.brand, 'anthropic');
  assert.equal(identity.vendor, 'Anthropic');
  assert.equal(identity.family, 'Claude');
});

test('known provider family is usable when a custom model ID has no manufacturer', () => {
  const identity = resolveModelIdentity({ ...model, metadata: { family: 'Llama3' } });
  assert.equal(identity.brand, 'meta');
  assert.equal(identity.family, 'Llama3');
  assert.equal(identity.icon, '/model-brands/meta-color.svg');
});

test('served model series recognizes known families including namespaced IDs', () => {
  for (const [model_name, brand, family] of [
    ['deepseek-r1:7b', 'deepseek', 'DeepSeek'],
    ['organization/Qwen3-8B', 'qwen', 'Qwen'],
    ['claude-sonnet-4', 'anthropic', 'Claude'],
    ['gemma3:4b', 'google', 'Gemma'],
    ['meta-llama/Llama-3.3-70B', 'meta', 'Llama'],
    ['mixtral-8x7b', 'mistral', 'Mistral'],
    ['gpt-4.1', 'openai', 'OpenAI'],
  ]) {
    const identity = resolveModelIdentity({ ...model, model_name });
    assert.equal(identity.brand, brand, model_name);
    assert.equal(identity.family, family, model_name);
  }
});

test('display aliases and provider protocols never brand an unknown served model', () => {
  for (const provider of ['openai', 'openai_compatible', 'deepseek', 'ollama']) {
    const identity = resolveModelIdentity({ ...model, provider, name: 'Qwen3 / DeepSeek / GPT-4' });
    assert.equal(identity.brand, 'unknown', provider);
    assert.equal(identity.vendor, '未知厂商', provider);
    assert.equal(identity.family, '未提供', provider);
    assert.equal(identity.icon, null, provider);
  }
});

test('unknown explicit manufacturer keeps its name and a neutral icon over a recognized series', () => {
  const identity = resolveModelIdentity({ ...model, vendor: 'Private Research Lab', model_name: 'qwen3:8b' });
  assert.equal(identity.vendor, 'Private Research Lab');
  assert.equal(identity.brand, 'unknown');
  assert.equal(identity.icon, null);
});

test('deployment follows explicit configuration then endpoint rather than protocol or brand', () => {
  for (const [changes, deployment] of [
    [{ provider: 'openai', base_url: 'http://localhost:8080/v1' }, 'local'],
    [{ provider: 'openai_compatible', base_url: 'http://[::1]:8080/v1' }, 'local'],
    [{ provider: 'ollama', base_url: 'https://models.example.test' }, 'api'],
    [{ base_url: 'http://192.168.1.20:11434' }, 'api'],
    [{ base_url: 'not a URL' }, 'unknown'],
    [{ deployment: 'api', base_url: 'http://localhost:8080/v1' }, 'api'],
    [{ deployment: 'local', base_url: 'https://models.example.test' }, 'local'],
  ]) {
    assert.equal(resolveModelIdentity({ ...model, ...changes }).deployment, deployment, JSON.stringify(changes));
  }
});

test('a successful connection check represents verification without claiming active inference', () => {
  assert.equal(modelStatus(model, '已验证 · 连接成功').state, 'verified');
  assert.equal(modelStatus(model, '已验证 · 连接成功', true).state, 'running');
  assert.equal(modelStatus(model).state, 'configured');
});

test('clearing a prior check removes success and failure status instead of retaining stale results', () => {
  assert.equal(modelStatus(model, '连接失败：服务不可用').state, 'failed');
  assert.equal(modelStatus(model, '正在测试连接…').state, 'checking');
  assert.equal(modelStatus(model, undefined).state, 'configured');
  assert.equal(modelStatus(model, '').state, 'configured');
});

test('disabled configuration takes precedence over cached verification and active-session status', () => {
  assert.equal(modelStatus({ ...model, enabled: false }, '已验证 · 连接成功', true).state, 'disabled');
});
