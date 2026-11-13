const { chromium } = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');

// Run node tests/model-selection-ui.cjs from frontend after installing the
// dependencies and Playwright Chromium. Vite starts on loopback.
const output = process.env.QA_OUTPUT || path.join(os.tmpdir(), 'noosphere-model-selection-qa');
const localModel = {
  id: 'local-model', name: 'Local fixture', model_name: 'fixture-local',
  provider: 'openai_compatible', base_url: 'http://127.0.0.1:1/v1', enabled: true,
};
const alternateModel = { ...localModel, id: 'alternate-model', name: 'Alternate fixture', model_name: 'fixture-alternate' };
const disabledModel = { ...localModel, id: 'disabled', name: 'Disabled fixture', model_name: 'fixture-disabled', enabled: false };
const errors = [];

async function createContext(browser, base, fixture) {
  const context = await browser.newContext({ viewport: { width: 1440, height: 960 }, reducedMotion: 'reduce' });
  const origin = new URL(base).origin;
  await context.route('**/*', route => new URL(route.request().url()).origin === origin ? route.continue() : route.abort());
  await context.addInitScript(({ models, workspaceDefault = '', otherWorkspace = false }) => {
    const local = {
      id: 'local', name: '本地工作区', project_path: '', project_id: '',
      default_model_id: workspaceDefault, created_at: '', updated_at: '',
    };
    const workspaces = otherWorkspace
      ? [local, { ...local, id: 'other', name: 'Other workspace', default_model_id: '' }]
      : [local];
    const qa = window.qa = { runs: [], selections: [], selectionMode: 'ok' };
    const bindingKey = (workspace, session) => 'qa-binding:' + workspace + ':' + session;
    let callbackId = 0;

    // The app makes its regular Tauri calls. The matching Go test uses an
    // actual loopback HTTP server to exercise Gateway provider serialization.
    window.__TAURI_INTERNALS__ = {
      transformCallback() { return ++callbackId; },
      unregisterCallback() {},
      async invoke(command, payload) {
        switch (command) {
          case 'check_health': return { status: 'ok', modules: {} };
          case 'get_backend_status': return { running: true, restarts: 0 };
          case 'list_workspaces': return { workspaces };
          case 'list_sessions': return { sessions: [] };
          case 'get_models': return { models };
          case 'prepare_context': return { workspace_id: payload.workspaceId, project_id: '', context: '', citations: [], warnings: [], mode: 'lexical' };
          case 'get_model_selection': {
            const { workspaceId, sessionId } = payload;
            const id = localStorage.getItem(bindingKey(workspaceId, sessionId)) || '';
            return { workspace_id: workspaceId, session_id: sessionId, model_id: id, bound: Boolean(id) };
          }
          case 'select_model': {
            const selection = payload.selection;
            qa.selections.push(selection);
            if (qa.selectionMode === 'fail') throw { message: 'Provider unavailable' };
            if (qa.selectionMode === 'hold') await new Promise(resolve => { qa.releaseSelection = resolve; });
            localStorage.setItem(bindingKey(selection.workspace_id, selection.session_id), selection.model_id);
            return { ...selection, bound: true };
          }
          case 'model_engineering':
            if (payload.request.operation === 'list_models') return { models: [], next_cursor: '' };
            if (payload.request.operation === 'environment') return { environment: null };
            return {};
          case 'gateway_chat_stream': qa.runs.push(payload); return;
          case 'gateway_chat_cancel': return;
          default: return {};
        }
      },
    };
  }, fixture);
  context.on('page', page => {
    page.on('pageerror', error => errors.push(error.message));
    page.on('console', message => { if (message.type() === 'error') errors.push(message.text()); });
  });
  return context;
}

async function checkDefaultOption(browser, base) {
  const globalModel = { ...alternateModel, is_default: true };
  const fixtures = [
    { models: [disabledModel, localModel, alternateModel], label: '自动回退 · Local fixture' },
    { models: [localModel, globalModel], label: '全局默认 · Alternate fixture' },
    { models: [localModel, globalModel], workspaceDefault: 'local-model', label: '工作区默认 · Local fixture' },
    { models: [{ ...disabledModel, is_default: true }, localModel], label: '自动回退 · Local fixture' },
    { models: [globalModel, disabledModel], workspaceDefault: 'disabled', label: '工作区默认 · Disabled fixture · 已禁用' },
    { models: [globalModel], workspaceDefault: 'missing', label: '工作区默认 · 模型不可用' },
    { models: [{ ...localModel, name: '' }], label: '自动回退 · fixture-local' },
    { models: [{ ...disabledModel, is_default: true }], workspaceDefault: 'disabled', label: '未配置可用模型' },
    { models: [], label: '未配置可用模型' },
  ];
  for (const { label, ...fixture } of fixtures) {
    const context = await createContext(browser, base, fixture);
    try {
      const page = await context.newPage();
      await page.goto(new URL('#/chat', base).href);
      const selector = page.getByRole('combobox');
      await selector.click();
      const options = page.getByRole('option');
      await options.nth(fixture.models.length).waitFor();
      assert.equal(await options.first().textContent(), label);
      await page.keyboard.press('Escape');
      await selector.getByText(label, { exact: true }).waitFor();
    } finally {
      await context.close();
    }
  }
  console.log('PASS ' + fixtures.length + ' default model labels');
}

