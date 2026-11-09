import type { Attempt, Evaluation, ModelEngineeringResponses, Report, SelectedAttempt } from '@/lib/model-engineering-types';
import type { Comparison } from './presentation';

export type CaseChange = 'improved' | 'regressed' | 'both_pass' | 'both_fail' | 'unavailable';
export interface ReportCase { id: string; category: string; expected: string; stable?: SelectedAttempt; candidate?: SelectedAttempt; change: CaseChange }
export interface ArmReading { label: string; modelName: string; pass: number; scored: number; planned: number; infra: number; unavailable: number }
export interface ReportInsights {
  baseline: boolean; title: string; explanation: string; cases: ReportCase[];
  arms: Record<'stable' | 'candidate', ArmReading>; improvements: number; regressions: number; comparable: number;
  performance: { headline: string; detail: string; samples: number | null; planned: number };
  cautions: string[];
}
export interface ArmEvidence { attemptID?: string; attemptNo?: number; state?: string; verdict?: string; verdictText?: string; output: string; notice: string; score?: string; scoreSummary?: string }
export interface PairedCaseEvidence { id: string; category: string; change: CaseChange; task: string; expected: string; stable: ArmEvidence; candidate: ArmEvidence; issue?: string }
const qualityVerdicts = new Set(['pass', 'fail', 'functional_fail', 'invalid_output', 'compile_fail', 'resource_fail', 'runtime_fail', 'execution_fail', 'sandbox_violation']);
const scored = (a?: SelectedAttempt) => a?.state === 'completed' && qualityVerdicts.has(a.verdict);
export const changeLabel = (change: CaseChange, baseline = false) => ({ improved: baseline ? '仅第二轮通过' : '候选模型改善', regressed: baseline ? '仅第一轮通过' : '候选模型变差', both_pass: '两边均通过', both_fail: '两边均未通过', unavailable: '暂无法比较' })[change];
export const verdictLabel = (state?: string, verdict?: string) => state === 'failed' ? '基础设施失败，未计入正确性' : state === 'completed' ? ({ pass: '通过', invalid_output: '输出格式或代码不符合要求', functional_fail: '预期结果未通过', compile_fail: '编译未通过', resource_fail: '输出程序超出执行资源限制', runtime_fail: '执行未通过' }[verdict || ''] || `未通过 · ${verdict}`) : ({ cancelled: '已取消，未评分', interrupted: '已中断，未评分', queued: '尚未执行', running: '报告生成时尚未完成' }[state || ''] || '本报告没有已保存结果');

