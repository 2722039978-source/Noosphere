import { Input } from '@/components/ui/input';
import type { ModelConfig, ProtocolOptions } from '@/lib/api';

type Flag = Exclude<keyof ProtocolOptions, 'top_p'>;
const flags: [Flag, string][] = [
  ['vision', '图像输入'], ['tools', '工具调用'], ['streaming', '流式响应'],
  ['reasoning', '推理输出'], ['structured_output', '结构化输出'],
];

function SupportSelect({ label, value, onChange }: { label: string; value?: boolean | null; onChange: (value?: boolean) => void }) {
  return <label className="ns-model-field">{label}<select className="ns-workspace-select" aria-label={label} value={value == null ? '' : String(value)}
    onChange={event => onChange(event.target.value === '' ? undefined : event.target.value === 'true')}>
    <option value="">未指定</option><option value="true">支持</option><option value="false">不支持</option>
  </select></label>;
}

export function ProtocolFields({ model, update }: { model: ModelConfig; update: (patch: Partial<ModelConfig>) => void }) {
  const options = model.protocol_options ?? {};
  const setOption = <K extends keyof ProtocolOptions>(key: K, value: ProtocolOptions[K]) => {
    const next = { ...options };
    if (value == null) delete next[key];
    else next[key] = value;
    update({ protocol_options: next });
  };
  return <div className="grid gap-2 sm:grid-cols-2">
    {flags.map(([key, label]) => <SupportSelect key={key} label={label} value={options[key]} onChange={value => setOption(key, value)} />)}
    <SupportSelect label="temperature 参数" value={model.supports_temperature} onChange={value => update({ supports_temperature: value ?? null, reset_temperature_support: value === undefined })} />
    <label className="ns-model-field">温度<Input type="number" min="0" max="2" step="any" disabled={model.supports_temperature === false}
      placeholder="默认" value={model.temperature ?? ''} onChange={event => update({ temperature: event.target.value ? event.target.valueAsNumber : undefined })} /></label>
    <label className="ns-model-field">top_p<Input type="number" min="0" max="1" step="any" value={options.top_p ?? ''} placeholder="省略"
      onChange={event => setOption('top_p', event.target.value === '' ? undefined : event.target.valueAsNumber)} /></label>
    <label className="ns-model-field">上下文上限<Input type="number" min="1" step="1" placeholder="未知" value={model.context_length || ''} onChange={event =>
      update({ context_length: event.target.value ? event.target.valueAsNumber : undefined })} /></label>
    <label className="ns-model-field">最大输出 tokens<Input type="number" min="1" max="131072" step="1" value={model.max_tokens || ''} placeholder="默认"
      onChange={event => update({ max_tokens: event.target.value ? event.target.valueAsNumber : 0 })} /></label>
    <label className="ns-model-field">输出长度参数<select className="ns-workspace-select" value={model.max_tokens_field ?? ''} onChange={event => update({ max_tokens_field: event.target.value })}>
      <option value="">自动</option><option>max_tokens</option><option>max_completion_tokens</option>
    </select></label>
    <label className="ns-model-field">工具调用接口<select className="ns-workspace-select" value={model.tool_transport ?? ''} onChange={event => update({ tool_transport: event.target.value })}>
      <option value="">自动</option><option value="responses">Responses</option><option value="chat">Chat Completions</option>
    </select></label>
  </div>;
}
