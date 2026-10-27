import { modelEngineering } from '@/lib/model-engineering-client';
import type { Evaluation, Metric, ModelEngineeringResponses, ModelRevision } from '@/lib/model-engineering-types';
import { readComparison } from './presentation';

export interface HubGenerationSample {
  value: number;
  unit: 'tokens/s';
  source: string;
  quality: string;
  caseID: string;
  attemptID: string;
}

export interface HubPerformance {
  reportID: string;
  evaluationID: string;
  finishedAt: string;
  /** One saved formal inference; this is neither an average nor a whole-model score. */
  generation: HubGenerationSample;
  generationSamples: number;
  /** regression-intelligence-v1 does not collect time to first token. */
  ttft: null;
  ttftMissingReason: string;
  note: string;
}

const isRecord = (value: unknown): value is Record<string, unknown> =>
  value !== null && typeof value === 'object' && !Array.isArray(value);

/** Match the frozen registered identity, never a Gateway name or alias alone. */
function sameRevision(a: ModelRevision, b: ModelRevision): boolean {
  const identityFields = [
    'id', 'gateway_model_id', 'provider', 'model_name', 'identity_level', 'digest',
    'deployment_version', 'model_hash', 'config_hash', 'metadata_hash', 'template_hash',
    'endpoint_fingerprint', 'engine_version',
  ] as const;
  return Boolean(a.id && b.id) && identityFields.every(key => (a[key] ?? '') === (b[key] ?? ''));
}

function matchingArm(evaluation: Evaluation, revision: ModelRevision): 'stable' | 'candidate' | undefined {
  if (sameRevision(evaluation.manifest.stable, revision)) return 'stable';
  if (sameRevision(evaluation.manifest.candidate, revision)) return 'candidate';
  return undefined;
}

function generationMetric(value: unknown): value is Metric & { value: number; unit: 'tokens/s' } {
  return isRecord(value) && typeof value.value === 'number' && Number.isFinite(value.value) && value.value >= 0
    && value.unit === 'tokens/s' && value.source === 'ollama_native_eval_count/eval_duration_ns'
    && value.quality === 'derived';
}

/** Read only the server's immutable, currently eligible selected-attempt report. */
export function readHubPerformance(
  evaluation: Evaluation,
  response: ModelEngineeringResponses['get_report'],
  revision: ModelRevision,
): HubPerformance | null {
  if (evaluation.status !== 'completed' || evaluation.manifest.source !== 'offline_real') return null;
  const arm = matchingArm(evaluation, revision);
  const { report, promotion_eligibility: eligibility } = response;
  if (!arm || !eligibility.eligible || eligibility.reasons.length > 0 || !response.report_hash) return null;
  if (report.evaluation_id !== evaluation.id || report.manifest_hash !== evaluation.manifest_hash
    || !report.attempt_set_hash || report.counts.planned < 1 || report.counts.scored !== report.counts.planned) return null;
  const analysis = readComparison(report.analysis);
  if (!analysis || analysis.source !== 'offline_real' || analysis.status !== 'complete'
    || !isRecord(report.analysis) || report.analysis.manifest_hash !== report.manifest_hash
    || report.analysis.attempt_set_hash !== report.attempt_set_hash) return null;

  const selected = new Map(report.selected.filter(attempt => attempt.arm === arm && attempt.state === 'completed')
    .map(attempt => [attempt.id, attempt]));
  const seen = new Set<string>();
  const samples: HubGenerationSample[] = [];
  for (const diagnostic of analysis.diagnostics) {
    const attempt = selected.get(diagnostic.attempt_id);
    if (!attempt || diagnostic.arm !== arm || diagnostic.case_id !== attempt.case_id
      || seen.has(attempt.id) || !generationMetric(diagnostic.tokens_per_second)) continue;
    seen.add(attempt.id);
    samples.push({
      value: diagnostic.tokens_per_second.value,
      unit: 'tokens/s',
      source: diagnostic.tokens_per_second.source,
      quality: diagnostic.tokens_per_second.quality,
      caseID: attempt.case_id,
      attemptID: attempt.id,
    });
  }
  // A deterministic saved sample keeps the card honest without inventing an aggregate.
  samples.sort((a, b) => a.caseID.localeCompare(b.caseID) || a.attemptID.localeCompare(b.attemptID));
  const generation = samples[0];
  if (!generation) return null;
  return {
    reportID: report.id,
    evaluationID: evaluation.id,
    finishedAt: evaluation.finished_at,
    generation,
    generationSamples: samples.length,
    ttft: null,
    ttftMissingReason: '当前测评协议未采集首 Token 延迟。',
    note: `真实离线测评 · 正式推理单样本（任务 ${generation.caseID}）；报告中此版本有 ${samples.length} 个生成速度样本。`,
  };
}

function checkCancelled(signal?: AbortSignal): void {
  if (signal?.aborted) throw new DOMException('Performance lookup cancelled', 'AbortError');
}

/**
 * History pagination is by immutable ID, not recency. Read every page before
 * choosing the newest completed report for this exact revision and workspace.
 * No inference, hardware collection, or evaluation is started by this lookup.
 */
export async function loadHubPerformance(
  workspaceID: string,
  revision: ModelRevision,
  signal?: AbortSignal,
): Promise<HubPerformance | null> {
  if (!workspaceID || !revision.id) return null;
  const candidates: Evaluation[] = [];
  const cursors = new Set<string>();
  let cursor = '';
  do {
    checkCancelled(signal);
    const page = await modelEngineering({ operation: 'list_evaluations', workspace_id: workspaceID, limit: 100, cursor });
    checkCancelled(signal);
    candidates.push(...page.evaluations.filter(evaluation => evaluation.manifest.workspace_id === workspaceID
      && evaluation.status === 'completed' && evaluation.manifest.source === 'offline_real'
      && matchingArm(evaluation, revision)));
    cursor = page.next_cursor;
    if (cursor && cursors.has(cursor)) throw new Error('测评历史分页游标重复，请重试。');
    if (cursor) cursors.add(cursor);
  } while (cursor);
  candidates.sort((a, b) => (Date.parse(b.finished_at) || 0) - (Date.parse(a.finished_at) || 0)
    || b.id.localeCompare(a.id));
  for (const candidate of candidates) {
    checkCancelled(signal);
    const detail = await modelEngineering({ operation: 'get_evaluation', workspace_id: workspaceID, id: candidate.id, limit: 1 });
    checkCancelled(signal);
    if (!detail.latest_report_id || detail.evaluation.status !== 'completed') continue;
    const response = await modelEngineering({ operation: 'get_report', workspace_id: workspaceID, id: detail.latest_report_id });
    checkCancelled(signal);
    const performance = readHubPerformance(detail.evaluation, response, revision);
    if (performance) return performance;
  }
  return null;
}

export function hubGenerationText(performance: HubPerformance | null): string {
  return performance ? `${Number(performance.generation.value.toFixed(1))} tok/s` : '暂无测评数据';
}
