import type { ArtifactContent, DatasetRevision, Evaluation, InferenceProfile, ModelEngineeringResponses, ModelRevision, Report } from '@/lib/model-engineering-types';

export const EXPERIMENT_MAX_BYTES = 12 * 1024 * 1024;
export const EXPERIMENT_MAX_CASES = 50;
export const EVIDENCE_TEXT_LIMIT = 65536;
type CaseDetail = ModelEngineeringResponses['get_case'];
export interface CollectedCase { id: string; detail: CaseDetail | null; issue?: string }
export interface PortableArtifact extends ArtifactContent { export_sha256: string }
export interface PortableCase { id: string; detail: (Omit<CaseDetail, 'artifacts'> & { artifacts: PortableArtifact[] }) | null; issue?: string }
export interface ExperimentBundle {
  format: 'noosphere-experiment'; version: 1; exported_at: string;
  payload: { evaluation: Evaluation; report: Report; report_hash: string; evidence: PortableCase[]; omissions: string[] };
  integrity: { algorithm: 'SHA-256'; canonicalization: 'sorted-json-v1'; payload_sha256: string };
}
const record = (v: unknown): v is Record<string, any> => !!v && typeof v === 'object' && !Array.isArray(v);
const hash = (v: unknown) => typeof v === 'string' && /^[a-f0-9]{64}$/i.test(v);
const text = (v: unknown) => typeof v === 'string';
const count = (v: unknown) => Number.isSafeInteger(v) && (v as number) >= 0;
const secretKey = /^(?:__proto__|prototype|constructor|api[_-]?key|authorization|proxy[_-]?authorization|password|passwd|secret|client[_-]?secret|access[_-]?token|refresh[_-]?token|cookie|set[_-]?cookie|headers|endpoint|base_url|analysis_response|health_response)$/i;

