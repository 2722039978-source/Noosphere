import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';
import { fileURLToPath } from 'node:url';

const identityEventsSource = await readFile(new URL('../src/lib/model-identity-events.ts', import.meta.url), 'utf8');
const identityEventsCompiled = ts.transpileModule(identityEventsSource, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } }).outputText;
const identityEventsURL = `data:text/javascript;base64,${Buffer.from(identityEventsCompiled).toString('base64')}`;
const source = await readFile(new URL('../src/lib/model-engineering-client.ts', import.meta.url), 'utf8');
const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } }).outputText
  .replace(/^import .*? from ['"]@tauri-apps\/api\/core['"];?$/m, 'const invoke = globalThis.__modelEngineeringInvoke;')
  .replace(/^import .*? from ['"]\.\/api['"];?$/m, 'const api = globalThis.__modelEngineeringApi;')
  .replace(/^import .*? from ['"]\.\/task-client['"];?$/m, 'const waitForWorkspaceRun = globalThis.__modelEngineeringObserver;')
  .replace(/from ['"]\.\/model-identity-events['"]/g, `from ${JSON.stringify(identityEventsURL)}`);
const calls = [];
globalThis.__modelEngineeringInvoke = async (command, args) => { calls.push({ command, args }); return { release_id: 'release-a', run_id: 'run-a', status: 'queued' }; };
globalThis.__modelEngineeringApi = { getWorkspaceRun: async () => ({ id: 'run-a', status: 'completed' }) };
globalThis.__modelEngineeringObserver = async (id, getRun) => getRun(id);
const { modelEngineering, waitForModelEngineeringRun, asModelEngineeringError } = await import(`data:text/javascript;base64,${Buffer.from(compiled).toString('base64')}`);

test('simulation submits exact caller key and revision on every explicit retry', async () => {
  const request = { operation: 'simulate_release', workspace_id: 'ws', id: 'release-a', scenario: 'replay_stage', expected_revision: 4, idempotency_key: 'same-key' };
  const first = await modelEngineering(request);
  const second = await modelEngineering(request);
  assert.deepEqual(first, { release_id: 'release-a', run_id: 'run-a', status: 'queued' });
  assert.deepEqual(second, first);
  assert.deepEqual(calls, [
    { command: 'model_engineering', args: { request } },
    { command: 'model_engineering', args: { request } },
  ]);
});

test('structured backend gate error remains accessible to UI', () => {
  const error = asModelEngineeringError({ message: 'gates', status: 409, code: 'GATE_DENIED', details: { current_revision: 8, gates: [{ status: 'insufficient' }] } });
  assert.equal(error.status, 409);
  assert.equal(error.code, 'GATE_DENIED');
  assert.equal(error.details.current_revision, 8);
  assert.equal(error.details.gates[0].status, 'insufficient');
  assert.equal(asModelEngineeringError({ message: 'old' }).message, 'old');
});

test('model engineering observer delegates to existing durable task observer', async () => {
  assert.deepEqual(await waitForModelEngineeringRun('run-a'), { id: 'run-a', status: 'completed' });
});

test('incomplete is an evaluation status but never a WorkspaceRun status', () => {
  const virtualPath = fileURLToPath(new URL('__model_engineering_type_contract.ts', import.meta.url));
  const contract = `import type { EvaluationStatus, RunStatus } from '../src/lib/model-engineering-types';
const evaluation: EvaluationStatus = 'incomplete';
// @ts-expect-error Evaluation incomplete is not a WorkspaceRun lifecycle state.
const run: RunStatus = evaluation;
void run;`;
  const options = { strict: true, noEmit: true, skipLibCheck: true, target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext, moduleResolution: ts.ModuleResolutionKind.Bundler };
  const host = ts.createCompilerHost(options);
  const sourceFile = ts.createSourceFile(virtualPath, contract, options.target);
  const sameVirtualPath = path => path.replaceAll('\\', '/') === virtualPath.replaceAll('\\', '/');
  const originalGetSourceFile = host.getSourceFile.bind(host);
  const originalFileExists = host.fileExists.bind(host);
  const originalReadFile = host.readFile.bind(host);
  host.getSourceFile = (path, ...rest) => sameVirtualPath(path) ? sourceFile : originalGetSourceFile(path, ...rest);
  host.fileExists = path => sameVirtualPath(path) || originalFileExists(path);
  host.readFile = path => sameVirtualPath(path) ? contract : originalReadFile(path);
  const diagnostics = ts.getPreEmitDiagnostics(ts.createProgram([virtualPath], options, host));
  assert.deepEqual(diagnostics.map(diagnostic => ts.flattenDiagnosticMessageText(diagnostic.messageText, '\n')), []);
});
