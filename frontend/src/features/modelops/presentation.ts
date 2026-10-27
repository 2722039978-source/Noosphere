import type { DecisionPreview, Release, ReleaseCommand } from '@/lib/model-engineering-types';
export const STAGES = ['DRAFT','OFFLINE_EVAL','SHADOW','CANARY_5','CANARY_25','CANARY_50','PROMOTED'] as const;
export const COMMAND_LABEL: Record<ReleaseCommand,string> = {start_offline:'开始离线验证',advance:'晋级下一阶段',pause:'暂停',resume:'恢复',reject:'拒绝候选',rollback:'回滚候选'};
export interface Assignment { arm:'stable'|'candidate'; admitted_epoch:number; revoked_at?:string; session_key?:string; cohort_id?:string }
export interface SimulationStage { state:string; admission_epoch:number; cohort_id:string; window_start_seconds:number; assignments:Assignment[]; replayed?:boolean; source?:string; offline_source?:string; report_id?:string; report_hash?:string; case_ids?:string[]; replay_count?:number; unique_cases?:number; real_inference_count?:number; candidate_user_outputs?:number; side_effects?:number }
export interface SimulationView { version:string; authorization:string; clock_seconds:number; source?:string; offline_source?:string; stages:SimulationStage[]; alerts:string[]; events:unknown[]; task?:{run_id:string;status:string;generation:number} }
const object=(x:unknown):x is Record<string,any>=>!!x&&typeof x==='object'&&!Array.isArray(x);
const integer=(x:unknown)=>Number.isSafeInteger(x)&&Number(x)>=0;
export function readSimulation(x:unknown):SimulationView|null {
 if(!object(x))return null;
 const alerts=x.alerts===null?[]:x.alerts;
 if(x.version!=='simulation-v1'||x.authorization!=='synthetic'||!integer(x.clock_seconds)||!Array.isArray(x.stages)||!Array.isArray(alerts)||!alerts.every(v=>typeof v==='string')||!Array.isArray(x.events))return null;
 if(!x.stages.every(s=>object(s)&&typeof s.state==='string'&&integer(s.admission_epoch)&&typeof s.cohort_id==='string'&&integer(s.window_start_seconds)&&Array.isArray(s.assignments)&&s.assignments.every((a:unknown)=>object(a)&&(a.arm==='stable'||a.arm==='candidate')&&integer(a.admitted_epoch)&&(a.revoked_at===undefined||typeof a.revoked_at==='string'))))return null;
 if(x.task!==undefined&&(!object(x.task)||typeof x.task.run_id!=='string'||typeof x.task.status!=='string'||!integer(x.task.generation)))return null;
 return {...x,alerts} as unknown as SimulationView;
}
export function allocation(r:Pick<Release,'state'|'route_epoch'|'admission_enabled'>,s:SimulationView|null) {
 if(!s)return null;
 const stage=[...s.stages].reverse().find(v=>v.admission_epoch===r.route_epoch)||(!r.admission_enabled?s.stages[s.stages.length-1]:undefined);
 if(!stage)return null;
 const active=r.admission_enabled?stage.assignments.filter(a=>!a.revoked_at&&a.admitted_epoch===r.route_epoch):[];
 return {stage,allocated:stage.assignments.length,active:active.length,candidate:active.filter(a=>a.arm==='candidate').length,revoked:stage.assignments.filter(a=>!!a.revoked_at).length};
}
export function commandsFor(r:Pick<Release,'state'|'paused_from'|'previous_stable'|'closed_at'>):ReleaseCommand[] {
 if(r.closed_at)return [];
 switch(r.state){
 case 'DRAFT':return ['start_offline','reject'];
 case 'OFFLINE_EVAL':case 'SHADOW':return ['advance','pause','reject'];
 case 'CANARY_5':case 'CANARY_25':case 'CANARY_50':return ['advance','pause',...(r.previous_stable?['rollback' as const]:[])];
 case 'PAUSED':return ['resume',...(['OFFLINE_EVAL','SHADOW'].includes(r.paused_from)?['reject' as const]:r.previous_stable?['rollback' as const]:[])];
 case 'PROMOTED':return r.previous_stable?['rollback']:[];
 default:return [];
 }
}
export function previewMatches(r:Pick<Release,'id'|'revision'|'mode'>,command:ReleaseCommand,p:{command:ReleaseCommand;value:DecisionPreview}|null):boolean {
 return !!p&&r.mode==='simulation'&&p.command===command&&p.value.release_id===r.id&&p.value.revision===r.revision&&p.value.decision.allowed&&!!p.value.evidence_hash&&p.value.evidence_hash===p.value.decision.evidence_hash;
}
