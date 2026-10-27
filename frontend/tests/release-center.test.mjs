import test from 'node:test';
import assert from 'node:assert/strict';
import { loadSource } from './load-source.mjs';

test('release controls use explicit transitions and matching server preview, never client quality averages', async () => {
 const { commandsFor, previewMatches } = await loadSource('../src/features/modelops/presentation.ts');
 const release={id:'r',workspace_id:'local',mode:'simulation',state:'CANARY_5',revision:4,closed_at:'',previous_stable:'s'};
 assert.deepEqual(commandsFor(release),['advance','pause','rollback']);
 const preview={release_id:'r',revision:4,next_state:'CANARY_25',evidence_hash:'hash',decision:{allowed:true,evidence_hash:'hash'}};
 assert.equal(previewMatches(release,'advance',{command:'advance',value:preview}),true);
 assert.equal(previewMatches({...release,revision:5},'advance',{command:'advance',value:preview}),false);
 assert.equal(previewMatches(release,'rollback',{command:'advance',value:preview}),false);
 assert.equal(previewMatches(release,'advance',{command:'advance',value:{...preview,decision:{allowed:false,evidence_hash:'hash'}}}),false);
 assert.deepEqual(commandsFor({...release,closed_at:'now'}),[]);
});
test('simulation allocation derives only current epoch, non-revoked, admitted assignments and preserves unknown', async()=>{
 const { readSimulation, allocation }=await loadSource('../src/features/modelops/presentation.ts');
 assert.equal(readSimulation({version:'future'}),null);
 const sim={version:'simulation-v1',authorization:'synthetic',clock_seconds:60,stages:[{state:'CANARY_5',admission_epoch:3,cohort_id:'c',window_start_seconds:0,assignments:[{arm:'candidate',admitted_epoch:3,revoked_at:''},{arm:'stable',admitted_epoch:3,revoked_at:'expired'},{arm:'candidate',admitted_epoch:2,revoked_at:''}]}],alerts:[],events:[]};
 const release={state:'CANARY_5',route_epoch:3,admission_enabled:true};
 const counts=allocation(release,readSimulation(sim)); assert.equal(counts.candidate,1);assert.equal(counts.active,1);assert.equal(counts.revoked,1);assert.equal(counts.allocated,3);
 assert.equal(allocation({...release,admission_enabled:false},readSimulation(sim)).candidate,0);
 assert.equal(allocation({...release,state:'ROLLED_BACK',route_epoch:4,admission_enabled:false},readSimulation(sim)).candidate,0);
 assert.equal(allocation(release,null),null);
});

test('resumed Go simulation with null alerts preserves current cohort allocation and raw JSON', async()=>{
 const { readSimulation, allocation }=await loadSource('../src/features/modelops/presentation.ts');
 const stage=(admission_epoch,cohort_id,window_start_seconds)=>({
  admission_epoch,state:'CANARY_5',cohort_id,window_start_seconds,replayed:true,
  assignments:Array.from({length:20},(_,i)=>({session_key:`${cohort_id}-${String(i).padStart(2,'0')}`,cohort_id,arm:i===0?'candidate':'stable',admitted_epoch:admission_epoch,source:'simulation_replay',offline_source:'fixture'})),
  source:'simulation_replay',offline_source:'fixture',report_id:'report-1',report_hash:'hash-1',
  case_ids:Array.from({length:12},(_,i)=>`case-${i+1}`),replay_count:12,unique_cases:12,real_inference_count:0,candidate_user_outputs:0,side_effects:0,
 });
 // Resume clears the Go []string to nil; encoding/json persists alerts as null.
 const savedJSON=JSON.stringify({
  source:'simulation_replay',offline_source:'fixture',version:'simulation-v1',authorization:'synthetic',seed:7,clock_seconds:120,
  stages:[stage(3,'cohort-1-CANARY_5',0),stage(5,'cohort-2-CANARY_5',60)],alerts:null,
  events:[{scenario:'replay_stage',source:'simulation_replay',offline_source:'fixture',report_id:'report-1',clock_seconds:60},{scenario:'replay_stage',source:'simulation_replay',offline_source:'fixture',report_id:'report-1',clock_seconds:120}],
  task:{run_id:'run-2',generation:3,status:'completed',request:{scenario:'replay_stage',seed:7,virtual_seconds:60}},
 });
 const raw=Object.freeze(JSON.parse(savedJSON));
 const sim=readSimulation(raw);
 assert.ok(sim,'resumed simulation-v1 remains readable');
 assert.deepEqual(sim.alerts,[]);
 const counts=allocation({state:'CANARY_5',route_epoch:5,admission_enabled:true},sim);
 assert.ok(counts);
 assert.equal(counts.stage.cohort_id,'cohort-2-CANARY_5');
 assert.equal(counts.stage.admission_epoch,5);
 assert.equal(counts.allocated,20);
 assert.equal(counts.active,20);
 assert.equal(counts.candidate,1);
 assert.equal(counts.revoked,0);
 assert.equal(counts.stage.window_start_seconds,60);
 assert.equal(sim.clock_seconds-counts.stage.window_start_seconds,60);
 assert.equal(raw.alerts,null);
 assert.equal(JSON.stringify(raw),savedJSON);
 assert.equal(readSimulation({...raw,alerts:'cleared'}),null);
});
