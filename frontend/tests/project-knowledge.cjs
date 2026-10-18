// Browser plugin not available. Isolated Playwright tests of real React UI; synthetic IPC, no model calls.
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const {createContext,errors}=require('./ui-regression.cjs');
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),os=require('node:os');
const output=process.env.QA_OUTPUT||path.join(os.tmpdir(),'noosphere-knowledge-qa');fs.mkdirSync(output,{recursive:true});
const base=process.env.QA_URL||'http://127.0.0.1:1420/';const passed=[];
async function check(name,fn){await fn();passed.push(name);console.log('PASS '+name);}
(async()=>{
 const browser=await chromium.launch({channel:'msedge',headless:true});
 try{
  const context=await createContext(browser,{width:1280,height:900});
  await context.addInitScript(()=>{
   const invoke=window.__TAURI_INTERNALS__.invoke;window.qa.memories=[];
   window.__TAURI_INTERNALS__.invoke=async(command,payload)=>{
    if(command==='list_knowledge')return {memories:window.qa.memories.filter(m=>m.workspace_id===payload.workspaceId)};
    if(command==='save_knowledge'){
     if(window.qa.failSave)throw new Error('记录已被更新，请刷新后重试');
     const input=payload.memory,old=window.qa.memories.find(m=>m.id===input.id);
     const item={...input,id:input.id||crypto.randomUUID(),workspace_id:payload.workspaceId,status:input.status||'proposed',revision:(old?.revision||0)+1,created_at:old?.created_at||new Date().toISOString(),updated_at:new Date().toISOString()};
     if(input.supersedes&&!old){const previous=window.qa.memories.find(m=>m.id===input.supersedes);if(previous.revision!==input.supersedes_revision)throw new Error('替代目标版本冲突');previous.status='superseded';previous.superseded_by=item.id;}
     window.qa.memories=window.qa.memories.filter(m=>m.id!==item.id).concat(item);return item;
    }
    if(command==='prepare_context')return {workspace_id:payload.workspaceId,project_id:'code-fixture',mode:'lexical',warnings:['部分代码已变化，已排除；请重新索引。'],context:'[R1] SQLite 事务约定',citations:[{id:'R1',kind:'code',title:'src/store.py',file:'src/store.py',start_line:1,end_line:3,symbol:'save',source_hash:'a'.repeat(64),excerpt:'def save():\n    commit()',workspace_id:payload.workspaceId,project_id:'code-fixture'}]};
    return invoke(command,payload);
   };
  });
  const page=await context.newPage();await page.goto(base+'#/nebula');
  const panel=page.getByLabel('工作区长期记忆',{exact:true});
  await check('create, confirm, edit and supersede curated memory',async()=>{
   await panel.getByLabel('长期记忆内容').fill('SQLite 写入使用事务');await panel.getByRole('button',{name:'保存为待确认',exact:true}).click();
   const record=panel.locator('article').filter({hasText:'SQLite 写入使用事务'});await record.getByText('待确认',{exact:true}).waitFor();
   await record.getByRole('button',{name:'确认采用',exact:true}).click();await record.getByText('已确认',{exact:true}).waitFor();
   await record.getByRole('button',{name:'编辑内容',exact:true}).click();await panel.getByLabel('长期记忆内容').fill('SQLite 使用 WAL 事务');await panel.getByRole('button',{name:'保存为待确认',exact:true}).click();
   const updated=panel.locator('article').filter({hasText:'SQLite 使用 WAL 事务'});await updated.getByText('待确认',{exact:true}).waitFor();
   await updated.getByRole('button',{name:'用新记录替代',exact:true}).click();await panel.getByLabel('长期记忆内容').fill('PostgreSQL 事务规范');await panel.getByRole('button',{name:'确认并替代旧记录',exact:true}).click();
   await updated.getByText('已被替代',{exact:true}).waitFor();await panel.locator('article').filter({hasText:'PostgreSQL 事务规范'}).getByText('已确认',{exact:true}).waitFor();
  });
  await check('conflict stays visible and retains editor text',async()=>{
   await page.evaluate(()=>{window.qa.failSave=true});await panel.getByLabel('长期记忆内容').fill('冲突时保留草稿');await panel.getByRole('button',{name:'保存为待确认',exact:true}).click();
   await panel.getByRole('alert').filter({hasText:'请刷新后重试'}).waitFor();assert.equal(await panel.getByLabel('长期记忆内容').inputValue(),'冲突时保留草稿');await page.evaluate(()=>{window.qa.failSave=false});
   await page.screenshot({path:path.join(output,'knowledge-desktop.png'),animations:'disabled'});
  });
  await check('desktop minimum and narrow layout keep memory actions accessible',async()=>{
   for(const size of [{width:900,height:600},{width:390,height:844}]){await page.setViewportSize(size);assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));await panel.getByLabel('长期记忆内容').fill('可编辑');await panel.getByRole('button',{name:'保存为待确认',exact:true}).scrollIntoViewIfNeeded();assert.ok(await panel.getByRole('button',{name:'保存为待确认',exact:true}).isVisible());}
   await page.screenshot({path:path.join(output,'knowledge-narrow.png'),animations:'disabled'});await page.setViewportSize({width:1280,height:900});
  });
  await check('chat displays evidence, stale-source warning and snapshot after reload',async()=>{
   await page.goto(base+'#/chat');await page.getByRole('textbox',{name:'输入问题',exact:true}).fill('如何保存事务');await page.getByRole('button',{name:'发送',exact:true}).click();await page.waitForFunction(()=>window.qa.runs.length===1);
   await page.evaluate(()=>window.qa.emit(0,{done:true,status:'completed',content:'应使用事务保存 [R1]。'}));await page.getByText('应使用事务保存 [R1]。',{exact:true}).waitFor();
   await page.getByText(/本次提供给模型的依据/).click();await page.getByText(/\[R1\] 代码/).click();await page.getByText('def save():',{exact:false}).waitFor();await page.getByText(/部分代码已变化/).waitFor();
   await page.screenshot({path:path.join(output,'knowledge-chat-evidence.png'),animations:'disabled'});
   await page.evaluate(async()=>await (await window.qa.store()).flushChatPersistence());await page.reload();await page.getByText(/本次提供给模型的依据/).waitFor();
   await page.getByRole('button',{name:'整理为待确认记忆',exact:true}).click();await page.getByText(/已保存为待确认；请在 Nebula/).waitFor();
   assert.equal(await page.evaluate(()=>window.qa.memories.at(-1).source.kind),'chat');assert.equal(await page.evaluate(()=>window.qa.memories.at(-1).status),'proposed');
  });
  await check('changing workspace resets memory editor and list',async()=>{
   await page.goto(base+'#/nebula');await panel.getByLabel('长期记忆内容').fill('原工作区草稿');
   await page.getByRole('button',{name:/工作区与任务：/}).click();const hub=page.getByRole('dialog',{name:'工作区与任务',exact:true});await hub.getByRole('button',{name:'新建',exact:true}).click();await hub.getByLabel('工作区名称').fill('独立知识工作区');await hub.getByRole('button',{name:'创建并切换',exact:true}).click();await hub.getByLabel('当前工作区').getByRole('option',{name:'独立知识工作区'}).waitFor({state:'attached'});await page.keyboard.press('Escape');
   assert.equal(await panel.getByLabel('长期记忆内容').inputValue(),'');assert.equal(await panel.locator('article').count(),0);
  });
  assert.deepEqual(errors,[]);fs.writeFileSync(path.join(output,'project-knowledge-results.json'),JSON.stringify({passed,errors},null,2));console.log(JSON.stringify({passed:passed.length,errors}));await context.close();
 }finally{await browser.close()}
})().catch(error=>{console.error(error);process.exitCode=1});
