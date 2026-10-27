import { useId, useRef, useState } from 'react';
import { useBackend } from '@/hooks/useBackend';
import { api, errMessage, type ModelConfig } from '@/lib/api';
import { useWorkspaceStore } from '@/stores/workspace-store';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
const EMPTY = { id: '', name: '', provider: 'openai_compatible', base_url: '', model_name: '', vendor:'',model_family:'',deployment:'' as ''|'local'|'api', api_key: '', is_default: false };
const FIELDS = [
  { key: 'id', label: '标识 ID', placeholder: '例如 deepseek' },
  { key: 'name', label: '显示名称', placeholder: '显示名称' },
  { key: 'provider', label: '协议', placeholder: 'openai_compatible / claude / gemini / ollama' },
  { key: 'base_url', label: '接口地址', placeholder: 'https://api.example.com/v1' },
  { key: 'model_name', label: '模型名称', placeholder: '例如 deepseek-chat' },
  { key: 'vendor', label: '模型厂商（可选）', placeholder: 'Qwen / DeepSeek / OpenAI / Anthropic；留空自动识别' },
  { key: 'model_family', label: '模型系列（可选）', placeholder: '例如 Qwen、DeepSeek、Llama' },
  { key: 'api_key', label: 'API 密钥', placeholder: '本机加密存储；本地模型可留空' },
] as const;
export function ModelForm({ onSaved, onCancel, initial }: { onSaved: () => void; onCancel: () => void; initial?: ModelConfig }) {
  const { addModel, loadModels } = useBackend();
  const prefix = useId();
  const [form, setForm] = useState(() => initial ? { ...EMPTY, ...initial, vendor:initial.vendor??'',model_family:initial.model_family??'',deployment:initial.deployment??'', api_key: '', is_default: Boolean(initial.is_default) } : EMPTY);
  const [saving, setSaving] = useState(false);
  const inFlight = useRef(false);
  const [error, setError] = useState('');
  async function submit(event: React.FormEvent) {
    event.preventDefault();
    if (inFlight.current) return;
    inFlight.current = true; setSaving(true); setError('');
    try {
      await addModel({ ...form, is_default: Boolean(initial?.is_default), id: form.id.trim(), name: form.name.trim(), provider: form.provider.trim(), base_url: form.base_url.trim(), model_name: form.model_name.trim() });
      if (form.is_default && !initial?.is_default) {
        try { await api.selectModel({scope:'global',workspace_id:useWorkspaceStore.getState().activeId,session_id:'',model_id:form.id.trim()}); }
        catch (error) { throw new Error(`配置已保存，设为全局默认未完成：${errMessage(error)}`); }
        await loadModels();
      }
      onSaved();
    } catch (error) { setError(errMessage(error)); }
    finally { inFlight.current = false; setSaving(false); }
  }
  return <form onSubmit={submit} className="ns-model-form" aria-label={initial ? '编辑模型接入' : '新增模型接入'}>
    {!initial && <div className="ns-provider-presets"><span>供应商预设</span><Button type="button" variant="outline" disabled={saving} onClick={() => setForm({ ...EMPTY, id: 'deepseek-flash', name: 'DeepSeek Flash', provider: 'deepseek', base_url: 'https://api.deepseek.com', model_name: 'deepseek-flash', vendor: 'DeepSeek', model_family: 'DeepSeek', deployment: 'api' })}>DeepSeek 官方</Button><Button type="button" variant="ghost" disabled={saving} onClick={() => setForm(EMPTY)}>自定义接入</Button><p>预设自动填写协议、地址、模型与厂商信息；填写密钥后保存。仅勾选“设为全局默认模型”才会更换全局默认。</p></div>}
    {form.base_url.replace(/\/$/, '') === 'https://api.deepseek.com' && <div className="ns-provider-presets"><label htmlFor={`${prefix}-deepseek`}>DeepSeek 模型</label><select id={`${prefix}-deepseek`} disabled={saving} value={form.model_name} onChange={event => setForm({ ...form, model_name: event.target.value, name: event.target.value === 'deepseek-flash' ? 'DeepSeek Flash' : 'DeepSeek V4 Pro', vendor: 'DeepSeek', model_family: 'DeepSeek', deployment: 'api', ...(!initial ? { id: event.target.value } : {}) })}><option value="deepseek-flash">Flash · 日常对话</option><option value="deepseek-v4-pro">V4 Pro · 复杂任务</option>{!['deepseek-flash','deepseek-v4-pro'].includes(form.model_name) && <option value={form.model_name}>{form.model_name}</option>}</select></div>}
    <fieldset disabled={saving}><legend>{initial ? '编辑模型接入' : '新增模型接入'}</legend><div className="ns-model-fields">{FIELDS.map(({ key, label, placeholder }) => <div key={key}><label htmlFor={`${prefix}-${key}`}>{label}</label><Input id={`${prefix}-${key}`} readOnly={key === 'id' && Boolean(initial)} required={!['api_key','vendor','model_family'].includes(key)} type={key === 'api_key' ? 'password' : key === 'base_url' ? 'url' : 'text'} autoComplete="off" value={form[key]} placeholder={key === 'api_key' && initial ? '留空保留本机已保存的密钥' : placeholder} onChange={(event) => setForm({ ...form, [key]: event.target.value })} /></div>)}</div><label className="ns-lab-field">部署方式<select aria-label="模型部署方式" value={form.deployment} onChange={event=>setForm({...form,deployment:event.target.value as ''|'local'|'api'})}><option value="">根据地址自动识别</option><option value="local">本地部署</option><option value="api">API 服务</option></select></label><p>{initial?.is_default ? '此配置为全局默认，保存会保留该状态。可在其他模型上选择“设为全局默认”来更换。' : '全局默认用于未指定模型的工作区与新会话；“使用”只切换当前会话，工作区默认需单独设置。'}</p><div className="ns-model-form-actions"><label><input type="checkbox" checked={form.is_default} disabled={Boolean(initial?.is_default)} onChange={(event) => setForm({ ...form, is_default: event.target.checked })} />设为全局默认模型</label><Button type="button" variant="ghost" onClick={onCancel}>取消</Button><Button type="submit" disabled={!form.id.trim() || !form.name.trim() || !form.model_name.trim() || !form.base_url.trim() || !form.provider.trim()}>{saving ? '保存中…' : '保存'}</Button></div></fieldset>
    {error && <p role="alert" className="ns-form-error">保存失败：{error}。已保留填写内容，请修正后重试。</p>}
  </form>;
}
