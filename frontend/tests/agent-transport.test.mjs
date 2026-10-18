import test from 'node:test';
import assert from 'node:assert/strict';
import { loadSource } from './load-source.mjs';
const { createAgentTransport } = await loadSource('../src/features/chat/agent-transport.ts');
const tick=()=>new Promise(resolve=>setTimeout(resolve,10));
const run={id:'run-1',workspace_id:'ws',status:'running',phase:'agent_model',result:null};

test('delayed registration remains cancellable by the original request id',async()=>{
 let accept;const cancelled=[];const updates=[];
 const transport=createAgentTransport({submit:()=>new Promise(resolve=>{accept=resolve}),get:async()=>run,events:async()=>({events:[]}),cancel:async id=>{cancelled.push(id)}},{update:(...a)=>updates.push(a),cancelError:()=>{}},5);
 const ack=transport.start('req','ws','session','model','goal',()=>{});
 accept(run);await ack;await transport.cancel('req');await tick();
 assert.deepEqual(cancelled,['run-1']);assert.equal(updates[0][1].agentRunId,'run-1');
});

test('persisted terminal output is delivered once and workspace mismatches cancel',async()=>{
 const received=[];const cancelled=[];
 const transport=createAgentTransport({submit:async()=>run,get:async()=>({...run,status:'completed',result:{answer:'done',steps:[],evidence:[],model_calls:1,tool_calls:0,tokens_in:0,tokens_out:0,stop_reason:'completed',workspace_id:'ws',project_id:'',model_id:'model'}}),events:async()=>({events:[]}),cancel:async id=>{cancelled.push(id)}},{update:()=>{},cancelError:()=>{}},5);
 await transport.start('req','ws','s','m','g',event=>received.push(event));await tick();await tick();assert.equal(received.length,1);assert.equal(received[0].content,'done');
 const invalid=createAgentTransport({submit:async()=>run,get:async()=>({...run,workspace_id:'other'}),events:async()=>({events:[]}),cancel:async id=>{cancelled.push(id)}},{update:()=>{},cancelError:()=>{}},5);
 await invalid.start('bad','ws','s','m','g',event=>received.push(event));await tick();await tick();assert.equal(received.at(-1).status,'failed');assert.deepEqual(cancelled,['run-1']);
});
