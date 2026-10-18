// Isolated synthetic Tauri IPC; no real models, credentials or user sessions.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const { createContext, errors } = require('./ui-regression.cjs');
const base = process.env.QA_URL || 'http://127.0.0.1:1420/';
(async () => {
  const browser = await chromium.launch({ channel: 'msedge', headless: true });
  try {
    const context = await createContext(browser, { width: 1440, height: 960 });
    await context.addInitScript(() => {
      const invoke = window.__TAURI_INTERNALS__.invoke;
      window.__TAURI_INTERNALS__.invoke = async (command, payload) => {
        if (command === 'add_model') throw { message: 'fixture: model configuration is read only' };
        return invoke(command, payload);
      };
    });
    const page = await context.newPage();
    await page.goto(base + '#/gateway');
    await page.getByRole('button', { name: '添加模型', exact: true }).click();
    await page.getByLabel('标识 ID', { exact: true }).fill('regression');
    await page.getByLabel('显示名称', { exact: true }).fill('Regression');
    await page.getByLabel('接口地址', { exact: true }).fill('https://example.invalid');
    await page.getByLabel('模型名称', { exact: true }).fill('fixture');
    await page.getByRole('button', { name: '保存', exact: true }).click();
    await page.getByRole('alert').filter({ hasText: 'fixture:' }).waitFor();
    assert.equal(await page.getByLabel('标识 ID', { exact: true }).inputValue(), 'regression');
    assert.deepEqual(errors, []);
    console.log('PASS failed model save keeps form input and exposes actionable error');
    await page.goto(base);
    await page.locator('.ns-full-scene[data-loaded="true"]').waitFor();
    await page.locator('.ns-boot').waitFor({ state: 'hidden' });
    assert.equal(await page.evaluate(() => document.elementFromPoint(innerWidth * .25, innerHeight * .45)?.tagName), 'CANVAS');
    await page.getByRole('button', { name: '展开档案预览' }).click();
    await page.getByRole('button', { name: '收回档案' }).waitFor();
    await page.keyboard.press('Escape');
    await page.getByRole('button', { name: '展开档案预览' }).waitFor();
    await page.getByRole('button', { name: /模块检索/ }).click();
    await page.getByRole('textbox', { name: '检索模块' }).fill('记忆档案');
    await page.getByRole('dialog').getByRole('link', { name: /Nebula/ }).click();
    await page.waitForURL('**/#/nebula'); await page.getByRole('heading', { name: 'Nebula', exact: true }).waitFor();
    console.log('PASS default archive, canvas pointer access, preview and module search');
    await page.goto(base + '#/settings');
    await page.locator('#graphics-mode').selectOption('off');
    await page.locator('#start-page').selectOption('chat');
    await page.goto(base);
    await page.getByRole('textbox', { name: '输入问题' }).waitFor();
    assert.equal(await page.locator('canvas').count(), 0);
    await page.goto(base + '#/home');
    await page.locator('.ns-static-orbits').waitFor();
    assert.equal(await page.locator('canvas').count(), 0);
    assert.deepEqual(errors, []);
    console.log('PASS entry and graphics preferences survive navigation and reload');
    const fallback = await createContext(browser, { width: 1280, height: 800 });
    await fallback.addInitScript(() => {
      const getContext = HTMLCanvasElement.prototype.getContext;
      HTMLCanvasElement.prototype.getContext = function(type, ...args) {
        if (type.includes('webgl')) throw new Error('fixture: WebGL unavailable');
        return getContext.call(this, type, ...args);
      };
    });
    const fallbackPage = await fallback.newPage();
    await fallbackPage.goto(base);
    await fallbackPage.getByText('三维预览暂不可用 · 所有模块仍可访问').waitFor();
    await fallbackPage.getByRole('link', { name: /进入AI 对话/ }).click();
    await fallbackPage.getByRole('textbox', { name: '输入问题' }).waitFor();
    await fallback.close();
    console.log('PASS unavailable WebGL keeps the actual desktop modules accessible');
    for (const viewport of [{ width: 390, height: 844 }, { width: 768, height: 600 }]) {
      const narrow = await createContext(browser, viewport, true, 'reduce');
      const np = await narrow.newPage(); await np.goto(base);
      await np.locator('.ns-static-orbits').waitFor();
      assert.ok(await np.locator('.ns-archive-app').evaluate(el => el.clientHeight <= innerHeight));
      await np.mouse.move(viewport.width / 2, viewport.height / 2);
      await np.mouse.wheel(0, 1200);
      await np.waitForFunction(() => document.querySelector('.ns-archive-app').scrollTop > 0);
      const gateway = await np.getByRole('button', { name: '选择Gateway', exact: true }).boundingBox();
      assert.ok(gateway.y >= 0 && gateway.y + gateway.height <= viewport.height);
      await narrow.close();
    }
    console.log('PASS narrow archive can be scrolled with the mouse to all five modules');
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
