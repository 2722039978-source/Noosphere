import type { Metric, Options } from '@/lib/model-engineering-types';

export const STATUS: Record<string, string> = { queued: '等待执行', running: '运行中', completed: '已完成', incomplete: '部分完成', failed: '失败', cancelled: '已取消', interrupted: '已中断' };
export const active = (status: string) => status === 'queued' || status === 'running';
export const evidenceLabel = (source: string) => source === 'offline_real' ? '真实离线测评' : source === 'offline_fixture' ? '测试夹具 · 非真实模型验证' : source.startsWith('simulation') ? '模拟证据 · 非生产验证' : source;
export const percent = (v: number | null) => v === null ? '未知' : `${(v * 100).toFixed(1)}%`;
export const short = (v?: string) => v ? v.slice(0, 16) : '未知';
export function metricText(m?: Metric): string {
  if (!m || m.value === null || !Number.isFinite(m.value)) return `未知 · ${m?.missing_reason || '未采集'}`;
  const value = m.unit === 'ns' ? `${(m.value / 1e9).toFixed(3)} s` : m.unit === 'bytes' ? `${(m.value / 1024 ** 3).toFixed(2)} GiB` : `${Number(m.value.toFixed(3))} ${m.unit}`;
  return `${value} · ${m.source} / ${m.quality}`;
}
export interface LocalBudget { max_context: number; max_output: number; inference_timeout_ns: number }
export function sameOptions(a: Options, b: Options): boolean {
  const normalize = (v: Options) => JSON.stringify(Object.fromEntries(Object.entries(v).filter(([, x]) => x !== undefined).sort(([a], [b]) => a.localeCompare(b))));
  return normalize(a) === normalize(b);
}
export function validateSetup(stable: string, candidate: string, dataset: string, p: Options, budget?: LocalBudget): string {
  if (!stable || !candidate) return '请先在 Model Hub 注册两个模型版本。';
  if (stable === candidate) return '请选择两个不同的模型版本。';
  if (!dataset) return '测评集尚未就绪。';
  const ctx = Math.min(budget?.max_context ?? 8192, 8192), out = Math.min(budget?.max_output ?? 2048, 2048), seconds = Math.min(budget?.inference_timeout_ns ?? 120e9, 120e9) / 1e9;
  if (!Number.isInteger(p.num_ctx) || p.num_ctx < 1 || p.num_ctx > ctx) return `上下文预算必须为 1–${ctx} tokens。`;
  if (!Number.isInteger(p.max_output) || p.max_output < 1 || p.max_output > out) return `输出预算必须为 1–${out} tokens。`;
  if (!Number.isFinite(p.temperature) || p.temperature < 0 || p.temperature > 2 || !Number.isSafeInteger(p.seed) || p.seed < 0) return '采样温度须为 0–2，种子须为非负安全整数。';
  const deadline = p.deadline_ns === undefined || p.deadline_ns === 0 ? 120e9 : p.deadline_ns;
  if (!Number.isFinite(deadline) || deadline < 1e6 || deadline > seconds * 1e9) return `每次推理时限须为 0.001–${seconds} 秒。`;
  if ((p.parallelism !== undefined && p.parallelism !== 1) || (p.retry_limit !== undefined && p.retry_limit !== 0) || (p.keep_alive && p.keep_alive !== '0' && p.keep_alive !== '0s')) return '此执行协议要求并发 1、重试 0、正式推理 keep_alive 为 0。';
  return '';
}

export interface Proportion { successes: number; n: number; rate: number | null; lower_95: number | null; upper_95: number | null }
export interface CategoryComparison { stable: Proportion; candidate: Proportion; pair_matrix: number[]; paired_complete: number; loss_rate: Proportion }
export interface Comparison {
  version: string; source: string; status: string; arms: Record<string, Proportion>; categories: Record<string, CategoryComparison>;
  loss_rate: Proportion; performance: { status: string; sample_count: number; planned_pairs: number; paired_median_ratio: Metric; reasons: string[]; warmup_calls: number };
  diagnostics: { case_id: string; arm: string; attempt_id: string; loading_protocol: string; warm_verified: boolean; [key: string]: unknown }[];
  metric_coverage: Record<string, Record<string, { planned: number; samples: number; missing: number; unit: string; missing_reasons: Record<string, number> }>>;
  limitations: string[];
}
const record = (x: unknown): x is Record<string, any> => !!x && typeof x === 'object' && !Array.isArray(x);
const numeric = (x: unknown) => typeof x === 'number' && Number.isFinite(x);
const nullableNumber = (x: unknown) => x === null || numeric(x);
const proportion = (x: unknown): x is Proportion => record(x) && numeric(x.successes) && numeric(x.n) && nullableNumber(x.rate) && nullableNumber(x.lower_95) && nullableNumber(x.upper_95);
const strings = (x: unknown): x is string[] => Array.isArray(x) && x.every(v => typeof v === 'string');
const metric = (x: unknown): x is Metric => record(x) && nullableNumber(x.value) && typeof x.unit === 'string' && typeof x.source === 'string' && typeof x.quality === 'string';
/** Older and future report bodies stay inspectable without inventing an analysis. */
export function readComparison(x: unknown): Comparison | null {
  if (!record(x) || x.version !== 'regression-intelligence-v1' || typeof x.source !== 'string' || typeof x.status !== 'string') return null;
  if (!record(x.arms) || !proportion(x.arms.stable) || !proportion(x.arms.candidate) || !proportion(x.loss_rate) || !record(x.categories)) return null;
  if (!Object.values(x.categories).every(c => record(c) && proportion(c.stable) && proportion(c.candidate) && proportion(c.loss_rate) && numeric(c.paired_complete) && Array.isArray(c.pair_matrix) && c.pair_matrix.length === 4 && c.pair_matrix.every(numeric))) return null;
  if (!record(x.performance) || !metric(x.performance.paired_median_ratio) || !numeric(x.performance.sample_count) || !numeric(x.performance.planned_pairs) || !strings(x.performance.reasons)) return null;
  if (!Array.isArray(x.diagnostics) || !x.diagnostics.every(d => record(d) && typeof d.case_id === 'string' && typeof d.arm === 'string' && typeof d.attempt_id === 'string')) return null;
  if (!record(x.metric_coverage) || !Object.values(x.metric_coverage).every(arm => record(arm) && Object.values(arm).every(m => record(m) && numeric(m.planned) && numeric(m.samples) && numeric(m.missing) && typeof m.unit === 'string' && record(m.missing_reasons)))) return null;
  if (!strings(x.limitations)) return null;
  return x as unknown as Comparison;
}
