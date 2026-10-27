import test from 'node:test';
import assert from 'node:assert/strict';
import { loadSource } from './load-source.mjs';

test('primary navigation has eight distinct destinations with model engineering grouped under one entry',async()=>{
 const {buildPrimaryNavigation}=await loadSource('../src/app/model-navigation.ts');
 const {WORKSPACE_MODULES}=await loadSource('../src/app/modules.ts');
 const items=buildPrimaryNavigation(WORKSPACE_MODULES);
 assert.deepEqual(items.map(item=>item.label),['工作台','AI 对话','CodeLens','Nebula','DevOps','Gateway','模型档案','设置']);
 assert.equal(new Set(items.map(item=>item.to)).size,8);
 assert.ok(!items.some(item=>['/model-lab','/evaluation-reports','/release-center'].includes(item.to)));
});

test('model detail routes retain their section and primary model archive selection',async()=>{
 const {MODEL_SECTIONS,modelSection,primaryNavigationPath}=await loadSource('../src/app/model-navigation.ts');
 assert.deepEqual(MODEL_SECTIONS.map(s=>s.label),['Model Hub','Model Lab','Reports','Release Center']);
 for(const [path,section] of [['/model-hub','hub'],['/model-lab','lab'],['/evaluation-reports/report-1','reports'],['/release-center/release-1','releases']]){
  assert.equal(modelSection(path)?.id,section);assert.equal(primaryNavigationPath(path),'/model-hub');
 }
 assert.equal(modelSection('/model-laboratory'),undefined);
 assert.equal(primaryNavigationPath('/home'),'/home');
 assert.equal(primaryNavigationPath('/gateway'),'/gateway');
});
