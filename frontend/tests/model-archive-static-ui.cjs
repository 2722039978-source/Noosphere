// Browser plugin not available; use the existing isolated headless Playwright
// harness. Synthetic IPC only: timing here is browser UI evidence, not native FPS,
// real inference, backend transport, or a production performance benchmark.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const { createContext, errors } = require('./ui-regression.cjs');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const base = process.env.QA_URL || 'http://127.0.0.1:1429/';
const output = process.env.QA_OUTPUT || path.join(os.tmpdir(), 'noosphere-art-qa-20261027');
fs.mkdirSync(output, { recursive: true });
const sceneResource = /ArchiveCanvas|archive-cassette|GLTFLoader|\/three(?:[/.?_-]|$)|\/home\/scene\//i;
const passed = [], measurements = [], warnings = [];
const round = value => Math.round(value * 100) / 100;

async function fixture(context, { graphics = 'auto', dark = false } = {}) {
  await context.addInitScript(({ graphics, dark }) => {
    if (!/^https?:$/.test(location.protocol)) return;
    localStorage.setItem('noosphere-experience-v1', JSON.stringify({ graphics, boot: false, startPage: 'archive' }));
    const unknown = { value: null, unit: 'bytes', source: 'unavailable', quality: 'unknown', missing_reason: 'Synthetic UI fixture: not measured' };
    const environment = { id: 'static-art-fixture', os: 'fixture-windows', arch: 'amd64', cores: 8, vram: unknown, collector_version: 'fixture-only' };
    const counts = { planned: 0, attempted: 0, scored: 0, pass: 0, fail: 0, infra: 0, cancelled: 0, interrupted: 0, unknown: 0, paired_complete: 0 };
    const report = { id: 'static-fixture-report', evaluation_id: 'static-fixture-evaluation', schema_version: 1, encoder_version: 'fixture', manifest_hash: 'fixture-manifest', evaluation_revision: 1, attempt_set_hash: 'fixture-attempts', selected: [], superseded: [], counts, categories: {}, pair_matrix: [0, 0, 0, 0] };
    const evaluation = { id: report.evaluation_id, manifest_hash: report.manifest_hash, revision: 1, status: 'completed', started_at: '2026-10-27T00:00:00Z', finished_at: '2026-10-27T00:00:01Z', run_id: 'fixture-run', manifest: { source: 'offline_fixture', workspace_id: 'local', workflow_id: 'codelens.codetask.go.v1', stable: { id: 'fixture-stable', model_name: 'Fixture Stable' }, candidate: { id: 'fixture-candidate', model_name: 'Fixture Candidate' }, environment } };
    const release = { id: 'static-fixture-release', workspace_id: 'local', workflow_id: 'codelens.codetask.go.v1', filter_hash: 'codetask_go', mode: 'simulation', state: 'DRAFT', paused_from: '', stable_revision_id: 'fixture-stable', candidate_revision_id: 'fixture-candidate', previous_stable: 'fixture-stable', serving_revision_id: 'fixture-stable', policy_id: 'simulation-demo-v1', report_id: report.id, revision: 1, route_epoch: 1, target_bps: 0, admission_enabled: false, closed_at: '', simulation: { source: 'simulation_replay', offline_source: 'offline_fixture', version: 'simulation-v1', authorization: 'synthetic', clock_seconds: 0, stages: [], alerts: [], events: [] } };
    window.staticArchiveQA = { calls: [], longTasks: [], dark, backdrop: null, image: null };
    if (PerformanceObserver.supportedEntryTypes.includes('longtask')) {
      new PerformanceObserver(list => window.staticArchiveQA.longTasks.push(...list.getEntries().map(entry => ({ start: entry.startTime, duration: entry.duration })))).observe({ type: 'longtask', buffered: true });
    }
    const original = window.__TAURI_INTERNALS__.invoke;
    window.__TAURI_INTERNALS__.invoke = async (command, payload) => {
      window.staticArchiveQA.calls.push({ command, operation: payload?.request?.operation });
      if (command === 'get_models') return { models: [
        { id: 'fixture-api', name: 'API Fixture', model_name: 'deepseek-chat', provider: 'deepseek', base_url: 'https://fixture.invalid', enabled: true, is_default: true },
        { id: 'fixture-local', name: 'Local Fixture', model_name: 'fixture-local-27b', provider: 'openai_compatible', base_url: 'http://127.0.0.1:18787/v1', enabled: true },
      ] };
      if (command !== 'model_engineering') return original(command, payload);
      switch (payload.request.operation) {
        case 'list_models': return { models: [], next_cursor: '' };
        case 'list_profiles': return { profiles: [], next_cursor: '' };
        case 'list_datasets': return { datasets: [] };
        case 'environment': return { environment, preflight: { available: false } };
        case 'list_evaluations': return { evaluations: [evaluation], next_cursor: '' };
        case 'get_evaluation': return { evaluation, attempts: [], progress: counts, latest_report_id: report.id, next_cursor: '' };
        case 'get_report': return { report, report_hash: 'fixture-report-hash', promotion_eligibility: { eligible: false, reasons: ['Synthetic UI fixture: no scored evidence'] } };
        case 'list_releases': return { releases: [release], next_cursor: '' };
        case 'list_policies': return { policies: [] };
        case 'get_release': return release;
        case 'release_events': return { events: [], next_after: 0 };
        default: throw new Error('Unexpected static-archive fixture operation: ' + payload.request.operation);
      }
    };
  }, { graphics, dark });
}

