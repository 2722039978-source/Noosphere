// Isolated, synthetic Tauri IPC: never reads the user's model configuration.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const base = process.env.QA_URL || 'http://127.0.0.1:1420/';
const output = process.env.QA_OUTPUT || path.resolve(__dirname, '../../.local/qa/startup');
fs.mkdirSync(output, { recursive: true });
const errors = [], passed = [];
async function check(name, action) { await action(); passed.push(name); console.log('PASS ' + name); }
async function mockContext(browser, { legacy = false, viewport = { width: 1280, height: 800 } } = {}) {
  const context = await browser.newContext({ viewport, reducedMotion: 'reduce' });
  await context.addInitScript(({ legacy }) => {
    window.startupQA = {
      online: legacy, starts: 0, healthChecks: 0, modelLoads: 0, sessionLoads: 0,
      nativeError: legacy ? null : '无法启动本地后端：可执行文件缺失（测试夹具）',
      failNextStart: false, releaseStart: null,
    };
    window.__TAURI_INTERNALS__ = {
      async invoke(command) {
        const qa = window.startupQA;
        switch (command) {
          case 'check_health':
            qa.healthChecks++;
            if (!qa.online) throw { message: '连接本地服务失败（测试夹具）' };
            return { status: 'ok', modules: { gateway: { name: 'gateway', state: 'running' } } };
          case 'get_backend_status':
            return legacy ? { running: qa.online, restarts: 0 } : {
              running: qa.online, restarts: qa.starts, error: qa.nativeError,
              data_dir: 'C:\\Users\\fixture\\AppData\\Local\\Noosphere\\data',
            };
          case 'start_backend':
            qa.starts++;
            if (qa.failNextStart) { qa.failNextStart = false; throw { message: '重试启动失败：数据目录无法写入（测试夹具）' }; }
            return new Promise(resolve => { qa.releaseStart = () => { qa.online = true; qa.nativeError = null; resolve({ status: 'started' }); }; });
          case 'get_models': qa.modelLoads++; return { models: [] };
          case 'list_sessions': qa.sessionLoads++; return { sessions: qa.online ? [{ id: 'synthetic-memory', name: '恢复后的测试记忆会话', created_at: '2026-09-09', memory_count: 0 }] : [] };
          case 'get_metrics': return { cpu: { usage_percent: 0 }, memory: { usage_percent: 0 }, disk: { usage_percent: 0 } };
          default: return {};
        }
      },
    };
  }, { legacy });
  context.on('page', page => {
    page.on('pageerror', error => errors.push(error.message));
    page.on('console', message => { if (message.type() === 'error') errors.push(message.text()); });
  });
  return context;
}
(async () => {
  const browser = await chromium.launch({ channel: 'msedge', headless: true });
  try {
    const oldContext = await mockContext(browser, { legacy: true });
    const oldPage = await oldContext.newPage();
    await check('legacy running/restarts-only status is compatible', async () => {
      await oldPage.goto(base + '#/settings');
      await oldPage.locator('footer').getByText('后端在线', { exact: true }).waitFor();
      await oldPage.getByText('等待桌面服务提供路径', { exact: true }).waitFor();
      assert.equal(await oldPage.getByRole('button', { name: '重试启动', exact: true }).count(), 0);
      assert.equal(await oldPage.locator('vite-error-overlay').count(), 0);
    });
    const context = await mockContext(browser); const page = await context.newPage();
    await check('startup failure shows the actual native error and data directory', async () => {
      await page.goto(base + '#/settings');
      assert.match(await page.title(), /Noosphere/);
      await page.locator('main [role="status"]').getByText('无法启动本地后端：可执行文件缺失（测试夹具）', { exact: true }).waitFor();
      await page.getByText('C:\\Users\\fixture\\AppData\\Local\\Noosphere\\data', { exact: true }).waitFor();
      assert.equal(await page.getByRole('button', { name: '重试启动', exact: true }).count(), 2);
      await page.screenshot({ path: path.join(output, 'backend-startup-error-desktop.png') });
    });
    await check('retry is shared and disabled until the single startup request completes', async () => {
      const before = await page.evaluate(() => ({ models: startupQA.modelLoads, sessions: startupQA.sessionLoads }));
      await page.locator('footer').getByRole('button', { name: '重试启动' }).click();
      await page.waitForFunction(() => typeof startupQA.releaseStart === 'function');
      assert.equal(await page.getByRole('button', { name: '启动中…', exact: true }).count(), 2);
      for (const button of await page.getByRole('button', { name: '启动中…', exact: true }).all()) assert.ok(await button.isDisabled());
      assert.equal(await page.evaluate(() => startupQA.starts), 1);
      await page.evaluate(() => startupQA.releaseStart());
      await page.locator('footer').getByText('后端在线', { exact: true }).waitFor();
      await page.waitForFunction(({ models, sessions }) => startupQA.modelLoads > models && startupQA.sessionLoads > sessions, before);
      assert.equal(await page.getByRole('button', { name: '重试启动', exact: true }).count(), 0);
      assert.equal(await page.locator('footer a[title]').count(), 0);
    });
    await check('passive polling refreshes cached resources after the service recovers', async () => {
      await page.evaluate(() => { startupQA.online = false; startupQA.nativeError = '测试：进程意外退出'; });
      await page.locator('footer').getByText('后端离线', { exact: true }).waitFor({ timeout: 15000 });
      const before = await page.evaluate(() => ({ models: startupQA.modelLoads, sessions: startupQA.sessionLoads }));
      await page.evaluate(() => { startupQA.online = true; startupQA.nativeError = null; });
      await page.locator('footer').getByText('后端在线', { exact: true }).waitFor({ timeout: 7000 });
      await page.waitForFunction(({ models, sessions }) => startupQA.modelLoads > models && startupQA.sessionLoads > sessions, before);
      assert.equal(await page.evaluate(() => startupQA.starts), 1);
    });
    const phoneContext = await mockContext(browser, { viewport: { width: 390, height: 844 } }); const phone = await phoneContext.newPage();
    await check('390px error state keeps the compact footer and retry visible', async () => {
      await phone.goto(base + '#/settings'); await phone.locator('main [role="status"]').waitFor();
      assert.ok(await phone.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      const footer = await phone.locator('footer').boundingBox(); const retry = await phone.locator('footer').getByRole('button', { name: '重试启动' }).boundingBox();
      assert.ok(retry.x >= 0 && retry.x + retry.width <= 390); assert.ok(footer.height <= 48);
      assert.ok(retry.y >= footer.y && retry.y + retry.height <= footer.y + footer.height);
      await phone.screenshot({ path: path.join(output, 'backend-startup-error-mobile.png') });
    });
    await check('failed retry retains the actionable error and allows another attempt', async () => {
      await phone.evaluate(() => { startupQA.failNextStart = true; });
      await phone.locator('main').getByRole('button', { name: '重试启动' }).click();
      await phone.locator('main [role="status"]').getByText('重试启动失败：数据目录无法写入（测试夹具）', { exact: true }).waitFor();
      assert.ok(await phone.locator('footer').getByRole('button', { name: '重试启动' }).isEnabled());
      assert.equal(await phone.evaluate(() => startupQA.starts), 1);
    });
    await check('rendered target flow has no runtime errors or framework overlay', async () => {
      assert.deepEqual(errors, []); assert.equal(await page.locator('vite-error-overlay').count(), 0);
      assert.equal(await phone.locator('vite-error-overlay').count(), 0);
    });
    fs.writeFileSync(path.join(output, 'backend-startup-feedback-results.json'), JSON.stringify({ passed, errors }, null, 2));
    await oldContext.close(); await context.close(); await phoneContext.close();
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
