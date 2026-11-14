import { hardwareCapacity } from './hardware-presentation';
import type { Comparison } from './presentation';

type Row = { key: string; name: string; unit: string; source: string; mode: 'median' | 'peak' };
const ROWS: Row[] = [
  { key: 'first_token_ns', name: '首 Token 延迟中位数', unit: 'ns', source: 'host_monotonic_stream_delta', mode: 'median' },
  { key: 'first_answer_ns', name: '首段最终答案延迟中位数', unit: 'ns', source: 'host_monotonic_answer_delta', mode: 'median' },
  { key: 'peak_working_set_bytes', name: '进程工作集采样峰值', unit: 'bytes', source: 'windows_loopback_listener_sampled', mode: 'peak' },
  { key: 'peak_dedicated_gpu_bytes', name: '独占显存采样峰值', unit: 'bytes', source: 'windows_loopback_listener_sampled', mode: 'peak' },
];

export function MeasuredPerformance({ analysis, baseline, planned }: { analysis: Comparison; baseline: boolean; planned: number }) {
  const samples = (row: Row, arm: 'stable' | 'candidate') => analysis.diagnostics.flatMap(record => {
    const value = record[row.key] as { value?: unknown; unit?: unknown; source?: unknown; quality?: unknown } | undefined;
    return record.arm === arm && value?.unit === row.unit && value.source === row.source && value.quality === 'observed'
      && typeof value.value === 'number' && Number.isFinite(value.value) && value.value >= 0 ? [value.value] : [];
  }).sort((a, b) => a - b);
  const rows = ROWS.map(row => ({ ...row, stable: samples(row, 'stable'), candidate: samples(row, 'candidate') }));
  if (!rows.some(row => row.stable.length || row.candidate.length)) return null;
  function cell(row: Row, values: number[]) {
    if (!values.length) return <>未采集 <small>0/{planned}</small></>;
    const middle = Math.floor(values.length / 2);
    const result = row.mode === 'peak' ? values[values.length - 1] : values.length % 2 ? values[middle] : (values[middle - 1] + values[middle]) / 2;
    return <>{row.unit === 'bytes' ? hardwareCapacity(result) : `${(result / 1e9).toFixed(2)} s`} <small>{values.length}/{planned}</small></>;
  }
  return <section className="ns-lab-record" aria-label="延迟与资源实测">
    <h3>延迟与资源实测</h3>
    <div className="ns-lab-table-wrap"><table className="ns-lab-table"><thead><tr><th>指标</th><th>{baseline ? '第一轮' : 'Stable'}</th><th>{baseline ? '第二轮' : 'Candidate'}</th></tr></thead><tbody>
      {rows.map(row => <tr key={row.key}><th>{row.name}</th><td>{cell(row, row.stable)}</td><td>{cell(row, row.candidate)}</td></tr>)}
    </tbody></table></div>
    <p className="ns-lab-muted">首 Token 包含第一个可见的思考或回答增量。Windows 按约 500 ms 采集本机模型服务进程，显存列统计专用分配，短时峰值可能落在采样间隔内。</p>
  </section>;
}
