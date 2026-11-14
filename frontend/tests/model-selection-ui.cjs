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
const localModel = {id:'local-model',name:'Local fixture',model_name:'fixture-local',provider:'ollama',base_url:'http://127.0.0.1:11434/v1',enabled:true};
const apiModel = {id:'api-model',name:'API fixture',model_name:'deepseek-chat',provider:'deepseek',base_url:'https://fixture.invalid',enabled:true};
const disabledModel = {...localModel,id:'disabled',name:'Disabled fixture',model_name:'disabled',enabled:false};

async function checkDefaultOption(browser) {
  const globalModel = {...apiModel,is_default:true};
  const fixtures = [
    {models:[disabledModel,localModel,apiModel], label:'自动回退 · Local fixture'},
    {models:[localModel,globalModel], label:'全局默认 · API fixture'},
    {models:[localModel,globalModel], workspaceDefault:'local-model', label:'工作区默认 · Local fixture'},
    {models:[{...disabledModel,is_default:true},localModel], label:'自动回退 · Local fixture'},
    {models:[globalModel,disabledModel], workspaceDefault:'disabled', label:'工作区默认 · Disabled fixture · 已禁用'},
    {models:[globalModel], workspaceDefault:'missing', label:'工作区默认 · 模型不可用'},
    {models:[{...localModel,name:''}], label:'自动回退 · fixture-local'},
    {models:[{...disabledModel,is_default:true}], workspaceDefault:'disabled', label:'未配置可用模型'},
    {models:[], label:'未配置可用模型'},
  ];
  const context = await createContext(browser, {width:1440,height:960});
  try {
    for (const {models, workspaceDefault = '', label} of fixtures) {
      const page = await context.newPage();
      await page.addInitScript(({models, workspaceDefault}) => {
        const invoke = window.__TAURI_INTERNALS__.invoke;
        window.qa.workspaces[0].default_model_id = workspaceDefault;
        window.__TAURI_INTERNALS__.invoke = async (command, payload) => command === 'get_models' ? {models} : invoke(command, payload);
      }, {models, workspaceDefault});
      await page.goto(base + '#/chat');
      const selector = page.getByRole('combobox');
      await selector.click();
      const options = page.getByRole('option');
      await options.nth(models.length).waitFor();
      assert.equal(await options.first().textContent(), label);
      await page.keyboard.press('Escape');
      await selector.getByText(label, {exact:true}).waitFor();
      await page.close();
    }
  } finally { await context.close(); }
  console.log('PASS chat selector labels for workspace/global defaults, automatic fallback and unavailable models');
}

