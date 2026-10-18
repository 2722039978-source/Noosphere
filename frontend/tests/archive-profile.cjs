// Isolated renderer probe. Frame time is informational; deterministic render cost
// and draw-buffer dimensions are compared on the same machine/browser.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const { createContext } = require('./ui-regression.cjs');
const fs = require('node:fs');
(async () => {
 const browser = await chromium.launch({channel:'msedge',headless:true});
 try {
  const context = await createContext(browser,{width:1280,height:800});
  await context.addInitScript(() => localStorage.setItem('noosphere-experience-v1',JSON.stringify({graphics:'high',boot:false,startPage:'archive'})));
  const page = await context.newPage();
  await page.goto('http://127.0.0.1:1420/#/chat');
  await page.getByRole('textbox',{name:'输入问题'}).waitFor();
  await page.evaluate(async()=>{
   const source=await (await fetch('/src/features/home/ArchiveCanvas.tsx')).text();
   const sceneUrl=source.match(/from "([^"\n]*\/scene\/scene\.ts[^"\n]*)"/)[1];
   const {ArchiveScene}=await import(sceneUrl);
   const update=ArchiveScene.prototype.update;
   window.probe={samples:[],scene:null};
   ArchiveScene.prototype.update=function(time){ const start=performance.now(); update.call(this,time); window.probe.scene=this; window.probe.samples.push({time:performance.now(),cpu:performance.now()-start,...this.renderer.info.render}); };
  });
  await page.getByRole('link',{name:'Noosphere 工作台',exact:true}).click();
  await page.locator('.ns-full-scene[data-loaded=true]').waitFor();
  await page.waitForTimeout(5000);
  await page.evaluate(()=>window.probe.samples=[]);
  await page.getByRole('button',{name:'下一个档案'}).click();
  await page.waitForTimeout(4700);
  const report=await page.evaluate(()=>{
   const {samples,scene}=window.probe, gl=scene.renderer.getContext(), ext=gl.getExtension('WEBGL_debug_renderer_info');
   const avg=k=>samples.reduce((n,x)=>n+x[k],0)/samples.length;
   const intervals=samples.slice(1).map((x,i)=>x.time-samples[i].time).sort((a,b)=>a-b);
   return {frames:samples.length,meanCpuMs:avg('cpu'),meanDrawCalls:avg('calls'),meanTriangles:avg('triangles'),p95FrameMs:intervals[Math.floor(intervals.length*.95)],buffer:[gl.drawingBufferWidth,gl.drawingBufferHeight],gpu:ext?gl.getParameter(ext.UNMASKED_RENDERER_WEBGL):'unknown',postprocessing:scene.composer?.passes.map(p=>({name:p.constructor.name,enabled:p.enabled}))};
  });
  console.log(JSON.stringify(report,null,2));
  if(process.env.PROFILE_ASSERT){
   const assert=require('node:assert/strict');
   assert.ok(report.meanDrawCalls<70, `too many draw calls: ${report.meanDrawCalls}`);
   assert.ok(report.meanTriangles<6000000, `too many rendered triangles: ${report.meanTriangles}`);
   assert.ok(!report.postprocessing?.some(p=>p.name==='BokehPass'&&p.enabled),'workspace should stay in focus');
  }
  if(process.env.PROFILE_REPORT)fs.writeFileSync(process.env.PROFILE_REPORT,JSON.stringify(report,null,2));
  if(process.env.PROFILE_SCREENSHOT)await page.screenshot({path:process.env.PROFILE_SCREENSHOT});
 } finally {await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