async function checkModelSwitch(browser, base) {
  const context = await createContext(browser, base, {
    models: [{ ...localModel, is_default: true }, alternateModel, disabledModel],
    otherWorkspace: true,
  });
  try {
    const page = await context.newPage();
    await page.goto(new URL('#/chat', base).href);
    const input = page.getByRole('textbox', { name: '输入问题', exact: true });
    const selector = page.getByRole('combobox');
    await selector.getByText('全局默认 · Local fixture', { exact: true }).waitFor();
    await selector.click();
    assert.equal(await page.getByRole('option', { name: 'Disabled fixture · 已禁用', exact: true }).getAttribute('aria-disabled'), 'true');
    await page.keyboard.press('Escape');
    await page.getByRole('listbox').waitFor({ state: 'hidden' });
    assert.equal(await selector.evaluate(el => el === document.activeElement), true);

    await page.getByRole('button', { name: 'Other workspace', exact: true }).click();
    await page.getByRole('button', { name: '新建对话', exact: true }).click();
    await page.getByRole('button', { name: '本地工作区', exact: true }).click();
    await page.getByRole('navigation', { name: '主导航' }).getByRole('link', { name: '模型', exact: true }).click();
    await page.getByRole('heading', { name: 'Model Hub', exact: true }).waitFor();

    const row = model => page.getByRole('article').filter({
      has: page.getByRole('button', { name: '查看模型 ' + model.name, exact: true }),
    });
    await page.evaluate(() => { window.qa.selectionMode = 'hold'; });
    await row(alternateModel).getByRole('button', { name: '使用', exact: true }).click();
    await page.getByRole('status').filter({ hasText: '正在验证模型连接，确认后切换目标会话' }).waitFor();
    await page.getByRole('region', { name: '主模型', exact: true }).getByRole('heading', { name: localModel.name, exact: true }).waitFor();
    await page.waitForFunction(() => Boolean(window.qa.releaseSelection));
    const selection = await page.evaluate(() => window.qa.selections[0]);
    assert.equal(selection.scope, 'session');
    assert.equal(selection.workspace_id, 'local');
    assert.equal(selection.model_id, alternateModel.id);
    await page.evaluate(() => { window.qa.selectionMode = 'ok'; window.qa.releaseSelection(); });
    const currentModel = row(alternateModel).getByRole('button', { name: '当前使用', exact: true });
    await currentModel.waitFor();
    await page.getByRole('status').filter({ hasText: '当前会话已切换到 Alternate fixture' }).waitFor();

    await page.evaluate(() => { window.qa.selectionMode = 'fail'; });
    await row(localModel).getByRole('button', { name: '使用', exact: true }).click();
    await page.getByRole('alert').filter({ hasText: 'Provider unavailable' }).waitFor();
    assert.equal(await currentModel.isDisabled(), true);
    await page.screenshot({ path: path.join(output, 'selection-failure-desktop.png'), animations: 'disabled' });

    await page.goto(new URL('#/chat', base).href);
    await selector.getByText(alternateModel.name, { exact: true }).waitFor();
    await page.evaluate(() => { window.qa.selectionMode = 'ok'; });
    await selector.click();
    await page.getByRole('option', { name: localModel.name, exact: true }).click();
    const sessionModel = selector.getByText(localModel.name, { exact: true });
    await sessionModel.waitFor();
    await input.fill('Verify the selected model');
    await page.getByRole('button', { name: '发送', exact: true }).click();
    await page.waitForFunction(() => window.qa.runs.length === 1);
    const request = await page.evaluate(() => window.qa.runs[0]);
    assert.equal(request.workspaceId, selection.workspace_id);
    assert.equal(request.sessionId, selection.session_id);
    assert.equal(request.modelId, localModel.id);
    assert.equal(await selector.isDisabled(), true);

    await page.getByRole('button', { name: '停止生成', exact: true }).click();
    await page.getByText('已停止生成', { exact: true }).waitFor();
    await page.reload();
    await sessionModel.waitFor();
    await page.screenshot({ path: path.join(output, 'selection-restored-desktop.png'), animations: 'disabled' });
    await page.setViewportSize({ width: 390, height: 844 });
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
  } finally {
    await context.close();
  }
  console.log('PASS workspace targeting, pending/failed selection, chat model ID, busy guard, reload, focus and narrow layout');
}

async function main() {
  fs.mkdirSync(output, { recursive: true });
  const { createServer } = await import('vite');
  const server = await createServer({
    root: path.resolve(__dirname, '..'),
    server: { host: '127.0.0.1', port: 0, strictPort: true },
  });
  let browser;
  try {
    await server.listen();
    const { port } = server.httpServer.address();
    const base = 'http://127.0.0.1:' + port + '/';
    browser = await chromium.launch({ channel: process.env.QA_BROWSER, headless: true });
    await checkDefaultOption(browser, base);
    await checkModelSwitch(browser, base);
    assert.deepEqual(errors, []);
    console.log(JSON.stringify({ output }));
  } finally {
    await browser?.close();
    await server.close();
  }
}

main().catch(error => { console.error(error); process.exitCode = 1; });
