import type { Counts, ModelRevision } from '@/lib/model-engineering-types';

export type EvaluationMode = 'baseline' | 'compare';

/** A link from a registered version is an explicit selection, never a silent fallback. */
export function initialEvaluationSelection(models: Pick<ModelRevision, 'id' | 'gateway_model_id'>[], preferredID: string) {
  const first = (preferredID ? models.find(model => model.id === preferredID) : models[0])?.id || '';
  return { stable: first, candidate: first, mode: 'baseline' as const };
}

export function startBlocker({ setupError, profileSaved, preflightReady }: { setupError: string; profileSaved: boolean; preflightReady: boolean }) {
  if (setupError) return setupError;
  if (!preflightReady) return '请先修复运行环境，再点击“重新检查环境”。';
  if (!profileSaved) return '请先保存当前推理配置，再开始测评。';
  return '';
}

export function reportReading(counts: Counts, status: string, baseline: boolean) {
  if (!counts.scored) return { title: '尚未形成质量结论', explanation: '先查看任务状态和基础设施失败原因；没有完成评分的输出不能算作模型通过或失败。' };
  const title = baseline ? '单模型基线已记录' : '成对测评结果已记录';
  if (counts.infra || counts.interrupted || counts.unknown || status !== 'completed') return { title, explanation: '这份结果包含未完成项或环境问题。先查看基础设施失败和未完成案例，再阅读已评分任务的质量；不能将环境失败当作模型质量失败。' };
  return { title, explanation: baseline ? '同一版本在相同任务集上执行两轮，用于了解任务表现与重复性；不代表相较其他模型胜出。请结合失败案例判断是否适合你的工作。' : '先看各模型通过数，再查看候选退化的案例与性能覆盖。有限任务集的结果不能直接代表全部使用场景。' };
}

export function preflightHelp(code?: string) {
  if (code?.includes('GO') || code?.includes('TOOLCHAIN')) return '请确认本机 Go 工具链可用；如果刚安装工具链，请重启 Noosphere 后重新检查。';
  if (code?.includes('CODELENS')) return '请在 CodeLens 页面检查本地分析服务，恢复后重新检查环境。';
  if (code?.includes('DISABLED')) return '请在本机配置中启用 Model Lab，再重启 Noosphere。';
  return '请检查后端、CodeLens 与本机 Go 工具链；修复后可在这里重新检查，不必丢弃当前参数。';
}
