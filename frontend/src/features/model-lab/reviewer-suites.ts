import type { DatasetRevision, Options } from '@/lib/model-engineering-types';

export function isTextSuite(dataset?: Pick<DatasetRevision, 'benchmark_id' | 'workflow_id'>) {
  return ['local.text.v1', 'local.review.v1', 'local.tools.v1'].includes(dataset?.workflow_id || '') || dataset?.benchmark_id === 'local-text-v1' || /^reviewer-(core|context-4k|context-8k|tools)-v1$/.test(dataset?.benchmark_id || '');
}

export function suiteDescription(dataset?: DatasetRevision): string {
  if (!dataset) return '先选择任务集，查看它能回答哪些问题。';
  if (dataset.benchmark_id === 'reviewer-core-v1') return '14 道原创任务：中英成对、约束对话、固定小仓库问答、短上下文检索、良性授权边界与 JSON 函数选择。按公开固定答案评分；函数选择是代理题，原生工具协议另测。';
  if (/^reviewer-context-(4k|8k)-v1$/.test(dataset.benchmark_id)) return '固定记录的首部、中部与尾部检索，各 1 题。上下文档位表示最低配置容量，实际分词数以引擎证据为准；检索成功不代表完整长文本推理能力。';
  if (dataset.benchmark_id === 'reviewer-tools-v1') return '真实原生工具协议：固定库存查询与长度换算，核对函数名、参数和工具反馈后的最终答案。工具无副作用；引擎不支持协议时明确未测。';
  if (isTextSuite(dataset)) return '中文指令遵循 + 结构化 JSON：按冻结规则核对语言、格式与字段值。只读取和评分文本，不执行模型输出；本组小样本不代表通用能力。';
  if (dataset.kind === 'workspace_custom') return '你定义的 Go 函数修复任务，按固定断言评分；需要本机 Go / WASI 执行器与 CodeLens。';
  return 'Go 函数修复：在受限执行器中编译并核对固定断言。需要本机 Go / WASI 与 CodeLens；结果只适用于本组修复任务。';
}

export function suiteBudgetIssue(dataset: DatasetRevision | undefined, options: Pick<Options, 'num_ctx' | 'max_output'>): string {
  const suite = dataset?.suite;
  if (suite?.minimum_context && options.num_ctx < suite.minimum_context) return `此套件最低需要 ${suite.minimum_context} 上下文；请先调整并保存推理配置。`;
  if (suite?.max_output && options.max_output > suite.max_output) return `此套件输出上限 ${suite.max_output}，建议 ${suite.recommended_max_output || suite.max_output}；请先调整并保存推理配置。`;
  return '';
}

export function suitePreflightReady(dataset: DatasetRevision | undefined, environmentAvailable: boolean, goReady: boolean): boolean {
  // Text readiness is decided by the workflow-specific prepare_evaluation check.
  return environmentAvailable && (isTextSuite(dataset) || goReady);
}
