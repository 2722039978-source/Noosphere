// Run against `npm run dev`. Uses isolated browser storage and synthetic Tauri IPC only.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const base = process.env.QA_URL || 'http://127.0.0.1:1420/';
const output = process.env.QA_OUTPUT || path.resolve('qa-output');
fs.mkdirSync(output, { recursive: true });
const passed = [], errors = [];
async function check(name, action) { await action(); passed.push(name); console.log('PASS ' + name); }
async function readStore(page) { return page.evaluate(async () => (await window.qa.store()).useChatStore.getState().sessions); }
async function emit(page, index, event) { await page.evaluate(({ index, event }) => window.qa.emit(index, event), { index, event }); }
async function waitStatus(page, status) { await page.waitForFunction(async (status) => { const state = (await window.qa.store()).useChatStore.getState(); return state.sessions.find(s => s.id === state.activeId)?.messages.at(-1)?.status === status; }, status); }
async function send(page, content) { await page.getByRole('textbox', { name: '输入问题', exact: true }).fill(content); await page.getByRole('button', { name: '发送', exact: true }).click(); await page.getByRole('button', { name: '停止生成', exact: true }).waitFor(); }
async function createContext(browser, viewport, mock = true, reducedMotion) {
  const context = await browser.newContext({ viewport, reducedMotion });
  if (mock) await context.addInitScript(() => {
    const callbacks = new Map(); let callbackId = 0;
    window.qa = { runs: [], cancels: [], holdContext: false, contextResolvers: [], failStorage: false,
      workspaces: [{id:'local',name:'本地工作区',project_path:'fixture',project_id:'code-fixture',default_model_id:'',created_at:'2026-10-08T00:00:00Z',updated_at:'2026-10-08T00:00:00Z'}], tasks: [], holdTasks: false,
      store() { const source = performance.getEntriesByType('resource').find(entry => new URL(entry.name).pathname === '/src/features/chat/chat-store.ts')?.name ?? '/src/features/chat/chat-store.ts'; return import(source); },
      emit(index, event) { const run = this.runs[index]; window.__TAURI_INTERNALS__.runCallback(run.onEvent.id, { index: run.index++, message: { request_id: run.requestId, ...event } }); },
      releaseContext() { this.holdContext = false; this.contextResolvers.splice(0).forEach(resolve => resolve({ results: [] })); },
    };
    if (location.protocol === 'http:') localStorage.setItem('noosphere-project-v1', JSON.stringify({state:{projectPath:'fixture',projectId:'code-fixture'},version:0}));
    const transaction = IDBDatabase.prototype.transaction;
    IDBDatabase.prototype.transaction = function (...args) { if (args[1] === 'readwrite' && window.qa.failStorage) throw new DOMException('Test quota', 'QuotaExceededError'); return transaction.apply(this, args); };
    window.__TAURI_INTERNALS__ = {
      transformCallback(fn) { callbacks.set(++callbackId, fn); return callbackId; },
      unregisterCallback(id) { callbacks.delete(id); },
      runCallback(id, value) { callbacks.get(id)?.(value); },
      async invoke(command, payload) {
        const qa = window.qa;
        switch (command) {
          case 'check_health': return { status: 'ok', modules: Object.fromEntries(['gateway', 'nebula', 'devops', 'codelens'].map(name => [name, { name, state: 'running' }])) };
          case 'get_backend_status': return { running: true, restarts: 0 };
          case 'list_workspaces': return {workspaces:qa.workspaces};
          case 'save_workspace': {
            const input=payload.workspace, previous=qa.workspaces.find(w=>w.id===input.id);
            const workspace={...input,id:input.id||crypto.randomUUID(),project_id:previous?.project_path===input.project_path?previous.project_id:'',created_at:previous?.created_at||new Date().toISOString(),updated_at:new Date().toISOString()};
            qa.workspaces=qa.workspaces.filter(w=>w.id!==workspace.id).concat(workspace);return workspace;
          }
          case 'submit_workspace_run': {
            const run={id:crypto.randomUUID(),workspace_id:payload.workspaceId,kind:payload.kind,title:payload.title,status:qa.holdTasks?'running':'completed',phase:qa.holdTasks?'planning':'completed',created_at:new Date().toISOString(),updated_at:new Date().toISOString(),error:'',result:{answer:'夹具诊断：Spooler 运行正常。',trace:[{thought:'读取服务状态',action:'service_status',args:{name:'Spooler'},result:{success:true,output:'RUNNING'}}]}};
            qa.tasks.push(run);return run;
          }
          case 'list_workspace_runs': return {runs:qa.tasks.filter(r=>r.workspace_id===payload.workspaceId).map(r=>({...r,result:null}))};
          case 'get_workspace_run': return qa.tasks.find(r=>r.id===payload.id);
          case 'get_run_events': return {events:payload.after?[]:[{seq:1,run_id:payload.id,phase:'planning',message:'正在分析诊断问题',data:null,created_at:new Date().toISOString()}]};
          case 'cancel_workspace_run': {const run=qa.tasks.find(r=>r.id===payload.id);run.status='cancelled';run.phase='cancelled';run.result=null;return run;}
          case 'get_models': return { models: [{ id: 'qa', name: '测试模型', model_name: 'fixture', provider: 'openai_compatible', base_url: 'http://fixture.invalid', enabled: true, is_default: true }] };
          case 'list_agent_actions': return {actions:[]};
          case 'list_knowledge': return {memories:[]};
          case 'prepare_context': {
            const result={workspace_id:payload.workspaceId,project_id:qa.workspaces.find(w=>w.id===payload.workspaceId)?.project_id||'',context:'',citations:[],warnings:[],mode:'lexical'};
            return qa.holdContext ? new Promise(resolve=>qa.contextResolvers.push(()=>resolve(result))) : result;
          }
          case 'search_memories': return qa.holdContext ? new Promise(resolve => qa.contextResolvers.push(resolve)) : { results: [] };
          case 'gateway_chat_stream': qa.runs.push({ ...payload, index: 0 }); return;
          case 'gateway_chat_cancel': qa.cancels.push(payload.requestId); return;
          case 'list_sessions': return { sessions: [] };
          case 'get_metrics': return { cpu: { usage_percent: 18, cores: 8 }, memory: { usage_percent: 42 }, disk: { usage_percent: 32 }, process_count: 100 };
          case 'codelens_health': return { status: 'ok' };
          case 'list_tools': return { tools: [] };
          case 'devops_loop': return { answer: '夹具诊断：Spooler 运行正常。', trace: [{thought:'读取服务状态',action:'service_status',args:{name:'Spooler'},result:{success:true,output:'RUNNING'}}] };
          default: return {};
        }
      },
    };
  });
  context.on('page', page => { page.on('pageerror', error => errors.push(error.message)); page.on('console', message => { if (message.type() === 'error') errors.push(message.text()); }); });
  return context;
}
module.exports = { createContext, errors };
if (require.main === module) (async () => {
  const browser = await chromium.launch({ channel: process.env.QA_BROWSER || 'msedge', headless: true });
  try {
    const context = await createContext(browser, { width: 1440, height: 960 });
    const page = await context.newPage(); await page.goto(base + '#/chat'); await page.getByRole('textbox', { name: '输入问题' }).waitFor();
    await check('stop while retrieving context never starts a model request', async () => {
      await page.evaluate(() => { window.qa.holdContext = true; }); await send(page, '准备阶段取消');
      assert.equal(await page.evaluate(() => window.qa.runs.length), 0);
      await page.getByRole('button', { name: '停止生成' }).click(); await waitStatus(page, 'cancelled');
      await page.evaluate(() => window.qa.releaseContext()); await page.waitForTimeout(80);
      assert.equal(await page.evaluate(() => window.qa.runs.length), 0); await page.getByText('已停止生成', { exact: true }).waitFor();
    });
    await check('stream survives route changes and final content replaces partial text', async () => {
      await send(page, '说明这个项目'); await page.waitForFunction(() => window.qa.runs.length === 1);
      await emit(page, 0, { token: '初始片段' }); await page.waitForTimeout(60);
      await page.getByRole('navigation', { name: '主导航' }).getByRole('link', { name: /工作台/ }).click();
      await emit(page, 0, { token: '接着输出' }); await emit(page, 0, { done: true, status: 'completed', content: '最终完整回答，不重复。' });
      await page.getByRole('link', { name: 'AI 对话', exact: true }).click();
      await waitStatus(page, 'completed'); assert.equal((await readStore(page))[0].messages.at(-1).content, '最终完整回答，不重复。');
    });
    await check('cancel flushes partial text and old callbacks cannot affect a new request', async () => {
      await send(page, '停止后继续'); await page.waitForFunction(() => window.qa.runs.length === 2);
      await emit(page, 1, { token: '保留的半段回答' }); await page.getByRole('button', { name: '停止生成' }).click(); await waitStatus(page, 'cancelled');
      assert.equal((await readStore(page))[0].messages.at(-1).content, '保留的半段回答');
      await send(page, '新的请求'); await page.waitForFunction(() => window.qa.runs.length === 3);
      await emit(page, 1, { token: '不应出现', done: true, error: '迟到错误' });
      assert.ok(await page.getByRole('button', { name: '停止生成' }).isVisible());
      await emit(page, 2, { content: '新的正确回答', status: 'completed', done: true }); await waitStatus(page, 'completed');
      assert.ok(!(await readStore(page)).flatMap(s => s.messages).some(m => m.content.includes('不应出现')));
    });
    await check('switching sessions keeps request ownership and source deletion cancels it', async () => {
      const source = (await readStore(page))[0]; await send(page, '跨会话生成'); await page.waitForFunction(() => window.qa.runs.length === 4);
      await page.getByRole('button', { name: '新会话', exact: true }).click();
      await page.getByRole('button', { name: /返回生成中的会话/ }).waitFor();
      await page.getByRole('textbox', { name: '输入问题' }).fill('不应提交'); assert.ok(await page.getByRole('button', { name: '发送', exact: true }).isDisabled());
      await page.getByRole('button', { name: '删除会话 ' + source.title, exact: true }).click();
      await page.waitForFunction(() => window.qa.cancels.length === 2);
      await emit(page, 3, { token: '删除后迟到', done: true }); assert.ok(!(await readStore(page)).some(s => s.id === source.id));
      await page.evaluate(async () => await (await window.qa.store()).flushChatPersistence());
      await page.reload(); await page.getByRole('textbox', { name: '输入问题' }).waitFor(); assert.ok(!(await readStore(page)).some(s => s.id === source.id));
    });
    await check('failed output, quota recovery, and reload preserve terminal state', async () => {
      await page.evaluate(() => { window.qa.failStorage = true; }); await send(page, '保存失败恢复'); await page.waitForFunction(() => window.qa.runs.length === 1);
      await emit(page, 0, { token: '已收到的内容' }); await emit(page, 0, { done: true, status: 'failed', error: '模拟连接中断' }); await waitStatus(page, 'failed');
      await page.getByRole('alert').filter({ hasText: '会话存储未完成' }).waitFor();
      await page.evaluate(() => { window.qa.failStorage = false; }); await page.getByRole('alert').getByRole('button', { name: '重试' }).click();
      await page.getByRole('alert').waitFor({ state: 'hidden' });
      await page.screenshot({ path: path.join(output, 'rhine-chat-response.png'), animations: 'disabled' });
      await page.reload(); await waitStatus(page, 'failed'); assert.equal((await readStore(page))[0].messages.at(-1).content, '已收到的内容');
    });
    await check('unfinished persisted generation becomes interrupted without automatic retry', async () => {
      await page.evaluate(async () => { const { useChatStore, flushChatPersistence } = await window.qa.store(); const store = useChatStore.getState(); store.appendMessage(store.activeId, { id: 'interrupted-fixture', role: 'assistant', content: '重启前的片段', streaming: true, createdAt: Date.now() }); await flushChatPersistence(); });
      await page.reload(); await waitStatus(page, 'interrupted'); assert.equal(await page.evaluate(() => window.qa.runs.length), 0);
    });
    await check('desktop archive selection, keyboard controls, and real module links', async () => {
      await page.goto(base + '#/home'); await page.locator('.ns-full-scene[data-loaded="true"]').waitFor();
      await page.locator('.ns-boot').waitFor({ state: 'hidden' });
      await page.locator('.ns-return-stage[data-phase="idle"]').waitFor();
      await page.getByRole('button', { name: '下一个档案' }).focus(); await page.keyboard.press('Enter');
      await page.locator('.ns-file-title h1').filter({ hasText: 'CodeLens' }).waitFor();
      assert.equal(await page.locator('.ns-archive-module-nav button').count(), 5);
      await page.getByRole('button', { name: '上一个档案' }).click();
      await page.getByRole('heading', {name:'AI 对话',exact:true}).waitFor();
      await page.getByRole('button', { name: '选择CodeLens', exact:true }).click();
      await page.screenshot({ path: path.join(output, 'rhine-home-desktop.png'), animations: 'disabled' });
      await page.getByRole('link', {name:/进入CodeLens/}).click(); await page.waitForURL('**/#/codelens'); await page.getByRole('heading', { name: 'CodeLens', exact: true }).waitFor(); assert.equal(await page.locator('canvas').count(), 0);
    });
    const phoneContext = await createContext(browser, { width: 390, height: 844 }); const phone = await phoneContext.newPage();
    await check('seven routes fit both desktop and phone without document overflow', async () => {
      for (const route of ['home', 'chat', 'codelens', 'nebula', 'devops', 'gateway', 'settings']) {
        for (const target of [page, phone]) {
          await target.goto(base + '#/' + route); await target.locator('main h1').waitFor();
          assert.ok(await target.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${route} document overflow`);
          assert.equal(await target.locator('vite-error-overlay').count(), 0);
        }
      }
      await phone.goto(base + '#/home'); await phone.locator('.ns-static-orbits').waitFor(); assert.equal(await phone.locator('canvas').count(), 0);
      await phone.screenshot({ path: path.join(output, 'rhine-home-mobile.png'), animations: 'disabled' });
      await phone.goto(base + '#/chat'); await phone.getByRole('textbox', { name: '输入问题' }).waitFor();
      await phone.screenshot({ path: path.join(output, 'rhine-chat-mobile.png'), animations: 'disabled' });
    });
    await check('mobile menus, default model picker, and compose remain usable', async () => {
      await phone.getByRole('button', { name: '打开主导航' }).click(); const dialog = phone.getByRole('dialog', { name: '主导航' });
      await dialog.getByRole('link', { name: /Gateway/ }).click(); await dialog.waitFor({ state: 'hidden' });
      await phone.getByRole('button', { name: '添加模型', exact: true }).click(); await phone.getByPlaceholder('显示名称', { exact: true }).fill('仅界面检查');
      assert.ok(await phone.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      await phone.goto(base + '#/chat'); await phone.getByRole('combobox').click(); await phone.getByRole('option', { name: '测试模型', exact: true }).click();
      await phone.getByRole('button', { name: '打开会话历史' }).click(); await phone.getByRole('dialog', { name: '会话历史' }).getByRole('button', { name: '新会话', exact: true }).click();
      await phone.getByRole('dialog').waitFor({ state: 'hidden' }); await send(phone, '手机停止检查'); await phone.getByRole('button', { name: '停止生成' }).click(); await waitStatus(phone, 'cancelled');
    });
    await check('reduced-motion desktop uses the static archive', async () => {
      const reduced = await createContext(browser, { width: 1440, height: 960 }, true, 'reduce'); const rp = await reduced.newPage(); await rp.goto(base + '#/home'); await rp.locator('.ns-static-orbits').waitFor(); assert.equal(await rp.locator('canvas').count(), 0); await reduced.close();
    });
    await check('dark theme persists and the native minimum viewport remains usable', async () => {
      await page.goto(base + '#/settings'); await page.getByRole('button', { name: '深色模式', exact: true }).click();
      await page.reload(); assert.ok(await page.locator('html').evaluate(el => el.classList.contains('dark')));
      await page.setViewportSize({ width: 900, height: 600 }); await page.goto(base + '#/chat'); await page.getByRole('textbox', { name: '输入问题' }).waitFor();
      const box = await page.getByRole('textbox', { name: '输入问题' }).boundingBox(); assert.ok(box.y + box.height < 600);
      await page.screenshot({ path: path.join(output, 'rhine-chat-dark.png'), animations: 'disabled' });
    });
    await check('backup export and validated import merge into durable conversations', async () => {
      await page.goto(base + '#/settings');
      const downloadPromise = page.waitForEvent('download');
      await page.getByRole('button', {name:'导出会话',exact:true}).click();
      const download = await downloadPromise; const exported = JSON.parse(fs.readFileSync(await download.path(), 'utf8'));
      assert.equal(exported.format, 'noosphere-chat-backup'); assert.ok(exported.state.sessions.length);
      const imported = {format:'noosphere-chat-backup',version:1,state:{sessions:[{id:'import-fixture',title:'导入会话夹具',modelId:'',createdAt:1,updatedAt:1,messages:[]}],activeId:'import-fixture'}};
      await page.getByLabel('选择会话备份').setInputFiles({name:'fixture.json',mimeType:'application/json',buffer:Buffer.from(JSON.stringify(imported))});
      await page.getByText('会话已合并并保存。',{exact:true}).waitFor();
      await page.reload();
      await page.waitForFunction(async () => (await window.qa.store()).useChatStore.getState().sessions.some(s=>s.id==='import-fixture'));
      await page.screenshot({path:path.join(output,'hardening-backup.png'),animations:'disabled'});
    });
    await check('diagnostic Agent renders tool trace from desktop bridge result', async () => {
      await page.goto(base + '#/devops');
      await page.getByLabel('诊断问题').fill('检查 Spooler 服务');
      await page.getByRole('button',{name:'开始诊断',exact:true}).click();
      await page.getByText('夹具诊断：Spooler 运行正常。',{exact:true}).waitFor();
      await page.locator('summary').filter({hasText:'service_status'}).click();
      await page.getByText('RUNNING',{exact:true}).waitFor();
      await page.screenshot({path:path.join(output,'hardening-diagnostic.png'),animations:'disabled'});
    });
    await check('browser reports no runtime errors', async () => assert.deepEqual(errors, []));
    fs.writeFileSync(path.join(output, 'ui-regression-results.json'), JSON.stringify({ passed: passed.length, checks: passed, errors }, null, 2));
    console.log(JSON.stringify({ passed: passed.length, output }));
    await context.close(); await phoneContext.close();
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
