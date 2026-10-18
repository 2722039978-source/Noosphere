import { useId, useRef, useState } from 'react';
import { useBackend } from '@/hooks/useBackend';
import { errMessage, type ModelConfig } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
const EMPTY = { id: '', name: '', provider: 'openai_compatible', base_url: '', model_name: '', api_key: '', is_default: false };
const FIELDS = [
  { key: 'id', label: '标识 ID', placeholder: '例如 deepseek' },
  { key: 'name', label: '显示名称', placeholder: '显示名称' },
  { key: 'provider', label: '协议', placeholder: 'openai_compatible / claude / gemini / ollama' },
  { key: 'base_url', label: '接口地址', placeholder: 'https://api.example.com/v1' },
  { key: 'model_name', label: '模型名称', placeholder: '例如 deepseek-chat' },
  { key: 'api_key', label: 'API 密钥', placeholder: '本机加密存储；本地模型可留空' },
] as const;
export function ModelForm({ onSaved, onCancel, initial }: { onSaved: () => void; onCancel: () => void; initial?: ModelConfig }) {
  const { addModel } = useBackend();
  const prefix = useId();
  const [form, setForm] = useState(() => initial ? { ...EMPTY, ...initial, api_key: '', is_default: Boolean(initial.is_default) } : EMPTY);
  const [saving, setSaving] = useState(false);
  const inFlight = useRef(false);
  const [error, setError] = useState('');
  async function submit(event: React.FormEvent) {
    event.preventDefault();
    if (inFlight.current) return;
    inFlight.current = true; setSaving(true); setError('');
    try {
      await addModel({ ...form, id: form.id.trim(), name: form.name.trim(), provider: form.provider.trim(), base_url: form.base_url.trim(), model_name: form.model_name.trim() });
      onSaved();
    } catch (error) { setError(errMessage(error)); }
    finally { inFlight.current = false; setSaving(false); }
  }
  return <form onSubmit={submit} className="ns-model-form" aria-label={initial ? '编辑模型接入' : '新增模型接入'}>
    {!initial && <div className="ns-provider-presets"><span>供应商预设</span><Button type="button" variant="outline" disabled={saving} onClick={() => setForm({ ...EMPTY, id: 'deepseek-flash', name: 'DeepSeek Flash', provider: 'deepseek', base_url: 'https://api.deepseek.com', model_name: 'deepseek-flash', is_default: true })}>DeepSeek 官方</Button><Button type="button" variant="ghost" disabled={saving} onClick={() => setForm(EMPTY)}>自定义接入</Button><p>预设自动填写协议、地址与模型；只需填写密钥。保存后可测试连接或切换默认模型。</p></div>}
    {form.base_url.replace(/\/$/, '') === 'https://api.deepseek.com' && <div className="ns-provider-presets"><label htmlFor={`${prefix}-deepseek`}>DeepSeek 模型</label><select id={`${prefix}-deepseek`} disabled={saving} value={form.model_name} onChange={event => setForm({ ...form, model_name: event.target.value, ...(!initial ? { id: event.target.value, name: event.target.value === 'deepseek-flash' ? 'DeepSeek Flash' : 'DeepSeek V4 Pro' } : {}) })}><option value="deepseek-flash">Flash · 日常对话</option><option value="deepseek-v4-pro">V4 Pro · 复杂任务</option>{!['deepseek-flash','deepseek-v4-pro'].includes(form.model_name) && <option value={form.model_name}>{form.model_name}</option>}</select></div>}
    <fieldset disabled={saving}><legend>{initial ? '编辑模型接入' : '新增模型接入'}</legend><div className="ns-model-fields">{FIELDS.map(({ key, label, placeholder }) => <div key={key}><label htmlFor={`${prefix}-${key}`}>{label}</label><Input id={`${prefix}-${key}`} readOnly={key === 'id' && Boolean(initial)} required={key !== 'api_key'} type={key === 'api_key' ? 'password' : key === 'base_url' ? 'url' : 'text'} autoComplete="off" value={form[key]} placeholder={key === 'api_key' && initial ? '留空保留本机已保存的密钥' : placeholder} onChange={(event) => setForm({ ...form, [key]: event.target.value })} /></div>)}</div><div className="ns-model-form-actions"><label><input type="checkbox" checked={form.is_default} onChange={(event) => setForm({ ...form, is_default: event.target.checked })} />设为默认模型</label><Button type="button" variant="ghost" onClick={onCancel}>取消</Button><Button type="submit" disabled={!form.id.trim() || !form.name.trim() || !form.model_name.trim() || !form.base_url.trim() || !form.provider.trim()}>{saving ? '保存中…' : '保存'}</Button></div></fieldset>
    {error && <p role="alert" className="ns-form-error">保存失败：{error}。已保留填写内容，请修正后重试。</p>}
  </form>;
}
