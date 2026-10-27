// Browser plugin not available. Deterministic UI contract fixtures only:
// this script does not claim real inference, native transport, or production evidence.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { createContext, errors } = require('./ui-regression.cjs');
const base = process.env.QA_URL || 'http://127.0.0.1:1429/';
const output = process.env.QA_OUTPUT || path.join(process.env.TEMP || process.cwd(), 'noosphere-t9-qa');
fs.mkdirSync(output, { recursive: true });

async function fixture(context) {
  await context.addInitScript(() => {
    const metric = { value: null, unit: 'bytes', source: 'unavailable', quality: 'unknown', missing_reason: 'fixture: GPU unavailable' };
    const environment = { id: 'fixture-env', os: 'fixture-windows', arch: 'amd64', cores: 8, vram: metric, collector_version: 'fixture-only' };
    const identities = ['stable', 'candidate'].map(name => ({ name: `fixture-${name}`, digest: `sha256:${name}`, engine_version: 'fixture', metadata_hash: `${name}-metadata`, config_hash: 'fixture', template_hash: 'fixture', format: 'gguf', family: 'fixture', parameter_size: '1B', quantization: 'fixture-Q4', size_bytes: 1e9, capabilities: ['completion'], local_verified: true }));
    const models = identities.map((m, i) => ({ id: `model-${i}`, gateway_model_id: 'ollama-fixture', provider: 'ollama', model_name: m.name, identity_level: 'native_verified', digest: m.digest, engine_version: m.engine_version, quantization: m.quantization }));
    const requested = { temperature: 0, seed: 7, num_ctx: 2048, max_output: 1024, deadline_ns: 90e9, parallelism: 1, retry_limit: 0, keep_alive: '0' };
    const profile = { id: 'fixture-profile', requested };
    const cases = [{ id: 'case-pass', category: 'boundary-string', expected: 'fixture expected A' }, { id: 'case-loss', category: 'boundary-string', expected: 'fixture expected B' }];
    const dataset = { id: 'fixture-dataset', benchmark_id: 'fixture-codetask', version: 'fixture-v1', content_hash: 'fixture-hash', scorer_version: 'fixture-scorer', order_seed: 7, cases };
    const counts = { planned: 4, attempted: 4, scored: 4, pass: 3, fail: 1, infra: 0, cancelled: 0, interrupted: 0, unknown: 0, paired_complete: 2 };
    const evaluation = { id: 'fixture-eval', manifest: { source: 'offline_fixture', workspace_id: 'local', stable: models[0], candidate: models[1], profile, dataset, environment, workflow_id: 'codelens.codetask.go.v1', tool_permissions: [], memory_mode: 'none' }, manifest_hash: 'fixture-manifest', status: 'running', revision: 2, run_id: 'fixture-run', lease_generation: 1, started_at: '2026-10-26T00:00:00Z', finished_at: '', cancel_requested: false };
    const attempts = cases.flatMap(c => ['stable', 'candidate'].map(arm => ({ id: `${c.id}-${arm}`, evaluation_id: evaluation.id, case_id: c.id, arm, attempt_no: 1, state: 'completed', verdict: c.id === 'case-loss' && arm === 'candidate' ? 'fail' : 'pass', result: { actual: 'fixture actual', expected: c.expected, difference: 'fixture difference', truncated: false, inference_completed: true, scorer_version: 'fixture-scorer' }, metrics: { wall_ns: { value: 0, unit: 'ns', source: 'fixture', quality: 'observed' }, load_ns: metric, prompt_ns: metric, decode_ns: metric, tokens: metric, cpu: metric, ram: metric, available_vram: metric, warm_verified: false }, started_at: '', finished_at: '' })));
    const proportion = (n, successes) => { const p = successes / n, z = 1.959963984540054, den = 1 + z * z / n, center = (p + z * z / (2 * n)) / den, half = z * Math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / den; return { status: n ? 'sufficient' : 'insufficient', successes, n, rate: n ? p : null, lower_95: n ? Math.max(0, center - half) : null, upper_95: n ? Math.min(1, center + half) : null }; };
    const analysis = { version: 'regression-intelligence-v1', source: 'offline_fixture', status: 'complete', arms: { stable: proportion(2, 2), candidate: proportion(2, 1) }, categories: { 'boundary-string': { stable: proportion(2, 2), candidate: proportion(2, 1), pair_matrix: [1, 1, 0, 0], paired_complete: 2, loss_rate: proportion(2, 1) } }, loss_rate: proportion(2, 1), performance: { status: 'insufficient', sample_count: 0, planned_pairs: 2, paired_median_ratio: { ...metric, unit: 'ratio' }, reasons: ['fixture: warm residency not verified; requires 12 independent cases'], warmup_calls: 0 }, diagnostics: [], metric_coverage: { stable: { available_vram_bytes: { planned: 2, samples: 0, missing: 2, unit: 'bytes', missing_reasons: { 'fixture GPU unavailable': 2 } } }, candidate: {} }, limitations: ['Fixture only; two independent cases do not establish general ability.'] };
    const report = { id: 'fixture-report', evaluation_id: evaluation.id, schema_version: 1, encoder_version: 'fixture', manifest_hash: 'fixture-manifest', evaluation_revision: 3, attempt_set_hash: 'fixture-attempt-set', selected: attempts, superseded: [], counts, categories: { 'boundary-string': { stable: { ...counts, planned: 2, scored: 2, pass: 2 }, candidate: { ...counts, planned: 2, scored: 2, pass: 1 } } }, pair_matrix: [1, 1, 0, 0], analysis };
    window.labQA = { mode: 'ok', identities, models: [], configs: [{ id: 'ollama-fixture', name: '测试 Ollama 连接', provider: 'ollama', base_url: 'http://fixture.invalid', model_name: 'fixture', enabled: true }], evaluation, hasEvaluation: false, completed: false, calls: [], profile, report, heldKey: '', casePhase: 0, holdNextRead: false, pendingRead: null, terminalSeen: false, failRunOnce: false, runFailures: 0 };
    const original = window.__TAURI_INTERNALS__.invoke;
    window.__TAURI_INTERNALS__.invoke = async (command, payload) => {
      const q = window.labQA;
      if (command === 'get_models') return { models: q.configs };
      if (command === 'get_workspace_run') { if (q.failRunOnce) { q.failRunOnce = false; q.runFailures++; throw { message: 'fixture transient run read' }; } if (q.completed) q.terminalSeen = true; return { id: 'fixture-run', workspace_id: 'local', kind: 'evaluation', status: q.completed ? 'completed' : q.evaluation.status, phase: q.evaluation.status, updated_at: '', created_at: '', error: '' }; }
      if (command !== 'model_engineering') return original(command, payload);
      const r = payload.request; q.calls.push(structuredClone(r));
      switch (r.operation) {
        case 'discover': if (q.mode === 'offline') throw { code: 'PROVIDER_UNAVAILABLE', status: 503, message: 'fixture provider offline' }; return { engine_version: 'fixture', models: identities };
        case 'register_model': { const m = models[identities.findIndex(x => x.metadata_hash === r.discovered_identity_hash)]; if (!q.models.some(x => x.id === m.id)) q.models.push(m); return m; }
        case 'list_models': return { models: q.models, next_cursor: '' };
        case 'list_profiles': return { profiles: [q.profile], next_cursor: '' };
        case 'create_profile': q.profile = { id: 'fixture-profile-new', requested: r.requested }; return q.profile;
        case 'list_datasets': return { datasets: [dataset] };
        case 'environment': return { environment, preflight: { available: q.mode !== 'executor-offline', code: q.mode === 'executor-offline' ? 'RUNNER_UNAVAILABLE' : undefined, budget: { max_context: 2048, max_output: 1024, inference_timeout_ns: 90e9, cleanup_timeout_ns: 10e9 } } };
        case 'create_evaluation':
          if (q.mode === 'codelens') throw { code: 'CODELENS_UNAVAILABLE', status: 503, message: 'fixture CodeLens unavailable' };
          if (q.mode === 'budget') throw { code: 'BUDGET_EXCEEDED', status: 409, message: 'fixture resource admission refused' };
          if (q.mode === 'lost-response' && !q.heldKey) { q.heldKey = r.idempotency_key; q.hasEvaluation = true; throw { code: 'DEPENDENCY_UNAVAILABLE', status: 503, message: 'fixture uncertain acknowledgement' }; }
          q.hasEvaluation = true; return { evaluation_id: evaluation.id, run_id: 'fixture-run', status: evaluation.status, revision: evaluation.revision };
        case 'list_evaluations': return { evaluations: q.hasEvaluation ? [evaluation] : [], next_cursor: '' };
        case 'get_evaluation': { const result = structuredClone({ evaluation: { ...evaluation, status: q.completed ? 'completed' : evaluation.status }, attempts, progress: q.completed ? counts : { ...counts, scored: 1, pass: 1, fail: 0 }, latest_report_id: q.completed ? report.id : '', next_cursor: '' }); if (q.holdNextRead) { q.holdNextRead = false; return new Promise(resolve => { q.pendingRead = () => { q.pendingRead = null; resolve(result); }; }); } return result; }
        case 'evaluation_command':
          if (q.mode === 'stale') throw { code: 'REVISION_CONFLICT', status: 409, message: 'fixture revision conflict', details: { current_revision: 7 } };
          evaluation.status = r.command === 'cancel' ? 'cancelled' : 'running'; evaluation.revision++; return { evaluation_id: evaluation.id, run_id: 'fixture-run', status: evaluation.status, revision: evaluation.revision };
        case 'get_report': return { report: q.mode === 'legacy' ? { ...report, analysis: undefined } : report, report_hash: 'fixture-report-hash', promotion_eligibility: { eligible: false, reasons: ['fixture: insufficient independent samples'] } };
        case 'get_case': return { case: cases.find(c => c.id === r.case_id), attempts: attempts.filter(a => a.case_id === r.case_id).map(a => ({ ...a, result: { ...a.result, actual: q.casePhase ? 'fixture refreshed live output' : a.result.actual } })), artifacts: [{ id: 'fixture-input', content: 'fixture frozen context / same input', missing: false, truncated: false, redacted: true }], next_cursor: '' };
        default: throw Error(`unexpected fixture operation ${r.operation}`);
      }
    };
  });
}

