// Browser plugin unavailable; isolated Edge/Playwright IPC fixtures. No user data.
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const {createContext,errors}=require('./ui-regression.cjs');
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const base=process.env.QA_URL||'http://127.0.0.1:1429/';
const output=process.env.QA_OUTPUT||path.join(require('node:os').tmpdir(),'noosphere-model-navigation');fs.mkdirSync(output,{recursive:true});
(async()=>{
 const browser=await chromium.launch({channel:'msedge',headless:true});const passed=[];
 const check=async(name,fn)=>{await fn();passed.push(name);console.log('PASS '+name);};
 try{
  const context=await createContext(browser,{width:1448,height:1086},true,'reduce');
  await context.addInitScript(()=>{
   const original=window.__TAURI_INTERNALS__.invoke;
   const configs=[{id:'api',name:'API fixture',model_name:'deepseek-chat',provider:'deepseek',base_url:'https://fixture.invalid',enabled:true,is_default:true},{id:'local',name:'Local fixture',model_name:'fixture-local-27b',provider:'openai_compatible',base_url:'http://127.0.0.1:18787/v1',enabled:true},{id:'disabled',name:'Disabled fixture',model_name:'disabled',provider:'ollama',base_url:'http://127.0.0.1:11434/v1',enabled:false}];
   window.hubQA={configs,hold:true,fail:false};
   window.__TAURI_INTERNALS__.invoke=async(command,payload)=>{
    const q=window.hubQA;
    if(command==='get_models')return {models:q.configs};
    if(command==='get_model_selection'){const id=localStorage.getItem('qa-binding:'+payload.workspaceId+':'+payload.sessionId)||'';return {workspace_id:payload.workspaceId,session_id:payload.sessionId,model_id:id,bound:!!id};}
    if(command==='select_model'){if(q.fail)throw {message:'Provider unavailable; original binding retained'};if(q.hold)await new Promise(resolve=>q.release=resolve);const s=payload.selection;localStorage.setItem('qa-binding:'+s.workspace_id+':'+s.session_id,s.model_id);return {...s,bound:true};}
    if(command==='delete_model'){q.configs=q.configs.filter(m=>m.id!==payload.id);return {deleted:payload.id};}
    if(command==='test_model')return {status:'ok',latency_ms:1};
    if(command==='model_engineering')switch(payload.request.operation){
     case 'list_models':return {models:[],next_cursor:''};case 'environment':return {environment:{id:'fixture',os:'windows',arch:'amd64',cores:8},preflight:{available:false}};
     case 'list_profiles':return {profiles:[],next_cursor:''};case 'list_datasets':return {datasets:[]};case 'list_evaluations':return {evaluations:[],next_cursor:''};case 'list_releases':return {releases:[],next_cursor:''};case 'list_policies':return {policies:[]};default:throw {code:'NOT_FOUND',message:'Fixture detail not found',status:404};
    }
    return original(command,payload);
   };
  });
  const page=await context.newPage();await page.goto(base+'#/');await page.getByRole('heading',{name:'Model Hub',exact:true}).waitFor();
  const nav=page.getByRole('navigation',{name:'主导航'});
  await check('eight primary destinations, four nested sections and detail selection',async()=>{
   assert.equal(await nav.getByRole('link').count(),8);
   assert.deepEqual(await nav.locator('.ns-index-label').allTextContents(),['工作台','AI 对话','CodeLens','Nebula','DevOps','Gateway','模型档案','设置']);
   for(const [label,url]of [['Model Lab','/model-lab'],['Reports','/evaluation-reports'],['Release Center','/release-center'],['Model Hub','/model-hub']]){
    await page.getByRole('navigation',{name:'模型档案导航'}).getByRole('link',{name:label,exact:true}).click();await page.waitForURL('**/#'+url);
    assert.equal(await nav.getByRole('link',{name:/模型档案/}).getAttribute('aria-current'),'page');
    assert.equal(await page.getByRole('navigation',{name:'模型档案导航'}).getByRole('link',{name:label,exact:true}).getAttribute('aria-current'),'page');
   }
   await page.goto(base+'#/evaluation-reports/missing-fixture');await page.getByRole('alert').filter({hasText:'Fixture detail not found'}).waitFor();
   assert.equal(await nav.getByRole('link',{name:/模型档案/}).getAttribute('aria-current'),'page');
   assert.equal(await page.getByRole('navigation',{name:'模型档案导航'}).getByRole('link',{name:'Reports',exact:true}).getAttribute('aria-current'),'page');
   await page.goto(base+'#/model-hub');
  });
  await check('add Modal traps focus and Esc restores trigger and scroll',async()=>{
   const trigger=page.getByRole('button',{name:'添加模型',exact:true});await trigger.click();const dialog=page.getByRole('dialog');await dialog.waitFor();
   for(let i=0;i<12;i++){await page.keyboard.press('Tab');assert.equal(await dialog.evaluate(el=>el.contains(document.activeElement)),true);}
   await page.keyboard.press('Escape');await dialog.waitFor({state:'hidden'});assert.equal(await trigger.evaluate(el=>el===document.activeElement),true);
  });
  await check('details and edit stay in a right drawer with focus restoration',async()=>{
   const trigger=page.getByRole('button',{name:'查看主模型详情'});const scroll=await page.locator('.ns-hub-scroll').evaluate(el=>el.scrollTop);
   await trigger.click();let dialog=page.getByRole('dialog');await dialog.waitFor();const box=await dialog.boundingBox();assert.ok(Math.abs(box.x+box.width-1448)<3);assert.ok(Math.abs(box.y)<3);
   await page.keyboard.press('Escape');await dialog.waitFor({state:'hidden'});assert.equal(await trigger.evaluate(el=>el===document.activeElement),true);assert.equal(await page.locator('.ns-hub-scroll').evaluate(el=>el.scrollTop),scroll);
   const more=page.getByRole('button',{name:'Local fixture 更多操作',exact:true});await more.click();await page.getByRole('menuitem',{name:'编辑配置',exact:true}).click();
   dialog=page.getByRole('dialog');await dialog.getByRole('heading',{name:'编辑模型',exact:true}).waitFor();assert.match(page.url(),/#\/model-hub$/);
   await page.keyboard.press('Escape');await dialog.waitFor({state:'hidden'});assert.equal(await more.evaluate(el=>el===document.activeElement),true);
  });
  await check('connection testing is inline and delete requires a cancellable confirmation',async()=>{
   const more=page.getByRole('button',{name:'Disabled fixture 更多操作',exact:true});await more.click();await page.getByRole('menuitem',{name:'删除模型',exact:true}).click();const alert=page.getByRole('alertdialog');await alert.waitFor();
   assert.equal(await alert.getByRole('button',{name:'取消',exact:true}).evaluate(el=>el===document.activeElement),true);await page.keyboard.press('Escape');await alert.waitFor({state:'hidden'});assert.equal(await page.locator('.ns-hub-model-row').count(),3);
   await page.getByRole('button',{name:'Local fixture 更多操作',exact:true}).click();await page.getByRole('menuitem',{name:'测试连接',exact:true}).click();await page.getByRole('status').filter({hasText:'已验证 · 连接成功'}).waitFor();assert.match(page.url(),/#\/model-hub$/);
  });
  await check('consecutive legacy intents wait for the pending switch and apply the latest intent',async()=>{
   await page.evaluate(()=>{window.hubQA.hold=true;});
   await page.goto(base+'#/chat?model=api');await page.getByRole('status').filter({hasText:'正在验证模型连接'}).waitFor();
   await page.goto(base+'#/chat?model=local');
   await page.evaluate(()=>{window.hubQA.hold=false;window.hubQA.release();});
   await page.waitForFunction(async()=>{const {useChatStore}=await window.qa.store();const s=useChatStore.getState();return s.sessions.find(s=>s.id===useChatStore.getState().activeId)?.modelId==='local';},{},{timeout:3000});
   await page.getByRole('textbox',{name:'输入问题',exact:true}).fill('latest intent ready');
   await page.waitForFunction(()=>!document.querySelector('button[aria-label="发送"]').disabled,{},{timeout:3000});
   await page.goto(base+'#/model-hub');
  });
  await check('Use targets the selected workspace session and updates the card after acknowledgment',async()=>{
   await page.evaluate(async()=>{
    const {useWorkspaceStore}=await import('/src/stores/workspace-store.ts');const {useChatStore}=await window.qa.store();
    const state=useWorkspaceStore.getState();useWorkspaceStore.setState({workspaces:[...state.workspaces,{...state.workspaces[0],id:'other',name:'Other workspace'}]});
    useChatStore.getState().newSession();useWorkspaceStore.getState().select('other');window.hubQA.otherSession=useChatStore.getState().newSession();useWorkspaceStore.getState().select('local');window.hubQA.hold=true;
   });
   const row=page.locator('.ns-hub-model-row').filter({hasText:'Local fixture'});await row.getByRole('button',{name:'使用',exact:true}).click();
   await page.getByRole('status').filter({hasText:'正在验证模型连接'}).waitFor();assert.equal(await page.locator('.ns-hub-feature h2').textContent(),'API fixture');
   await page.evaluate(()=>{window.hubQA.hold=false;window.hubQA.release();});await page.getByRole('heading',{name:'Local fixture',exact:true}).waitFor();
   assert.match(page.url(),/#\/model-hub$/);assert.equal(await row.getByRole('button',{name:'当前使用',exact:true}).isDisabled(),true);
   assert.equal(await page.evaluate(async()=>{const {useChatStore}=await window.qa.store();return useChatStore.getState().sessions.find(s=>s.id===window.hubQA.otherSession).modelId;}),'');
   await page.screenshot({path:path.join(output,'model-hub-switched.png'),animations:'disabled'});
   await page.getByRole('link',{name:'开始对话',exact:true}).click();await page.getByRole('textbox',{name:'输入问题',exact:true}).fill('target session inference');await page.getByRole('button',{name:'发送',exact:true}).click();await page.waitForFunction(()=>window.qa.runs.length===1);assert.equal(await page.evaluate(()=>window.qa.runs[0].modelId),'local');
   assert.equal(await page.getByRole('combobox').isDisabled(),true);await page.getByRole('button',{name:'停止生成',exact:true}).click();
  });
  await check('mobile drawer and nested navigation remain reachable without overflow',async()=>{
   await page.goto(base+'#/model-hub');await page.setViewportSize({width:390,height:844});await page.getByRole('button',{name:'查看主模型详情'}).click();const dialog=page.getByRole('dialog');await dialog.waitFor();assert.equal(await dialog.evaluate(el=>el.scrollWidth<=el.clientWidth+1),true);await page.screenshot({path:path.join(output,'model-details-mobile.png'),animations:'disabled'});await page.keyboard.press('Escape');await dialog.waitFor({state:'hidden'});
   assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
  });
  assert.deepEqual(errors,[]);console.log(JSON.stringify({passed:passed.length,output,errors}));
 }finally{await browser.close();}
})().catch(error=>{console.error(error);process.exitCode=1;});
