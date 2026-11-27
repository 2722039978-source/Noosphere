import { useState, type FormEvent } from 'react';
import { useQuery } from '@tanstack/react-query';
import { FilePlus2, FolderSearch, Cpu } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { api, errMessage, type LibraryChange, type LibraryModel, type ModelLibrary } from '@/lib/api';
import { modelEngineering } from '@/lib/model-engineering-client';
import { hardwareCapacity } from '@/features/model-lab/hardware-presentation';

export type ChangeLibrary = (change: LibraryChange) => Promise<ModelLibrary>;
type Run = (task: () => Promise<void>) => Promise<void>;
type Controls = { busy: boolean; change: ChangeLibrary; run: Run };
export const libraryStatus: Record<LibraryModel['status'], string> = {
  ready: '可启动', file_missing: '权重文件缺失，请重新定位。', file_changed: 'GGUF 文件已变化，请重新定位权重或重新关联投影器。',
  runtime_missing: '请选择或安装运行时。', runtime_changed: '运行时已变化，请重新授权。',
  projector_missing: '视觉投影器缺失，请重新选择或移除。',
};

export function ImportModels({ busy, change, run, onImport, models }: Controls & { models: LibraryModel[]; onImport: (id: string) => void }) {
  async function pick(directory: boolean) {
    const paths = await api.pickModelLibraryFiles(directory ? 'directory' : 'models');
    if (!paths.length) return;
    const previous = new Set(models.map(model => model.id));
    const next = await change(directory ? { action: 'scan', path: paths[0] } : { action: 'import', paths });
    const picked = next.models.find(model => paths.includes(model.path)) ?? next.models.find(model => !previous.has(model.id));
    if (picked) onImport(picked.id);
  }
  return <div className="flex flex-wrap gap-2">
    <Button size="sm" disabled={busy} onClick={() => void run(() => pick(false))}><FilePlus2 />导入 GGUF</Button>
    <Button size="sm" variant="outline" disabled={busy} onClick={() => void run(() => pick(true))}><FolderSearch />扫描目录</Button>
  </div>;
}

export function RuntimeManager({ library, required, busy, run, change }: Controls & { library?: ModelLibrary; required: boolean }) {
  const [actionsOpen, setActionsOpen] = useState(false);
  const [backend, setBackend] = useState('');
  const [installing, setInstalling] = useState(false);
  const open = required || actionsOpen;
  return <section aria-label="推理运行时" className="ns-library-runtime">
    <Button size="sm" variant="ghost" aria-expanded={open} disabled={busy} onClick={() => setActionsOpen(!actionsOpen)}><Cpu />推理运行时</Button>
    {open && <div className="grid min-w-0 gap-2.5">
      {library?.runtimes.map(runtime => <div key={runtime.id} className="ns-runtime-row">
        <div><strong>{runtime.version || 'llama-server'}</strong><p>{runtime.path}</p><small>{runtime.status === 'ready' ? runtime.devices.join(' · ') || '未报告加速设备' : runtime.status === 'changed' ? '程序已变化' : '程序缺失'}</small></div>
        {runtime.status === 'ready' && <Button size="sm" variant="outline" disabled={busy} onClick={() => void run(async () => {
          await change({ action: 'diagnose_runtime', id: runtime.id });
        })}>检测设备</Button>}
        <Button size="sm" variant="ghost" disabled={busy} onClick={() => void run(async () => { await change({ action: 'forget_runtime', id: runtime.id }); })}>撤销授权</Button>
      </div>)}
      {library?.candidates.filter(path => !library.runtimes.some(runtime => runtime.path === path && runtime.status === 'ready')).map(path =>
        <div key={path} className="ns-runtime-row"><span>{path}</span><Button size="sm" variant="outline" disabled={busy} onClick={() => void run(async () => { await change({ action: 'authorize_runtime', path }); })}>授权此运行时</Button></div>)}
      <div className="flex flex-wrap gap-2">
        <Button variant="outline" size="sm" disabled={busy} onClick={() => void run(async () => {
          const [path] = await api.pickModelLibraryFiles('runtime');
          if (path) await change({ action: 'authorize_runtime', path });
        })}>选择 llama-server…</Button>
        <Button variant="outline" size="sm" disabled={busy} onClick={() => void run(async () => {
          setInstalling(true);
          try { await change({ action: 'install_runtime', backend: backend || undefined }); } finally { setInstalling(false); }
        })}>{installing ? '正在安装…' : '安装官方运行时'}</Button>
      </div>
      {!!library?.install_options?.length && <details className="ns-record-detail"><summary>安装选项</summary><div className="ns-detail-body">
        <label className="ns-model-field">运行时构建<select className="ns-workspace-select" value={backend} disabled={busy} onChange={event => setBackend(event.target.value)}>
          <option value="">平台默认</option>{library.install_options.map(option => <option key={option.backend} value={option.backend}>
            {option.backend.toUpperCase()} · {(option.size / 1024 / 1024).toFixed(0)} MiB
          </option>)}
        </select></label>
      </div></details>}
    </div>}
  </section>;
}

