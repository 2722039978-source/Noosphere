import type { DatasetDocument, DatasetRevision, DatasetTask, GoTaskType } from '@/lib/model-engineering-types';

export const GO_TYPES: GoTaskType[] = ['int', 'int64', 'float64', 'string', 'bool', '[]int', '[]int64', '[]float64', '[]string', '[]bool'];
export const DATASET_LIMIT = 64 * 1024;
export interface DatasetIssue { path: string; message: string }
export interface DraftAssertion { input: string; expected: string }
export interface DraftCase extends Omit<DatasetTask, 'assertions'> { assertions: DraftAssertion[] }
export interface DatasetDraft extends Omit<DatasetDocument, 'cases'> { cases: DraftCase[] }
export interface DatasetValidation { document?: DatasetDocument; issues: DatasetIssue[] }
export function blankCase(index = 1): DraftCase {
  return { id: `case-${index}`, category: '', task: '', function: 'Solve', input_type: 'int', output_type: 'int', faulty_source: '', assertions: [{ input: '', expected: '' }] };
}
export function blankDataset(): DatasetDraft {
  return { schema_version: 1, benchmark_id: '', version: '', title: '', scoring_rule: 'exact', cases: [blankCase()] };
}
export function exampleDataset(): DatasetDocument {
  return { schema_version: 1, benchmark_id: 'example-go', version: '1.0.0', title: '模板示例：整数加一', description: '明确标记的教学模板，请替换为自己的任务后保存。', scoring_rule: 'exact', cases: [{ id: 'add-one', category: 'logic', task: '修复 Solve，返回输入整数加一。', function: 'Solve', input_type: 'int', output_type: 'int', faulty_source: 'package main\n\nfunc Solve(input int) int {\n\treturn input\n}', assertions: [{ input: 0, expected: 1 }, { input: -2, expected: -1 }] }] };
}
export function datasetToDraft(document: DatasetDocument): DatasetDraft {
  return { ...document, cases: document.cases.map(task => ({ ...task, assertions: task.assertions.map(a => ({ input: JSON.stringify(a.input), expected: JSON.stringify(a.expected) })) })) };
}
export function datasetUsesUnsafeIntegers(document: DatasetDocument): boolean {
  const unsafe = (value: unknown, type: string): boolean => type.startsWith('[]') ? Array.isArray(value) && value.some(item => unsafe(item, type.slice(2))) : (type === 'int' || type === 'int64') && typeof value === 'number' && !Number.isSafeInteger(value);
  return document.cases.some(task => task.assertions.some(a => unsafe(a.input, task.input_type) || unsafe(a.expected, task.output_type)));
}
function record(value: unknown): value is Record<string, unknown> { return value !== null && typeof value === 'object' && !Array.isArray(value); }
function typedValue(value: unknown, type: string, preserveIntegers = false): string | undefined {
  if (type.startsWith('[]')) {
    if (!Array.isArray(value)) return '需要 JSON 数组；空数组请写 []，不接受 null';
    const issue = value.map(v => typedValue(v, type.slice(2), preserveIntegers)).find(Boolean);
    return issue ? `数组元素：${issue}` : undefined;
  }
  if (type === 'bool') return typeof value === 'boolean' ? undefined : '需要 JSON 布尔值 true 或 false';
  if (type === 'string') return typeof value === 'string' ? undefined : '需要 JSON 字符串（包含双引号）';
  if (type === 'float64') return typeof value === 'number' && Number.isFinite(value) ? undefined : '需要有限 JSON 数字';
  if (type === 'int' || type === 'int64') return typeof value === 'number' && Number.isFinite(value) && (preserveIntegers ? Number.isInteger(value) : Number.isSafeInteger(value)) ? undefined : '需要 JavaScript 可精确表示的安全整数（此表单不接受超出 ±9007199254740991 的整数）';
  return '不支持此 Go 类型';
}
export function validateDatasetDocument(value: unknown, workspaceID = 'local', preserveIntegers = false): DatasetIssue[] {
  const issues: DatasetIssue[] = [], issue = (path: string, message: string) => issues.push({ path, message });
  const fields = (obj: Record<string, unknown>, keys: string[], path = '') => Object.keys(obj).filter(key => !keys.includes(key)).forEach(key => issue(path ? `${path}.${key}` : key, '不支持的字段；禁止自定义执行器、参考代码或评分脚本'));
  const bytes = (text: string) => new TextEncoder().encode(text).length;
  const required = (obj: Record<string, unknown>, key: string, path = key, max = 8192) => { if (typeof obj[key] !== 'string' || !(obj[key] as string).trim()) issue(path, '不能为空'); else if (bytes(obj[key] as string) > max) issue(path, `最多 ${max} 个 UTF-8 字节`); };
  if (!record(value)) return [{ path: '$', message: '任务集必须是 JSON 对象' }];
  fields(value, ['schema_version', 'benchmark_id', 'version', 'title', 'description', 'source', 'license', 'scoring_rule', 'cases']);
  if (value.schema_version !== 1) issue('schema_version', '只支持任务格式版本 1');
  required(value, 'benchmark_id', 'benchmark_id', 64); required(value, 'version', 'version', 64); required(value, 'title', 'title', 200);
  if (typeof value.benchmark_id === 'string' && !/^[a-z][a-z0-9-]{1,63}$/.test(value.benchmark_id)) issue('benchmark_id', '使用 2–64 位小写字母、数字或连字符，以字母开头');
  if (value.benchmark_id === 'codetask-go-v1') issue('benchmark_id', '此标识由内置任务集保留；复制时请填写自己的任务集标识');
  if (typeof value.version === 'string' && !/^[A-Za-z0-9][A-Za-z0-9_.:-]{0,63}$/.test(value.version)) issue('version', '使用字母、数字、下划线、点、冒号或连字符，以字母或数字开头');
  for (const [key, max] of [['description', 4000], ['source', 1000], ['license', 128]] as const) if (value[key] !== undefined) { if (typeof value[key] !== 'string') issue(key, '需要字符串'); else if (bytes(value[key] as string) > max) issue(key, `最多 ${max} 个 UTF-8 字节`); }
  if (value.scoring_rule !== 'exact') issue('scoring_rule', '当前仅支持固定的精确匹配规则 exact');
  if (!Array.isArray(value.cases) || !value.cases.length || value.cases.length > 50) issue('cases', '每个任务集需要 1–50 个独立案例');
  const ids = new Set<string>();
  if (Array.isArray(value.cases)) value.cases.forEach((task, index) => {
    const p = `cases[${index}]`;
    if (!record(task)) { issue(p, '案例必须是对象'); return; }
    fields(task, ['id', 'category', 'task', 'function', 'input_type', 'output_type', 'faulty_source', 'assertions'], p);
    for (const key of ['id', 'category', 'task', 'function', 'faulty_source']) required(task, key, `${p}.${key}`, key === 'category' ? 80 : key === 'task' ? 8000 : key === 'faulty_source' ? DATASET_LIMIT : 8192);
    if (typeof task.id === 'string') { if (!/^[a-z][a-z0-9-]{1,63}$/.test(task.id)) issue(`${p}.id`, '使用 2–64 位小写字母、数字或连字符，以字母开头'); if (ids.has(task.id)) issue(`${p}.id`, '案例 ID 重复'); ids.add(task.id); }
    if (typeof task.function === 'string' && !/^[A-Za-z_][A-Za-z0-9_]*$/.test(task.function)) issue(`${p}.function`, '需要有效的 Go 函数名');
    for (const key of ['input_type', 'output_type']) if (!GO_TYPES.includes(task[key] as GoTaskType)) issue(`${p}.${key}`, '只支持指定的标量和一维切片类型');
    if (!Array.isArray(task.assertions) || !task.assertions.length || task.assertions.length > 100) issue(`${p}.assertions`, '每个案例需要 1–100 组输入与预期结果');
    if (Array.isArray(task.assertions)) task.assertions.forEach((assertion, ai) => {
      const ap = `${p}.assertions[${ai}]`;
      if (!record(assertion)) { issue(ap, '断言必须是对象'); return; }
      fields(assertion, ['input', 'expected'], ap);
      for (const [key, type] of [['input', task.input_type], ['expected', task.output_type]]) {
        const problem = typedValue(assertion[key as string], String(type), preserveIntegers); if (problem) issue(`${ap}.${key}`, problem);
      }
    });
  });
  const requestBytes = bytes(JSON.stringify({ operation: 'create_dataset', workspace_id: workspaceID, document: value }));
  if (requestBytes > DATASET_LIMIT) issue('$', `请求为 ${requestBytes.toLocaleString()} 字节，超过 64 KiB 总量限制；请缩小或拆分任务集`);
  return issues;
}
export function parseDatasetImport(text: string, workspaceID = 'local', preserveIntegers = false): DatasetValidation {
  let value: unknown;
  try { value = JSON.parse(text); } catch (e) { return { issues: [{ path: '$', message: `JSON 语法错误：${e instanceof Error ? e.message : String(e)}` }] }; }
  const issues = validateDatasetDocument(value, workspaceID, preserveIntegers);
  if (preserveIntegers) {
    const bytes = new TextEncoder().encode(JSON.stringify({ operation: 'create_dataset', workspace_id: workspaceID, document_json: text })).length;
    if (bytes > DATASET_LIMIT && !issues.some(issue => issue.path === '$')) issues.push({ path: '$', message: `请求为 ${bytes.toLocaleString()} 字节，超过 64 KiB 总量限制；请缩小或拆分任务集` });
  }
  return { issues, document: issues.length ? undefined : value as DatasetDocument };
}
export function draftToDocument(draft: DatasetDraft, workspaceID = 'local'): DatasetValidation {
  const syntax: DatasetIssue[] = [];
  const cases = draft.cases.map((task, ci) => ({ ...task, assertions: task.assertions.map((a, ai) => {
    const parsed: { input: unknown; expected: unknown } = { input: undefined, expected: undefined };
    for (const key of ['input', 'expected'] as const) { try { parsed[key] = JSON.parse(a[key]); } catch { syntax.push({ path: `cases[${ci}].assertions[${ai}].${key}`, message: '请输入有效 JSON；字符串须带双引号' }); } }
    return parsed;
  }) }));
  const document = { ...draft, cases };
  const issues = [...syntax, ...validateDatasetDocument(document, workspaceID)];
  return { issues, document: issues.length ? undefined : document as DatasetDocument };
}
export function datasetWorkflow(dataset?: Pick<DatasetRevision, 'workflow_id'>): string { return dataset?.workflow_id || 'codelens.codetask.go.v1'; }
