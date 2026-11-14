import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { modelEngineering } from '@/lib/model-engineering-client';
import type { HardwareInventory } from '@/lib/hardware-types';
import type { ModelRevision } from '@/lib/model-engineering-types';
import { useWorkspaceStore } from '@/stores/workspace-store';
import { Button } from '@/components/ui/button';
import { engineAdvice, estimateModelFit } from './hardware-fit';
import { hardwareCapacity } from './hardware-presentation';
import { loadHubPerformance, type HubPerformance } from './hub-performance';
import { LabError } from './shared';

export function HardwareAdvisor({ hardware, ram }: { hardware: HardwareInventory; ram?: number | null }) {
  const workspace = useWorkspaceStore(state => state.activeId);
  const [models, setModels] = useState<ModelRevision[]>([]);
  const [modelID, setModelID] = useState('');
  const [context, setContext] = useState(2048);
  const [cache, setCache] = useState('');
  const [device, setDevice] = useState(hardware.gpus.length ? '0' : 'cpu');
  const [reserve, setReserve] = useState(1.5);
  const [error, setError] = useState<unknown>();
  const [observation, setObservation] = useState<HubPerformance | null | undefined>();
  const [busy, setBusy] = useState(false);
  const [loadCount, setLoadCount] = useState(0);
  const [requested, setRequested] = useState<string>();

  useEffect(() => {
    let disposed = false;
    setError(undefined);
    async function readModels() {
      const found: ModelRevision[] = [];
      const cursors = new Set<string>();
      let cursor = '';
      do {
        const result = await modelEngineering({ operation: 'list_models', limit: 100, ...(cursor ? { cursor } : {}) });
        if (disposed) return;
        found.push(...result.models.filter(model => model.identity_level === 'digest'));
        cursor = result.next_cursor;
        if (cursor && cursors.has(cursor)) throw new Error('模型档案分页游标重复。');
        cursors.add(cursor);
      } while (cursor);
      setModels(found);
      setModelID(old => found.some(model => model.id === old) ? old : found[0]?.id ?? '');
    }
    void readModels().catch(error => { if (!disposed) setError(error); });
    return () => { disposed = true; };
  }, [loadCount]);

  const model = models.find(value => value.id === modelID);
  useEffect(() => {
    setObservation(undefined);
    setBusy(false);
    if (!model || requested !== `${workspace}/${model.id}`) return;
    const controller = new AbortController();
    setBusy(true);
    setError(undefined);
    loadHubPerformance(workspace, model, controller.signal).then(value => {
      if (!controller.signal.aborted) setObservation(value);
    }).catch(error => { if (!controller.signal.aborted) setError(error); }).finally(() => {
      if (!controller.signal.aborted) setBusy(false);
    });
    return () => controller.abort();
  }, [model, requested, workspace]);

  const gpu = device === 'cpu' ? undefined : hardware.gpus[Number(device)];
  const capacity = device === 'cpu' ? ram ?? hardware.memory.installed_bytes : gpu?.total_vram_bytes;
  const target = device === 'cpu' ? 'cpu' : 'gpu';
  const cacheKiB = cache.trim() ? Number(cache) : undefined;
  const estimate = model && estimateModelFit({ model, context, cacheKiB, capacity, reserveGiB: reserve });
  const notice = estimate && {
    unknown: '容量或模型体积未知，先获取完整硬件与模型身份。',
    over: '当前估算超出容量。请试更小的量化文件、较短上下文或分层卸载。',
    partial: '权重文件未超过容量。填写此引擎每 Token 的 KV 缓存预算后，可以估算上下文开销。',
    tight: '估算余量偏小，先缩短上下文并确认当前空闲资源。',
    trial: '该配置值得试跑。开始前检查剩余资源，运行后查看采样记录。',
  }[estimate.kind];
  let lowerContext: number | undefined;
  if (model && (estimate?.kind === 'over' || estimate?.kind === 'tight')) {
    lowerContext = [16384, 8192, 4096, 2048, 1024, 512].find(value => value < context && estimateModelFit({ model, context: value, cacheKiB, capacity, reserveGiB: reserve }).kind === 'trial');
  }
  return <section className="ns-lab-record" aria-label="模型硬件适配建议">
    <div className="ns-lab-section"><h3>模型硬件适配建议</h3><Button size="sm" variant="ghost" onClick={() => setLoadCount(value => value + 1)}>刷新模型版本</Button></div>
    <LabError error={error} />
    {model ? <>
      <div className="ns-lab-grid">
        <label className="ns-lab-field">模型与量化<select value={modelID} onChange={event => { setModelID(event.target.value); setCache(''); setRequested(undefined); }}>
          {models.map(value => <option key={value.id} value={value.id}>{value.model_name} · {value.quantization || value.format || value.provider} · {value.id.slice(-8)}</option>)}
        </select></label>
        <label className="ns-lab-field">执行设备<select value={device} onChange={event => setDevice(event.target.value)}>
          {hardware.gpus.map((value, index) => <option key={`${value.model}-${index}`} value={index}>{value.model} · {hardwareCapacity(value.total_vram_bytes)}</option>)}
          <option value="cpu">CPU / 系统内存</option>
        </select></label>
        <label className="ns-lab-field">上下文预算（Token）<select value={context} onChange={event => setContext(Number(event.target.value))}>
          {[512, 1024, 2048, 4096, 8192, 16384, 32768, 65536, 131072].map(value => <option key={value}>{value}</option>)}
        </select></label>
        <label className="ns-lab-field">已知 KV 缓存（KiB / Token）<input type="number" min="0.25" max="4096" step="0.25" value={cache} placeholder="按实际引擎配置填写" onChange={event => setCache(event.target.value)} /></label>
        <label className="ns-lab-field">运行余量（GiB）<select value={reserve} onChange={event => setReserve(Number(event.target.value))}>
          {[0.5, 1, 1.5, 2, 3, 4].map(value => <option key={value}>{value}</option>)}
        </select></label>
      </div>
      <p>{engineAdvice(model, [gpu?.manufacturer, gpu?.model].filter(Boolean).join(' '), target)}</p>
      <p>容量 {hardwareCapacity(capacity)} · 权重文件 {hardwareCapacity(estimate?.weights)} · KV 预算 {hardwareCapacity(estimate?.kv)} · 合计估算 {hardwareCapacity(estimate?.estimated)}</p>
      <p className="ns-lab-notice" role="status">{notice}{lowerContext ? ` 可先试 ${lowerContext} Token。` : ''}</p>
      <p className="ns-lab-muted">按单模型、单并发、全部权重驻留估算。文件体积、缓存精度、实际卸载和驱动开销会影响结果。常规注意力的缓存可按 2 × 层数 × KV 头数 × 头维度 × 每元素字节数估算。</p>
      <div className="ns-lab-actions"><Link to={`/model-lab?model=${encodeURIComponent(model.id)}`}>用此版本做单模型测评 →</Link><Button size="sm" variant="outline" disabled={busy} onClick={() => setRequested(`${workspace}/${model.id}`)}>{busy ? '正在读取…' : '读取实测记录'}</Button></div>
      {observation ? <p><Link to={`/evaluation-reports/${encodeURIComponent(observation.reportID)}`}>查看此版本的实测报告</Link> · 上下文 {observation.contextTokens ?? '未知'} · {observation.generation.value.toFixed(1)} tok/s · 工作集采样峰值 {hardwareCapacity(observation.workingSetPeak)} · 独占显存采样峰值 {hardwareCapacity(observation.dedicatedPeak)}</p> : observation === null && <p className="ns-lab-muted">当前工作区还没有此注册版本的完整实测记录。</p>}
    </> : <p>在 <Link to="/model-hub">Model Hub</Link> 注册本地模型版本后即可估算。</p>}
  </section>;
}
