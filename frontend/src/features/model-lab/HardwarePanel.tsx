import { useEffect, useRef, useState } from 'react';
import type { EnvironmentSnapshot } from '@/lib/model-engineering-types';
import { modelEngineering } from '@/lib/model-engineering-client';
import { Button } from '@/components/ui/button';
import { Surface } from '@/components/ui/surface';
import { hardwareCapacity, readHardware } from './hardware-presentation';
import { JsonView, LabError } from './shared';

const STATUS = { complete: '检测完成', partial: '部分信息可用', unavailable: '检测不可用', unsupported: '当前平台仅提供基础信息' };

export function HardwarePanel({ environment, load = true }: { environment?: EnvironmentSnapshot | null; load?: boolean }) {
  const [snapshot, setSnapshot] = useState<EnvironmentSnapshot | null>(null), [busy, setBusy] = useState(false), [error, setError] = useState<unknown>();
  const epoch = useRef(0), pending = useRef(false);
  const refresh = async () => {
    if (pending.current) return;
    pending.current = true; const generation = epoch.current; setBusy(true); setError(undefined);
    try { const result = await modelEngineering({ operation: 'environment' }); if (generation === epoch.current) setSnapshot(result.environment); }
    catch (e) { if (generation === epoch.current) setError(e); }
    finally { if (generation === epoch.current) { pending.current = false; setBusy(false); } }
  };
  useEffect(() => { if (load) void refresh(); return () => { epoch.current++; pending.current = false; }; }, [load]);
  const current = snapshot ?? environment, hardware = readHardware(current?.hardware);
  return <Surface className="ns-lab-panel" aria-label="本机硬件检测">
    <div className="ns-lab-section"><h2>本机配置检测</h2><Button size="sm" variant="outline" disabled={busy} onClick={() => void refresh()}>{busy ? '正在读取本机型号…' : '重新检测本机配置'}</Button></div>
    <p className="ns-lab-muted">只读获取本机硬件型号，保留在本地；不读取序列号、账号或网络标识，不自动发送给模型。检测结果可缓存最多 30 秒。</p>
    <LabError error={error} />
    {hardware ? <><p role="status">{STATUS[hardware.status]} · {hardware.platform} · {hardware.collector_version}</p>
      <div className="ns-lab-grid">
        <div><h3>整机与主板</h3><p>整机：{[hardware.system.manufacturer, hardware.system.model].filter(Boolean).join(' · ') || '厂商未提供型号'}</p>{hardware.system.missing_reason && <p className="ns-lab-muted">{hardware.system.missing_reason.includes('generic') ? '固件仅提供通用整机名称，具体组装机型号未知。' : hardware.system.missing_reason}</p>}<p>主板：{[hardware.motherboard.manufacturer, hardware.motherboard.model].filter(Boolean).join(' · ') || '未知'}</p></div>
        <div><h3>操作系统</h3><p>{hardware.os.name || current?.os || '未知'} · {hardware.os.architecture || current?.arch}</p><p>版本 {hardware.os.version || '未知'} · Build {hardware.os.build || '未知'}</p></div>
        <div><h3>CPU</h3>{hardware.cpus.length ? hardware.cpus.map((c, i) => <p key={i}>{c.model || c.manufacturer || '型号未知'}<br /><span className="ns-lab-muted">{c.physical_cores || '?'} 物理核 / {c.logical_cores || '?'} 逻辑核</span></p>) : <p>型号未采集</p>}</div>
        <div><h3>内存</h3><p>已安装：{hardwareCapacity(hardware.memory.installed_bytes)}<br />系统可用总容量：{hardwareCapacity(current?.ram_bytes)}</p><p className="ns-lab-muted">这里的容量不表示当前空闲内存。</p></div>
      </div>
      <h3>显卡与驱动</h3>{hardware.gpus.length ? hardware.gpus.map((g, i) => <article className="ns-lab-record" key={i}><strong>{g.model || '显卡型号未知'}</strong><p>驱动 {g.driver_version || '未知'} · 显存总容量 {hardwareCapacity(g.total_vram_bytes)}</p><p className="ns-lab-muted">显存来源：{g.vram_source || '未知'}{g.vram_missing_reason ? ` · ${g.vram_missing_reason}` : ''}</p></article>) : <p>显卡信息不可用，请查看检测缺失项。</p>}
      <p className="ns-lab-notice">显存总容量不等于当前可用显存，也不保证某个模型能够运行；不会据此自动修改模型、参数或发布策略。</p>
      <details className="ns-lab-details"><summary>内存条与系统盘具体型号</summary>
        {hardware.memory.modules.map((m, i) => <p key={`m${i}`}>内存 {i + 1}：{m.manufacturer || '厂商未知'} · {m.part_number || '料号未知'} · {hardwareCapacity(m.capacity_bytes)} · {m.speed_mts ? `${m.speed_mts} MT/s（固件报告）` : '速率未知'}</p>)}
        {hardware.system_disks.map((d, i) => <p key={`d${i}`}>系统盘：{d.model || '型号未知'} · {hardwareCapacity(d.capacity_bytes)} · {d.interface_type || '接口未知'}</p>)}
      </details>
      <details className="ns-lab-details"><summary>采集来源、缺失项与限制</summary>{hardware.issues.map((issue, i) => <p key={i}>{issue.component} · {issue.code} · {issue.message}</p>)}{hardware.limitations.map((line, i) => <p key={i}>{line}</p>)}<JsonView value={hardware} title="完整本地硬件档案" /></details>
    </> : <p role="status" className="ns-lab-muted">{busy ? '正在检测 CPU、显卡、内存及整机型号…' : current ? '这份环境记录没有受支持的硬件型号档案；可以重新检测，历史测评内容不会被改写。' : '本机配置尚未读取；请检测或检查本地后端状态。'}</p>}
  </Surface>;
}
