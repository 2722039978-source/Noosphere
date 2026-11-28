// Render the normal chat and task routes. The native IPC fixture tracks requests across reloads.
const assert = require('node:assert/strict');
const path = require('node:path');
const { createRequire } = require('node:module');
const { chromium } = createRequire(path.resolve(__dirname, '../frontend/package.json'))('playwright');
const { createContext } = require('../frontend/tests/ui-regression.cjs');

(async () => {
  const browser = await chromium.launch({channel: process.env.QA_BROWSER || 'chromium', headless: true});
  try {
    const context = await createContext(browser, {width: 1280, height: 840});
    await context.addInitScript(() => {
      const ipc = window.__TAURI_INTERNALS__;
      const previous = ipc.invoke;
      window.qa.workspaces[0].project_binding_id = 'binding-test';
      const task = () => JSON.parse(sessionStorage.getItem('test-agent-run') || 'null');
      const write = run => sessionStorage.setItem('test-agent-run', JSON.stringify(run));
      ipc.invoke = async (name, args) => {
        const run = task();
        if (name === 'agent_grant') {
          if (args.bindingId) sessionStorage.setItem('test-agent-grant', args.bindingId);
          return {binding_id: sessionStorage.getItem('test-agent-grant') || ''};
        }
        if (name === 'submit_workspace_run' && args.kind === 'agent') {
          if (args.input.budget?.active_seconds !== 3600 || args.input.budget?.model_calls !== 96 || args.input.budget?.tool_calls !== 320 || args.input.authorized_binding_id !== 'binding-test' || args.input.mode !== 'auto' || args.input.engine_version !== 2)
            throw Error('unexpected agent authorization');
          const next = { id:'fixture-run', workspace_id:'local', kind:'agent', engine_version:2, status:'paused',
            title:'Browser report', goal:'Generate report', model_id:'qa', session_id:args.input.session_id, updated_at:new Date().toISOString() };
          write(next); return next;
        }
        if (name === 'get_workspace_run' && run) return run;
        if (name === 'list_workspace_runs' && run) return {runs:[run]};
        if (name === 'get_run_events' && run) {
          const created_at = new Date().toISOString();
          return { events:args.after ? [] : [
            {run_id:run.id,seq:1,phase:'tool_completed',message:'策略拒绝 read_file',created_at,
              data:{policy:{decision:'deny',reason:'路径超出工作区范围或属于受保护文件'}}},
            {run_id:run.id,seq:2,phase:'paused',message:'等待继续',created_at}
          ] };
        }
        if (name === 'agent_delivery') return {
          digest:'diff-v1', approval:run.status === 'waiting_approval' ? 'approval-v1' : '',
          publication_state:run.undone ? 'undone' : run.status === 'completed' ? 'published' : 'draft',
          published:run.status === 'completed', can_undo:run.status === 'completed' && !run.undone, undone:!!run.undone,
          change_offset:args.changeOffset || 0, change_total:101, next_change_offset:args.changeOffset ? undefined : 100,
          changes:args.changeOffset === 100 ? [{path:'last-report.txt',kind:'created',diff:'+last page'}] :
            Array.from({length:100}, (_, index) => ({path:`report-${index + 1}.txt`,kind:'created',diff:'+report'})),
          artifacts:run.status === 'completed' ? [{id:'doc',name:'report.txt',path:'report.txt',mime:'text/plain',size:6,sha256:'a'.repeat(64),checks:['结构检查通过'],preview:true}] : []
        };
        if (name === 'agent_control') {
          const req = args.control;
          if (!req.request_id) throw Error('missing request id');
          if (req.action === 'steer') { if (req.value !== '生成摘要') throw Error('bad steer'); }
          if (req.action === 'resume') run.status = 'waiting_approval';
          if (req.action === 'approve') {
            if (req.expected !== 'approval-v1') throw Error('bad approval');
            run.status = 'completed'; run.result = {answer:'交付完成'};
          }
          if (req.action === 'undo') {
            if (req.expected !== 'diff-v1') throw Error('bad undo digest');
            run.undone = true;
          }
          write(run); return run;
        }
        if (name === 'agent_preview') return new TextEncoder().encode('report').buffer;
        if (name === 'export_agent_artifact') return true;
        return previous(name, args);
      };
    });
    const page = await context.newPage();
    await page.goto((process.env.QA_URL || 'http://127.0.0.1:1420/') + '#/chat?task=new');
    const click = name => page.getByRole('button', {name, exact:true}).click();
    await page.getByRole('textbox',{name:'输入问题'}).fill('生成一份报告');
    await page.getByRole('button',{name:'授权此工作区'}).waitFor();
    assert.ok(await page.getByRole('button',{name:'发送',exact:true}).isDisabled());
    await click('授权此工作区');
    await page.getByRole('combobox', {name:'任务预算'}).selectOption('long');
    await click('发送');
    const inlineTimeline = page.getByRole('list', {name:'执行时间线'});
    await inlineTimeline.getByText('拒绝', {exact:true}).waitFor();
    assert.ok(await inlineTimeline.getByText('拒绝', {exact:true}).evaluate(node => node.classList.contains('text-err')));
    await inlineTimeline.getByText('路径超出工作区范围或属于受保护文件', {exact:true}).waitFor();
    await page.getByRole('link',{name:'打开任务并调整执行'}).click();
    await page.getByText('过程与产物', {exact:true}).click();
    await page.getByRole('list', {name:'执行时间线'}).getByText('拒绝', {exact:true}).waitFor();
    await page.getByRole('textbox',{name:'给当前任务补充指令'}).fill('生成摘要');
    await click('发送指令');
    await page.getByText('指令已登记，将在下一个安全步骤处理。').waitFor();
    await click('从检查点继续');
    await page.getByRole('button',{name:'确认此次操作'}).waitFor();
    await page.getByText('1–100 / 101', {exact:true}).waitFor();
    await click('下一页');
    await page.getByText('新增 · last-report.txt', {exact:true}).click();
    await page.getByText('+last page', {exact:true}).waitFor();
    assert.ok(await page.getByRole('button', {name:'下一页', exact:true}).isDisabled());
    await click('上一页');
    await page.getByText('1–100 / 101', {exact:true}).waitFor();
    await page.reload();
    await click('确认此次操作');
    await page.getByRole('heading',{name:'交付文件'}).waitFor();
    await click('预览');
    await page.locator('pre').getByText('report',{exact:true}).waitFor();
    await click('导出到文件夹');
    await page.getByText('文件已导出，SHA-256 校验通过。').waitFor();
    await click('撤销本轮全部文件改动');
    await page.getByText('本轮文件改动已撤销。').waitFor();
    console.log('PASS workspace grant, detached chat, policy timeline, diff pagination, steer, approval after reload, preview, export and undo');
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
