// Browser plugin is absent; use the project's isolated Playwright/IPC harness.
// Flow: cold archive -> select module -> continuous departure -> workspace -> provider preset/test/switch.
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const {createContext,errors}=require('./ui-regression.cjs');
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),os=require('node:os');
const output=process.env.QA_OUTPUT||path.join(os.tmpdir(),'noosphere-20261008');fs.mkdirSync(output,{recursive:true});
(async()=>{
 const browser=await chromium.launch({channel:'msedge',headless:true});
 try {
  const context=await createContext(browser,{width:1280,height:800});
  await context.addInitScript(()=>{
   if (location.protocol !== 'http:') return;
   localStorage.setItem('noosphere-experience-v1',JSON.stringify({boot:false,graphics:'high',startPage:'archive'}));
   const invoke=window.__TAURI_INTERNALS__.invoke;
   window.qa.models=[];window.qa.testFail=false;
   window.__TAURI_INTERNALS__.invoke=async(command,payload)=>{
    if(command==='get_models')return {models:window.qa.models};
    if(command==='add_model') {window.qa.models=window.qa.models.filter(m=>m.id!==payload.model.id).map(m=>({...m,is_default:payload.model.is_default?false:m.is_default}));window.qa.models.push({...payload.model,api_key:undefined,enabled:true});return {};}
    if(command==='test_model')return window.qa.testFail?{status:'error',error:'upstream 401'}:{status:'ok',latency_ms:120};
    return invoke(command,payload);
   };
  });
  const page=await context.newPage();
  await page.goto('http://127.0.0.1:1420/#/home');
  await page.locator('.ns-full-scene[data-loaded=true]').waitFor({timeout:30000});
  await page.getByRole('button',{name:'选择CodeLens',exact:true}).click();
  await page.waitForTimeout(1200);
  await page.getByRole('link',{name:'进入CodeLens',exact:false}).click();
  assert.match(page.url(),/#\/home$/,'route must wait for the archive departure');
  assert.equal(await page.locator('.ns-archive-app.is-departing.is-inspecting').count(),1);
  await page.waitForTimeout(180);
  assert.notEqual(await page.locator('.ns-archive-visual').evaluate(el=>getComputedStyle(el).transform),'none');
  await page.screenshot({path:path.join(output,'archive-departing.png')});
  await page.waitForURL('**/#/codelens');
  await page.getByRole('heading',{name:'CodeLens',exact:true}).waitFor();
  await page.locator('.ns-dossier-handoff').waitFor({state:'detached'});
  await page.screenshot({path:path.join(output,'workspace-arrived.png')});
  assert.equal(await page.locator('canvas').count(),0,'archive renderer must be released after navigation');
  console.log('PASS archive selection, departure animation, route handoff, and scene disposal');
  await page.goto('http://127.0.0.1:1420/#/gateway');
  await page.getByRole('button',{name:'添加模型',exact:true}).click();
  await page.getByRole('button',{name:'DeepSeek 官方',exact:true}).click();
  assert.equal(await page.getByLabel('接口地址',{exact:true}).inputValue(),'https://api.deepseek.com');
  assert.equal(await page.getByLabel('模型名称',{exact:true}).inputValue(),'deepseek-flash');
  await page.getByLabel('API 密钥',{exact:true}).fill('fixture-not-a-real-key');
  await page.getByRole('button',{name:'保存',exact:true}).click();
  await page.getByRole('button',{name:'测试连接',exact:true}).click();
  await page.getByText('连接成功 · 120 ms').waitFor();
  await page.evaluate(()=>window.qa.testFail=true);
  await page.getByRole('button',{name:'测试连接',exact:true}).click();
  await page.getByRole('alert').filter({hasText:'密钥无效'}).waitFor();
  await page.getByRole('button',{name:'编辑配置',exact:true}).click();
  assert.equal(await page.getByLabel('API 密钥',{exact:true}).inputValue(),'');
  await page.getByLabel('显示名称',{exact:true}).fill('DeepSeek 主模型');
  await page.getByRole('button',{name:'保存',exact:true}).click();
  await page.getByText('DeepSeek 主模型',{exact:true}).waitFor();
  await page.screenshot({path:path.join(output,'gateway-connected-ui.png')});
  console.log('PASS DeepSeek preset, saved model, success/error feedback, secret-free editing');
  await page.emulateMedia({reducedMotion:'reduce'});
  await page.goto('http://127.0.0.1:1420/#/home');
  await page.getByRole('link',{name:'进入CodeLens',exact:false}).click();
  await page.waitForURL('**/#/codelens');
  assert.equal(await page.locator('.ns-dossier-handoff').count(),0);
  console.log('PASS reduced motion immediate navigation');
  await page.emulateMedia({reducedMotion:'no-preference'});
  await page.evaluate(async () => {
   const source=await (await fetch('/src/features/home/ArchiveCanvas.tsx')).text();
   const sceneUrl=source.match(/from "([^"\n]*\/scene\/scene\.ts[^"\n]*)"/)[1];
   const {ArchiveScene}=await import(sceneUrl),prepare=ArchiveScene.prototype.prepare;
   window.qa.earlyDispose=false;
   ArchiveScene.prototype.prepare=function(){
    const compile=this.renderer.compileAsync.bind(this.renderer),dispose=this.renderer.dispose.bind(this.renderer);
    this.renderer.compileAsync=async(...args)=>{window.qa.compiling=true;await compile(...args);await new Promise(resolve=>setTimeout(resolve,200));window.qa.compiling=false;};
    this.renderer.dispose=()=>{if(window.qa.compiling)window.qa.earlyDispose=true;dispose();};
    return prepare.call(this);
   };
   location.hash='/home';
  });
  await page.waitForFunction(()=>window.qa.compiling);
  await page.evaluate(()=>location.hash='/chat');
  await page.waitForFunction(()=>!window.qa.compiling);
  assert.equal(await page.evaluate(()=>window.qa.earlyDispose),false);
  assert.equal(await page.locator('canvas').count(),0);
  console.log('PASS leaving during shader compilation safely releases the renderer after compilation');
  assert.deepEqual(errors,[]);
  await context.close();
 } finally {await browser.close();}
})().catch(error=>{console.error(error);process.exitCode=1;});
