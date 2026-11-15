// The mini dialogue renders against the same Tauri fixture as main chat.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const { createContext, errors } = require('./ui-regression.cjs');
const base = process.env.QA_URL || 'http://127.0.0.1:1420/';

(async () => {
  const browser = await chromium.launch({ channel: process.env.QA_BROWSER || 'msedge', headless: true });
  try {
    const context = await createContext(browser, { width: 1280, height: 800 });
    await context.addInitScript(() => {
      const invoke = window.__TAURI_INTERNALS__.invoke;
      const model = { id: 'fixture-local', name: '本机夹具', model_name: 'fixture', provider: 'openai_compatible', base_url: 'http://127.0.0.1:18787/v1', enabled: true };
      window.qa.commands = [];
      window.__TAURI_INTERNALS__.invoke = (command, payload) => {
        window.qa.commands.push(command);
        if (command === 'get_models') return { models: [
          model,
          { ...model, id: 'fixture-remote', name: '远程夹具', base_url: 'https://fixture.invalid/v1', deployment: 'local' },
          { ...model, id: 'fixture-prefixed', name: '非回环夹具', base_url: 'http://127.example.invalid/v1' },
          { ...model, id: 'fixture-disabled', name: '停用夹具', enabled: false },
        ] };
        return invoke(command, payload);
      };
    });
    const page = await context.newPage();
    await page.goto(base + '#/model-check');
    await page.getByRole('heading', { name: '模型试聊', exact: true }).waitFor();
    const model = page.getByRole('combobox', { name: '试聊模型', exact: true });
    await model.getByText('本机夹具', { exact: true }).waitFor();
    await model.click();
    assert.deepEqual(await page.getByRole('option').allTextContents(), ['本机夹具', '查找本地模型…']);
    await page.keyboard.press('Escape');
    const input = page.getByRole('textbox', { name: '输入试聊问题', exact: true });
    await input.fill('试聊问题');
    await input.press('Enter');
    await page.waitForFunction(() => window.qa.runs.length === 1);
    const first = await page.evaluate(() => window.qa.runs[0]);
    assert.equal(first.modelId, 'fixture-local');
    assert.equal(first.sessionId, '');
    assert.equal(first.preview, true);
    assert.deepEqual(first.messages, [{ role: 'user', content: '试聊问题' }]);
    await page.evaluate(() => {
      window.qa.emit(0, { token: '中间片段' });
      window.qa.emit(0, { done: true, status: 'completed', content: '试聊夹具回答' });
    });
    await page.getByText('试聊夹具回答', { exact: false }).waitFor();
    await input.fill('继续问');
    await page.getByRole('button', { name: '发送试聊', exact: true }).click();
    await page.waitForFunction(() => window.qa.runs.length === 2);
    assert.deepEqual(await page.evaluate(() => window.qa.runs[1].messages), [
      { role: 'user', content: '试聊问题' }, { role: 'assistant', content: '试聊夹具回答' },
      { role: 'user', content: '继续问' },
    ]);
    await page.getByRole('button', { name: '停止试聊', exact: true }).click();
    await page.getByText('已停止试聊', { exact: true }).waitFor();
    await page.waitForFunction(() => window.qa.cancels[0] === window.qa.runs[1].requestId);
    await page.evaluate(() => window.qa.emit(1, { content: '已经过期的回应', done: true }));
    assert.equal(await page.getByText('已经过期的回应', { exact: false }).count(), 0);
    const original = await page.evaluate(async () => (await window.qa.store()).useChatStore.getState().sessions);
    assert.deepEqual(original, []);
    assert.equal(await page.evaluate(() => window.qa.commands.some(command => ['select_model', 'submit_workspace_run', 'save_knowledge'].includes(command))), false);
    assert.equal(await page.getByRole('navigation', { name: '测评导航' }).getByRole('link', { name: '正式测评' }).getAttribute('href'), '#/model-lab');
    if (process.env.QA_OUTPUT) {
      fs.mkdirSync(process.env.QA_OUTPUT, { recursive: true });
      await page.screenshot({ path: path.join(process.env.QA_OUTPUT, 'model-check-preview.png'), animations: 'disabled' });
    }
    await page.getByRole('button', { name: '清空试聊', exact: true }).click();
    assert.equal(await page.getByText('试聊夹具回答', { exact: false }).count(), 0);
    await page.setViewportSize({ width: 900, height: 600 });
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
    await model.click();
    await page.getByRole('option', { name: '查找本地模型…' }).click();
    const setup = page.getByRole('dialog', { name: '选择模型', exact: true });
    await setup.getByRole('heading', { name: '本机模型' }).waitFor();
    assert.equal(await setup.getByRole('checkbox', { name: /自动启动/ }).count(), 0);
    assert.deepEqual(errors, []);
    await context.close();
    console.log('PASS only loopback models, streamed follow-up, cancel/clear, isolated chat, compact layout and local wizard');
  } finally {
    await browser.close();
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
