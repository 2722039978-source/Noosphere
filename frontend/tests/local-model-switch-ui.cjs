const assert = require('node:assert/strict');
const { chromium } = require('playwright');
const { createContext, errors } = require('./ui-regression.cjs');

const base = process.env.QA_URL || 'http://127.0.0.1:1420/';
const provider = 'openai_compatible';
const bonsai = { id: 'bonsai-a', name: 'Bonsai 2 27B', provider, model_name: 'bonsai-local-27b', base_url: 'http://127.0.0.1:18787/v1', enabled: true };
const deepseek = { name: 'DeepSeek R1 8B', provider, model_name: 'deepseek-r1-0528-qwen3-8b', base_url: 'http://127.0.0.1:18788/v1', enabled: true };

async function main() {
  const browser = await chromium.launch({ channel: process.env.QA_BROWSER || 'msedge', headless: true });
  try {
    const context = await createContext(browser, { width: 1440, height: 930 });
    await context.addInitScript(({ bonsai, deepseek }) => {
      const invoke = window.__TAURI_INTERNALS__.invoke;
      const models = [bonsai, { ...bonsai, id: 'bonsai-b', base_url: 'http://localhost:18787/v1/' }];
      let active = 'bonsai';
      const engine = (id, model) => ({ id, name: model.name, provider: model.provider, base_url: model.base_url, installed: true,
        can_start: true, managed: active === id, state: active === id ? 'online' : 'stopped',
        models: [{ id: model.model_name, name: model.name, loaded: active === id, downloaded: true }] });
      const diagnostic = { status: 'ok', inference: 'succeeded' };
      window.qa.modelOperations = [];
      window.qa.failSwitch = false;
      window.__TAURI_INTERNALS__.invoke = async (command, payload) => {
        if (command === 'get_models') return { models: [...models] };
        if (command === 'list_local_engines') return { engines: [engine('bonsai', bonsai), engine('deepseek_local', deepseek)] };
        if (command === 'control_local_engine') {
          window.qa.modelOperations.push([payload.engineId, payload.action]);
          if (window.qa.failSwitch) throw { message: '模型启动失败，请重试。' };
          active = payload.engineId;
          return engine(active, active === 'bonsai' ? bonsai : deepseek);
        }
        if (command === 'probe_model' || command === 'test_model') return diagnostic;
        if (command === 'add_model') { models.push(payload.model); return {}; }
        return invoke(command, payload);
      };
    }, { bonsai, deepseek });
    const page = await context.newPage();
    page.setDefaultTimeout(6000);
    await page.goto(base + '#/chat');
    const picker = page.getByRole('combobox', { name: '选择对话模型' });
    await picker.getByText('Bonsai 2 27B', { exact: true }).waitFor();
    await picker.click();
    assert.deepEqual(await page.getByRole('option').filter({ hasText: 'Bonsai' }).allTextContents(), ['Bonsai 2 27B']);
    await page.getByRole('option', { name: /DeepSeek R1 8B/ }).click();
    const dialog = page.getByRole('dialog', { name: '选择模型', exact: true });
    const switchButton = dialog.getByRole('button', { name: '切换并启动', exact: true });
    await switchButton.waitFor();
    assert.deepEqual(await page.evaluate(() => window.qa.modelOperations), []);
    await page.evaluate(() => { window.qa.failSwitch = true; });
    await switchButton.click();
    await dialog.getByRole('alert').filter({ hasText: '模型启动失败' }).waitFor();
    await page.evaluate(() => { window.qa.failSwitch = false; });
    await switchButton.click();
    await dialog.waitFor({ state: 'hidden' });
    await picker.getByText('DeepSeek R1 8B', { exact: true }).waitFor();
    await picker.click();
    await page.getByRole('option', { name: /Bonsai 2 27B/ }).click();
    await switchButton.click();
    await dialog.waitFor({ state: 'hidden' });
    await picker.getByText('Bonsai 2 27B', { exact: true }).waitFor();
    assert.deepEqual(await page.evaluate(() => window.qa.modelOperations), [['deepseek_local', 'switch'], ['deepseek_local', 'switch'], ['bonsai', 'switch']]);

    const rail = page.getByRole('navigation', { name: '主导航' });
    const tasks = page.getByRole('complementary', { name: '项目与会话' });
    await tasks.waitFor();
    await rail.getByRole('link', { name: '模型', exact: true }).click();
    await page.getByRole('heading', { name: '本地模型', exact: true }).waitFor();
    assert.equal(await tasks.count(), 0);
    await page.getByRole('navigation', { name: '模型导航' }).getByRole('link', { name: '高级管理' }).waitFor();
    await rail.getByRole('link', { name: '系统', exact: true }).click();
    await page.getByRole('navigation', { name: '系统导航' }).getByRole('link', { name: '诊断工具' }).click();
    await page.getByRole('heading', { name: '诊断工具', exact: true }).waitFor();
    assert.equal(await tasks.count(), 0);
    await rail.getByRole('link', { name: '测评', exact: true }).click();
    await page.getByRole('heading', { name: '模型试聊', exact: true }).waitFor();
    assert.equal(await tasks.count(), 0);
    await rail.getByRole('link', { name: '对话', exact: true }).click();
    await tasks.waitFor();
    await picker.getByText('Bonsai 2 27B', { exact: true }).waitFor();
    assert.deepEqual(errors, []);
    console.log('PASS local targets, explicit restart, retry, existing binding, and section navigation');
  } finally {
    await browser.close();
  }
}

main().catch(error => { console.error(error); process.exitCode = 1; });