(async()=>{
 const browser=await chromium.launch({channel:process.env.QA_BROWSER || 'msedge',headless:true});
 try {
  await checkDefaultOption(browser);
  const context=await createContext(browser,{width:1440,height:960});
  await context.addInitScript(({localModel,apiModel,disabledModel})=>{
   const invoke=window.__TAURI_INTERNALS__.invoke;
   const models=[{...localModel,is_default:true},apiModel,disabledModel];
   window.qa.selectionMode='hold';
   window.__TAURI_INTERNALS__.invoke=async(command,payload)=>{
    if(command==='get_models')return {models};
    if(command==='model_engineering'){
     if(payload.request.operation==='list_models')return {models:[],next_cursor:''};
     if(payload.request.operation==='environment')return {environment:null};
    }
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
  }, {localModel,apiModel,disabledModel});
  const page=await context.newPage();await page.goto(base+'#/chat');
  const input=page.getByRole('textbox',{name:'输入问题',exact:true});await input.waitFor();
  await page.getByRole('combobox').waitFor({state:'visible'});
  await page.waitForFunction(()=>!document.querySelector('[role=combobox]').disabled);
  await page.getByRole('combobox').click();
  assert.equal(await page.getByRole('option',{name:/Disabled fixture/}).getAttribute('aria-disabled'),'true');
  await page.keyboard.press('Escape');await page.getByRole('listbox').waitFor({state:'hidden'});
  assert.equal(await page.getByRole('combobox').evaluate(el=>el===document.activeElement),true);
  await page.getByRole('navigation',{name:'主导航'}).getByRole('link',{name:'模型',exact:true}).click();
  await page.getByRole('heading',{name:'Model Hub',exact:true}).waitFor();
  // The Model Hub must switch a session in the current workspace.
  await page.evaluate(async()=>{
    const {useWorkspaceStore}=await import('/src/stores/workspace-store.ts');
    const {useChatStore}=await window.qa.store();const state=useWorkspaceStore.getState();
    useWorkspaceStore.setState({workspaces:[...state.workspaces,{...state.workspaces[0],id:'other',name:'Other workspace'}]});
    useWorkspaceStore.getState().select('other');window.qa.otherSession=useChatStore.getState().newSession();
    useWorkspaceStore.getState().select('local');
  });
  const apiCard=page.getByRole('article').filter({has:page.getByRole('button',{name:'查看模型 API fixture',exact:true})});
  await apiCard.getByRole('button',{name:'使用',exact:true}).click();
  await page.getByRole('status').filter({hasText:'正在验证模型连接，确认后切换目标会话'}).waitFor();
  assert.equal(await page.evaluate(async()=>{const {useChatStore}=await window.qa.store();return useChatStore.getState().sessions.find(s=>s.id===useChatStore.getState().activeId).modelId;}),'');
  await page.evaluate(()=>{window.qa.selectionMode='ok';window.qa.releaseSelection();});
  await apiCard.getByRole('button',{name:'当前使用',exact:true}).waitFor();
  assert.equal(await page.evaluate(async()=>{const {useChatStore}=await window.qa.store();return useChatStore.getState().sessions.find(s=>s.id===window.qa.otherSession).modelId;}),'');
  await page.getByRole('status').filter({hasText:'当前会话已切换到 API fixture'}).waitFor();
  const localCard=page.getByRole('article').filter({has:page.getByRole('button',{name:'查看模型 Local fixture',exact:true})});
  await page.evaluate(()=>window.qa.selectionMode='fail');
  await localCard.getByRole('button',{name:'使用',exact:true}).click();
  await page.getByRole('alert').filter({hasText:'Provider unavailable'}).waitFor();
  assert.equal(await apiCard.getByRole('button',{name:'当前使用',exact:true}).isDisabled(),true);
  await page.screenshot({path:path.join(output,'selection-failure-desktop.png'),animations:'disabled'});
  await page.goto(base+'#/chat');
  const selector=page.getByRole('combobox');
  await selector.getByText('API fixture',{exact:true}).waitFor();
  await page.evaluate(()=>{window.qa.selectionMode='ok';});
  await selector.click();
  await page.getByRole('option',{name:'Local fixture',exact:true}).click();
  const sessionModel=selector.getByText('Local fixture',{exact:true});
  await sessionModel.waitFor();
  await input.fill('prove outgoing model');await page.getByRole('button',{name:'发送',exact:true}).click();
  await page.waitForFunction(()=>window.qa.runs.length===1);
  assert.equal(await page.evaluate(()=>window.qa.runs[0].modelId),'local-model');
  assert.equal(await page.getByRole('combobox').isDisabled(),true);
  await page.getByRole('button',{name:'停止生成',exact:true}).click();
  await page.reload();await sessionModel.waitFor();
  await page.screenshot({path:path.join(output,'selection-restored-desktop.png'),animations:'disabled'});
  await page.setViewportSize({width:390,height:844});
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
  await page.screenshot({path:path.join(output,'selection-mobile.png'),animations:'disabled'});
  assert.deepEqual(errors,[]);
  console.log('PASS model switch pending, rollback, target binding, disabled/active guards, outgoing model, restart UI, Esc/focus, mobile and console');
  console.log(JSON.stringify({output}));
 }finally{await browser.close();}
})().catch(error=>{console.error(error);process.exitCode=1;});
