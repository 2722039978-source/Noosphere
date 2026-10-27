import type {ModelConfig} from '@/lib/api';
export function modelStatus(model:ModelConfig,check?:string,running=false){
 if(model.enabled===false)return {label:'已禁用',detail:'此连接不接受新请求',state:'disabled'};
 if(running)return {label:'正在运行',detail:'目标会话正在执行推理或 Agent 任务',state:'running'};
 if(check?.startsWith('连接失败'))return {label:'连接失败',detail:check,state:'failed'};
 if(check?.startsWith('正在'))return {label:'正在验证',detail:'正在向 Provider 验证连接',state:'checking'};
 if(check?.startsWith('已验证'))return {label:'已验证',detail:'最近一次连接验证成功',state:'verified'};
 return {label:'已配置',detail:'尚未验证连接；不代表模型正在运行',state:'configured'};
}
