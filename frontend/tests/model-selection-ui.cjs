// Rendered interaction QA. Provider HTTP serialization is tested separately by
// backend/internal/gateway/selection_test.go; this suite replaces only desktop IPC.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const { createContext, errors } = require('./ui-regression.cjs');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const base = process.env.QA_URL || 'http://127.0.0.1:1420/';
const output = process.env.QA_OUTPUT || path.join(require('node:os').tmpdir(), 'noosphere-model-selection-qa');
fs.mkdirSync(output, { recursive: true });
(async()=>{
 const browser=await chromium.launch({channel:'msedge',headless:true});
 try {
  const context=await createContext(browser,{width:1440,height:960});
  await context.addInitScript(()=>{
   const invoke=window.__TAURI_INTERNALS__.invoke;
   const models=[
    {id:'local-model',name:'Local fixture',model_name:'fixture-local',provider:'ollama',base_url:'http://127.0.0.1:11434/v1',enabled:true,is_default:true},
    {id:'api-model',name:'API fixture',model_name:'deepseek-chat',provider:'deepseek',base_url:'https://fixture.invalid',enabled:true},
    {id:'disabled',name:'Disabled fixture',model_name:'disabled',provider:'ollama',base_url:'http://127.0.0.1:11434/v1',enabled:false},
   ];
   window.qa.selectionMode='hold';
   window.__TAURI_INTERNALS__.invoke=async(command,payload)=>{
    if(command==='get_models')return {models};
    if(command==='get_model_selection'){
     const id=localStorage.getItem('qa-binding:'+payload.workspaceId+':'+payload.sessionId)||'';
     return {workspace_id:payload.workspaceId,session_id:payload.sessionId,model_id:id,bound:!!id};
    }
    if(command==='select_model'){
     const selection=payload.selection;
     if(window.qa.selectionMode==='fail')throw {message:'Provider unavailable; original binding retained'};
     if(window.qa.selectionMode==='hold')await new Promise(resolve=>window.qa.releaseSelection=resolve);
     localStorage.setItem('qa-binding:'+selection.workspace_id+':'+selection.session_id,selection.model_id);
     return {...selection,bound:true};
    }
    return invoke(command,payload);
   };
  });
  const page=await context.newPage();await page.goto(base+'#/chat');
  const input=page.getByRole('textbox',{name:'输入问题',exact:true});await input.waitFor();
  await page.getByRole('combobox').waitFor({state:'visible'});
  await page.waitForFunction(()=>!document.querySelector('[role=combobox]').disabled);
  await page.getByRole('combobox').click();
  assert.equal(await page.getByRole('option',{name:/Disabled fixture/}).getAttribute('aria-disabled'),'true');
  await page.keyboard.press('Escape');await page.getByRole('listbox').waitFor({state:'hidden'});
  assert.equal(await page.getByRole('combobox').evaluate(el=>el===document.activeElement),true);
  await page.getByRole('navigation',{name:'主导航'}).getByRole('link',{name:/Gateway/}).click();
  // Keep the active chat in another workspace, exactly as a workspace switch
  // from Gateway does. Use must resolve and select this workspace's session.
  await page.evaluate(async()=>{
    const {useWorkspaceStore}=await import('/src/stores/workspace-store.ts');
    const {useChatStore}=await window.qa.store();const state=useWorkspaceStore.getState();
    useWorkspaceStore.setState({workspaces:[...state.workspaces,{...state.workspaces[0],id:'other',name:'Other workspace'}]});
    useWorkspaceStore.getState().select('other');window.qa.otherSession=useChatStore.getState().newSession();
    useWorkspaceStore.getState().select('local');
  });
  const apiCard=page.locator('.ns-surface').filter({hasText:'API fixture'});
  await apiCard.getByRole('button',{name:'使用',exact:true}).click();
  await page.getByRole('button',{name:'验证中…',exact:true}).first().waitFor({timeout:3000});
  assert.equal(await page.evaluate(async()=>{const {useChatStore}=await window.qa.store();return useChatStore.getState().sessions.find(s=>s.id===useChatStore.getState().activeId).modelId;}),'');
  await page.evaluate(()=>{window.qa.selectionMode='ok';window.qa.releaseSelection();});
  await apiCard.getByRole('button',{name:'当前使用',exact:true}).waitFor();
  assert.equal(await page.evaluate(async()=>{const {useChatStore}=await window.qa.store();return useChatStore.getState().sessions.find(s=>s.id===window.qa.otherSession).modelId;}),'');
  await page.getByRole('status').filter({hasText:'当前会话已切换到 API fixture'}).waitFor();
  const localCard=page.locator('.ns-surface').filter({hasText:'Local fixture'});
  await page.evaluate(()=>window.qa.selectionMode='fail');
  await localCard.getByRole('button',{name:'使用',exact:true}).click();
  await page.getByRole('alert').filter({hasText:'Provider unavailable'}).waitFor();
  assert.equal(await apiCard.getByRole('button',{name:'当前使用',exact:true}).isDisabled(),true);
  await page.screenshot({path:path.join(output,'selection-failure-desktop.png'),animations:'disabled'});
  await page.getByRole('navigation',{name:'主导航'}).getByRole('link',{name:/AI 对话/}).click();
  await page.getByText('当前会话 · API fixture',{exact:true}).waitFor();
  await input.fill('prove outgoing model');await page.getByRole('button',{name:'发送',exact:true}).click();
  await page.waitForFunction(()=>window.qa.runs.length===1);
  assert.equal(await page.evaluate(()=>window.qa.runs[0].modelId),'api-model');
  assert.equal(await page.getByRole('combobox').isDisabled(),true);
  await page.getByRole('button',{name:'停止生成',exact:true}).click();
  await page.reload();await page.getByText('当前会话 · API fixture',{exact:true}).waitFor();
  await page.screenshot({path:path.join(output,'selection-restored-desktop.png'),animations:'disabled'});
  await page.setViewportSize({width:390,height:844});
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
  await page.screenshot({path:path.join(output,'selection-mobile.png'),animations:'disabled'});
  assert.deepEqual(errors,[]);
  console.log('PASS model switch pending, rollback, target binding, disabled/active guards, outgoing model, restart UI, Esc/focus, mobile and console');
  console.log(JSON.stringify({output}));
 }finally{await browser.close();}
})().catch(error=>{console.error(error);process.exitCode=1;});
