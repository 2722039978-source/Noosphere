import { useRef, useState, type FormEvent } from 'react';
import { useBackend } from '@/hooks/useBackend';
import { api, errMessage, type ModelConfig, type ModelDiagnostic } from '@/lib/api';
import { useWorkspaceStore } from '@/stores/workspace-store';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { ConnectionDiagnostics } from './ConnectionDiagnostics';

const providers = [
  { id: 'deepseek', name: 'DeepSeek', url: 'https://api.deepseek.com/v1' },
  { id: 'openai', name: 'OpenAI', url: 'https://api.openai.com/v1' },
  { id: 'claude', name: 'Anthropic / Claude', url: 'https://api.anthropic.com/v1' },
  { id: 'gemini', name: 'Google Gemini', url: 'https://generativelanguage.googleapis.com/v1beta' },
  { id: 'openai_compatible', name: 'OpenAI 兼容', url: '' },
  { id: 'ollama', name: 'Ollama', url: 'http://127.0.0.1:11434' },
  { id: 'openvino', name: 'OpenVINO', url: '' },
];

export function ModelForm({ onSaved, onCancel, initial }: {
  onSaved: (model: ModelConfig) => void | Promise<void>;
  onCancel?: () => void;
  initial?: ModelConfig;
}) {
  const { addModel, loadModels } = useBackend();
  const [form, setForm] = useState<ModelConfig>(() => initial ? { ...initial, api_key: '' } : {
    id: `model-${crypto.randomUUID()}`, name: '', provider: 'openai_compatible',
    base_url: '', model_name: '', enabled: true, api_key: '', deployment: '',
  });
  const [makeDefault, setMakeDefault] = useState(Boolean(initial?.is_default));
  const [working, setWorking] = useState('');
  const [result, setResult] = useState<ModelDiagnostic>();
  const [error, setError] = useState('');
  const inFlight = useRef(false);
  const update = (patch: Partial<ModelConfig>) => { setForm(previous => ({ ...previous, ...patch })); setError(''); };
  const choice = providers.find(provider => provider.id === form.provider);
  const draft = (): ModelConfig => ({ ...form, capabilities: undefined, is_default: Boolean(initial?.is_default), id: form.id.trim(), name: form.name.trim() || form.model_name.trim(), base_url: form.base_url.trim(), model_name: form.model_name.trim() });

  async function run(infer: boolean) {
    if (inFlight.current) return;
    inFlight.current = true;
    setWorking(infer ? '正在验证真实推理…' : '正在扫描模型…');
    setError(''); setResult(undefined);
    const model = draft();
    try {
      const checked = await api.probeModel(model, infer);
      setResult(checked);
      if (!infer || checked.inference !== 'succeeded' || checked.status !== 'ok') return;
      await addModel({ ...model });
      if (makeDefault && !initial?.is_default) {
        await api.selectModel({ scope: 'global', workspace_id: useWorkspaceStore.getState().activeId, session_id: '', model_id: model.id });
        await loadModels();
      }
      await onSaved(model);
    } catch (problem) { setError(errMessage(problem)); }
    finally { inFlight.current = false; setWorking(''); }
  }
  function submit(event: FormEvent) { event.preventDefault(); void run(true); }
  return <form onSubmit={submit} aria-label={initial ? '编辑模型接入' : '新增模型接入'} className="grid gap-3">
    <fieldset disabled={!!working} className="grid gap-3">
      <label className="ns-model-field">服务商<select className="ns-workspace-select" value={form.provider} onChange={event => {
        const provider = providers.find(item => item.id === event.target.value);
        if (provider) { setResult(undefined); update({ provider: provider.id, base_url: provider.url, model_name: '', api_key: '' }); }
      }}>{!choice && <option value={form.provider}>{form.provider}</option>}{providers.map(provider => <option key={provider.id} value={provider.id}>{provider.name}</option>)}</select></label>
      {!['ollama', 'openvino'].includes(form.provider) && <label className="ns-model-field">API 密钥<Input type="password" autoComplete="off" value={form.api_key ?? ''} placeholder={initial ? '留空保留已保存的密钥' : '按需填写'} onChange={event => update({ api_key: event.target.value })} /></label>}
      <div className="flex flex-wrap items-end gap-2">
        <label className="ns-model-field min-w-0 flex-1">模型 ID<Input required value={form.model_name} placeholder="输入 ID，或扫描后选择" list="ns-provider-models" onChange={event => update({ model_name: event.target.value })} /><datalist id="ns-provider-models">{result?.models?.map(model => <option key={model.id} value={model.id}>{model.name}</option>)}</datalist></label>
        <Button type="button" variant="outline" disabled={!form.base_url.trim()} onClick={() => void run(false)}>扫描模型</Button>
      </div>
      {result?.models && result.models.length > 0 && <label className="ns-model-field">发现的模型<select className="ns-workspace-select" value={result.models.some(model => model.id === form.model_name) ? form.model_name : ''} onChange={event => update({ model_name: event.target.value })}><option value="">请选择</option>{result.models.map(model => <option key={model.id} value={model.id}>{model.id}</option>)}</select></label>}
      <details className="ns-record-detail" open={!choice?.url || undefined}>
        <summary>高级连接参数</summary>
        <div className="ns-detail-body grid gap-3">
          <label className="ns-model-field">接口地址<Input type="url" value={form.base_url} placeholder="https://api.example.com/v1" onChange={event => update({ base_url: event.target.value })} /></label>
          <label className="ns-model-field">显示名称<Input value={form.name} placeholder="默认使用模型 ID" onChange={event => update({ name: event.target.value })} /></label>
          <div className="grid gap-2 sm:grid-cols-2"><label className="ns-model-field">厂商<Input value={form.vendor ?? ''} placeholder="可选" onChange={event => update({ vendor: event.target.value })} /></label><label className="ns-model-field">模型系列<Input value={form.model_family ?? ''} placeholder="可选" onChange={event => update({ model_family: event.target.value })} /></label></div>
          <label className="text-xs text-text-2"><input type="checkbox" checked={makeDefault} disabled={Boolean(initial?.is_default)} onChange={event => setMakeDefault(event.target.checked)} /> 设为全局默认</label>
        </div>
      </details>
    </fieldset>
    {result && <ConnectionDiagnostics result={result} />}
    {error && <p role="alert" className="ns-form-error">{error}</p>}
    {working && <p role="status" className="text-xs text-text-2">{working}</p>}
    <div className="flex flex-wrap justify-end gap-2">
      {onCancel && <Button type="button" variant="ghost" disabled={!!working} onClick={onCancel}>取消</Button>}
      <Button type="submit" disabled={!!working || !form.base_url.trim() || !form.model_name.trim()}>{initial ? '验证并保存' : '连接模型'}</Button>
    </div>
  </form>;
}