export function buildReportInsights(report: Report, analysis: Comparison | null, evaluation?: Evaluation | null): ReportInsights {
  const baseline = evaluation?.manifest.evaluation_mode === 'baseline' || analysis?.evaluation_mode === 'baseline';
  const manifestCases = evaluation?.manifest.dataset.cases;
  const source = manifestCases?.length ? manifestCases : [...new Map(report.selected.map(a => [a.case_id, { id: a.case_id, category: '', expected: '' }])).values()];
  const cases = source.map(c => {
    const stable = report.selected.find(a => a.case_id === c.id && a.arm === 'stable'), candidate = report.selected.find(a => a.case_id === c.id && a.arm === 'candidate');
    let change: CaseChange = 'unavailable';
    if (scored(stable) && scored(candidate)) change = stable!.verdict === 'pass' ? candidate!.verdict === 'pass' ? 'both_pass' : 'regressed' : candidate!.verdict === 'pass' ? 'improved' : 'both_fail';
    return { id: c.id, category: c.category, expected: c.expected, stable, candidate, change };
  });
  const planned = Math.max(cases.length, Math.floor(report.counts.planned / 2));
  const arm = (key: 'stable' | 'candidate', label: string): ArmReading => {
    const selected = cases.map(c => c[key]);
    const n = selected.filter(scored).length;
    return { label, modelName: evaluation?.manifest[key].model_name || '模型版本暂不可读取', pass: selected.filter(a => scored(a) && a?.verdict === 'pass').length, scored: n, planned, infra: selected.filter(a => a?.state === 'failed').length, unavailable: Math.max(0, planned - n) };
  };
  const arms = { stable: arm('stable', baseline ? '第一轮' : '原模型'), candidate: arm('candidate', baseline ? '第二轮' : '候选模型') };
  const improvements = cases.filter(c => c.change === 'improved').length, regressions = cases.filter(c => c.change === 'regressed').length;
  const comparable = cases.filter(c => c.change !== 'unavailable').length;
  const unavailable = Math.max(0, planned - comparable);
  let title = '两边已评分任务的通过情况一致';
  if (!comparable) title = '当前结果不足以比较正确性';
  else if (improvements && regressions) title = '候选模型有改善，也出现退化';
  else if (regressions) title = `候选模型在 ${regressions} 道题上变差`;
  else if (improvements) title = `候选模型在 ${improvements} 道题上改善`;
  if (baseline) title = comparable ? improvements || regressions ? '同一模型的两轮结果存在差异' : '同一模型的两轮已评分结果一致' : '两轮结果尚不足以判断重复性';
  const explanation = baseline ? `这是同一模型在相同任务和配置下的两轮结果。共有 ${comparable} 道题可成对核对，${unavailable} 道题暂无法比较。` : `在 ${comparable} 道可比较的题目中，候选模型多做对 ${improvements} 道，也少做对 ${regressions} 道；${unavailable} 道题暂无法成对比较。这里描述本次任务表现，不推断通用能力。`;
  const cautions: string[] = [];
  if (unavailable) cautions.push(`${unavailable} 道题缺少可成对比较的评分结果。中断、取消和未执行的任务不能当作模型答错。`);
  if (arms.stable.infra + arms.candidate.infra) cautions.push(`有 ${arms.stable.infra + arms.candidate.infra} 次基础设施失败，不能据此认定模型能力退化。`);
  if (planned < 12) cautions.push(`本次只有 ${planned} 个独立任务，结果只反映这组有限案例；重试不会增加独立样本数。`);
  let performance: ReportInsights['performance'] = { headline: '性能数据不足，暂无法判断快慢', detail: '缺少可核验的成对性能分析；未采集的数据不会补零。', samples: null, planned };
  const a = analysis as (Comparison & { manifest_hash?: string; attempt_set_hash?: string }) | null;
  const validAnalysis = a && (!a.manifest_hash || a.manifest_hash === report.manifest_hash) && (!a.attempt_set_hash || a.attempt_set_hash === report.attempt_set_hash) && a.arms.stable.successes === arms.stable.pass && a.arms.candidate.successes === arms.candidate.pass && a.arms.stable.n === arms.stable.scored && a.arms.candidate.n === arms.candidate.scored;
  if (validAnalysis) {
    const p = a.performance, samples = p.sample_count, ratio = p.paired_median_ratio;
    const required = (p as typeof p & { required_pairs?: number }).required_pairs;
    const validCounts = Number.isInteger(samples) && samples >= 0 && samples <= planned && p.planned_pairs === planned;
    performance = { headline: '可比性能样本不足，暂不判断快慢', detail: `${samples}/${p.planned_pairs} 对正式推理耗时样本可比较。样本需满足同一任务配对、有效计时与执行协议等条件。`, samples: validCounts ? samples : null, planned };
    if (validCounts && samples > 0 && (required === undefined || Number.isInteger(required) && required > 0 && samples >= required) && p.status === 'sufficient' && ratio.unit === 'ratio' && typeof ratio.value === 'number' && Number.isFinite(ratio.value) && ratio.value > 0 && !!ratio.source && ratio.source !== 'unavailable' && ['observed', 'derived', 'reported'].includes(ratio.quality)) {
      const delta = (ratio.value - 1) * 100;
      performance.headline = `${baseline ? '第二轮' : '候选模型'}的正式推理耗时${Math.abs(delta) < .005 ? '基本相近' : `${delta > 0 ? '高' : '低'}约 ${Math.abs(delta).toFixed(1)}%`}`;
      performance.detail = `由 ${samples}/${planned} 对可比样本支持；${baseline ? '第二轮 / 第一轮' : '候选模型 / 原模型'}成对耗时比的中位数为 ${ratio.value.toFixed(3)}。只描述本组样本${baseline ? '的轮次差异' : ''}，不等于整体任务或端到端使用速度。`;
    } else if (!validCounts) performance.detail = '性能样本记录与本报告的可比较任务不一致，暂不据此判断快慢。';
    const names: Record<string, string> = { cpu_percent: 'CPU 占用', ram_bytes: '内存占用', available_vram_bytes: '可用显存', tokens_per_second: '生成速度', decode_ns: '生成耗时' };
    const missing = new Set<string>();
    for (const coverage of Object.values(a.metric_coverage)) for (const [name, metric] of Object.entries(coverage)) if (metric.samples < metric.planned) missing.add(names[name] || '部分资源指标');
    if (missing.size) cautions.push(`${[...missing].join('、')}记录不完整，暂不能据此判断资源优势；具体缺失原因见技术分析。`);
  } else cautions.push('缺少与本报告一致的统计分析；题目通过情况可核对，统计区间与性能结论暂不可用。');
  if (!performance.headline.includes('约') && performance.headline.includes('不足')) cautions.push(performance.headline + '。');
  return { baseline, title, explanation, arms, improvements, regressions, comparable, cases, performance, cautions };
}

