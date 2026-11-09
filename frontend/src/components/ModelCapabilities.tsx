import type { ModelConfig } from '@/lib/api';
import { capabilityFor, capabilityLabel, type CapabilityKey } from '@/lib/model-capabilities';
import './model-capabilities.css';

const entries: [CapabilityKey, string][] = [['chat', 'AI 对话'], ['agent_tools', 'Agent 工具'], ['native_evaluation', '原生测评']];
export function ModelCapabilities({ model, compact = false }: { model: ModelConfig; compact?: boolean }) {
  return <section className="ns-model-capabilities" aria-label={`模型能力 ${model.name || model.model_name}`}><div className="ns-model-capability-badges">{entries.map(([key]) => { const item = capabilityFor(model, key); return <span key={key} data-capability={key} data-available={item?.available ? 'yes' : 'no'} title={item?.reason || '未取得服务端能力说明，不能据此判断支持范围。'}>{capabilityLabel(key, item)}</span>; })}</div>{!compact && <details><summary>能力范围与不可用原因</summary><dl>{entries.map(([key, label]) => { const item = capabilityFor(model, key); return <div key={key}><dt>{label}</dt><dd>{item?.reason || '未取得服务端能力说明，请刷新连接信息或检查后端版本。'}</dd></div>; })}</dl><p>能力说明表示本应用已接入的协议范围。连接是否在线、模型是否正确响应或调用工具，需要实际验证；原生测评还需发现身份、注册版本并检查执行环境。</p></details>}</section>;
}
