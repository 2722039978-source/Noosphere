// Browser plugin unavailable; isolated Playwright, real React UI with synthetic desktop IPC.
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const {createContext,errors}=require('./ui-regression.cjs');
const assert=require('node:assert/strict');
const base=process.env.QA_URL||'http://127.0.0.1:1420/';
const passed=[];
async function check(name,fn){await fn();passed.push(name);console.log('PASS '+name)}
(async()=>{
 const browser=await chromium.launch({channel:'msedge',headless:true});
 try {
  const context=await createContext(browser,{width:1280,height:900});
  await context.addInitScript(()=>{
   if(location.protocol!=='http:')return;
   const invoke=window.__TAURI_INTERNALS__.invoke;
   window.qa.agentCancels=[];
   window.qa.tasks=JSON.parse(sessionStorage.getItem('agent-fixture-tasks')||'[]');
   const evidence={id:'E1',kind:'code',title:'main.go',file:'main.go',start_line:1,end_line:2,source_hash:'a'.repeat(64),excerpt:'package main\nfunc start() {}',observed_at:'2026-10-11T00:00:00Z'};
   const step={number:1,tool:'project_read_file',duration_ms:3,result:{success:true,evidence:[evidence]}};
   window.__TAURI_INTERNALS__.invoke=async(command,payload)=>{
    if(command==='submit_workspace_run'&&payload.kind==='agent'){
     if(window.qa.holdAck)await new Promise(resolve=>window.qa.releaseAck=resolve);
     const run={id:crypto.randomUUID(),workspace_id:payload.workspaceId,kind:'agent',title:payload.title,status:window.qa.holdAgent?'running':'completed',phase:'tool_completed',error:'',created_at:new Date().toISOString(),updated_at:new Date().toISOString(),result:window.qa.holdAgent?null:{answer:'启动函数为空 [E1]。',steps:[step],evidence:[evidence],model_calls:2,tool_calls:1}};
     window.qa.tasks.push(run);sessionStorage.setItem('agent-fixture-tasks',JSON.stringify(window.qa.tasks));return run;
    }
    if(command==='get_run_events'&&window.qa.tasks.find(t=>t.id===payload.id)?.kind==='agent')return {events:payload.after?[]:[{seq:1,phase:'tool_completed',message:'工具完成 · project_read_file',data:step}]};
    if(command==='cancel_workspace_run')window.qa.agentCancels.push(payload.id);
    return invoke(command,payload);
   };
  });
  const page=await context.newPage();await page.goto(base+'#/chat');
  await check('chat executes durable Agent and exposes verifiable file evidence',async()=>{
   await page.getByRole('button',{name:'只读 Agent',exact:true}).click();
   await page.getByRole('textbox',{name:'输入问题',exact:true}).fill('检查启动入口');await page.getByRole('button',{name:'发送',exact:true}).click();
   await page.getByText('启动函数为空 [E1]。',{exact:true}).waitFor();
   assert.equal(await page.evaluate(()=>window.qa.runs.length),0);
   await page.getByText('Agent 执行记录',{exact:true}).click();await page.getByText('[E1] main.go:1–2',{exact:true}).click();await page.getByText('SHA-256:',{exact:false}).waitFor();
   await page.evaluate(async()=>await (await window.qa.store()).flushChatPersistence());
  });
  await check('reload reads saved task without replaying and regeneration keeps Agent mode',async()=>{
   await page.reload();await page.getByText('启动函数为空 [E1]。',{exact:true}).waitFor();await page.getByText('Agent 执行记录',{exact:true}).click();await page.getByText('[E1] main.go:1–2',{exact:true}).waitFor();
   assert.equal(await page.evaluate(()=>window.qa.tasks.length),1);
   await page.getByRole('button',{name:'重新生成',exact:true}).click();await page.waitForFunction(()=>window.qa.tasks.length===2);assert.equal(await page.evaluate(()=>window.qa.runs.length),0);
   await page.getByRole('button',{name:'重新生成',exact:true}).waitFor();
  });
  await check('stop during registration waits for ACK and cancels durable task',async()=>{
   await page.evaluate(()=>{window.qa.holdAck=true;window.qa.holdAgent=true});
   const toggle=page.getByRole('button',{name:'只读 Agent',exact:true});if(await toggle.getAttribute('aria-pressed')!=='true')await toggle.click();
   await page.getByRole('textbox',{name:'输入问题',exact:true}).fill('可取消任务');await page.getByRole('button',{name:'发送',exact:true}).click();await page.waitForFunction(()=>!!window.qa.releaseAck);
   await page.getByRole('button',{name:'停止生成',exact:true}).click();await page.evaluate(()=>window.qa.releaseAck());await page.waitForFunction(()=>window.qa.agentCancels.length===1);
   assert.equal(await page.evaluate(()=>window.qa.tasks.at(-1).status),'cancelled');
   assert.equal(await page.evaluate(async()=>{const s=(await window.qa.store()).useChatStore.getState();return s.sessions.find(x=>x.id===s.activeId).messages.at(-1).agentRunId}),await page.evaluate(()=>window.qa.tasks.at(-1).id));await page.evaluate(()=>{window.qa.holdAck=false});
  });
  await check('reload during execution recovers final answer without replay',async()=>{
   await page.evaluate(()=>{window.qa.holdAgent=true});
   await page.getByRole('textbox',{name:'输入问题',exact:true}).fill('后台完成的任务');await page.getByRole('button',{name:'发送',exact:true}).click();
   await page.waitForFunction(async()=>{const s=(await window.qa.store()).useChatStore.getState();return !!s.sessions.find(x=>x.id===s.activeId).messages.at(-1).agentRunId});
   await page.evaluate(async()=>await (await window.qa.store()).flushChatPersistence());const count=await page.evaluate(()=>window.qa.tasks.length);
   await page.reload();await page.evaluate(()=>{const run=window.qa.tasks.at(-1);run.status='completed';run.result={answer:'重启后取回的结论 [E1]',steps:[],evidence:[],model_calls:2,tool_calls:1}});
   await page.getByText('Agent 执行记录',{exact:true}).last().click();await page.getByText('重启后取回的结论 [E1]',{exact:true}).waitFor();assert.equal(await page.evaluate(()=>window.qa.tasks.length),count);
   await page.evaluate(()=>{window.qa.holdAgent=true});
  });
  await check('DevOps continues after navigation and task center retains failed-step evidence',async()=>{
   await page.goto(base+'#/devops');await page.getByLabel('Agent 任务目标',{exact:true}).fill('检查项目');await page.getByRole('button',{name:'执行只读任务',exact:true}).click();await page.getByRole('button',{name:'取消 Agent 任务',exact:true}).waitFor();
   await page.goto(base+'#/chat');await page.evaluate(()=>{const run=window.qa.tasks.at(-1);run.status='failed';run.error='模型调用失败';});
   await page.getByRole('button',{name:/工作区与任务：/}).click();const hub=page.getByRole('dialog',{name:'工作区与任务',exact:true});await hub.getByRole('button',{name:/Agent · 检查项目/}).click();await hub.getByText('模型调用失败',{exact:true}).waitFor();await hub.getByText('工具完成 · project_read_file',{exact:true}).waitFor();
  });
  assert.deepEqual(errors,[]);console.log(JSON.stringify({passed:passed.length,errors}));await context.close();
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