/** Hashes verify the portable view, not publisher identity or the original stored bytes. */
export function canonicalJSON(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(canonicalJSON).join(',')}]`;
  if (record(value)) return `{${Object.keys(value).filter(key => value[key] !== undefined).sort().map(key => `${JSON.stringify(key)}:${canonicalJSON(value[key])}`).join(',')}}`;
  return JSON.stringify(value);
}
export async function sha256Text(value: string): Promise<string> {
  const bytes = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(value));
  return [...new Uint8Array(bytes)].map(byte => byte.toString(16).padStart(2, '0')).join('');
}

function safeString(value: string): string {
  return value.replace(/\bBearer\s+[A-Za-z0-9._~+\/-]+=*/gi, 'Bearer [REDACTED]')
    .replace(/\bsk-[A-Za-z0-9_-]{16,}/g, '[REDACTED]')
    .replace(/(https?:\/\/)[^\s/@]+:[^\s/@]+@/gi, '$1[REDACTED]@')
    .replace(/(["']?(?:api[_-]?key|access[_-]?token|refresh[_-]?token|password|authorization|secret)["']?\s*[:=]\s*)(?:"[^"\r\n]*"|'[^'\r\n]*'|[^\s,;&}\r\n]+)/gi, '$1"[REDACTED]"');
}
function sanitized<T>(value: T): T {
  return JSON.parse(JSON.stringify(value, (key, item) => secretKey.test(key) ? undefined : typeof item === 'string' ? safeString(item) : item)) as T;
}

export async function buildExperimentBundle(evaluation: Evaluation, report: Report, reportHash: string, cases: CollectedCase[], exportedAt = new Date().toISOString()): Promise<ExperimentBundle> {
  if (report.manifest_hash !== evaluation.manifest_hash || report.evaluation_id !== evaluation.id) throw new Error('报告与冻结清单不匹配，不能合并导出。');
  if (evaluation.manifest.dataset.cases.length > EXPERIMENT_MAX_CASES) throw new Error('实验包最多包含 50 个任务，请缩小任务集。');
  const omissions = ['仅保存本报告选中的尝试；被替代的尝试与原始私有协议不随包导出。', '内容哈希用于发现文件改动，不是作者签名；原始服务端哈希保留为引用，不能用脱敏阅读副本反推原始字节。'];
  const evidence: PortableCase[] = [];
  for (const frozen of evaluation.manifest.dataset.cases) {
    const collected = cases.find(item => item.id === frozen.id);
    if (!collected?.detail || collected.detail.case.id !== frozen.id) { evidence.push({ id: frozen.id, detail: null, issue: collected?.issue || '案例证据不可读取。' }); continue; }
    const refs = report.selected.filter(item => item.case_id === frozen.id);
    const attempts = collected.detail.attempts.filter(a => refs.some(r => r.id === a.id && r.arm === a.arm && r.attempt_no === a.attempt_no && r.state === a.state && r.verdict === a.verdict));
    const allowed = new Set([frozen.input_artifact, frozen.fixture_artifact, ...attempts.flatMap(a => [a.input_artifact, a.output_artifact, a.metrics?.warmup_artifact, ...(a.result.evidence_artifacts || [])])]);
    const artifacts: PortableArtifact[] = [];
    for (const a of collected.detail.artifacts.filter(a => allowed.has(a.id))) {
      const clean = sanitized(a.content || ''), content = clean.slice(0, EVIDENCE_TEXT_LIMIT);
      const redacted = a.redacted || clean !== a.content && !!a.content;
      const truncated = a.truncated || clean.length > EVIDENCE_TEXT_LIMIT;
      artifacts.push({ id: a.id, content, missing: a.missing, truncated, redacted, export_sha256: await sha256Text(content) });
    }
    const issue = [collected.issue, refs.length !== attempts.length ? '部分报告选中尝试不可读取。' : '', artifacts.some(a => a.missing || a.truncated || a.redacted) ? '存在缺失、截断或脱敏的证据；导出哈希对应本包文本。' : ''].filter(Boolean).join(' ');
    evidence.push({ id: frozen.id, detail: { case: frozen, attempts, artifacts, next_cursor: '' }, issue: issue || undefined });
  }
  const cleanEvaluation = sanitized(evaluation);
  if (canonicalJSON(cleanEvaluation) !== canonicalJSON(evaluation)) omissions.push('冻结清单的阅读副本已移除可识别的凭据字段或文本；源清单哈希不能用此副本逐字验证。');
  const payload = sanitized({ evaluation: cleanEvaluation, report: { ...report, analysis: undefined, superseded: [] }, report_hash: reportHash, evidence, omissions });
  const bundle: ExperimentBundle = { format: 'noosphere-experiment', version: 1, exported_at: exportedAt, payload, integrity: { algorithm: 'SHA-256', canonicalization: 'sorted-json-v1', payload_sha256: await sha256Text(canonicalJSON(payload)) } };
  if (new TextEncoder().encode(JSON.stringify(bundle, null, 2)).length > EXPERIMENT_MAX_BYTES) throw new Error('实验包超过 12 MiB，未导出；请使用逐题证据与 HTML 报告。');
  return bundle;
}

function validateShape(value: unknown): string[] {
  const issues: string[] = [];
  if (!record(value) || value.format !== 'noosphere-experiment' || value.version !== 1) return ['只支持 Noosphere 实验包版本 1。'];
  if (Object.keys(value).some(k => !['format', 'version', 'exported_at', 'payload', 'integrity'].includes(k))) issues.push('实验包包含不支持的顶层字段。');
  if (!text(value.exported_at) || !Number.isFinite(Date.parse(value.exported_at))) issues.push('导出时间无效。');
  if (!record(value.integrity) || value.integrity.algorithm !== 'SHA-256' || value.integrity.canonicalization !== 'sorted-json-v1' || !hash(value.integrity.payload_sha256)) issues.push('完整性信息无效。');
  const p = value.payload;
  if (!record(p) || !record(p.evaluation) || !record(p.report) || !Array.isArray(p.evidence) || !Array.isArray(p.omissions) || !p.omissions.every(text)) return [...issues, '实验包缺少清单、报告或案例证据。'];
  if (Object.keys(p).some(k => !['evaluation', 'report', 'report_hash', 'evidence', 'omissions'].includes(k))) issues.push('实验包包含不支持的内容。');
  const e = p.evaluation, r = p.report, m = e.manifest;
  if (!record(m) || !record(m.dataset) || !Array.isArray(m.dataset.cases) || !record(m.profile) || !record(m.profile.requested) || !record(m.environment)) return [...issues, '冻结清单结构无效。'];
  if (!text(e.id) || e.id !== r.evaluation_id || e.manifest_hash !== r.manifest_hash || !hash(r.manifest_hash) || !hash(r.attempt_set_hash) || !hash(p.report_hash) || !text(r.id)) issues.push('报告身份或来源哈希不匹配。');
  if (!['offline_real', 'offline_fixture'].includes(m.source) || !['baseline', 'compare', undefined].includes(m.evaluation_mode) || !text(m.workflow_id)) issues.push('实验模式或来源无效。');
  for (const key of ['stable', 'candidate']) if (!record(m[key]) || !['id', 'model_name', 'provider', 'digest', 'identity_level'].every(k => text(m[key][k]))) issues.push(`${key} 模型身份无效。`);
  if (!text(m.profile.id) || !['temperature', 'seed', 'num_ctx', 'max_output'].every(k => typeof m.profile.requested[k] === 'number' && Number.isFinite(m.profile.requested[k]))) issues.push('冻结推理配置无效。');
  const d = m.dataset;
  if (!['id', 'benchmark_id', 'version', 'scorer_version'].every(k => text(d[k])) || !hash(d.content_hash)) issues.push('冻结任务集身份无效。');
  if (!d.cases.length || d.cases.length > EXPERIMENT_MAX_CASES || !d.cases.every((c: unknown) => record(c) && text(c.id) && text(c.category) && text(c.expected))) return [...issues, '任务集须包含 1–50 个有效案例。'];
  const ids = new Set(d.cases.map((c: any) => c.id));
  if (ids.size !== d.cases.length) issues.push('冻结任务 ID 重复。');
  const validCounts = (c: unknown) => record(c) && ['planned', 'attempted', 'scored', 'pass', 'fail', 'infra', 'cancelled', 'interrupted', 'unknown', 'paired_complete'].every(k => count(c[k]) && c[k] <= d.cases.length * 2) && c.scored <= c.planned && c.pass + c.fail === c.scored;
  if (!validCounts(r.counts) || !record(r.categories) || !Object.values(r.categories).every((c: any) => record(c) && validCounts(c.stable) && validCounts(c.candidate)) || !Array.isArray(r.pair_matrix) || r.pair_matrix.length !== 4 || !r.pair_matrix.every(count)) issues.push('报告计数无效。');
  if (r.counts?.planned !== d.cases.length * 2) issues.push('报告分母与冻结任务数量不一致。');
  if (!Array.isArray(r.selected) || r.selected.length > d.cases.length * 2) return [...issues, '选中尝试无效。'];
  const selected = new Map<string, any>();
  for (const a of r.selected) {
    if (!record(a) || !ids.has(a.case_id) || !['stable', 'candidate'].includes(a.arm) || !text(a.id) || !count(a.attempt_no) || !text(a.state) || !text(a.verdict) || !hash(a.result_hash) || !hash(a.attempt_hash)) { issues.push('选中尝试与任务或证据哈希不匹配。'); continue; }
    const key = `${a.case_id}/${a.arm}`;
    if (selected.has(key)) issues.push('同一任务和轮次有重复的选中尝试。');
    selected.set(key, a);
  }
  const seen = new Set<string>();
  if (p.evidence.length !== d.cases.length) issues.push('案例覆盖不完整；缺失案例须明确记录 detail:null。');
  for (const c of p.evidence) {
    if (!record(c) || !ids.has(c.id) || seen.has(c.id)) { issues.push('案例证据重复或不在冻结目录中。'); continue; }
    seen.add(c.id);
    if (c.issue !== undefined && !text(c.issue)) issues.push('案例缺失说明无效。');
    if (c.detail === null) continue;
    const detail = c.detail;
    if (!record(detail) || !record(detail.case) || canonicalJSON(detail.case) !== canonicalJSON(d.cases.find((row: any) => row.id === c.id)) || !Array.isArray(detail.attempts) || detail.attempts.length > 2 || !Array.isArray(detail.artifacts) || detail.artifacts.length > 24) { issues.push('案例证据结构或冻结任务不匹配。'); continue; }
    const arms = new Set<string>();
    for (const a of detail.attempts) {
      const ref = record(a) && selected.get(`${c.id}/${a.arm}`);
      if (!ref || arms.has(a.arm) || a.case_id !== c.id || a.evaluation_id !== e.id || !['id', 'attempt_no', 'state', 'verdict'].every(k => a[k] === ref[k]) || !record(a.result) || !record(a.metrics)) { issues.push('案例尝试不是本报告选中的结果。'); continue; }
      arms.add(a.arm);
      if (a.result.evidence_artifacts !== undefined && (!Array.isArray(a.result.evidence_artifacts) || !a.result.evidence_artifacts.every(text))) issues.push('评分证据引用无效。');
    }
    const artifactIDs = new Set<string>();
    for (const a of detail.artifacts) {
      if (!record(a) || !text(a.id) || artifactIDs.has(a.id) || !text(a.content) || a.content.length > EVIDENCE_TEXT_LIMIT || typeof a.missing !== 'boolean' || typeof a.truncated !== 'boolean' || a.redacted !== undefined && typeof a.redacted !== 'boolean' || !hash(a.export_sha256)) issues.push('证据文本、长度或哈希无效。');
      if (record(a)) artifactIDs.add(a.id);
    }
  }
  return [...new Set(issues)];
}

export async function parseExperimentBundle(raw: string): Promise<{ bundle?: ExperimentBundle; issues: string[] }> {
  if (new TextEncoder().encode(raw).length > EXPERIMENT_MAX_BYTES) return { issues: ['文件超过 12 MiB 限制。'] };
  let value: unknown;
  try { value = JSON.parse(raw); } catch { return { issues: ['文件不是有效 JSON。'] }; }
  // Bound nesting and object count before recursive canonicalization. Never merge imported objects into application state.
  const stack: Array<[unknown, number]> = [[value, 0]]; let nodes = 0;
  while (stack.length) {
    const [item, depth] = stack.pop()!;
    if (++nodes > 100000 || depth > 32) return { issues: ['实验包的结构过深或内容过多。'] };
    if (typeof item === 'number' && !Number.isFinite(item)) return { issues: ['实验包包含非有限数值。'] };
    if (typeof item === 'string' && item.length > 512 * 1024) return { issues: ['实验包含超长字段。'] };
    if (record(item) || Array.isArray(item)) for (const [key, child] of Object.entries(item)) {
      if (secretKey.test(key)) return { issues: ['实验包包含连接凭据、私有协议或不安全字段，已拒绝导入。'] };
      stack.push([child, depth + 1]);
    }
  }
  const issues = validateShape(value); if (issues.length) return { issues };
  const bundle = value as ExperimentBundle;
  if (await sha256Text(canonicalJSON(bundle.payload)) !== bundle.integrity.payload_sha256) return { issues: ['实验包内容哈希不匹配；文件可能已被改动或损坏。'] };
  for (const item of bundle.payload.evidence) for (const artifact of item.detail?.artifacts || []) if (await sha256Text(artifact.content || '') !== artifact.export_sha256) return { issues: [`案例 ${item.id} 的导出文本哈希不匹配。`] };
  return { bundle, issues: [] };
}

export interface ReplayPlan { ready: boolean; reasons: string[]; notices: string[]; mode: 'baseline' | 'compare'; stable?: ModelRevision; candidate?: ModelRevision; profile?: InferenceProfile; dataset?: DatasetRevision; workflow: string }
export function matchingModel(source: ModelRevision, local: ModelRevision): boolean {
  const localProvider = local.provider === 'ollama'
    || local.provider === 'openai_compatible' && local.format === 'gguf' && local.engine_version?.startsWith('llama.cpp/')
    || local.provider === 'openvino' && local.format === 'openvino_ir' && local.engine_version?.startsWith('openvino-genai/');
  return !!source.digest && source.identity_level === 'digest' && local.identity_level === 'digest' && !!localProvider &&
    ['provider', 'digest', 'model_hash', 'config_hash', 'metadata_hash', 'template_hash', 'engine_version', 'quantization', 'format'].every(key => (source[key as keyof ModelRevision] || '') === (local[key as keyof ModelRevision] || ''));
}
export function assessReplay(bundle: ExperimentBundle, models: ModelRevision[], profiles: InferenceProfile[], datasets: DatasetRevision[]): ReplayPlan {
  const m = bundle.payload.evaluation.manifest, d = m.dataset, mode = m.evaluation_mode || 'compare';
  const reasons: string[] = [], notices = ['复跑使用本机已注册连接与当前硬件；权重和参数匹配不能保证逐字复现，结果会保存为新测评。'];
  const workflow = d.benchmark_id === 'local-text-v1' ? 'local.text.v1' : d.benchmark_id === 'codetask-go-v1' ? 'codelens.codetask.go.v1' : /^reviewer-(core|context-4k|context-8k)-v1$/.test(d.benchmark_id) ? 'local.review.v1' : d.benchmark_id === 'reviewer-tools-v1' ? 'local.tools.v1' : '';
  if (!workflow || m.workflow_id !== workflow) reasons.push('只能复跑匹配的内置文本、Reviewer、工具或 Go 任务集；此任务集仍可只读核对证据。');
  if (m.source !== 'offline_real') reasons.push('源包是流程验证数据，不自动转成真实模型复跑。');
  const dataset = datasets.find(local => !local.archived && local.kind !== 'workspace_custom' && local.benchmark_id === d.benchmark_id && local.version === d.version && local.scorer_version === d.scorer_version && local.prompt_template_hash === d.prompt_template_hash && (d.source_content_hash ? local.content_hash === d.source_content_hash : local.content_hash === d.content_hash) && local.cases.length === d.cases.length && local.cases.every(c => d.cases.some(frozen => frozen.id === c.id && frozen.category === c.category)));
  if (!dataset) reasons.push('本机没有匹配来源内容哈希、评分器和提示模板的内置任务版本；旧包未记录来源哈希时不能证明任务一致。');
  const stable = models.find(local => matchingModel(m.stable, local));
  const candidate = mode === 'baseline' && matchingModel(m.stable, m.candidate) ? stable : models.find(local => matchingModel(m.candidate, local));
  if (!stable) reasons.push('未找到第一轮 / 原模型的本机已注册权重与配置身份，请在 Model Hub 注册相同版本后重试。');
  if (!candidate) reasons.push('未找到第二轮 / 候选模型的本机已注册权重与配置身份。');
  if (mode === 'baseline' && (!matchingModel(m.stable, m.candidate) || stable?.id !== candidate?.id)) reasons.push('源包的单模型两轮身份不一致，不能按基线复跑。');
  if (mode === 'compare' && stable && candidate && stable.id === candidate.id) reasons.push('两个版本比较需要本机两个不同的注册版本。');
  const profile = profiles.find(local => canonicalJSON(local.requested) === canonicalJSON(m.profile.requested));
  if (!profile) reasons.push('本机尚无完全相同的已保存推理配置。请在 Model Lab 核对并保存源包所示参数，再重新准备。');
  if (dataset?.suite?.minimum_context && m.profile.requested.num_ctx < dataset.suite.minimum_context) reasons.push(`源配置低于套件最低上下文 ${dataset.suite.minimum_context}，不能复跑。`);
  if (dataset?.suite?.max_output && m.profile.requested.max_output > dataset.suite.max_output) reasons.push(`源配置超过套件输出上限 ${dataset.suite.max_output}，不能复跑。`);
  if (bundle.payload.evidence.some(c => !c.detail || c.issue)) notices.push('源包包含部分缺失或脱敏证据；复跑不会补写或改动源报告。');
  return { ready: reasons.length === 0, reasons, notices, mode, stable, candidate, profile, dataset, workflow };
}