async function assertArt(page, retain = false) {
  const backdrop = page.locator('[data-model-archive-backdrop]');
  await backdrop.waitFor({ state: 'visible', timeout: 10000 });
  assert.equal(await backdrop.getAttribute('data-renderer'), 'static-image');
  assert.equal(await backdrop.getAttribute('data-model-archive-active'), 'true');
  assert.equal(await backdrop.count(), 1);
  const image = backdrop.locator('img[data-model-archive-image]');
  await image.evaluate(image => image.decode());
  assert.match(await image.getAttribute('src'), /^\/assets\/model-archive-background\.webp$/);
  assert.equal(await image.evaluate(image => {
    const bounds = image.getBoundingClientRect();
    return image.complete && image.naturalWidth > 0 && bounds.width > 0 && bounds.height > 0 && bounds.right > 0 && bounds.top < innerHeight && Number(getComputedStyle(image).opacity) > 0;
  }), true);
  if (retain) assert.equal(await backdrop.evaluate(element => element === window.staticArchiveQA.backdrop && element.querySelector('img') === window.staticArchiveQA.image), true, 'tab and detail navigation must preserve the same decoded backdrop nodes');
  assert.equal(await page.locator('canvas').count(), 0, 'model pages use raster art without WebGL');
  assert.equal(await page.locator('vite-error-overlay').count(), 0);
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
}

async function check(name, run) { await run(); passed.push(name); console.log('PASS ' + name); }
async function screenshot(page, name) { await page.screenshot({ path: path.join(output, name + '.png'), animations: 'disabled' }); }

