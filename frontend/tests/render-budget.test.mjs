import test from 'node:test';
import assert from 'node:assert/strict';
import { loadSource } from './load-source.mjs';
test('frame budget detects sustained GPU/RAF stalls even when CPU submission is fast', async()=>{
 const {FrameBudget}=await loadSource('../src/features/home/render-budget.ts');
 const budget=new FrameBudget(); let slow=false;
 for(let n=0;n<55;n++)slow=budget.sample(n*40)||slow;
 assert.equal(slow,true);
 const fast=new FrameBudget();
 for(let n=0;n<120;n++)assert.equal(fast.sample(n*16.67),false);
 fast.reset(); assert.equal(fast.sample(90000),false);
});
test('render size preserves native clarity while bounding high-DPI pixel cost',async()=>{
 const {renderPixelRatio}=await loadSource('../src/features/home/render-budget.ts');
 assert.equal(renderPixelRatio(1280,800,1.25,true),1.25);
 assert.equal(renderPixelRatio(1280,800,1.25,false),1.25);
 assert.ok(renderPixelRatio(3840,2160,2,true)<=1.01);
 assert.equal(renderPixelRatio(900,600,1,true),1);
});
