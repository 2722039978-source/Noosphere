import type {ModelConfig} from '@/lib/api';
import {resolveModelIdentity} from './model-identity';
export function ModelIdentitySummary({model}:{model:ModelConfig}){
 const identity=resolveModelIdentity(model);
 return <dl className="ns-hub-identity-facts"><div><dt>厂商 / 系列</dt><dd>{identity.vendor} / {identity.family}</dd></div><div><dt>Provider / 部署</dt><dd>{identity.label}</dd></div><div><dt>推理服务模型 ID</dt><dd>{model.model_name}</dd></div><div><dt>参数 / 量化</dt><dd>{model.metadata?.parameter_size||'未提供'} / {model.metadata?.quantization||(identity.deployment==='api'?'不适用':'未提供')}</dd></div><div><dt>当前部署上下文</dt><dd>{model.context_length?`${model.context_length.toLocaleString()} tokens`:'未提供'}</dd></div>{model.metadata?.context_length&&<div><dt>架构上下文上限</dt><dd>{model.metadata.context_length.toLocaleString()} tokens（不是当前服务配置）</dd></div>}{model.metadata?.source&&<div><dt>身份信息来源</dt><dd>{model.metadata.source}</dd></div>}</dl>;
}
