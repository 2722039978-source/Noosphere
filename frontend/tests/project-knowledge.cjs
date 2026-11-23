// Browser plugin not available. Isolated Playwright tests of real React UI; synthetic IPC, no model calls.
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const {createContext,errors}=require('./ui-regression.cjs');
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),os=require('node:os');
const output=process.env.QA_OUTPUT||path.join(os.tmpdir(),'noosphere-knowledge-qa');fs.mkdirSync(output,{recursive:true});
const base=process.env.QA_URL||'http://127.0.0.1:1420/';const passed=[];
async function check(name,fn){await fn();passed.push(name);console.log('PASS '+name);}
(async()=>{
 const browser=await chromium.launch({channel:process.env.QA_BROWSER||'msedge',headless:true});
 try{
  const context=await createContext(browser,{width:1280,height:900});
  await context.addInitScript(()=>{
   const invoke=window.__TAURI_INTERNALS__.invoke;window.qa.memories=[];
   window.__TAURI_INTERNALS__.invoke=async(command,payload)=>{
    if(command==='list_knowledge')return {memories:window.qa.memories.filter(m=>m.workspace_id===payload.workspaceId)};
    if(command==='save_knowledge'){
     if(window.qa.failSave)throw new Error('记录已被更新，请刷新后重试');
     const input=payload.memory,old=window.qa.memories.find(m=>m.id===input.id);
     const workspace=window.qa.workspaces.find(item=>item.id===payload.workspaceId);
     if(input.expected_project_path!==workspace.project_path)throw new Error('记录所属项目已变化');
     if(old&&old.revision!==input.revision)throw new Error('记录已被更新，请刷新后重试');
     const item={...input,id:input.id||crypto.randomUUID(),workspace_id:payload.workspaceId,status:input.status||'proposed',revision:(old?.revision||0)+1,source:{...input.source,project_path:workspace.project_path},created_at:old?.created_at||new Date().toISOString(),updated_at:new Date().toISOString()};
     if(input.supersedes&&!old){const previous=window.qa.memories.find(m=>m.id===input.supersedes);if(previous.revision!==input.supersedes_revision)throw new Error('替代目标版本冲突');previous.status='superseded';previous.superseded_by=item.id;}
     window.qa.memories=window.qa.memories.filter(m=>m.id!==item.id).concat(item);return item;
    }
    if(command==='suggest_knowledge_revision'){
     const item=window.qa.memories.find(m=>m.id===payload.memoryId&&m.workspace_id===payload.workspaceId);
     if(!item||item.revision!==payload.input.revision||item.source.project_path!==payload.input.expected_project_path)throw new Error('记录已被更新，请刷新后重试');
     if(!payload.input.instruction.includes('北京时间'))throw new Error('测试需要明确提出时区变更');
     return {before:item,after:'本项目统一使用北京时间（UTC+8）。'};
    }
    if(command==='delete_knowledge'){
     const item=window.qa.memories.find(m=>m.id===payload.memoryId&&m.workspace_id===payload.workspaceId);
     if(!item||item.revision!==payload.input.revision)throw new Error('记录已被更新，请刷新后重试');
     window.qa.memories=window.qa.memories.filter(m=>m!==item);
     return {deleted:true};
    }
    if(command==='prepare_context')return {workspace_id:payload.workspaceId,project_id:'code-fixture',mode:'lexical',warnings:['部分代码已变化，已排除；请重新索引。'],context:'[R1] SQLite 事务约定',citations:[{id:'R1',kind:'code',title:'src/store.py',file:'src/store.py',start_line:1,end_line:3,symbol:'save',source_hash:'a'.repeat(64),excerpt:'def save():\n    commit()',workspace_id:payload.workspaceId,project_id:'code-fixture'}]};
    return invoke(command,payload);
   };
  });
  const page=await context.newPage();await page.goto(base+'#/nebula');
  const panel=page.getByLabel('工作区长期记忆',{exact:true});
  async function actions(record) {
   const summary=record.getByText('更多操作',{exact:true});
   if(!(await summary.evaluate(element=>element.parentElement.open))) await summary.click();
  }
  await check('create, confirm, edit and supersede curated memory',async()=>{
   await panel.getByRole('button',{name:'新建记忆',exact:true}).click();
   await panel.getByText('高级选项',{exact:true}).click();await panel.getByRole('checkbox',{name:'保存为待确认',exact:true}).check();
   await panel.getByLabel('长期记忆内容').fill('SQLite 写入使用事务');await panel.getByRole('button',{name:'保存为待确认',exact:true}).click();
   const record=panel.locator('article').filter({hasText:'SQLite 写入使用事务'});await record.getByText('待确认',{exact:true}).waitFor();
   await record.getByRole('button',{name:'确认采用',exact:true}).click();await record.getByText('已确认',{exact:true}).waitFor();
   await actions(record);await record.getByRole('button',{name:'编辑内容',exact:true}).click();
   await panel.getByLabel('长期记忆内容').fill('SQLite 使用 WAL 事务');await panel.getByRole('button',{name:'保存修改',exact:true}).click();
   const updated=panel.locator('article').filter({hasText:'SQLite 使用 WAL 事务'});await updated.getByText('已确认',{exact:true}).waitFor();
   await actions(updated);
   await updated.getByRole('button',{name:'用新记录替代',exact:true}).click();await panel.getByLabel('长期记忆内容').fill('PostgreSQL 事务规范');await panel.getByRole('button',{name:'确认并替代旧记录',exact:true}).click();
   await updated.getByText('已被替代',{exact:true}).waitFor();await panel.locator('article').filter({hasText:'PostgreSQL 事务规范'}).getByText('已确认',{exact:true}).waitFor();
  });
  await check('conflict stays visible and retains editor text',async()=>{
   await panel.getByRole('button',{name:'新建记忆',exact:true}).click();
   await page.evaluate(()=>{window.qa.failSave=true});await panel.getByLabel('长期记忆内容').fill('冲突时保留草稿');await panel.getByRole('button',{name:'保存记忆',exact:true}).click();
   await panel.getByRole('alert').filter({hasText:'请刷新后重试'}).waitFor();assert.equal(await panel.getByLabel('长期记忆内容').inputValue(),'冲突时保留草稿');await page.evaluate(()=>{window.qa.failSave=false});
   await page.screenshot({path:path.join(output,'knowledge-desktop.png'),animations:'disabled'});
  });
  await check('desktop minimum and narrow layout keep memory actions accessible',async()=>{
   for(const size of [{width:900,height:600},{width:390,height:844}]){await page.setViewportSize(size);assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));await panel.getByLabel('长期记忆内容').fill('可编辑');await panel.getByRole('button',{name:'保存记忆',exact:true}).scrollIntoViewIfNeeded();assert.ok(await panel.getByRole('button',{name:'保存记忆',exact:true}).isVisible());}
   await page.screenshot({path:path.join(output,'knowledge-narrow.png'),animations:'disabled'});await page.setViewportSize({width:1280,height:900});
  });
  await check('chat displays evidence, stale-source warning and snapshot after reload',async()=>{
   await page.goto(base+'#/chat');await page.getByRole('textbox',{name:'输入问题',exact:true}).fill('如何保存事务');await page.getByRole('button',{name:'发送',exact:true}).click();await page.waitForFunction(()=>window.qa.runs.length===1);
   await page.evaluate(()=>window.qa.emit(0,{done:true,status:'completed',content:'应使用事务保存 [R1]。'}));await page.getByText('应使用事务保存 [R1]。',{exact:true}).waitFor();
   await page.getByText(/本次对话的依据/).click();await page.getByText(/\[R1\] 代码/).click();await page.getByText('def save():',{exact:false}).waitFor();await page.getByText(/部分代码已变化/).waitFor();
   await page.screenshot({path:path.join(output,'knowledge-chat-evidence.png'),animations:'disabled'});
   await page.evaluate(async()=>await (await window.qa.store()).flushChatPersistence());await page.reload();await page.getByText(/本次对话的依据/).waitFor();
   await page.getByRole('button',{name:'整理为待确认记忆',exact:true}).click();await page.getByText('已保存为待确认，可在记忆区查看。',{exact:true}).waitFor();
   assert.equal(await page.evaluate(()=>window.qa.memories.at(-1).source.kind),'chat');assert.equal(await page.evaluate(()=>window.qa.memories.at(-1).status),'proposed');
  });
  await check('conversation candidate saves only by confirmation and rewrite stays advisory until accepted',async()=>{
   const input=page.getByRole('textbox',{name:'输入问题',exact:true});
   const send=page.getByRole('button',{name:'发送',exact:true});
   await input.fill('请记住：本项目统一使用 UTC 时间。');await send.click();
   await page.waitForFunction(()=>window.qa.runs.length===1);
   const candidate=page.getByLabel('记忆候选',{exact:true});
   await candidate.getByText('本项目统一使用 UTC 时间。',{exact:true}).waitFor();
   assert.match(await page.evaluate(()=>window.qa.runs[0].messages.at(-1).content),/confirmation in the app/);
   assert.equal(await page.evaluate(()=>window.qa.memories.some(m=>m.content.includes('UTC 时间'))),false);
   await page.evaluate(()=>window.qa.emit(0,{done:true,status:'completed',content:'好的。'}));
   await candidate.getByRole('button',{name:'保存记忆',exact:true}).click();
   await page.getByText('已保存到工作区记忆。',{exact:true}).waitFor();
   await input.fill('修改这条记忆：改为北京时间');await send.click();
   const review=page.getByLabel('改写工作区记忆',{exact:true});
   await review.getByLabel('要改写的记忆').selectOption({label:'本项目统一使用 UTC 时间。'});
   await review.getByRole('button',{name:'让模型提出改写',exact:true}).click();
   await review.getByText('模型建议',{exact:true}).waitFor();
   assert.equal(await page.evaluate(()=>window.qa.runs.length),1);
   assert.equal(await page.evaluate(()=>window.qa.memories.some(m=>m.content.includes('北京时间'))),false);
   await page.screenshot({path:path.join(output,'memory-revision-review.png'),animations:'disabled'});
   await review.getByRole('button',{name:'确认更新',exact:true}).click();
   await review.getByText('记忆已更新。',{exact:true}).waitFor();
   assert.equal(await page.evaluate(()=>window.qa.memories.filter(m=>m.content==='本项目统一使用北京时间（UTC+8）。').length),1);
  });
  await check('source opens the original chat and saved memory can be paused, resumed or deleted',async()=>{
   await page.goto(base+'#/nebula');
   const record=panel.locator('article').filter({hasText:'本项目统一使用北京时间（UTC+8）。'});
   await record.getByText('查看来源',{exact:true}).click();
   await record.getByText('请记住：本项目统一使用 UTC 时间。',{exact:true}).waitFor();
   await record.getByRole('link',{name:'回到原对话',exact:true}).click();
   await page.locator('[data-search-match="true"]').getByText('请记住：本项目统一使用 UTC 时间。',{exact:true}).waitFor();
   await page.goto(base+'#/nebula');await actions(record);
   await record.getByRole('button',{name:'暂停使用',exact:true}).click();await record.getByText('已暂停',{exact:true}).waitFor();
   await actions(record);
   await record.getByRole('button',{name:'恢复使用',exact:true}).click();await record.getByText('已确认',{exact:true}).waitFor();
   await actions(record);
   await record.getByRole('button',{name:'删除',exact:true}).click();
   const dialog=page.getByRole('dialog',{name:'删除这条记忆？'});
   await dialog.getByRole('button',{name:'取消',exact:true}).click();await record.waitFor();
   await record.getByRole('button',{name:'删除',exact:true}).click();
   await dialog.getByRole('button',{name:'确认删除',exact:true}).click();await record.waitFor({state:'detached'});
   await page.goto(base+'#/chat');
   await page.getByText('请记住：本项目统一使用 UTC 时间。',{exact:true}).waitFor();
   assert.equal(await page.getByLabel('记忆候选',{exact:true}).count(),0);
  });
  await check('changing workspace resets memory editor and list',async()=>{
   await page.goto(base+'#/nebula');await panel.getByRole('button',{name:'新建记忆',exact:true}).click();await panel.getByLabel('长期记忆内容').fill('原工作区草稿');
   await page.evaluate(async()=>{
    const module=performance.getEntriesByType('resource').find(entry=>new URL(entry.name).pathname==='/src/stores/workspace-store.ts');
    const {useWorkspaceStore}=await import(module?.name??'/src/stores/workspace-store.ts');
    const state=useWorkspaceStore.getState();const workspace=await state.save({name:'独立知识工作区',project_path:'independent',default_model_id:''});state.select(workspace.id);
   });
   await panel.getByRole('heading',{name:'独立知识工作区',exact:true}).waitFor();
   assert.equal(await panel.getByLabel('长期记忆内容').count(),0);assert.equal(await panel.locator('article').count(),0);
  });
  assert.deepEqual(errors,[]);fs.writeFileSync(path.join(output,'project-knowledge-results.json'),JSON.stringify({passed,errors},null,2));console.log(JSON.stringify({passed:passed.length,errors}));await context.close();
 }finally{await browser.close()}
})().catch(error=>{console.error(error);process.exitCode=1});
