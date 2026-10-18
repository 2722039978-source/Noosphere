// Headless isolated React tests, synthetic Tauri bridge. No computer-use APIs.
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const {createContext,errors}=require('./ui-regression.cjs');
const assert=require('node:assert/strict');
const base=process.env.QA_URL||'http://127.0.0.1:1420/';
const passed=[];async function check(name,fn){await fn();passed.push(name);console.log('PASS '+name)}
(async()=>{
 const browser=await chromium.launch({channel:'msedge',headless:true});
 try{
  const context=await createContext(browser,{width:1280,height:900});
  await context.addInitScript(()=>{
   if(location.protocol!=='http:')return;
   const original=window.__TAURI_INTERNALS__.invoke;
   const saved=JSON.parse(sessionStorage.getItem('actions-fixture')||'null');window.qa.actions=saved?.actions||[];window.qa.tasks=saved?.tasks||[];window.qa.decisions=[];
   const persist=()=>sessionStorage.setItem('actions-fixture',JSON.stringify({actions:window.qa.actions,tasks:window.qa.tasks}));
   window.__TAURI_INTERNALS__.invoke=async(command,payload)=>{
    if(command==='submit_workspace_run'&&payload.kind==='agent'){
     window.qa.reviewed=payload.input.reviewed_actions;
     const run=await original(command,payload);run.result={answer:'操作预览已保存，尚未执行。',steps:[],evidence:[]};
     if(payload.input.reviewed_actions)for(const kind of ['file','test','service'])window.qa.actions.push({id:crypto.randomUUID(),workspace_id:payload.workspaceId,run_id:run.id,kind,status:'pending',reason:'fixture action',project_path:'fixture',path:kind==='file'?'main.go':kind==='test'?'.':undefined,before:'// before',after:'// after',existed:true,command:kind==='test'?'python -m unittest discover -v':'sc start FixtureService',service:'FixtureService',previous:'stopped',target:'running'});
     persist();return run;
    }
    if(command==='list_agent_actions')return {actions:window.qa.actions.filter(a=>a.workspace_id===payload.workspaceId&&a.run_id===payload.runId)};
    if(command==='decide_agent_action'){
     const a=window.qa.actions.find(a=>a.id===payload.id);window.qa.decisions.push(payload);
     if(payload.decision==='approve'&&a.kind==='file'&&window.qa.conflict)throw new Error('文件已被外部修改，未覆盖');
     a.status=payload.decision==='reject'?'rejected':payload.decision==='rollback'?'rolled_back':payload.decision==='cancel'?'failed':a.kind==='test'?'running':'applied';persist();return a;
    }
    return original(command,payload);
   };
  });
  const page=await context.newPage();await page.goto(base+'#/chat');
  await check('reviewed mode submits proposal only and requires explicit per-action approval',async()=>{
   await page.getByRole('button',{name:'只读 Agent',exact:true}).click();await page.getByRole('button',{name:'操作提案',exact:true}).click();
   await page.getByLabel('输入问题',{exact:true}).fill('修改注释并提出测试');await page.getByRole('button',{name:'发送',exact:true}).click();await page.getByText('操作预览已保存，尚未执行。',{exact:true}).waitFor();
   assert.equal(await page.evaluate(()=>window.qa.reviewed),true);assert.equal(await page.evaluate(()=>window.qa.decisions.length),0);
   await page.getByText('Agent 执行记录',{exact:true}).click();await page.getByText('文件修改 · main.go · 待确认',{exact:true}).click();
   await page.getByText('// before',{exact:true}).waitFor();await page.getByText('// after',{exact:true}).waitFor();
  });
  const file=page.locator('details').filter({has:page.locator('summary').filter({hasText:/^文件修改 ·/})}).last();
  await check('conflict remains visible; apply and rollback use the saved action identity',async()=>{
   await page.evaluate(()=>window.qa.conflict=true);await file.getByRole('button',{name:'确认执行此操作',exact:true}).click();await file.getByRole('alert').filter({hasText:'文件已被外部修改'}).waitFor();
   await page.evaluate(()=>window.qa.conflict=false);await file.getByRole('button',{name:'确认执行此操作',exact:true}).click();await page.getByText('文件修改 · main.go · 已执行',{exact:true}).waitFor();
   await file.getByRole('button',{name:'恢复原文件',exact:true}).click();await page.getByText('文件修改 · main.go · 已恢复',{exact:true}).waitFor();
   assert.equal(await page.evaluate(()=>new Set(window.qa.decisions.map(d=>d.id)).size),1);
  });
  await check('fixed test preview discloses effects and running test is cancellable',async()=>{
   await page.getByText('项目测试 · . · 待确认',{exact:true}).click();const test=page.locator('details').filter({has:page.locator('summary').filter({hasText:/^项目测试 ·/})}).last();
   await test.getByText('python -m unittest discover -v',{exact:true}).waitFor();await test.getByText(/测试会执行项目代码/).waitFor();await test.getByRole('button',{name:'确认执行此操作',exact:true}).click();await test.getByRole('button',{name:'停止执行',exact:true}).click();await page.getByText('项目测试 · . · 执行失败',{exact:true}).waitFor();
  });
  await check('pending service preview survives reload without approval or resubmission',async()=>{
   await page.evaluate(async()=>await (await window.qa.store()).flushChatPersistence());const count=await page.evaluate(()=>window.qa.tasks.length);await page.reload();await page.getByText('Agent 执行记录',{exact:true}).click();await page.getByText('服务操作 · FixtureService · 待确认',{exact:true}).click();
   const service=page.locator('details').filter({has:page.locator('summary').filter({hasText:/^服务操作 ·/})}).last();await service.getByText(/原状态：stopped/).waitFor();assert.equal(await page.evaluate(()=>window.qa.tasks.length),count);assert.equal(await page.evaluate(()=>window.qa.decisions.length),0);
   await service.getByRole('button',{name:'拒绝此操作',exact:true}).click();await page.getByText('服务操作 · FixtureService · 已拒绝',{exact:true}).waitFor();
  });
  assert.deepEqual(errors,[]);console.log(JSON.stringify({passed:passed.length,errors}));await context.close();
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