export function LibraryModelDetails({ model, library, busy, run, change, onForget }: Controls & {
  model: LibraryModel; library: ModelLibrary; onForget: () => void;
}) {
  const [name, setName] = useState(model.name);
  const [runtimeId, setRuntimeId] = useState(model.runtime_id ?? '');
  const [device, setDevice] = useState(model.device ?? '');
  const [context, setContext] = useState(model.context?.toString() ?? '');
  const [layers, setLayers] = useState(model.gpu_layers?.toString() ?? '');
  const [projector, setProjector] = useState(model.projector ?? '');
  const [forget, setForget] = useState(false);
  const effectiveId = runtimeId || (!model.runtime_id ? model.effective_runtime_id : null);
  const runtime = library.runtimes.find(item => item.id === effectiveId);
  const environment = useQuery({ queryKey: ['model-library-hardware'], queryFn: () => modelEngineering({ operation: 'environment' }), staleTime: 300_000, retry: false });
  const host = environment.data?.environment;
  const ram = host?.ram_bytes || host?.hardware?.memory?.installed_bytes;
  const available = library.available_ram_bytes;
  const fit = available && model.size > available ? '当前可用内存小于权重体积。请关闭其他模型，或使用有足够显存的设备。' : ram ? model.size >= ram ? '权重已超过系统内存容量，请核对可用显存或选择更小量化。' : '实际加载取决于当前可用内存、显存和上下文长度。' : '系统内存容量未知。';
  const locate = () => run(async () => {
    const [path] = await api.pickModelLibraryFiles('model');
    if (path) await change({ action: 'relocate', id: model.id, path });
  });
  function save(event: FormEvent) {
    event.preventDefault();
    void run(async () => {
      await change({ action: 'configure', id: model.id, name: name.trim(), runtime_id: runtimeId || null,
        context: context ? Number(context) : null, gpu_layers: layers ? Number(layers) : null, device: device || null, projector: projector || null });
    });
  }
  return <div className="grid gap-2">
    <p className="text-xs text-text-2">{hardwareCapacity(model.size)} · {model.architecture || '架构未知'} · {libraryStatus[model.status]}</p>
    {model.fit && <p role="status" className="text-xs text-text-2">{model.fit === 'likely' ? '容量初估可容纳此模型，启动后验证。' : model.fit === 'tight' ? '容量可能不足，建议缩短上下文或选择更小的量化。' : '可用容量未知，启动后验证。'}</p>}
    {model.status.startsWith('file_') && <Button className="justify-self-start" size="sm" variant="outline" disabled={busy} onClick={() => void locate()}>重新定位…</Button>}
    <details className="ns-record-detail" open={model.status === 'projector_missing' || undefined}><summary>模型高级设置</summary>
      <div className="ns-detail-body grid gap-3">
        <p className="break-all text-xs text-text-3">{model.path}</p>
        <p className="text-xs text-text-2">建议上下文 {model.suggested_context.toLocaleString()} tokens{model.trained_context ? ' · 模型上限 ' + model.trained_context.toLocaleString() : ''}。{fit}</p>
        {host && <p className="text-xs text-text-2">{[host.os, host.arch, host.cpu, ram && '内存 ' + hardwareCapacity(ram), ...host.hardware?.gpus.map(gpu => gpu.model + (gpu.total_vram_bytes ? ' · ' + hardwareCapacity(gpu.total_vram_bytes) : '')) ?? []].filter(Boolean).join(' · ')}</p>}
        {environment.isError && <p className="text-xs text-text-2">{errMessage(environment.error)}</p>}
        <form className="grid gap-3" onSubmit={save}><fieldset disabled={busy} className="grid gap-3">
          <label className="ns-model-field">模型名称<Input required value={name} onChange={e => setName(e.target.value)} /></label>
          <label className="ns-model-field">运行时<select className="ns-workspace-select" value={runtimeId} onChange={e => { setRuntimeId(e.target.value); setDevice(''); }}>
            <option value="">自动选择已授权运行时</option>{library.runtimes.map(runtime => <option key={runtime.id} value={runtime.id}>{runtime.path}</option>)}
          </select></label>
          <label className="ns-model-field">计算设备<select className="ns-workspace-select" value={device} onChange={e => setDevice(e.target.value)}>
            <option value="">运行时默认</option>{Array.from(new Set([...(runtime?.devices ?? []), ...(device ? [device] : [])])).map(id => <option key={id} value={id}>{id}</option>)}
          </select></label>
          {runtime?.device_details?.length ? <ul className="text-xs text-text-2">{runtime.device_details.map(info =>
            <li key={info.id}>{info.id} · {info.description}{info.free_mib != null ? ' · 检测时可用 ' + info.free_mib + ' MiB' : ''}</li>)}</ul> : null}
          <div className="grid gap-2 sm:grid-cols-2">
            <label className="ns-model-field">上下文长度<Input type="number" min="64" max={Math.min(model.trained_context || 2097152, 2097152)} step="1" placeholder={String(model.suggested_context)} value={context} onChange={e => setContext(e.target.value)} /></label>
            <label className="ns-model-field">GPU 卸载层数<Input type="number" min="0" max="999" step="1" placeholder="自动" value={layers} onChange={e => setLayers(e.target.value)} /></label>
          </div>
          <div className="grid gap-1"><span className="text-xs text-text-2">视觉投影器</span><span className="break-all text-xs">{projector || '未关联'}</span>
            <div className="flex flex-wrap gap-2"><Button type="button" size="sm" variant="outline" onClick={() => void run(async () => {
              const [path] = await api.pickModelLibraryFiles('projector'); if (path) setProjector(path);
            })}>选择投影器…</Button>
            {projector && <Button type="button" size="sm" variant="ghost" onClick={() => setProjector('')}>移除关联</Button>}</div>
          </div>
          <Button size="sm" type="submit" className="justify-self-end">保存模型设置</Button>
        </fieldset></form>
        <div className="flex flex-wrap gap-2"><Button size="sm" variant="outline" disabled={busy} onClick={() => void locate()}>重新定位文件…</Button>
          <Button size="sm" variant="ghost" disabled={busy} onClick={() => setForget(!forget)}>移除模型记录</Button></div>
        {forget && <div className="flex flex-wrap items-center gap-2 text-xs">保留磁盘上的权重文件。
          <Button size="sm" variant="outline" disabled={busy} onClick={() => void run(async () => { await change({ action: 'forget', id: model.id }); onForget(); })}>确认移除记录</Button></div>}
      </div>
    </details>
  </div>;
}
