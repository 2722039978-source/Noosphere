// Exercises the renderer and real stores against explicit desktop IPC fixtures.
const assert = require('node:assert/strict');
const { chromium } = require('playwright');
const { createContext, errors } = require('./ui-regression.cjs');

async function main() {
  const browser = await chromium.launch({ channel: process.env.QA_BROWSER || 'msedge', headless: true });
  try {
    const context = await createContext(browser, { width: 1220, height: 860 });
    await context.addInitScript(() => {
      if (location.protocol === 'http:') localStorage.setItem('noosphere-local-engine-permissions', JSON.stringify({ version: 0, state: {
        grants: { legacy: ['bonsai', '/portable', 'openai_compatible', 'http://127.0.0.1:18787/v1', 'bonsai-local-27b'].join('\n') },
      } }));
      const original = window.__TAURI_INTERNALS__.invoke;
      const records = [], connections = [], runtimes = [];
      const runtimePath = '/opt/fixture/bin/llama-server';
      let active = '';
      window.qa.authorized = [];
      window.qa.trace = [];
      window.qa.launches = [];
      const snapshot = () => ({ version: 1, models: records, runtimes, candidates: [runtimePath], warnings: [] });
      const engine = () => ({
        id: 'custom_llama', name: 'GGUF', provider: 'openai_compatible', base_url: 'http://127.0.0.1:18789/v1',
        managed: !!active, state: active ? 'online' : 'stopped', installed: !!runtimes.length, can_start: !!runtimes.length,
        models: records.map(row => ({ id: row.id, name: row.name, launch_identity: row.launch_identity, loaded: active === row.id, vision: row.id === 'alias-alpha' ? false : undefined })),
      });
      window.__TAURI_INTERNALS__.invoke = async (cmd, payload, ...rest) => {
        window.qa.trace.push(cmd);
        if (cmd === 'model_engineering' && payload.request.operation === 'list_models') return { models: [], next_cursor: '' };
        if (cmd === 'get_models') return { models: structuredClone(connections) };
        if (cmd === 'model_library_snapshot') return structuredClone(snapshot());
        if (cmd === 'list_local_engines') return { engines: [engine()] };
        if (cmd === 'pick_model_library_files') return ['/weights/alpha.gguf', '/weights/beta.gguf'];
        if (cmd === 'modify_model_library') {
          const { change } = payload;
          if (change.action === 'import') {
            for (const path of change.paths) {
              const name = path.split('/').pop().split('.')[0];
              records.push({ id: 'alias-' + name, name, path, size: 5000000, suggested_context: 2048,
                status: 'runtime_missing', launch_identity: name + '/runtime-1' });
            }
          } else if (change.action === 'authorize_runtime') {
            const allowed = await new Promise(resolve => { window.qa.runtimeConsent = resolve; });
            if (!allowed) throw Error('用户取消了授权');
            window.qa.authorized.push(change.path);
            runtimes.push({ id: 'runtime-1', path: change.path, version: 'fixture', status: 'ready', devices: ['CPU'] });
            for (const row of records) row.status = 'ready';
          } else if (change.action === 'forget') records.splice(records.findIndex(row => row.id === change.id), 1);
          else throw new Error('unexpected mutation ' + change.action);
          return structuredClone(snapshot());
        }
        if (cmd === 'control_local_engine') {
          window.qa.launches.push([payload.action, payload.model]);
          if (payload.action !== 'stop' && (!runtimes.length || !records.some(row => row.id === payload.model))) throw Error('invalid launch');
          if (payload.action !== 'stop' && payload.expectedIdentity !== records.find(row => row.id === payload.model).launch_identity) throw Error('stale launch consent');
          active = payload.action === 'stop' ? '' : payload.model;
          return engine();
        }
        if (cmd === 'add_model') { connections.push(payload.model); window.qa.config = payload.model; return {}; }
        if (cmd === 'probe_model' || cmd === 'test_model') return { status: 'ok', inference: 'succeeded' };
        if (cmd === 'stage_chat_attachment') {
          const info = JSON.parse(decodeURIComponent(rest[0].headers['x-noosphere-attachment']));
          return { received: info.size, attachment: { id: info.id, name: info.name, size: info.size, source_hash: '0'.repeat(64) } };
        }
        return original(cmd, payload, ...rest);
      };
    });
    const page = await context.newPage();
    await page.goto((process.env.QA_URL || 'http://127.0.0.1:1420/') + '#/model-hub');
    const button = (name) => page.getByRole('button', { name, exact: true });
    await button('导入 GGUF').click();
    await page.getByText('请选择或安装运行时。', { exact: false }).waitFor();
    assert.equal(await button('启动并使用').isDisabled(), true);
    await page.getByRole('searchbox').fill('beta');
    assert.equal(await page.getByLabel('本地模型列表').getByRole('button').count(), 1);
    await page.getByRole('searchbox').fill('');
    await button('授权此运行时').click();
    await page.waitForFunction(() => typeof window.qa.runtimeConsent === 'function');
    assert.deepEqual(await page.evaluate(() => window.qa.authorized), []);
    await page.evaluate(() => window.qa.runtimeConsent(true));
    await button('启动并使用').click();
    const picker = page.getByRole('combobox', { name: '选择对话模型' });
    await picker.getByText('alpha', { exact: true }).waitFor();
    assert.deepEqual(await page.evaluate(() => window.qa.config.protocol_options), { tools: false });
    assert.equal(await page.evaluate(() => window.qa.config.metadata.features.vision), 'unsupported');

    const input = page.getByRole('textbox', { name: '输入问题' });
    await input.fill('本地推理');
    await button('发送').click();
    await page.waitForFunction(() => window.qa.runs.length === 1, null, { timeout: 8000 }).catch(async error => {
      console.error('GGUF stream did not start', { page: await page.locator('body').innerText(), text: await input.inputValue(), trace: await page.evaluate(() => window.qa.trace), errors });
      throw error;
    });
    await page.evaluate(() => {
      window.qa.emit(0, { token: '本地' });
      window.qa.emit(0, { content: '本地回答', done: true, status: 'completed' });
    });
    await page.getByText('本地回答', { exact: true }).waitFor();
    await page.getByLabel('对话附件').setInputFiles({ name: 'scene.png', mimeType: 'image/png', buffer: Buffer.from([137, 80, 78, 71]) });
    await page.getByText(/当前模型尚未确认支持图像/).waitFor();
    await input.fill('查看图片');
    assert.equal(await button('发送').isDisabled(), true);
    await button('移除附件 scene.png').click();
    await picker.click();
    await page.getByRole('option', { name: /beta/ }).click();
    await button('切换并启动').click();
    await picker.getByText('beta', { exact: true }).waitFor();
    assert.equal(await page.evaluate(() => window.qa.config.metadata.features.vision), undefined);
    assert.deepEqual(await page.evaluate(() => window.qa.launches), [['start', 'alias-alpha'], ['switch', 'alias-beta']]);

    await page.getByRole('navigation', { name: '主导航' }).getByRole('link', { name: '模型', exact: true }).click();
    await page.getByLabel('本地模型列表').getByRole('button', { name: /alpha/ }).click();
    await page.getByText('模型高级设置', { exact: true }).click();
    await button('移除模型记录').click();
    await button('确认移除记录').click();
    await page.getByText('模型高级设置', { exact: true }).waitFor();
    await page.getByLabel('本地模型列表').getByRole('button', { name: /alpha/ }).waitFor({ state: 'detached' });
    assert.equal(await page.evaluate(() => window.qa.authorized.length), 1);
    await page.getByRole('navigation', { name: '模型导航' }).getByRole('link', { name: '高级管理' }).click();
    await button('手动配置').click();
    const form = page.getByRole('form', { name: '新增模型接入' });
    await form.getByLabel('模型 ID', { exact: true }).fill('remote-model');
    await form.getByLabel('接口地址').fill('https://relay.example/v1');
    await form.getByLabel('流式响应').selectOption('false');
    await form.getByLabel('结构化输出').selectOption('true');
    await form.getByLabel('工具调用', { exact: true }).selectOption('true');
    await form.getByLabel('工具调用接口').selectOption('responses');
    await form.getByLabel('temperature 参数').selectOption('true');
    await form.getByLabel('温度', { exact: true }).fill('0');
    await form.getByLabel('top_p').fill('0.75');
    await form.getByLabel('最大输出 tokens').fill('128');
    await form.getByLabel('输出长度参数').selectOption('max_completion_tokens');
    await form.getByRole('button', { name: '连接模型' }).click();
    await form.waitFor({ state: 'detached' });
    const saved = await page.evaluate(() => window.qa.config);
    assert.equal(saved.model_name, 'remote-model');
    assert.equal(saved.temperature, 0);
    const grants = await page.evaluate(() => JSON.parse(localStorage.getItem('noosphere-local-engine-permissions')).state.grants);
    assert.equal(grants.legacy, ['bonsai', '/portable', 'openai_compatible', 'http://127.0.0.1:18787/v1', 'bonsai-local-27b'].join('\n'));
    assert.equal(saved.max_tokens, 128);
    assert.equal(saved.max_tokens_field, 'max_completion_tokens');
    assert.equal(saved.tool_transport, 'responses');
    assert.deepEqual(saved.protocol_options, { streaming: false, structured_output: true, tools: true, top_p: 0.75 });
    assert.deepEqual(errors, []);
    console.log('PASS GGUF batch import, trust prompt, model switch, stream, image capability gate and removal');
  } finally { await browser.close(); }
}
main().catch(error => { console.error(error); process.exitCode = 1; });
