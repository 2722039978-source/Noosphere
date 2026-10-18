// Real React stores/routes with a synthetic desktop IPC boundary; no Computer Use or live model.
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const {createContext,errors}=require('./ui-regression.cjs');
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),os=require('node:os');
const output=process.env.QA_OUTPUT||path.join(os.tmpdir(),'noosphere-workspaces-qa');fs.mkdirSync(output,{recursive:true});
const base=process.env.QA_URL||'http://127.0.0.1:1420/';
const passed=[];
async function check(name,fn){await fn();passed.push(name);console.log('PASS '+name);}
async function openHub(page){await page.getByRole('button',{name:/工作区与任务：/}).click();return page.getByRole('dialog',{name:'工作区与任务',exact:true});}
async function closeHub(page){await page.keyboard.press('Escape');await page.getByRole('dialog',{name:'工作区与任务',exact:true}).waitFor({state:'hidden'});}
(async()=>{
 const browser=await chromium.launch({channel:'msedge',headless:true});
 try{
  const context=await createContext(browser,{width:1280,height:800});
  await context.addInitScript(()=>{
   const invoke=window.__TAURI_INTERNALS__.invoke;
   window.__TAURI_INTERNALS__.invoke=async(command,payload)=>{
    if(command==='get_models'){const response=await invoke(command,payload);return {models:[...response.models,{...response.models[0],id:'workspace-model',name:'项目专用模型',is_default:false}]};}
    return invoke(command,payload);
   };
  });
  const page=await context.newPage();await page.goto(base+'#/chat');await page.getByRole('textbox',{name:'输入问题',exact:true}).waitFor();
  let alpha;
  await check('workspace creation, default model, and chat isolation',async()=>{
   const hub=await openHub(page);await hub.getByRole('button',{name:'新建',exact:true}).click();
   await hub.getByLabel('工作区名称').fill('项目 Alpha');await hub.getByLabel('工作区项目目录').fill('C:/fixture/alpha');await hub.getByLabel('工作区默认模型').selectOption('workspace-model');
   await hub.getByRole('button',{name:'创建并切换'}).click();await hub.getByLabel('当前工作区').getByRole('option',{name:'项目 Alpha'}).waitFor({state:'attached'});
   alpha=await hub.getByLabel('当前工作区').inputValue();assert.notEqual(alpha,'local');await closeHub(page);
   await page.getByRole('combobox').filter({hasText:'项目专用模型'}).waitFor();
   await page.getByRole('textbox',{name:'输入问题',exact:true}).fill('Alpha 项目问题');await page.getByRole('button',{name:'发送',exact:true}).click();
   await page.waitForFunction(()=>window.qa.runs.length===1);
   assert.equal(await page.evaluate(()=>window.qa.runs[0].workspaceId),alpha);
   const dialog=await openHub(page);await dialog.getByLabel('当前工作区').selectOption('local');await closeHub(page);
   await page.getByRole('button',{name:/返回生成中的会话/}).waitFor();
   assert.equal(await page.getByText('Alpha 项目问题',{exact:true}).count(),0);
   await page.getByRole('button',{name:/返回生成中的会话/}).click();
   await page.getByRole('button',{name:'停止生成',exact:true}).waitFor();
   await page.evaluate(()=>window.qa.emit(0,{done:true,status:'completed',content:'Alpha 专属回复'}));await page.getByText('Alpha 专属回复',{exact:true}).waitFor();
  });
  await check('background diagnosis survives route change and is cancellable from task center',async()=>{
   await page.evaluate(()=>{window.qa.holdTasks=true;});await page.goto(base+'#/devops');
   await page.getByLabel('诊断问题').fill('任务取消验证');await page.getByRole('button',{name:'开始诊断',exact:true}).click();
   await page.getByRole('button',{name:'取消诊断',exact:true}).waitFor();
   await page.goto(base+'#/codelens');const hub=await openHub(page);
   await hub.getByRole('button',{name:'取消任务',exact:true}).waitFor();
   assert.equal(await page.evaluate(()=>window.qa.tasks[0].status),'running');
   await hub.getByRole('button',{name:'取消任务',exact:true}).click();
   await page.waitForFunction(()=>window.qa.tasks[0].status==='cancelled');
   await hub.getByRole('button',{name:'取消任务',exact:true}).waitFor({state:'hidden'});await closeHub(page);
  });
  await check('completed task details survive navigation and stay workspace-scoped',async()=>{
   await page.evaluate(()=>{window.qa.holdTasks=false;});await page.goto(base+'#/devops');
   await page.getByLabel('诊断问题').fill('完成验证');await page.getByRole('button',{name:'开始诊断',exact:true}).click();
   await page.getByText('夹具诊断：Spooler 运行正常。',{exact:true}).waitFor();await page.goto(base+'#/gateway');
   const hub=await openHub(page);await hub.getByRole('button',{name:/诊断 · 完成验证/}).click();
   await hub.getByText('夹具诊断：Spooler 运行正常。',{exact:true}).waitFor();
   await page.screenshot({path:path.join(output,'workspace-task-center.png'),animations:'disabled'});
   await hub.getByLabel('当前工作区').selectOption('local');await hub.getByText(/还没有任务/).waitFor();
   assert.equal(await hub.getByText('夹具诊断：Spooler 运行正常。',{exact:true}).count(),0);
   await hub.getByLabel('当前工作区').selectOption(alpha);await hub.getByRole('button',{name:/诊断 · 完成验证/}).waitFor();await closeHub(page);
  });
  await check('late index results never change another workspace project',async()=>{
   await page.evaluate(()=>{
    const invoke=window.__TAURI_INTERNALS__.invoke;
    window.qa.holdTasks=true;
    window.__TAURI_INTERNALS__.invoke=async(command,payload)=>{
     const result=await invoke(command,payload);
     if(command==='submit_workspace_run'&&payload.kind==='index'){result.status='running';result.result=null;window.qa.indexTask=result;}
     return result;
    };
   });
   await page.goto(base+'#/codelens');assert.equal(await page.getByLabel('项目目录',{exact:true}).inputValue(),'C:/fixture/alpha');
   await page.getByRole('button',{name:'开始索引',exact:true}).click();await page.waitForFunction(()=>!!window.qa.indexTask);
   const hub=await openHub(page);await hub.getByLabel('当前工作区').selectOption('local');await closeHub(page);
   await page.evaluate(()=>{const r=window.qa.indexTask;r.status='completed';r.phase='completed';r.result={project_id:'alpha-index',retrieval_ready:true,total_files:2,indexed_chunks:3};const w=window.qa.workspaces.find(w=>w.id===r.workspace_id);w.project_id='alpha-index';});
   await page.waitForTimeout(1200);assert.equal(await page.getByLabel('项目目录',{exact:true}).inputValue(),'fixture');
   assert.equal(await page.getByText(/索引完成并已写入记忆引擎/).count(),0);
   const updated=await openHub(page);await updated.getByLabel('当前工作区').selectOption(alpha);await updated.getByRole('button',{name:/索引 · 项目 Alpha/}).click();
   await updated.getByText(/已解析 2 个文件，保存 3 个代码片段/).waitFor();await closeHub(page);
  });
  await check('task dialog fits native minimum size and narrow viewport',async()=>{
   for(const size of [{width:900,height:600},{width:390,height:844}]){
    await page.setViewportSize(size);await openHub(page);assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
    const box=await page.getByRole('dialog',{name:'工作区与任务',exact:true}).boundingBox();assert.ok(box.x>=0&&box.x+box.width<=size.width+1&&box.height<=size.height);await closeHub(page);
   }
  });
  await check('Windows path normalization preserves the pending index and completion UI',async()=>{
   await page.setViewportSize({width:1280,height:800});await page.goto(base+'#/codelens');
   await page.evaluate(()=>{
    const invoke=window.__TAURI_INTERNALS__.invoke;
    window.__TAURI_INTERNALS__.invoke=async(command,payload)=>{
     if(command==='save_workspace')payload.workspace.project_path=payload.workspace.project_path.replace(/\//g,'\\').replace(/\\$/,'');
     return invoke(command,payload);
    };
   });
   await page.getByLabel('项目目录',{exact:true}).fill('C:/fixture/canonical/');await page.getByRole('button',{name:'开始索引',exact:true}).click();
   await page.waitForFunction(()=>window.qa.workspaces.some(w=>w.project_path==='C:\\fixture\\canonical'));
   await page.waitForFunction(()=>window.qa.indexTask?.status==='running'&&window.qa.tasks.filter(t=>t.kind==='index').length===2);
   assert.ok(await page.getByRole('button',{name:'索引中…',exact:true}).isDisabled());
   await page.evaluate(()=>{const r=window.qa.indexTask;r.status='completed';r.phase='completed';r.result={project_id:'canonical-index',retrieval_ready:true,project_root:'C:\\fixture\\canonical',total_files:1,indexed_chunks:1};window.qa.workspaces.find(w=>w.id===r.workspace_id).project_id='canonical-index';});
   await page.getByText(/索引完成并已写入记忆引擎/).waitFor();
  });
  await check('foreign backup import restores a selectable workspace with visible history',async()=>{
   await page.goto(base+'#/settings');
   const backup={format:'noosphere-chat-backup',version:1,state:{sessions:[{id:'foreign-backup-fixture',workspaceId:'from-another-installation',title:'外部工作区历史',modelId:'unknown-model',messages:[{id:'foreign-msg',role:'user',content:'恢复的原始消息',createdAt:1}],createdAt:1,updatedAt:1}],activeId:'foreign-backup-fixture'}};
   await page.getByLabel('选择会话备份').setInputFiles({name:'foreign.json',mimeType:'application/json',buffer:Buffer.from(JSON.stringify(backup))});
   await page.getByText(/已恢复 1 个导入工作区/).waitFor();const hub=await openHub(page);
   const restored=await page.evaluate(()=>window.qa.workspaces.find(w=>w.name.startsWith('导入工作区')));
   assert.equal(restored.project_path,'');assert.equal(restored.default_model_id,'');
   await hub.getByLabel('当前工作区').selectOption(restored.id);await closeHub(page);await page.goto(base+'#/chat');
   await page.getByText('恢复的原始消息',{exact:true}).waitFor();await page.getByRole('heading',{name:'外部工作区历史',exact:true}).waitFor();
  });
  assert.deepEqual(errors,[]);console.log(JSON.stringify({passed:passed.length,errors}));fs.writeFileSync(path.join(output,'workspace-runtime-results.json'),JSON.stringify({passed,errors},null,2));await context.close();
 }finally{await browser.close();}
})().catch(error=>{console.error(error);process.exitCode=1;});