(async () => {
  const browser = await chromium.launch({ channel: process.env.QA_BROWSER || 'msedge', headless: true });
  const passed = [];
  async function check(name, fn) { await fn(); passed.push(name); console.log('PASS ' + name); }
  try {
    const context = await createContext(browser, { width: 1440, height: 960 }, true, 'reduce'); await fixture(context);
    const page = await context.newPage(); const warnings = [];
    page.on('console', message => { if (message.type() === 'warning') warnings.push(message.text()); });
    await check('Hub empty, offline Provider, discovery and exact revision registration', async () => {
      await page.goto(base + '#/model-hub'); await page.getByRole('heading', { name: 'Model Hub', exact: true }).waitFor();
      assert.match(page.url(), /#\/model-hub$/); assert.match(await page.title(), /Noosphere/i);
      await page.getByRole('button', { name: '查看主模型详情' }).click();
      await page.getByText('已注册的不可变版本（0）', {exact:true}).click();
      await page.getByText(/尚无模型版本档案/).waitFor();
      await page.evaluate(() => { window.labQA.mode = 'offline'; }); await page.getByRole('button', { name: '发现模型版本' }).click();
      await page.getByRole('alert').getByText('PROVIDER_UNAVAILABLE').waitFor();
      await page.evaluate(() => { window.labQA.mode = 'ok'; }); await page.getByRole('button', { name: '发现模型版本' }).click();
      await page.getByRole('button', { name: '注册此版本' }).first().click(); await page.getByRole('button', { name: '注册此版本' }).last().click();
      assert.equal(await page.evaluate(() => window.labQA.models.length), 2);
      await page.keyboard.press('Escape');
    });
    await check('Lab uses server budget and shows CodeLens and admission errors with exact-key retry', async () => {
      await page.goto(base + '#/model-lab'); await page.getByText(/服务端预算：上下文 ≤2048/).waitFor();
      await page.getByRole('button', { name: '开始真实离线测评' }).waitFor();
      await page.evaluate(() => { window.labQA.mode = 'codelens'; }); await page.getByRole('button', { name: '开始真实离线测评' }).click();
      await page.getByRole('alert').getByText('CODELENS_UNAVAILABLE', { exact: true }).waitFor();
      await page.evaluate(() => { window.labQA.mode = 'budget'; }); await page.getByRole('button', { name: '重试同一请求' }).click();
      await page.getByRole('alert').getByText('BUDGET_EXCEEDED', { exact: true }).waitFor();
      await page.evaluate(() => { window.labQA.mode = 'ok'; }); await page.getByRole('button', { name: '重试同一请求' }).click();
      await page.getByRole('heading', { name: '测评进度' }).waitFor();
      const keys = await page.evaluate(() => window.labQA.calls.filter(r => r.operation === 'create_evaluation').map(r => r.idempotency_key)); assert.equal(new Set(keys).size, 1);
      await page.screenshot({ path: path.join(output, 'lab-desktop-fixture.png') });
    });
    await check('Route changes preserve durable work, reopen progress, cancel and CAS refusal are explicit', async () => {
      await page.getByRole('link', { name: 'Model Hub', exact: true }).click();
      assert.equal(await page.evaluate(() => window.labQA.calls.filter(r => r.command === 'cancel').length), 0);
      await page.getByRole('link', { name: 'Model Lab', exact: true }).click();
      await page.getByRole('button', { name: '重开测评', exact: true }).click(); await page.getByRole('button', { name: '取消测评', exact: true }).click();
      await page.getByText('已取消', { exact: true }).first().waitFor();
      await page.evaluate(() => { window.labQA.mode = 'stale'; }); await page.getByRole('button', { name: '校验原身份后继续' }).click();
      await page.getByText(/当前服务端版本：7/).waitFor();
      await page.evaluate(() => { window.labQA.mode = 'ok'; window.labQA.evaluation.status = 'running'; });
      await page.getByRole('button', { name: '刷新任务', exact: true }).click(); await page.getByRole('button', { name: '取消测评', exact: true }).waitFor();
    });
    await check('Opened running case refreshes on persisted revision changes without switching case', async () => {
      await page.getByRole('button', { name: '查看 case-loss', exact: true }).first().click();
      const panel = page.getByRole('region', { name: '任务证据 case-loss' }); await panel.getByText('实际输出、预期与差异', { exact: true }).first().waitFor();
      const before = await page.evaluate(() => window.labQA.calls.filter(r => r.operation === 'get_case').length);
      await page.evaluate(() => { window.labQA.evaluation.revision++; window.labQA.casePhase = 1; });
      await page.waitForFunction(n => window.labQA.calls.filter(r => r.operation === 'get_case').length > n, before);
      await panel.getByText('实际输出、预期与差异', { exact: true }).first().click();
      await panel.locator('details[open] pre').filter({ hasText: 'fixture refreshed live output' }).first().waitFor();
    });
    await check('Terminal run drains stale running read, exact case retry and transient run errors retain observation', async () => {
      await page.evaluate(() => { window.labQA.holdNextRead = true; });
      await page.waitForFunction(() => !!window.labQA.pendingRead);
      await page.evaluate(() => { window.labQA.completed = true; });
      await page.waitForFunction(() => window.labQA.terminalSeen);
      await page.evaluate(() => { window.labQA.pendingRead(); });
      await page.getByRole('link', { name: /查看持久化比较报告/ }).waitFor();
      await page.getByRole('button', { name: '重试 Candidate 此例', exact: true }).click();
      const retry = await page.evaluate(() => window.labQA.calls.find(r => r.command === 'retry_case'));
      assert.equal(retry.arm, 'candidate'); assert.equal(retry.case_id, 'case-loss'); assert.equal(retry.expected_revision, 4); assert.ok(retry.idempotency_key);
      await page.evaluate(() => { window.labQA.completed = false; window.labQA.evaluation.status = 'running'; window.labQA.failRunOnce = true; });
      await page.getByRole('button', { name: '刷新任务', exact: true }).click();
      await page.waitForFunction(() => window.labQA.runFailures === 1);
      await page.waitForTimeout(40); // let the failed WorkspaceRun observer and its immediate reads settle
      await page.evaluate(() => { window.labQA.completed = true; });
      await page.getByRole('link', { name: /查看持久化比较报告/ }).waitFor();
      await page.getByRole('link', { name: /查看持久化比较报告/ }).click();
    });
    await check('Persisted report displays categories, paired uncertainty, missing metrics and inspectable failures', async () => {
      await page.getByRole('heading', { name: 'Evaluation Reports', exact: true }).waitFor();
      await page.getByText(/测试夹具 · 非真实模型验证/).first().waitFor();
      await page.getByRole('columnheader', { name: '成对样本 / 退化率', exact: true }).waitFor();
      await page.getByText('1 / 1 / 0 / 0', { exact: true }).first().waitFor();
      await page.getByText(/Wilson 95%/).first().waitFor();
      await page.locator('.ns-model-lab').evaluate(e => { e.parentElement.scrollTop = 0; });
      await page.screenshot({ path: path.join(output, 'report-desktop-fixture.png') });
      await page.getByRole('button', { name: '查看 case-loss', exact: true }).first().click();
      await page.getByRole('region', { name: '任务证据 case-loss' }).waitFor();
      const panel = page.getByRole('region', { name: '任务证据 case-loss' });
      await panel.getByText('实际输出、预期与差异', { exact: true }).first().click(); await panel.locator('details[open] pre').filter({ hasText: 'fixture difference' }).first().waitFor();
      await page.screenshot({ path: path.join(output, 'failure-case-fixture.png') });
      await page.reload(); await page.getByRole('heading', { name: '比较报告', exact: true }).waitFor();
      await page.evaluate(() => { window.labQA.mode = 'legacy'; }); await page.reload(); // init resets fixture; switch after reload below
      await page.evaluate(() => { window.labQA.mode = 'legacy'; }); await page.goto(base + '#/evaluation-reports/legacy-report');
      await page.getByText(/此报告缺少受支持/).waitFor();
    });
    await check('Unsupported OpenAI connection is honest, narrow layout and keyboard discovery are usable', async () => {
      await page.evaluate(() => { window.labQA.configs = [{ id: 'bonsai-fixture', name: 'fixture llama.cpp', provider: 'openai_compatible', base_url: 'http://fixture.invalid', model_name: 'fixture-Bonsai' }]; });
      await page.setViewportSize({ width: 390, height: 844 }); await page.goto(base + '#/model-hub');
      await page.getByRole('button', { name: '查看主模型详情' }).click();
      await page.getByText(/尚无精确身份与驻留验证适配/).waitFor();
      assert.equal(await page.getByRole('button', { name: '发现模型版本' }).count(), 0);
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      await page.screenshot({ path: path.join(output, 'hub-mobile-unsupported-fixture.png') });
      await page.getByRole('combobox', { name: 'Gateway 连接' }).focus(); assert.equal(await page.getByRole('combobox', { name: 'Gateway 连接' }).evaluate(e => e === document.activeElement), true);
      await page.keyboard.press('Escape');
      await page.setViewportSize({ width: 960, height: 900 }); await page.goto(base + '#/model-lab');
      await page.getByRole('heading', { name: 'Model Lab', exact: true }).waitFor(); await page.screenshot({ path: path.join(output, 'lab-960-fixture.png') });
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    });
    await check('Existing Gateway, chat and settings remain reachable without page errors', async () => {
      for (const [route, title] of [['gateway', 'Gateway'], ['chat', ''], ['settings', '设置']]) {
        await page.goto(base + '#/' + route);
        if (route === 'chat') await page.getByRole('textbox', { name: '输入问题', exact: true }).waitFor();
        else await page.getByRole('heading', { name: title, exact: true }).waitFor();
      }
      assert.equal(await page.locator('vite-error-overlay').count(), 0);
    });
    assert.deepEqual(errors, []);
    fs.writeFileSync(path.join(output, 'model-lab-ui.json'), JSON.stringify({ source: 'synthetic_tauri_ipc_fixture', browser: 'msedge', browser_plugin: 'not_available', passed, errors, warnings, screenshots: fs.readdirSync(output).filter(n => n.endsWith('.png')) }, null, 2));
    console.log(`UI ${passed.length}/${passed.length} checks passed; fixture-only evidence: ${output}`);
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