export function pairedCaseEvidence(row: ReportCase, data: ModelEngineeringResponses['get_case'] | null, issue = ''): PairedCaseEvidence {
  if (data && data.case.id !== row.id) { data = null; issue = '案例证据与本报告任务不一致，未用于展示。'; }
  const artifacts = new Map(data?.artifacts.map(a => [a.id, a]) || []);
  const evidence = (ref?: SelectedAttempt): ArmEvidence => {
    if (!ref) return { output: '', notice: '本报告未选中此轮尝试。' };
    const a: Attempt | undefined = data?.attempts.find(a => a.id === ref.id && a.arm === ref.arm && a.case_id === row.id && a.attempt_no === ref.attempt_no && a.state === ref.state && a.verdict === ref.verdict);
    const base = { attemptID: ref.id, attemptNo: ref.attempt_no, state: ref.state, verdict: ref.verdict, verdictText: verdictLabel(ref.state, ref.verdict), output: '', notice: '' };
    if (!a) return { ...base, notice: issue || '该报告选中的尝试证据暂不可读取，不使用后续重试替代。' };
    const output = a.output_artifact ? artifacts.get(a.output_artifact) : undefined;
    base.output = output?.content || '';
    base.notice = output?.missing ? '原始输出证据缺失。' : output?.truncated ? '输出显示已截断，不能视为完整代码。' : !output?.content ? '该尝试未保存可读取的原始模型输出。' : '';
    if (a.result.truncated) base.notice += '模型生成达到输出限制，结果不完整。';
    for (const id of a.result.evidence_artifacts || []) {
      const artifact = artifacts.get(id);
      if (!artifact?.content || artifact.missing || artifact.truncated) continue;
      try {
        const score = JSON.parse(artifact.content);
        if (score.case_id === row.id && score.scorer_version && Array.isArray(score.assertions)) return { ...base, score: artifact.content, scoreSummary: `已核对 ${score.assertions.filter((x: { passed?: boolean }) => x.passed === true).length}/${score.assertions.length} 组断言` };
      } catch { /* A non-score artifact remains available in the technical evidence. */ }
    }
    return base;
  };
  let task = '', inputIssue = '';
  if (data) {
    const input = data.case.input_artifact ? artifacts.get(data.case.input_artifact) : undefined;
    if (!input || input.missing || !input.content) inputIssue = '冻结任务输入证据缺失或暂不可读取，任务说明未补造。';
    else if (input.truncated) inputIssue = '冻结任务输入显示已截断，任务说明暂不可核验。';
    else {
      try { const value = JSON.parse(input.content); if (value.schema_version === 1 && value.case?.id === row.id && typeof value.case.task === 'string') task = value.case.task; else inputIssue = '冻结输入中缺少可读取的任务说明。'; }
      catch { inputIssue = '冻结输入的格式无法读取，任务说明保持未知。'; }
    }
  }
  return { id: row.id, category: data?.case.category || row.category, change: row.change, task, expected: data?.case.expected || row.expected, stable: evidence(row.stable), candidate: evidence(row.candidate), issue: issue || inputIssue || undefined };
}
