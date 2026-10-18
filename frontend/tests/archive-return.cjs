// Browser plugin not available: isolated Playwright + synthetic IPC, no user data.
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const {createContext,errors}=require('./ui-regression.cjs');
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),os=require('node:os');
const output=process.env.QA_OUTPUT||path.join(os.tmpdir(),'noosphere-archive-return');fs.mkdirSync(output,{recursive:true});
const base=process.env.QA_URL||'http://127.0.0.1:1420/';
async function settled(page){await page.locator('.ns-return-stage[data-phase="idle"]').waitFor();await page.locator('.ns-archive-app').waitFor();}
(async()=>{
 const browser=await chromium.launch({channel:'msedge',headless:true});
 try {
  const context=await createContext(browser,{width:1440,height:960});
  await context.addInitScript(()=>{if(location.protocol==='http:')localStorage.setItem('noosphere-experience-v1',JSON.stringify({boot:true,graphics:'high',startPage:'archive'}));});
  const page=await context.newPage();await page.goto(base+'#/codelens');
  await page.getByRole('heading',{name:'CodeLens',exact:true}).waitFor();
  await page.getByRole('link',{name:'Noosphere 工作台',exact:true}).click();
  await page.locator('.ns-return-stage[data-phase="departing"]').waitFor({timeout:2500});
  assert.equal(await page.locator('.ns-app-shell').count(),1,'outgoing workspace stays mounted for its exit');
  await page.screenshot({path:path.join(output,'return-departure.png')});
  await page.locator('.ns-return-stage[data-phase="loading"]').waitFor();
  assert.equal(await page.locator('.ns-boot').count(),0,'return must not replay first-launch boot');
  await page.locator('.ns-return-stage[data-phase="arriving"]').waitFor({timeout:15000});
  assert.equal(await page.locator('.ns-full-scene[data-loaded=true]').count(),1,'reveal must wait for the scene');
  await page.screenshot({path:path.join(output,'return-reveal.png')});await settled(page);
  assert.equal(await page.getByRole('button',{name:'选择CodeLens',exact:true}).getAttribute('aria-pressed'),'true');
  await page.screenshot({path:path.join(output,'returned-home.png')});
  console.log('PASS workspace exit, scene-ready reveal, restored selection, no repeated boot');
  await page.getByRole('link',{name:/进入CodeLens/}).click();await page.waitForURL('**/#/codelens');
  await page.locator('.ns-dossier-handoff').waitFor({state:'detached'});await page.goBack();
  await page.locator('.ns-return-stage[data-phase="departing"]').waitFor();await settled(page);
  console.log('PASS history back uses the same return transition');
  await page.getByRole('link',{name:/进入CodeLens/}).click();await page.waitForURL('**/#/codelens');
  await page.locator('.ns-dossier-handoff').waitFor({state:'detached'});
  await page.getByRole('navigation',{name:'主导航'}).getByRole('link',{name:/工作台/}).click();
  await page.locator('.ns-return-stage[data-phase="departing"]').waitFor();
  await page.evaluate(()=>{location.hash='/nebula';});await page.getByRole('heading',{name:'Nebula',exact:true}).waitFor();
  await page.waitForTimeout(800);assert.match(page.url(),/#\/nebula$/);assert.equal(await page.locator('.ns-return-handoff').count(),0);
  console.log('PASS newer navigation cancels stale return timers');
  await page.emulateMedia({reducedMotion:'reduce'});await page.getByRole('link',{name:'Noosphere 工作台',exact:true}).click();
  await page.locator('.ns-archive-app').waitFor();assert.equal(await page.locator('.ns-return-handoff').count(),0);
  console.log('PASS reduced motion returns directly');
  const mobile=await createContext(browser,{width:390,height:844});
  await mobile.addInitScript(()=>{if(location.protocol==='http:')localStorage.setItem('noosphere-experience-v1',JSON.stringify({boot:false,graphics:'off',startPage:'archive'}));});
  const compact=await mobile.newPage();await compact.goto(base+'#/gateway');
  await compact.getByRole('button',{name:'添加模型',exact:true}).click();await compact.getByLabel('显示名称',{exact:true}).fill('Unsaved fixture');
  await compact.getByRole('link',{name:'Noosphere 工作台',exact:true}).click();await settled(compact);
  assert.equal(await compact.locator('canvas').count(),0);assert.equal(await compact.getByRole('button',{name:'选择Gateway',exact:true}).getAttribute('aria-pressed'),'true');
  await compact.screenshot({path:path.join(output,'returned-compact.png')});
  console.log('PASS nested form -> home and compact static fallback');
  const fallback=await createContext(browser,{width:1280,height:800});
  await fallback.addInitScript(()=>{
   if(location.protocol==='http:')localStorage.setItem('noosphere-experience-v1',JSON.stringify({boot:false,graphics:'high',startPage:'archive'}));
   const getContext=HTMLCanvasElement.prototype.getContext;
   HTMLCanvasElement.prototype.getContext=function(type,...args){if(type.includes('webgl'))throw new Error('fixture: GPU unavailable');return getContext.call(this,type,...args);};
  });
  const failed=await fallback.newPage();await failed.goto(base+'#/nebula');
  await failed.getByRole('link',{name:'Noosphere 工作台',exact:true}).click();await settled(failed);
  await failed.getByText('三维预览暂不可用 · 所有模块仍可访问').waitFor();
  assert.equal(await failed.locator('[inert]').count(),0);
  console.log('PASS unavailable GPU releases the cover and leaves the home usable');
  const slow=await createContext(browser,{width:1280,height:800});
  await slow.addInitScript(()=>{if(location.protocol==='http:')localStorage.setItem('noosphere-experience-v1',JSON.stringify({boot:false,graphics:'high',startPage:'archive'}));});
  const delayed=await slow.newPage();await delayed.goto(base+'#/chat');
  await delayed.evaluate(async()=>{
   const src=await(await fetch('/src/features/home/ArchiveCanvas.tsx')).text();
   const {ArchiveScene}=await import(src.match(/from "([^"\n]*\/scene\/scene\.ts[^"\n]*)"/)[1]);
   const prepare=ArchiveScene.prototype.prepare;
   ArchiveScene.prototype.prepare=async function(){await prepare.call(this);await new Promise(resolve=>{window.releaseReturnPreparation=resolve;});};
  });
  await delayed.getByRole('link',{name:'Noosphere 工作台',exact:true}).click();
  await delayed.locator('.ns-return-stage[data-phase="loading"]').waitFor();await settled(delayed);
  assert.equal(await delayed.locator('.ns-full-scene[data-loaded=true]').count(),0,'timeout must not depend on stalled GPU preparation');
  await delayed.getByRole('link',{name:'AI 对话',exact:true}).click();await delayed.waitForURL('**/#/chat');
  await delayed.evaluate(()=>window.releaseReturnPreparation());await delayed.waitForTimeout(100);
  assert.equal(await delayed.locator('.ns-return-handoff').count(),0);
  assert.deepEqual(errors.filter(message=>message!=='THREE.WebGLRenderer: fixture: GPU unavailable'),[]);
  console.log('PASS preparation timeout unlocks navigation and late completion cannot revive the return (only deliberate GPU fixture errors)');
 } finally {await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
