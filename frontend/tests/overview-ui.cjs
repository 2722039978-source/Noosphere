// Isolated IPC fixtures, no inference or user data. Browser plugin unavailable;
// use the project's existing headless Playwright harness instead of Computer Use.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { createContext, errors } = require('./ui-regression.cjs');
const base = process.env.QA_URL || 'http://127.0.0.1:1420/';
const output = process.env.QA_OUTPUT || path.resolve('qa-output/overview');
fs.mkdirSync(output, { recursive: true });
const passed = [];
async function check(name, action) { await action(); passed.push(name); console.log('PASS ' + name); }
async function fixture(context, options = {}) {
  await context.addInitScript(options => {
    if (location.protocol !== 'http:' && location.protocol !== 'https:') return;
    localStorage.setItem('noosphere-experience-v1', JSON.stringify({ graphics: 'high', boot: true, startPage: 'archive' }));
    window.overviewQA = { calls: [], mode: options.mode || 'ok', configs: [
      { id: 'deepseek', name: 'DeepSeek', model_name: 'deepseek-chat', provider: 'deepseek', base_url: 'https://api.deepseek.com', is_default: true, enabled: true },
      { id: 'fixture-local-27b', name: 'Local 27B Fixture', model_name: 'fixture-local-27b', provider: 'openai_compatible', base_url: 'http://127.0.0.1:18787/v1', enabled: true },
      { id: 'disabled-model', name: 'Disabled fixture', model_name: 'disabled-fixture', provider: 'ollama', base_url: 'http://127.0.0.1:11434', enabled: false },
    ] };
    const original = window.__TAURI_INTERNALS__.invoke;
    window.__TAURI_INTERNALS__.invoke = async (command, payload) => {
      const q = window.overviewQA; q.calls.push({ command, payload });
      if (command === 'get_models') { if (q.mode === 'offline') throw 'fixture: backend unavailable'; return { models: q.mode === 'empty' ? [] : structuredClone(q.configs) }; }
      if (command === 'add_model') { q.configs.push({ ...payload.model, enabled: true }); return { status: 'ok' }; }
      if (command === 'model_engineering') {
        switch (payload.request.operation) {
          case 'list_models': return { models: [], next_cursor: '' };
          case 'environment': return { environment: { id: 'fixture', os: 'windows', arch: 'amd64', cores: 12 }, preflight: { available: false } };
          default: throw 'unexpected fixture operation';
        }
      }
      return original(command, payload);
    };
  }, options);
}
async function snapshot(page, name) { await page.screenshot({ path: path.join(output, name + '.png') }); }
async function staticArchive(page) {
  await page.getByRole('heading', { name: 'Model Hub', exact: true }).waitFor();
  const backdrop = page.locator('[data-model-archive-backdrop]');
  await backdrop.waitFor({ state: 'visible' });
  const image = backdrop.locator('img[data-model-archive-image]');
  await image.evaluate(image => image.decode());
  assert.match(await image.getAttribute('src'), /\/assets\/model-archive-background\.webp$/);
  assert.equal(await image.evaluate(image => image.complete && image.naturalWidth > 0), true);
  assert.equal(await page.locator('canvas').count(), 0);
  assert.deepEqual(await page.evaluate(() => performance.getEntriesByType('resource').filter(entry => /ArchiveCanvas|archive-cassette|GLTFLoader|\/three(?:[/.?_-]|$)/i.test(entry.name)).map(entry => entry.name)), []);
}
(async () => {
  const browser = await chromium.launch({ channel: process.env.QA_BROWSER || 'msedge', headless: true });
  try {
    const context = await createContext(browser, { width: 1448, height: 1086 }, true, 'reduce');
    await fixture(context);
    const page = await context.newPage();
    await check('historic archive startup opens usable overview without an animation gate', async () => {
      await page.goto(base + '#/');
      await page.getByRole('heading', { name: 'Model Hub', exact: true }).waitFor();
      await page.getByRole('link', { name: '开始对话', exact: true }).waitFor();
      assert.equal(await page.locator('.ns-archive-return-cover').count(), 0);
      await staticArchive(page);
      await snapshot(page, 'overview-1448');
      assert.equal(await page.evaluate(() => window.qa.runs.length), 0);
      assert.equal(await page.evaluate(() => window.overviewQA.calls.filter(c => /test_model|gateway_chat|submit_workspace/.test(c.command)).length), 0);
    });
    await check('filters, search, list/grid and exact selected-model handoff work', async () => {
      await page.getByRole('button', { name: /^本地/ }).click();
      assert.equal(await page.locator('.ns-hub-model-row').count(), 2);
      await page.getByRole('textbox', { name: '搜索模型', exact: true }).fill('Bonsai');
      assert.equal(await page.locator('.ns-hub-model-row').count(), 1);
      await page.getByRole('button', { name: '网格视图' }).click();
      await page.getByRole('button', { name: '列表视图' }).click();
      await page.locator('.ns-hub-model-row').filter({hasText:'Local 27B Fixture'}).getByRole('button',{name:'使用',exact:true}).click();
      await page.getByRole('heading', { name: 'Local 27B Fixture', exact: true }).waitFor();
      await page.getByRole('link', { name: '开始对话', exact: true }).click();
      const input = page.getByRole('textbox', { name: '输入问题', exact: true });
      await input.waitFor();
      await page.waitForFunction(async () => { const s = (await window.qa.store()).useChatStore.getState(); return s.sessions.find(x => x.id === s.activeId)?.modelId === 'fixture-local-27b' && document.activeElement?.getAttribute('aria-label') === '输入问题'; });
      assert.match(page.url(), /#\/chat$/);
      assert.equal(await page.evaluate(() => window.qa.runs.length), 0);
      await snapshot(page, 'chat-selected-model');
    });
    let originalID, originalCount;
    await check('new model intent preserves an existing conversation and history reentry is idempotent', async () => {
      originalID = await page.evaluate(async () => { const s = (await window.qa.store()).useChatStore.getState(); s.appendMessage(s.activeId, { id: 'fixture-existing', role: 'user', content: 'Existing fixture conversation', createdAt: Date.now() }); return s.activeId; });
      // Preserve legacy explicit model-link behavior independently of Hub Use,
      // whose new contract is to switch the current session in place.
      await page.goto(base + '#/chat?model=deepseek');
      await page.waitForFunction(async id => { const s = (await window.qa.store()).useChatStore.getState(); return s.activeId !== id && s.sessions.find(x => x.id === s.activeId)?.modelId === 'deepseek'; }, originalID);
      originalCount = await page.evaluate(async () => (await window.qa.store()).useChatStore.getState().sessions.length);
      await page.goBack(); await page.goForward();
      await page.getByRole('textbox', { name: '输入问题', exact: true }).waitFor();
      await page.reload(); await page.getByRole('textbox', { name: '输入问题', exact: true }).waitFor();
      assert.equal(await page.evaluate(async () => (await window.qa.store()).useChatStore.getState().sessions.length), originalCount);
      assert.equal(await page.evaluate(async id => (await window.qa.store()).useChatStore.getState().sessions.find(s => s.id === id)?.messages[0].content, originalID), 'Existing fixture conversation');
    });
    await check('disabled and stale intentions block sending with an explicit model choice', async () => {
      await page.goto(base + '#/chat?model=disabled-model');
      await page.getByRole('alert').filter({ hasText: '已停用' }).waitFor();
      assert.ok(await page.getByRole('button', { name: '发送', exact: true }).isDisabled());
      await page.goto(base + '#/chat?model=missing-model');
      await page.getByRole('alert').filter({ hasText: '不存在或已被删除' }).waitFor();
      await page.getByRole('combobox', { name: '选择可用对话模型' }).selectOption('fixture-local-27b');
      await page.waitForFunction(async () => { const s = (await window.qa.store()).useChatStore.getState(); return s.sessions.find(x => x.id === s.activeId)?.modelId === 'fixture-local-27b'; });
      assert.equal(await page.evaluate(() => window.qa.runs.length), 0);
    });
    await check('global search reaches focused overview and add uses existing Gateway form', async () => {
      await page.locator('.ns-header-search').click();
      await page.getByRole('textbox', { name: '搜索模型', exact: true }).waitFor();
      await page.waitForFunction(() => document.activeElement?.id === 'hub-model-search');
      await page.getByRole('button', { name: '添加模型', exact: true }).click();
      const dialog = page.getByRole('dialog');
      for (const [name, value] of [['标识 ID', 'new-fixture'], ['显示名称', 'New fixture'], ['接口地址', 'http://127.0.0.1:11434'], ['模型名称', 'fixture']]) await dialog.getByRole('textbox', { name, exact: true }).fill(value);
      await dialog.getByRole('button', { name: /保存/ }).click();
      await dialog.waitFor({ state: 'hidden' });
      await snapshot(page, 'overview-added-model');
      await page.getByRole('button', { name: '查看模型 New fixture', exact: true }).waitFor();
      assert.equal(await page.evaluate(() => window.overviewQA.configs.find(c => c.id === 'deepseek').is_default), true);
      await page.getByRole('button', { name: '查看主模型详情' }).click();
      await page.getByRole('dialog').getByText(/当前原生测评仅支持 Ollama/).waitFor();
      await page.keyboard.press('Escape');
      await page.getByRole('dialog').waitFor({state:'hidden'});
    });
    await check('reference and minimum desktop viewports have reachable actions without document overflow', async () => {
      for (const [width, height] of [[1280, 960], [900, 600]]) {
        await page.setViewportSize({ width, height });
        assert.ok(await page.getByRole('link', { name: '开始对话', exact: true }).isVisible());
        assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
        await snapshot(page, `overview-${width}`);
      }
    });
    await context.close();
    for (const mode of ['empty', 'offline']) {
      const c = await createContext(browser, { width: 1280, height: 800 }, true, 'reduce'); await fixture(c, { mode });
      const p = await c.newPage();
      await check(`${mode} metadata is explicit and sends no request`, async () => {
        await p.goto(base + '#/'); await p.getByRole('heading', { name: 'Model Hub', exact: true }).waitFor();
        await p.getByText(mode === 'empty' ? /尚未配置模型连接/ : /模型连接读取失败/).waitFor();
        assert.equal(await p.getByRole('link', { name: '开始对话', exact: true }).count(), 0);
        assert.equal(await p.evaluate(() => window.qa.runs.length), 0);
      });
      await c.close();
    }
    const motionContext = await createContext(browser, { width: 1448, height: 1086 }, true, 'no-preference'); await fixture(motionContext);
    const motion = await motionContext.newPage();
    await check('decoded static archive avoids WebGL while orbit motion respects reduced-motion preference', async () => {
      await motion.goto(base + '#/model-hub');
      await staticArchive(motion);
      assert.equal(await motion.locator('[data-model-archive-backdrop]').evaluate(element => element.getAnimations({ subtree: true }).filter(animation => animation.playState === 'running').length), 0);
      const animations = await motion.evaluate(() => document.querySelector('.ns-hub-orbit').getAnimations({ subtree: true }).map(a => a.playState));
      assert.ok(animations.includes('running'));
      await motion.emulateMedia({ reducedMotion: 'reduce' });
      assert.equal(await motion.evaluate(() => document.querySelector('.ns-hub-orbit').getAnimations({ subtree: true }).filter(a => a.playState === 'running').length), 0);
      await staticArchive(motion);
      await snapshot(motion, 'overview-motion');
    });
    await motionContext.close();
    assert.deepEqual(errors, []);
    fs.writeFileSync(path.join(output, 'overview-ui-result.json'), JSON.stringify({ passed, consoleErrors: errors, provenance: 'isolated synthetic IPC; no inference/native-service acceptance' }, null, 2));
    console.log(JSON.stringify({ passed: passed.length, errors: errors.length, output }));
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
