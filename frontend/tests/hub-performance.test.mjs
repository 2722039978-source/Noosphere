import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

const compile = source => ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
}).outputText;
const moduleURL = compiled => `data:text/javascript;base64,${Buffer.from(compiled).toString('base64')}`;
const presentationURL = moduleURL(compile(await readFile(new URL('../src/features/model-lab/presentation.ts', import.meta.url), 'utf8')));
const source = await readFile(new URL('../src/features/model-lab/hub-performance.ts', import.meta.url), 'utf8');
const compiled = compile(source)
  .replace(/^import .*? from ['"]@\/lib\/model-engineering-client['"];?$/m,
    'const modelEngineering = request => globalThis.__hubPerformanceClient(request);')
  .replace(/from ['"]\.\/presentation['"]/g, `from ${JSON.stringify(presentationURL)}`);
const { readHubPerformance, loadHubPerformance, hubGenerationText } = await import(moduleURL(compiled));

const revision = {
  id: 'revision-bonsai', gateway_model_id: 'gateway-local', provider: 'ollama', model_name: 'acme/bonsai:8b',
  identity_level: 'digest', digest: `sha256:${'a'.repeat(64)}`, deployment_version: 'deployment-1',
  model_hash: 'a'.repeat(64), config_hash: 'b'.repeat(64), metadata_hash: 'c'.repeat(64),
  template_hash: 'd'.repeat(64), endpoint_fingerprint: 'e'.repeat(64), engine_version: '0.12.1',
  format: 'gguf', quantization: 'Q5_K_M', parameter_size: '8.2B', capabilities: ['completion'],
};
const metric = (value, unit, source = 'ollama_native', quality = 'reported') => ({ value, unit, source, quality });
const unavailable = unit => ({ value: null, unit, source: 'unavailable', quality: 'unknown', missing_reason: 'not collected' });
const proportion = { status: 'sufficient', successes: 2, n: 2, rate: 1, lower_95: 0.342, upper_95: 1 };
const loss = { status: 'sufficient', successes: 0, n: 2, rate: 0, lower_95: 0, upper_95: 0.658 };
const counts = { planned: 4, attempted: 4, scored: 4, pass: 4, fail: 0, infra: 0, cancelled: 0, interrupted: 0, unknown: 0, paired_complete: 2 };

function diagnostic(caseID, arm, attemptID, speed) {
  return {
    case_id: caseID, arm, attempt_id: attemptID, loading_protocol: 'warm-residency-v1', warm_verified: true,
    formal_wall_ns: metric(3e9, 'ns', 'host', 'observed'), formal_engine_total_ns: metric(2.8e9, 'ns'),
    formal_load_ns: metric(0.1e9, 'ns'), prompt_ns: metric(0.4e9, 'ns'), decode_ns: metric(2e9, 'ns'),
    tokens: metric(speed * 2, 'tokens'), tokens_per_second: metric(speed, 'tokens/s', 'ollama_native_eval_count/eval_duration_ns', 'derived'),
    warmup_cost_ns: metric(0.8e9, 'ns', 'host', 'observed'), warmup_engine_total_ns: metric(0.7e9, 'ns'),
    warmup_load_ns: metric(0.2e9, 'ns'), warmup_artifact: `warmup-${attemptID}`,
    compiler_ns: unavailable('ns'), wasi_preparation_ns: unavailable('ns'), wasi_execution_ns: unavailable('ns'),
    scorer_total_ns: unavailable('ns'), cpu_percent: unavailable('percent'), ram_bytes: unavailable('bytes'), available_vram_bytes: unavailable('bytes'),
  };
}

// These are synthetic test values in the actual backend v1 report schema. The
// offline_real label exercises provenance handling; it is not measured speed.
function fixture({ id = 'evaluation-current', workspace = 'workspace-a', finished = '2026-10-27T12:00:00Z' } = {}) {
  const manifestHash = `manifest-${id}`;
  const attemptHash = `selected-${id}`;
  const selected = [
    { case_id: 'zeta', arm: 'stable', id: `${id}-stable-z`, attempt_no: 1, state: 'completed', verdict: 'pass', result_hash: 'result-z', attempt_hash: 'attempt-z' },
    { case_id: 'alpha', arm: 'stable', id: `${id}-stable-a`, attempt_no: 2, state: 'completed', verdict: 'pass', result_hash: 'result-a', attempt_hash: 'attempt-a' },
    { case_id: 'zeta', arm: 'candidate', id: `${id}-candidate-z`, attempt_no: 1, state: 'completed', verdict: 'pass', result_hash: 'candidate-result-z', attempt_hash: 'candidate-attempt-z' },
    { case_id: 'alpha', arm: 'candidate', id: `${id}-candidate-a`, attempt_no: 1, state: 'completed', verdict: 'pass', result_hash: 'candidate-result-a', attempt_hash: 'candidate-attempt-a' },
  ];
  const environment = { id: 'machine-snapshot', os: 'windows', arch: 'amd64', vram: unavailable('bytes'), collector_version: 'host-v1' };
  const evaluation = {
    id, manifest_hash: manifestHash, status: 'completed', revision: 8, lease_generation: 1,
    run_id: `run-${id}`, cancel_requested: false, started_at: '2026-10-27T11:00:00Z', finished_at: finished,
    manifest: {
      schema_version: 1, encoder_version: 'canonical-json-v1', workspace_id: workspace,
      workflow_id: 'codelens.codetask.go.v1', source: 'offline_real', stable: structuredClone(revision),
      candidate: { ...revision, id: 'revision-candidate', model_name: 'acme/bonsai:14b', digest: `sha256:${'f'.repeat(64)}` },
      profile: { id: 'profile', requested: { temperature: 0, seed: 7, num_ctx: 4096, max_output: 1024, parallelism: 1, retry_limit: 0, keep_alive: '0' } },
      dataset: { id: 'dataset', benchmark_id: 'go', version: 'v1', content_hash: 'dataset-hash', scorer_version: 'host-v1', order_seed: 7,
        cases: [{ id: 'alpha', category: 'logic', expected: '42' }, { id: 'zeta', category: 'logic', expected: '7' }] },
      environment, memory_mode: 'not_applicable',
    },
  };
  const analysis = {
    version: 'regression-intelligence-v1', source: 'offline_real', status: 'complete', manifest_hash: manifestHash,
    attempt_set_hash: attemptHash, pair_matrix_labels: ['stable_pass_candidate_pass', 'stable_pass_candidate_fail', 'stable_fail_candidate_pass', 'stable_fail_candidate_fail'],
    arms: { stable: { ...proportion }, candidate: { ...proportion } }, completion: { scored: 4, planned: 4, rate: 1 },
    categories: { logic: { stable: { ...proportion }, candidate: { ...proportion }, pair_matrix: [2, 0, 0, 0], paired_complete: 2, loss_rate: { ...loss } } },
    loss_rate: { ...loss }, failures: [], environment, limitations: ['Two synthetic cases do not establish whole-model capability.'],
    performance: { status: 'insufficient', required_pairs: 12, sample_count: 2, planned_pairs: 2, paired_median_ratio: unavailable('ratio'),
      cases: [], reasons: ['requires 12 comparable independent pairs'], warmup_calls: 4, candidate_oom: false },
    diagnostics: [diagnostic('zeta', 'stable', `${id}-stable-z`, 21.5), diagnostic('alpha', 'stable', `${id}-stable-a`, 42.5),
      diagnostic('zeta', 'candidate', `${id}-candidate-z`, 100), diagnostic('alpha', 'candidate', `${id}-candidate-a`, 200)],
    metric_coverage: {
      stable: { tokens_per_second: { planned: 2, samples: 2, missing: 0, unit: 'tokens/s', missing_reasons: {} } },
      candidate: { tokens_per_second: { planned: 2, samples: 2, missing: 0, unit: 'tokens/s', missing_reasons: {} } },
    },
  };
  return { evaluation, response: { report: { id: `report-${id}`, evaluation_id: id, schema_version: 1, encoder_version: 'canonical-json-v1',
    manifest_hash: manifestHash, evaluation_revision: 7, attempt_set_hash: attemptHash, selected, superseded: [], counts: { ...counts },
    categories: { logic: { stable: { ...counts, planned: 2, attempted: 2, scored: 2, pass: 2 }, candidate: { ...counts, planned: 2, attempted: 2, scored: 2, pass: 2 } } },
    pair_matrix: [2, 0, 0, 0], analysis }, report_hash: `immutable-${id}`, promotion_eligibility: { eligible: true, reasons: [] } } };
}

test('real-schema completed report exposes one exact selected sample for the requested frozen revision', () => {
  const { evaluation, response } = fixture();
  const result = readHubPerformance(evaluation, response, revision);
  assert.ok(result);
  assert.deepEqual(result.generation, { value: 42.5, unit: 'tokens/s', source: 'ollama_native_eval_count/eval_duration_ns', quality: 'derived', caseID: 'alpha', attemptID: 'evaluation-current-stable-a' });
  assert.equal(result.generationSamples, 2);
  assert.equal(result.reportID, 'report-evaluation-current');
  assert.equal(result.evaluationID, 'evaluation-current');
  assert.equal(result.finishedAt, '2026-10-27T12:00:00Z');
  assert.equal(hubGenerationText(result), '42.5 tok/s');
  const candidate = readHubPerformance(evaluation, response, evaluation.manifest.candidate);
  assert.equal(candidate.generation.value, 200);
  assert.equal(candidate.generation.attemptID, 'evaluation-current-candidate-a');
});

test('every frozen identity field prevents unrelated registered revisions from borrowing performance', async t => {
  for (const field of ['id', 'gateway_model_id', 'provider', 'model_name', 'identity_level', 'digest', 'deployment_version',
    'model_hash', 'config_hash', 'metadata_hash', 'template_hash', 'endpoint_fingerprint', 'engine_version']) {
    await t.test(field, () => {
      const { evaluation, response } = fixture();
      assert.equal(readHubPerformance(evaluation, response, { ...revision, [field]: `wrong-${field}` }), null);
    });
  }
});

test('incomplete, synthetic, ineligible and hash-mismatched reports remain unavailable', async t => {
  for (const [name, mutate] of [
    ['evaluation still running', f => { f.evaluation.status = 'running'; }],
    ['fixture evaluation', f => { f.evaluation.manifest.source = 'offline_fixture'; }],
    ['fixture analysis', f => { f.response.report.analysis.source = 'offline_fixture'; }],
    ['incomplete analysis', f => { f.response.report.analysis.status = 'incomplete'; }],
    ['server revoked eligibility', f => { f.response.promotion_eligibility.eligible = false; }],
    ['server eligibility reason', f => { f.response.promotion_eligibility.reasons = ['IDENTITY_CHANGED']; }],
    ['missing report hash', f => { f.response.report_hash = ''; }],
    ['wrong evaluation', f => { f.response.report.evaluation_id = 'another-evaluation'; }],
    ['wrong manifest hash', f => { f.response.report.manifest_hash = 'another-manifest'; }],
    ['wrong analysis manifest hash', f => { f.response.report.analysis.manifest_hash = 'another-manifest'; }],
    ['missing attempt set hash', f => { f.response.report.attempt_set_hash = ''; }],
    ['wrong analysis attempt set', f => { f.response.report.analysis.attempt_set_hash = 'another-attempt-set'; }],
    ['nothing planned', f => { f.response.report.counts.planned = 0; }],
    ['partially scored report', f => { f.response.report.counts.scored = 3; }],
    ['unsupported analysis version', f => { f.response.report.analysis.version = 'future-v2'; }],
    ['malformed real v1 analysis', f => { delete f.response.report.analysis.arms.candidate; }],
  ]) {
    await t.test(name, () => {
      const f = fixture(); mutate(f);
      assert.equal(readHubPerformance(f.evaluation, f.response, revision), null);
      assert.equal(hubGenerationText(null), '暂无测评数据');
    });
  }
});

test('only a selected completed attempt with native derived generation provenance supplies a sample', async t => {
  for (const [name, mutate] of [
    ['superseded attempt', (d) => { d.attempt_id = 'superseded-success'; }],
    ['wrong arm', d => { d.arm = 'candidate'; }],
    ['wrong case', d => { d.case_id = 'another-case'; }],
    ['selected attempt failed', (_, f) => { f.response.report.selected[1].state = 'failed'; }],
    ['wrong metric source', d => { d.tokens_per_second.source = 'host_estimate'; }],
    ['wrong metric quality', d => { d.tokens_per_second.quality = 'reported'; }],
    ['wrong metric unit', d => { d.tokens_per_second.unit = 'tokens/ms'; }],
    ['unknown metric', d => { d.tokens_per_second.value = null; }],
    ['negative speed', d => { d.tokens_per_second.value = -1; }],
    ['NaN speed', d => { d.tokens_per_second.value = NaN; }],
    ['infinite speed', d => { d.tokens_per_second.value = Infinity; }],
  ]) {
    await t.test(name, () => {
      const f = fixture();
      const diagnostic = f.response.report.analysis.diagnostics[1];
      f.response.report.analysis.diagnostics = [diagnostic];
      mutate(diagnostic, f);
      assert.equal(readHubPerformance(f.evaluation, f.response, revision), null);
    });
  }
});

test('duplicate diagnostics do not inflate samples and timing fields never fabricate TTFT', () => {
  const f = fixture();
  f.response.report.analysis.diagnostics.push({ ...f.response.report.analysis.diagnostics[1], tokens_per_second: metric(999, 'tokens/s', 'ollama_native_eval_count/eval_duration_ns', 'derived') });
  f.response.report.analysis.diagnostics[1].ttft_ns = metric(123, 'ns');
  f.response.report.analysis.performance.ttft = metric(456, 'ns');
  const result = readHubPerformance(f.evaluation, f.response, revision);
  assert.equal(result.generationSamples, 2);
  assert.equal(result.generation.value, 42.5);
  assert.equal(result.ttft, null);
  assert.match(result.ttftMissingReason, /未采集/);
});

test('history lookup reads every page before choosing the newest exact revision in the requested workspace', async () => {
  const old = fixture({ id: 'z-old', finished: '2026-10-25T12:00:00Z' });
  const newest = fixture({ id: 'a-newest', finished: '2026-10-27T12:00:00Z' });
  const middle = fixture({ id: 'm-middle', finished: '2026-10-26T12:00:00Z' });
  const foreign = fixture({ id: 'foreign', workspace: 'workspace-b', finished: '2026-10-30T12:00:00Z' });
  const wrongProvider = fixture({ id: 'wrong-provider', finished: '2026-10-29T12:00:00Z' });
  wrongProvider.evaluation.manifest.stable.provider = 'openai_compatible';
  const calls = [];
  globalThis.__hubPerformanceClient = async request => {
    calls.push(request);
    if (request.operation === 'list_evaluations') {
      const pages = {
        '': { evaluations: [old.evaluation, foreign.evaluation, wrongProvider.evaluation], next_cursor: 'page-2' },
        'page-2': { evaluations: [newest.evaluation], next_cursor: 'page-3' },
        'page-3': { evaluations: [middle.evaluation], next_cursor: '' },
      };
      assert.ok(Object.hasOwn(pages, request.cursor), 'unexpected history cursor');
      return pages[request.cursor];
    }
    if (request.operation === 'get_evaluation' && request.id === 'a-newest') return { evaluation: newest.evaluation, latest_report_id: 'report-a-newest', attempts: [], progress: { ...counts }, next_cursor: '' };
    if (request.operation === 'get_report' && request.id === 'report-a-newest') return newest.response;
    assert.fail(`lookup attempted unexpected operation: ${JSON.stringify(request)}`);
  };
  const result = await loadHubPerformance('workspace-a', revision);
  assert.equal(result.evaluationID, 'a-newest');
  assert.equal(result.generation.value, 42.5);
  assert.deepEqual(calls, [
    { operation: 'list_evaluations', workspace_id: 'workspace-a', limit: 100, cursor: '' },
    { operation: 'list_evaluations', workspace_id: 'workspace-a', limit: 100, cursor: 'page-2' },
    { operation: 'list_evaluations', workspace_id: 'workspace-a', limit: 100, cursor: 'page-3' },
    { operation: 'get_evaluation', workspace_id: 'workspace-a', id: 'a-newest', limit: 1 },
    { operation: 'get_report', workspace_id: 'workspace-a', id: 'report-a-newest' },
  ]);
});

test('history lookup skips reports that are no longer eligible without launching replacement inference', async () => {
  const old = fixture({ id: 'old-valid', finished: '2026-10-25T12:00:00Z' });
  const revoked = fixture({ id: 'new-revoked', finished: '2026-10-26T12:00:00Z' });
  const resumed = fixture({ id: 'newest-resumed', finished: '2026-10-27T12:00:00Z' });
  revoked.response.promotion_eligibility = { eligible: false, reasons: ['ATTEMPT_SET_CHANGED'] };
  const entries = new Map([old, revoked, resumed].map(f => [f.evaluation.id, f]));
  const calls = [];
  globalThis.__hubPerformanceClient = async request => {
    calls.push(request);
    if (request.operation === 'list_evaluations') return { evaluations: [old.evaluation, revoked.evaluation, resumed.evaluation], next_cursor: '' };
    if (request.operation === 'get_evaluation') {
      const f = entries.get(request.id); assert.ok(f);
      return { evaluation: { ...f.evaluation, status: request.id === 'newest-resumed' ? 'running' : 'completed' }, latest_report_id: f.response.report.id };
    }
    if (request.operation === 'get_report') {
      const f = [...entries.values()].find(f => f.response.report.id === request.id); assert.ok(f);
      return f.response;
    }
    assert.fail(`lookup started a write or inference: ${request.operation}`);
  };
  assert.equal((await loadHubPerformance('workspace-a', revision)).evaluationID, 'old-valid');
  assert.deepEqual(calls.map(c => `${c.operation}:${c.id ?? ''}`), [
    'list_evaluations:', 'get_evaluation:newest-resumed', 'get_evaluation:new-revoked', 'get_report:report-new-revoked',
    'get_evaluation:old-valid', 'get_report:report-old-valid',
  ]);
});

test('empty selection, cancelled lookup and repeated pagination stop with bounded read-only work', async () => {
  const calls = [];
  globalThis.__hubPerformanceClient = async request => { calls.push(request); return { evaluations: [], next_cursor: 'same-cursor' }; };
  assert.equal(await loadHubPerformance('', revision), null);
  assert.equal(await loadHubPerformance('workspace-a', { ...revision, id: '' }), null);
  const abort = new AbortController(); abort.abort();
  await assert.rejects(loadHubPerformance('workspace-a', revision, abort.signal), { name: 'AbortError' });
  assert.equal(calls.length, 0);
  await assert.rejects(loadHubPerformance('workspace-a', revision), /游标重复/);
  assert.equal(calls.length, 2);
  assert.ok(calls.every(c => c.operation === 'list_evaluations'));
});
