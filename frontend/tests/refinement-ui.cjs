const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const {createContext,errors}=require('./ui-regression.cjs');
const assert=require('node:assert/strict'),path=require('node:path'),fs=require('node:fs');
const output=process.env.QA_OUTPUT||path.resolve('../.local/qa/refinement-20261007');fs.mkdirSync(output,{recursive:true});
(async()=>{
 const browser=await chromium.launch({channel:'msedge',headless:true});
 try{
  const context=await createContext(browser,{width:1440,height:900});
  await context.addInitScript(()=>{const invoke=window.__TAURI_INTERNALS__.invoke; window.__TAURI_INTERNALS__.invoke=async(command,payload)=>{
   if(command==='codelens_search'){if(window.qa.failSearch)throw new Error('fixture: index unavailable');return {results:[{file:'src/main.ts',line:12,content:'export function connectKnowledge() { return "Noosphere"; }'}]};}
   if(command==='codelens_analyze')return {entities:[{kind:'function',name:'connectKnowledge',line:12}]};
   return invoke(command,payload);
  };});
  const page=await context.newPage();await page.goto('http://127.0.0.1:1420/#/codelens');
  await page.getByRole('heading',{name:'CodeLens',exact:true}).waitFor();
  assert.equal(await page.locator('.ns-section-number').count(),3);
  assert.equal(await page.locator('.ns-workspace-signal span').evaluate(el=>getComputedStyle(el).animationName),'ns-orbit-turn');
  const orbitBefore=await page.locator('.ns-workspace-signal span').evaluate(el=>getComputedStyle(el).transform);
  await page.waitForTimeout(150);
  assert.notEqual(await page.locator('.ns-workspace-signal span').evaluate(el=>getComputedStyle(el).transform),orbitBefore);
  await page.screenshot({animations:'disabled',path:path.join(output,'codelens-overview.png')});
  await page.getByRole('textbox',{name:'代码检索'}).fill('connect');await page.getByRole('button',{name:'搜索',exact:true}).click();
  await page.locator('.ns-record-detail summary').click();
  await page.getByText('export function connectKnowledge()', {exact:false}).waitFor();
  assert.equal(await page.locator('.ns-record-detail[open] .ns-detail-body').evaluate(el=>getComputedStyle(el).animationName),'ns-panel-enter');
  await page.screenshot({animations:'disabled',path:path.join(output,'codelens-detail.png')});
  await page.getByRole('button',{name:'在文件分析中打开'}).click();
  assert.equal(await page.getByRole('textbox',{name:'分析文件路径'}).inputValue(),'src/main.ts');
  await page.getByRole('button',{name:'分析',exact:true}).click();await page.getByText('connectKnowledge',{exact:true}).waitFor();
  console.log('PASS numbered CodeLens flow, expanding code record, and file analysis handoff');
  await page.evaluate(()=>window.qa.failSearch=true);await page.getByRole('button',{name:'搜索',exact:true}).click();
  await page.getByRole('alert').filter({hasText:'fixture: index unavailable'}).waitFor();
  console.log('PASS search failure is distinct from an empty result');
  await page.goto('http://127.0.0.1:1420/#/gateway');await page.getByRole('button',{name:'添加模型',exact:true}).click();
  assert.equal(await page.getByRole('form',{name:'新增模型接入'}).evaluate(el=>getComputedStyle(el).animationName),'ns-panel-enter');
  await page.screenshot({animations:'disabled',path:path.join(output,'gateway-form.png')});
  await page.emulateMedia({reducedMotion:'reduce'});
  assert.equal(await page.locator('.ns-workspace-signal span').evaluate(el=>getComputedStyle(el).animationName),'none');
  console.log('PASS persistent lightweight signal, form transitions, and reduced-motion setting');
  assert.deepEqual(errors,[]);
  await context.close();
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
