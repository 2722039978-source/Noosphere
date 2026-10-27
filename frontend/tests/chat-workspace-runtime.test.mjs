import test from 'node:test';
import assert from 'node:assert/strict';
import { build } from 'esbuild';
import { IDBFactory } from 'fake-indexeddb';
import { mkdtemp, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

// Only desktop IPC is replaced. The real stores, persistence and stream runner run below.
const requests=[], searches=[];
let resolveSearch;
globalThis.__workspaceChatApi={
  selectModel: async (selection) => ({...selection,model_id:selection.model_id || 'default',bound:true}),
  getModelSelection: async (workspaceId,sessionId) => ({workspace_id:workspaceId,session_id:sessionId,model_id:'',bound:false}),
  prepareContext: (id) => { searches.push(id); return new Promise(resolve=>{resolveSearch=resolve;}); },
  chatStream: async (...args)=>{requests.push(args);}, cancelChat:async()=>{},
  listWorkspaces:async()=>({workspaces:[]}),
};
globalThis.indexedDB=new IDBFactory();
globalThis.localStorage={getItem:()=>null,setItem:()=>{},removeItem:()=>{}};
globalThis.window={localStorage:globalThis.localStorage,addEventListener(){},removeEventListener(){}};
globalThis.document={addEventListener(){},removeEventListener(){}};
const result=await build({stdin:{contents:`export * from './src/features/chat/chat-runtime'; export * from './src/features/chat/chat-store'; export {useWorkspaceStore} from './src/stores/workspace-store';`,resolveDir:process.cwd()},bundle:true,write:false,format:'esm',platform:'browser',plugins:[{name:'desktop-boundary',setup(build){
  build.onResolve({filter:/^@\/lib\/api$/},()=>({path:'api',namespace:'stub'}));
  build.onLoad({filter:/.*/,namespace:'stub'},()=>({contents:'export const api=globalThis.__workspaceChatApi; export const errMessage=e=>String(e);',loader:'js'}));
  build.onResolve({filter:/^@tauri-apps\/api\/core$/},()=>({path:'channel',namespace:'channel'}));
  build.onLoad({filter:/.*/,namespace:'channel'},()=>({contents:'export class Channel {}',loader:'js'}));
}}]});
const folder=await mkdtemp(join(tmpdir(),'noosphere-chat-test-'));
const bundle=join(folder,'runtime.mjs');
await writeFile(bundle,result.outputFiles[0].text);
const runtime=await import(pathToFileURL(bundle).href);
test.after(async()=>{await runtime.prepareChatShutdown();await rm(folder,{recursive:true});});
const tick=()=>new Promise(setImmediate);
while(!runtime.useChatPersistence.getState().ready) await tick();
const workspace=(id, project_id)=>({id,name:id,project_id,project_path:`C:/${id}`,default_model_id:'default',created_at:'',updated_at:''});
runtime.useWorkspaceStore.setState({activeId:'alpha',workspaces:[workspace('alpha','project-alpha'),workspace('beta','project-beta')]});

test('new sessions and send capture workspace/project/model before a project switch',async()=>{
  const id=runtime.useChatStore.getState().newSession();
  assert.equal(runtime.useChatStore.getState().sessions.find(s=>s.id===id).workspaceId,'alpha');
  await runtime.useChatStore.getState().setModel('explicit');
  assert.equal(runtime.sendChat(id,'hello alpha'),true);
  runtime.useWorkspaceStore.getState().select('beta');
  const beta=runtime.useChatStore.getState().newSession();
  await runtime.useChatStore.getState().setModel('other');
  assert.equal(runtime.sendChat(beta,'busy'),false);
  assert.deepEqual(searches,['alpha']);
  resolveSearch({workspace_id:'alpha',project_id:'project-alpha',context:'[R1] SQLite decision',mode:'lexical',warnings:[],citations:[{id:'R1',kind:'memory',title:'decision',excerpt:'SQLite',workspace_id:'alpha',memory_id:'m1',revision:1}]}); await tick();
  assert.equal(requests.length,1);
  assert.equal(requests[0][3],'explicit');
  assert.equal(requests[0][4],'alpha');
  assert.equal(requests[0][5],id);
  assert.match(requests[0][1].at(-1).content,/\[R1\] SQLite decision/);
  assert.equal(runtime.useChatStore.getState().sessions.find(s=>s.id===id).messages.at(-1).citations[0].memory_id,'m1');
  requests[0][2].onmessage({done:true,content:'answer alpha'});
  assert.equal(runtime.useChatStore.getState().sessions.find(s=>s.id===id).messages.at(-1).content,'answer alpha');
  assert.equal(runtime.useChatStore.getState().sessions.find(s=>s.id===beta).messages.length,0);
  assert.equal(runtime.sendChat(id,'wrong workspace'),false);
});

test('selection and model edits cannot mutate a session outside selected workspace',async()=>{
  const alpha=runtime.useChatStore.getState().sessions.find(s=>s.workspaceId==='alpha');
  const beta=runtime.useChatStore.getState().activeId;
  runtime.useChatStore.getState().setActive(alpha.id);
  assert.equal(runtime.useChatStore.getState().activeId,beta);
  runtime.useChatStore.setState({activeId:alpha.id});
  await runtime.useChatStore.getState().setModel('leak');
  assert.equal(runtime.useChatStore.getState().sessions.find(s=>s.id===alpha.id).modelId,'explicit');
});

test('session switch waits for backend validation, updates captured session and persists across reload',async()=>{
  runtime.useWorkspaceStore.getState().select('alpha');
  const target=runtime.useChatStore.getState().newSession();
  await runtime.useChatStore.getState().setModel('old');
  let finish;
  const original=globalThis.__workspaceChatApi.selectModel;
  globalThis.__workspaceChatApi.selectModel=(input)=>new Promise(resolve=>{finish=()=>resolve({...input,bound:true});});
  const switching=runtime.useChatStore.getState().setModel('new');
  assert.equal(runtime.useChatStore.getState().sessions.find(s=>s.id===target).modelId,'old');
  assert.equal(runtime.sendChat(target,'cannot race pending switch'),false);
  runtime.useWorkspaceStore.getState().select('beta');
  const other=runtime.useChatStore.getState().newSession();
  finish();assert.equal(await switching,true);
  assert.equal(runtime.useChatStore.getState().activeId,other);
  assert.equal(runtime.useChatStore.getState().sessions.find(s=>s.id===target).modelId,'new');
  assert.equal(runtime.useChatStore.getState().sessions.find(s=>s.id===other).modelId,'');
  await runtime.flushChatPersistence();
  await runtime.useChatStore.persist.rehydrate();
  assert.equal(runtime.useChatStore.getState().sessions.find(s=>s.id===target).modelId,'new');
  globalThis.__workspaceChatApi.selectModel=original;
});

test('backend failure and active generation preserve the old session model',async()=>{
  runtime.useWorkspaceStore.getState().select('alpha');
  const id=runtime.useChatStore.getState().newSession();
  await runtime.useChatStore.getState().setModel('old');
  const original=globalThis.__workspaceChatApi.selectModel;
  globalThis.__workspaceChatApi.selectModel=async()=>{throw new Error('Provider unavailable');};
  assert.equal(await runtime.useChatStore.getState().setModel('bad'),false);
  assert.equal(runtime.useChatStore.getState().sessions.find(s=>s.id===id).modelId,'old');
  assert.match(runtime.useChatStore.getState().modelSelectionError,/Provider unavailable/);
  globalThis.__workspaceChatApi.selectModel=original;
  runtime.useChatStore.getState().appendMessage(id,{id:'running',role:'assistant',content:'',streaming:true,status:'preparing',createdAt:Date.now()});
  assert.equal(await runtime.useChatStore.getState().setModel('new'),false);
  assert.equal(runtime.useChatStore.getState().sessions.find(s=>s.id===id).modelId,'old');
  runtime.useChatStore.getState().finishMessage(id,'running');
});

test('restoring a session replaces stale frontend binding with the backend binding',async()=>{
  runtime.useWorkspaceStore.getState().select('alpha');
  const id=runtime.useChatStore.getState().newSession();
  await runtime.useChatStore.getState().setModel('old-cache');
  const original=globalThis.__workspaceChatApi.getModelSelection;
  globalThis.__workspaceChatApi.getModelSelection=async(workspaceId,sessionId)=>({workspace_id:workspaceId,session_id:sessionId,model_id:'server-model',bound:true});
  assert.equal(await runtime.useChatStore.getState().syncModel(id),true);
  assert.equal(runtime.useChatStore.getState().sessions.find(s=>s.id===id).modelId,'server-model');
  globalThis.__workspaceChatApi.getModelSelection=original;
});

test('foreign workspace backups create isolated selectable workspaces and repeated imports do not duplicate them',async()=>{
  let count=0;
  globalThis.__workspaceChatApi.listWorkspaces=async()=>({workspaces:runtime.useWorkspaceStore.getState().workspaces});
  globalThis.__workspaceChatApi.saveWorkspace=async(input)=>({...workspace(`restored-${++count}`,''),...input,project_id:''});
  const incoming={sessions:['one','two'].map(id=>({id:`foreign-${id}`,workspaceId:'foreign-origin',title:id,modelId:'remote-model',messages:[],createdAt:1,updatedAt:1})),activeId:'foreign-one'};
  const backup=JSON.stringify({format:'noosphere-chat-backup',version:1,state:incoming});
  await runtime.importChatBackup(backup);
  const restored=runtime.useWorkspaceStore.getState().workspaces.find(w=>w.id==='restored-1');
  assert.ok(restored,'imported conversations have no selectable workspace');
  assert.equal(restored.project_path,'');assert.equal(restored.project_id,'');
  const sessions=runtime.useChatStore.getState().sessions.filter(s=>s.id.startsWith('foreign-'));
  assert.equal(sessions.length,2);assert.ok(sessions.every(s=>s.workspaceId===restored.id&&s.modelId===''));
  await runtime.importChatBackup(backup);assert.equal(count,1);
});

test('failed workspace recovery cannot silently merge inaccessible conversations',async()=>{
  globalThis.__workspaceChatApi.saveWorkspace=async()=>{throw new Error('backend unavailable');};
  const sessions=runtime.useChatStore.getState().sessions;
  const backup=JSON.stringify({state:{sessions:[{id:'not-imported',workspaceId:'another-foreign',title:'new',modelId:'',messages:[],createdAt:1,updatedAt:1}],activeId:'not-imported'}});
  await assert.rejects(runtime.importChatBackup(backup),/backend unavailable/);
  assert.equal(runtime.useChatStore.getState().sessions,sessions);
});

test('foreign retrieval evidence is excluded and produces a visible warning',async()=>{
  runtime.useWorkspaceStore.getState().select('beta');
  const id=runtime.useChatStore.getState().newSession();
  assert.equal(runtime.sendChat(id,'isolation check'),true);
  resolveSearch({workspace_id:'alpha',project_id:'project-alpha',context:'FOREIGN SECRET',mode:'lexical',warnings:[],citations:[]});await tick();
  const request=requests.at(-1);assert.ok(!request[1].at(-1).content.includes('FOREIGN SECRET'));
  const message=runtime.useChatStore.getState().sessions.find(s=>s.id===id).messages.at(-1);
  assert.deepEqual(message.citations,[]);assert.match(message.contextWarnings[0],/绑定已变化/);
  request[2].onmessage({done:true,content:'safe answer'});
});