(async () => {
  const browser = await chromium.launch({ channel: process.env.QA_BROWSER || 'msedge', headless: true });
  try {
    for (const [width, height] of [[1448, 1054], [960, 900]]) {
      const context = await createContext(browser, { width, height }, true, 'no-preference');
      await fixture(context);
      const page = await context.newPage(), requested = [];
      page.on('request', request => requested.push(request.url()));
      page.on('console', message => { if (message.type() === 'warning') warnings.push(message.text()); });
      await check(`${width}: direct Hub decodes raster/card texture without importing Three or GLB`, async () => {
        await page.goto(base + '#/model-hub');
        await page.getByRole('heading', { name: 'Model Hub', exact: true }).waitFor();
        assert.match(await page.title(), /Noosphere/i);
        await page.getByRole('button', { name: '添加模型', exact: true }).waitFor();
        await assertArt(page);
        assert.match(await page.locator('.ns-hub-feature-main').evaluate(element => getComputedStyle(element).backgroundImage), /\/assets\/model-card-texture\.webp/);
        const startup = await page.evaluate(() => {
          window.staticArchiveQA.backdrop = document.querySelector('[data-model-archive-backdrop]');
          window.staticArchiveQA.image = document.querySelector('[data-model-archive-image]');
          window.staticArchiveQA.decodedAt = performance.now();
          return { decodedAt: performance.now(), domContentLoadedAt: performance.getEntriesByType('navigation')[0].domContentLoadedEventEnd, initialLongTasks: window.staticArchiveQA.longTasks.slice() };
        });
        await page.waitForTimeout(1200);
        const idle = await page.evaluate(() => ({ idleEnd: performance.now(), postDecodeLongTasks: window.staticArchiveQA.longTasks.filter(task => task.start >= window.staticArchiveQA.decodedAt), resourceCount: performance.getEntriesByType('resource').length }));
        measurements.push({ viewport: { width, height }, timingKind: 'isolated browser dev-server UI, informational; not native FPS', decodedAtMS: round(startup.decodedAt), domContentLoadedAtMS: round(startup.domContentLoadedAt), initialLongTasks: startup.initialLongTasks, idleWindowMS: round(idle.idleEnd - startup.decodedAt), postDecodeLongTasks: idle.postDecodeLongTasks, resourceCount: idle.resourceCount });
        assert.deepEqual(requested.filter(url => sceneResource.test(url)), []);
        await screenshot(page, `model-hub-static-${width}`);
      });
      await check(`${width}: keyboard search and pointer controls respond with art mounted`, async () => {
        const samples = {};
        let start = performance.now();
        await page.keyboard.press('/');
        await page.waitForFunction(() => document.activeElement?.id === 'hub-model-search', null, { timeout: 3000 });
        samples.keyboardFocusMS = round(performance.now() - start);
        start = performance.now();
        await page.getByRole('textbox', { name: '搜索模型', exact: true }).fill('Local Fixture');
        await page.waitForFunction(() => document.querySelectorAll('.ns-hub-model-row').length === 1, null, { timeout: 3000 });
        assert.match(await page.locator('.ns-hub-model-row').textContent(), /Local Fixture/);
        samples.searchFilterMS = round(performance.now() - start);
        start = performance.now();
        await page.getByRole('button', { name: '清除搜索', exact: true }).click();
        await page.getByRole('button', { name: '网格视图', exact: true }).click();
        await page.locator('.ns-hub-models.is-grid').waitFor({ timeout: 3000 });
        await page.getByRole('button', { name: '列表视图', exact: true }).click();
        await page.locator('.ns-hub-models.is-list').waitFor({ timeout: 3000 });
        samples.pointerViewSwitchMS = round(performance.now() - start);
        await page.getByRole('button', { name: '查看主模型详情', exact: true }).click();
        await page.getByRole('dialog').waitFor({ timeout: 3000 });
        await page.keyboard.press('Escape');
        await page.getByRole('dialog').waitFor({ state: 'hidden', timeout: 3000 });
        await assertArt(page, true);
        measurements.at(-1).interactions = samples;
      });
      await check(`${width}: four tabs and report/release details retain one backdrop instance`, async () => {
        for (const [label, route, heading, name] of [
          ['Model Lab', '/model-lab', 'Model Lab', 'model-lab'],
          ['Reports', '/evaluation-reports', 'Evaluation Reports', 'reports'],
          ['Release Center', '/release-center', 'Release Center', 'release-center'],
          ['Model Hub', '/model-hub', 'Model Hub', 'model-hub-returned'],
        ]) {
          await page.getByRole('navigation', { name: '模型档案导航' }).getByRole('link', { name: label, exact: true }).click();
          await page.waitForURL('**/#' + route);
          await page.getByRole('heading', { name: heading, exact: true }).waitFor();
          await assertArt(page, true);
          assert.equal(await page.getByRole('navigation', { name: '模型档案导航' }).getByRole('link', { name: label, exact: true }).getAttribute('aria-current'), 'page');
          await screenshot(page, `${name}-static-${width}`);
        }
        await page.getByRole('navigation', { name: '模型档案导航' }).getByRole('link', { name: 'Reports', exact: true }).click();
        await page.getByRole('button', { name: '重开报告或任务', exact: true }).click();
        await page.waitForURL('**/#/evaluation-reports/static-fixture-report');
        await page.getByRole('heading', { name: '比较报告', exact: true }).waitFor();
        await assertArt(page, true);
        await page.getByRole('navigation', { name: '模型档案导航' }).getByRole('link', { name: 'Release Center', exact: true }).click();
        await page.getByRole('link', { name: '打开模拟发布 →', exact: true }).click();
        await page.waitForURL('**/#/release-center/static-fixture-release');
        await page.getByText('DRAFT · SIMULATION', { exact: true }).waitFor();
        await assertArt(page, true);
        assert.deepEqual(requested.filter(url => sceneResource.test(url)), []);
        assert.equal(await page.evaluate(() => window.qa.runs.length), 0);
      });
      await context.close();
    }
    for (const scenario of [
      { name: 'graphics-off', graphics: 'off', reducedMotion: 'no-preference', dark: false },
      { name: 'reduced-motion', graphics: 'auto', reducedMotion: 'reduce', dark: false },
      { name: 'dark-theme', graphics: 'auto', reducedMotion: 'no-preference', dark: true },
    ]) {
      const context = await createContext(browser, { width: 1448, height: 1054 }, true, scenario.reducedMotion);
      await fixture(context, scenario);
      const page = await context.newPage(), requested = [];
      page.on('request', request => requested.push(request.url()));
      await check(`${scenario.name}: art remains visible and navigation remains usable`, async () => {
        await page.goto(base + '#/model-hub');
        await page.getByRole('heading', { name: 'Model Hub', exact: true }).waitFor();
        if (scenario.dark) await page.evaluate(async () => { const { useThemeStore } = await import('/src/stores/theme-store.ts'); useThemeStore.getState().setTheme('dark'); });
        if (scenario.dark) await page.waitForFunction(() => document.documentElement.classList.contains('dark'));
        await assertArt(page);
        await page.locator('[data-model-archive-backdrop]').evaluate(element => { window.staticArchiveQA.backdrop = element; window.staticArchiveQA.image = element.querySelector('img'); });
        await page.getByRole('navigation', { name: '模型档案导航' }).getByRole('link', { name: 'Model Lab', exact: true }).click();
        await page.getByRole('heading', { name: 'Model Lab', exact: true }).waitFor();
        await assertArt(page, true);
        await screenshot(page, `model-archive-${scenario.name}`);
        assert.deepEqual(requested.filter(url => sceneResource.test(url)), []);
      });
      await context.close();
    }
    assert.deepEqual(errors, []);
    const result = { passed, errors, warnings: [...new Set(warnings)], measurements, provenance: 'Isolated synthetic IPC and headless Edge/Playwright. Development-server timings are informational browser UI measurements; no native FPS or real inference claim.', output };
    fs.writeFileSync(path.join(output, 'model-archive-static-result.json'), JSON.stringify(result, null, 2));
    console.log(JSON.stringify(result, null, 2));
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
