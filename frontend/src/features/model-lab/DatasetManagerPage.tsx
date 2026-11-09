import { useEffect, useRef, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { modelEngineering } from '@/lib/model-engineering-client';
import type { DatasetDocument, DatasetRevision, ModelEngineeringRequest, ModelEngineeringResponses } from '@/lib/model-engineering-types';
import { useWorkspaceStore } from '@/stores/workspace-store';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Surface } from '@/components/ui/surface';
import { EmptyState, LoadingState } from '@/components/ui/feedback';
import { JsonView, LabError, LabShell } from './shared';
import { GO_TYPES, blankCase, blankDataset, datasetToDraft, datasetUsesUnsafeIntegers, draftToDocument, exampleDataset, parseDatasetImport, type DatasetDraft, type DatasetIssue, type DraftCase } from './custom-datasets';
import './datasets.css';

type Detail = ModelEngineeringResponses['get_dataset'];
function Issues({ issues }: { issues: DatasetIssue[] }) {
  return issues.length ? <div className="ns-dataset-errors" role="alert"><strong>请检查 {issues.length} 处问题</strong><ul>{issues.slice(0, 30).map((issue, i) => <li key={`${issue.path}-${i}`}><code>{issue.path}</code> · {issue.message}</li>)}</ul>{issues.length > 30 && <p>另有 {issues.length - 30} 处问题；先修正以上内容再校验。</p>}</div> : null;
}
function download(document: DatasetDocument, originalJSON?: string) {
  const url = URL.createObjectURL(new Blob([originalJSON || JSON.stringify(document, null, 2) + '\n'], { type: 'application/json' }));
  const link = window.document.createElement('a'); link.href = url; link.download = `${document.benchmark_id}-${document.version}.json`; link.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

export function DatasetManagerPage() {
  const workspace = useWorkspaceStore(s => s.activeId), [query, setQuery] = useSearchParams();
  const selectedID = query.get('dataset') || '';
  const [datasets, setDatasets] = useState<DatasetRevision[]>([]), [detail, setDetail] = useState<Detail | null>(null);
  const [loading, setLoading] = useState(true), [reading, setReading] = useState(false), [busy, setBusy] = useState(false), [refresh, setRefresh] = useState(0);
  const [error, setError] = useState<unknown>(), [notice, setNotice] = useState(''), [failedIntent, setFailedIntent] = useState<ModelEngineeringRequest | null>(null);
  const [draft, setDraft] = useState<DatasetDraft | null>(null), [copiedFrom, setCopiedFrom] = useState('');
  const [importOpen, setImportOpen] = useState(false), [importText, setImportText] = useState(''), [importChecked, setImportChecked] = useState(false);
  const [issues, setIssues] = useState<DatasetIssue[]>([]), [preview, setPreview] = useState<DatasetDocument | null>(null);
  const [fileError, setFileError] = useState('');
  const epoch = useRef(0), mutation = useRef(false), fileEpoch = useRef(0);
  const currentWorkspace = useRef(workspace); currentWorkspace.current = workspace;
  const groupMap = new Map<string, DatasetRevision[]>();
  for (const dataset of datasets) { const key = `${dataset.kind || 'builtin'}:${dataset.benchmark_id}`; groupMap.set(key, [...groupMap.get(key) || [], dataset]); }
  const ownCount = datasets.filter(dataset => dataset.kind === 'workspace_custom').length;
  const formValidation = draft ? draftToDocument(draft, workspace) : null;
  const detailFormSafe = detail ? !datasetUsesUnsafeIntegers(detail.document) : false;
  useEffect(() => {
    epoch.current++; fileEpoch.current++; mutation.current = false; setBusy(false); setDraft(null); setCopiedFrom(''); setPreview(null); setIssues([]); setImportText(''); setImportChecked(false); setImportOpen(false); setNotice(''); setError(undefined); setFailedIntent(null);
    return () => { epoch.current++; fileEpoch.current++; };
  }, [workspace]);
  useEffect(() => {
    let disposed = false; setLoading(true); setDatasets([]); setError(undefined);
    modelEngineering({ operation: 'list_datasets', workspace_id: workspace, include_archived: true }).then(r => { if (!disposed) setDatasets(r.datasets); }).catch(e => { if (!disposed) setError(e); }).finally(() => { if (!disposed) setLoading(false); });
    return () => { disposed = true; };
  }, [workspace, refresh]);
  useEffect(() => {
    let disposed = false; setDetail(null); setReading(!!selectedID);
    if (selectedID) modelEngineering({ operation: 'get_dataset', workspace_id: workspace, id: selectedID }).then(r => { if (!disposed) setDetail(r); }).catch(e => { if (!disposed) setError(e); }).finally(() => { if (!disposed) setReading(false); });
    return () => { disposed = true; };
  }, [workspace, selectedID, refresh]);
  const edit = (document?: DatasetDocument) => {
    setDraft(document ? datasetToDraft({ ...document, benchmark_id: document.benchmark_id === 'codetask-go-v1' ? '' : document.benchmark_id, version: '' }) : blankDataset()); setCopiedFrom(document ? `${document.benchmark_id} / ${document.version}` : ''); setIssues([]); setNotice(''); setPreview(null); setError(undefined); setFailedIntent(null); setImportOpen(false);
  };
  const changeCase = (index: number, change: Partial<DraftCase>) => { setDraft(old => old ? { ...old, cases: old.cases.map((task, i) => i === index ? { ...task, ...change } : task) } : old); setIssues([]); };
  const submit = async (request: ModelEngineeringRequest) => {
    if (mutation.current) return;
    mutation.current = true; const generation = epoch.current, owner = workspace;
    setBusy(true); setError(undefined); setFailedIntent(null); setNotice('');
    try {
      const response = await modelEngineering(request);
      if (generation !== epoch.current || owner !== currentWorkspace.current) return;
      if ('revision' in response && typeof response.revision === 'object') {
        const revision = response.revision as DatasetRevision;
        setDatasets(old => [revision, ...old.filter(dataset => dataset.id !== revision.id)]);
        setQuery({ dataset: revision.id });
        if (request.operation === 'create_dataset') {
          setDetail(response as Detail); setDraft(null); setPreview(null); setImportOpen(false); setIssues([]); setNotice(`已保存不可变版本 ${revision.version}。输入、预期结果与评分规则将随测评冻结。`);
        } else {
          setDetail(old => old?.revision.id === revision.id ? { ...old, revision } : old);
          setNotice(revision.archived ? '此版本已归档；历史测评和报告仍然保留。' : '此版本已恢复，可以再次选择测评。');
        }
      }
    } catch (e) { if (generation === epoch.current && owner === currentWorkspace.current) { setError(e); setFailedIntent(request); } }
    finally { if (generation === epoch.current && owner === currentWorkspace.current) { mutation.current = false; setBusy(false); } }
  };
  const importResult = importChecked ? parseDatasetImport(importText, workspace, true) : null;
  const formImport = importChecked ? parseDatasetImport(importText, workspace) : null;
  const startSave = () => {
    if (!draft) return;
    const result = draftToDocument(draft, workspace); setIssues(result.issues);
    if (result.document) { setPreview(result.document); setNotice('本地格式校验通过。请核对下面的版本预览，再保存；服务端将校验 Go 源码与签名。'); }
  };
  return <LabShell title="Dataset Archive" subtitle="你自己的 Go / WASI 测评任务 · 创建、导入与管理不可变版本">
    <div className="ns-dataset-intro"><div><p className="ns-dataset-eyebrow">TASK COLLECTIONS / 任务档案</p><h2>让每次测评，使用同一份标准。</h2><p>保存任务描述、Go 初始代码、JSON 输入与预期结果。编辑已有版本会创建一个新版本，历史报告保留当时的任务与评分证据。</p></div><Link to="/model-lab">返回 Model Lab →</Link></div>
    <LabError error={error} onRetry={failedIntent ? () => void submit(failedIntent) : undefined} />
    {notice && <p className="ns-lab-notice" role="status">{notice}</p>}
    <div className="ns-dataset-layout"><Surface className="ns-lab-panel ns-dataset-catalog"><div className="ns-lab-section"><h2>任务集档案</h2><span className="ns-dataset-count">{ownCount} 个自定义版本</span></div>
      <div className="ns-lab-actions"><Button disabled={busy} onClick={() => edit()}>新建任务集</Button><Button disabled={busy} variant="outline" onClick={() => { setImportOpen(true); setImportChecked(false); setNotice(''); setPreview(null); }}>导入 JSON</Button><Button disabled={busy} size="sm" variant="ghost" onClick={() => setRefresh(v => v + 1)}>刷新档案</Button></div>
      {loading ? <LoadingState label="正在读取任务集档案…" /> : <>{ownCount === 0 && <p className="ns-lab-muted">你还没有保存自定义任务。新建空白任务集，或导入已有 JSON 文件。</p>}{[...groupMap.values()].map(versions => <section className="ns-dataset-family" key={`${versions[0].kind}:${versions[0].benchmark_id}`}><h3>{versions[0].title || versions[0].benchmark_id}</h3><p>{versions[0].kind === 'workspace_custom' ? '工作区自定义' : '内置任务集'} · {versions[0].benchmark_id}</p><ol>{versions.map(revision => <li key={revision.id}><button disabled={busy} aria-pressed={selectedID === revision.id} onClick={() => { setQuery({ dataset: revision.id }); setDraft(null); setPreview(null); setIssues([]); setImportOpen(false); setError(undefined); setNotice(''); }}><span><strong>{revision.version}</strong>{revision.archived && <em>已归档</em>}</span><small>{revision.cases.length} 个案例 · {revision.created_at ? new Date(revision.created_at).toLocaleDateString() : revision.kind === 'workspace_custom' ? '创建时间未知' : '内置版本'}</small></button></li>)}</ol></section>)}</>}
      <p className="ns-dataset-scope">当前支持受限 Go 函数修复、WASI 执行与固定精确评分。每集 1–50 个案例，每例 1–100 组断言；请求总量 ≤64 KiB。</p>
    </Surface><div className="ns-dataset-main">
      {importOpen && <Surface className="ns-lab-panel"><div className="ns-lab-section"><h2>导入任务集 JSON</h2><Button variant="ghost" size="sm" onClick={() => setImportOpen(false)}>收起导入</Button></div><p className="ns-lab-muted">粘贴任务规范或选择本地 .json 文件。预览通过后可载入表单继续编辑，再明确保存；导入不会直接启动任务。</p>
        <label className="ns-lab-field">选择 JSON 文件<input type="file" accept=".json,application/json" disabled={busy} onChange={async event => { const file = event.target.files?.[0], generation = ++fileEpoch.current, owner = workspace; if (!file) return; setFileError(''); if (file.size > 64 * 1024) { setFileError('文件超过 64 KiB，请缩小或拆分任务集。'); return; } try { const text = await file.text(); if (generation === fileEpoch.current && currentWorkspace.current === owner) { setImportText(text); setImportChecked(true); } } catch { if (generation === fileEpoch.current) setFileError('文件读取失败，请重新选择或粘贴 JSON。'); } }} /></label>
        {fileError && <p role="alert" className="ns-dataset-errors">{fileError}</p>}
        <label className="ns-lab-field ns-dataset-code">JSON 内容<textarea aria-label="JSON 内容" rows={12} value={importText} onChange={e => { setImportText(e.target.value); setImportChecked(false); }} spellCheck={false} /></label>
        <div className="ns-lab-actions"><Button variant="outline" disabled={busy || !importText.trim()} onClick={() => setImportChecked(true)}>校验并预览导入</Button><Button variant="ghost" disabled={busy} onClick={() => { setImportText(JSON.stringify(exampleDataset(), null, 2)); setImportChecked(true); }}>载入明确标记的模板示例</Button></div>
        {importResult && <><Issues issues={importResult.issues} />{importResult.document && <div className="ns-dataset-import-preview"><p role="status">格式校验通过 · {importResult.document.title} · 版本 {importResult.document.version} · {importResult.document.cases.length} 个案例</p><p className="ns-lab-muted">保存将原样提交上方 JSON 文本，保留 int64 数字精度；服务端再次检查整数边界、Go 源码与签名。确认版本号后保存。</p>{!formImport?.document && <p className="ns-lab-notice">此 JSON 包含超出浏览器安全整数范围的断言，请直接编辑上方 JSON 并保存；结构化表单已禁用，避免数字被舍入。</p>}<details className="ns-lab-details"><summary>核对原始待导入文本</summary><pre>{importText}</pre></details><div className="ns-lab-actions"><Button disabled={busy} onClick={() => void submit({ operation: 'create_dataset', workspace_id: workspace, document_json: importText })}>{busy ? '正在保存…' : '保存导入的不可变版本'}</Button><Button variant="outline" disabled={busy || !formImport?.document} onClick={() => { if (formImport?.document) { setDraft(datasetToDraft(formImport.document)); setCopiedFrom('JSON 导入'); setPreview(null); setIssues([]); setImportOpen(false); setNotice('已载入表单，尚未保存。核对名称、版本与案例后预览保存。'); } }}>载入表单继续编辑</Button></div></div>}</>}
      </Surface>}
      {draft ? <Surface className="ns-lab-panel"><div className="ns-lab-section"><h2>{copiedFrom ? '创建新版本' : '新建 Go 任务集'}</h2><Button disabled={busy} size="sm" variant="ghost" onClick={() => { setDraft(null); setPreview(null); setIssues([]); setNotice(''); }}>放弃草稿</Button></div>{copiedFrom && <p className="ns-lab-notice">来源：{copiedFrom}。请填写新的版本号；保存不会覆盖来源版本。</p>}
        <fieldset disabled={busy} className="ns-dataset-fields" onChange={() => setPreview(null)}><div className="ns-lab-grid"><label className="ns-lab-field">任务集名称<Input value={draft.title} maxLength={200} onChange={e => setDraft({ ...draft, title: e.target.value })} placeholder="如：项目 API 边界回归" /></label><label className="ns-lab-field">任务集标识<Input value={draft.benchmark_id} maxLength={64} onChange={e => setDraft({ ...draft, benchmark_id: e.target.value })} placeholder="如：project-api-go" /></label><label className="ns-lab-field">版本号<Input value={draft.version} maxLength={64} onChange={e => setDraft({ ...draft, version: e.target.value })} placeholder="如：1.0.0" /></label><label className="ns-lab-field">版本说明<Input value={draft.description || ''} onChange={e => setDraft({ ...draft, description: e.target.value })} placeholder="本版本覆盖什么，或为什么修改" /></label></div>
          <details className="ns-lab-details"><summary>来源与许可（可选）</summary><div className="ns-lab-grid"><label className="ns-lab-field">来源<Input value={draft.source || ''} onChange={e => setDraft({ ...draft, source: e.target.value })} /></label><label className="ns-lab-field">许可<Input value={draft.license || ''} onChange={e => setDraft({ ...draft, license: e.target.value })} /></label></div></details>
          <p className="ns-dataset-rule"><strong>评分规则 · 精确匹配</strong><span>输出按声明的 Go 类型比较 JSON 预期值。该例全部断言通过才计为通过；编译、执行与资源失败独立记录。规则随版本保存。</span></p>
          <div className="ns-lab-section"><h3>案例 · {draft.cases.length} / 50</h3><Button type="button" size="sm" variant="outline" disabled={draft.cases.length >= 50} onClick={() => { let i = draft.cases.length + 1; while (draft.cases.some(task => task.id === `case-${i}`)) i++; setDraft({ ...draft, cases: [...draft.cases, blankCase(i)] }); setPreview(null); }}>添加案例</Button></div>
          {draft.cases.map((task, index) => <section className="ns-dataset-case" key={index} aria-label={`编辑案例 ${index + 1}`}><div className="ns-lab-section"><h3>案例 {String(index + 1).padStart(2, '0')}</h3><Button size="sm" variant="ghost" disabled={draft.cases.length <= 1} onClick={() => { setDraft({ ...draft, cases: draft.cases.filter((_, i) => i !== index) }); setPreview(null); }}>移除案例 {index + 1}</Button></div>
            <div className="ns-lab-grid"><label className="ns-lab-field">案例 ID<Input value={task.id} onChange={e => changeCase(index, { id: e.target.value })} /></label><label className="ns-lab-field">分类<Input value={task.category} onChange={e => changeCase(index, { category: e.target.value })} placeholder="如：boundary" /></label></div>
            <label className="ns-lab-field">任务描述<textarea aria-label="任务描述" rows={3} value={task.task} onChange={e => changeCase(index, { task: e.target.value })} placeholder="说明函数应完成的行为与边界条件" /></label>
            <div className="ns-lab-grid ns-dataset-signature"><label className="ns-lab-field">函数名<Input value={task.function} onChange={e => changeCase(index, { function: e.target.value })} /></label><label className="ns-lab-field">输入类型<select value={task.input_type} onChange={e => changeCase(index, { input_type: e.target.value as DraftCase['input_type'] })}>{GO_TYPES.map(type => <option key={type}>{type}</option>)}</select></label><label className="ns-lab-field">输出类型<select value={task.output_type} onChange={e => changeCase(index, { output_type: e.target.value as DraftCase['output_type'] })}>{GO_TYPES.map(type => <option key={type}>{type}</option>)}</select></label></div>
            <p className="ns-lab-hash">func {task.function || '…'}(input {task.input_type}) {task.output_type}</p><label className="ns-lab-field ns-dataset-code">初始 Go 代码<textarea aria-label="初始 Go 代码" rows={7} spellCheck={false} value={task.faulty_source} onChange={e => changeCase(index, { faulty_source: e.target.value })} placeholder={'package main\n\nfunc Solve(input int) int {\n    // 待修复的函数体\n}'} /></label><p className="ns-lab-muted">单入参、单返回值；仅 bool / string / int / int64 / float64 与一维切片。仅 package main 与指定函数，禁止 import、main、init、额外声明和间接函数调用；不支持 []byte、[]rune、自定义评分器或宿主脚本。保存时由服务端检查源码；模型仅能修改函数体。</p>
            <h4>JSON 输入与预期结果 · {task.assertions.length} / 100</h4>{task.assertions.map((assertion, ai) => <div className="ns-dataset-assertion" key={ai}><span>{ai + 1}</span><label className="ns-lab-field">输入 JSON<textarea aria-label={`案例 ${index + 1} 断言 ${ai + 1} 输入 JSON`} rows={2} spellCheck={false} value={assertion.input} onChange={e => changeCase(index, { assertions: task.assertions.map((a, i) => i === ai ? { ...a, input: e.target.value } : a) })} placeholder="有效 JSON" /></label><label className="ns-lab-field">预期 JSON<textarea aria-label={`案例 ${index + 1} 断言 ${ai + 1} 预期 JSON`} rows={2} spellCheck={false} value={assertion.expected} onChange={e => changeCase(index, { assertions: task.assertions.map((a, i) => i === ai ? { ...a, expected: e.target.value } : a) })} placeholder="有效 JSON" /></label><Button size="sm" variant="ghost" disabled={task.assertions.length <= 1} aria-label={`移除案例 ${index + 1} 断言 ${ai + 1}`} onClick={() => changeCase(index, { assertions: task.assertions.filter((_, i) => i !== ai) })}>移除</Button></div>)}<Button size="sm" variant="outline" disabled={task.assertions.length >= 100} onClick={() => changeCase(index, { assertions: [...task.assertions, { input: '', expected: '' }] })}>添加输入与预期</Button>
          </section>)}
          <p className="ns-lab-muted">字符串须带双引号；切片使用 JSON 数组。此表单接受 ±9007199254740991 范围内的整数，避免浏览器舍入。任务描述与初始源码会提供给模型，预期结果用于执行后评分。</p>
          <Issues issues={issues} /><div className="ns-lab-actions"><Button onClick={startSave}>校验并预览版本</Button><Button variant="outline" disabled={!formValidation?.document} onClick={() => { if (formValidation?.document) download(formValidation.document); }}>导出草稿 JSON</Button></div>
        </fieldset>
        {preview && <div className="ns-dataset-save-preview"><h3>保存前核对</h3><p>{preview.title} · {preview.benchmark_id} / {preview.version} · {preview.cases.length} 个独立案例 · 精确匹配</p><JsonView value={preview} title="查看将要保存的完整版本" /><Button disabled={busy} onClick={() => void submit({ operation: 'create_dataset', workspace_id: workspace, document: preview })}>{busy ? '正在保存…' : '保存不可变版本'}</Button></div>}
      </Surface> : reading ? <Surface className="ns-lab-panel"><LoadingState label="正在读取冻结任务版本…" /></Surface> : detail ? <Surface className="ns-lab-panel"><div className="ns-lab-section"><h2>{detail.revision.title || detail.document.title}</h2><span className="ns-dataset-count">{detail.revision.kind === 'workspace_custom' ? '工作区自定义' : '内置'} · {detail.revision.archived ? '已归档' : '可用于测评'}</span></div><p>{detail.document.description || '此版本尚无说明。'}</p><div className="ns-dataset-meta"><div><small>版本</small><strong>{detail.revision.version}</strong></div><div><small>独立案例</small><strong>{detail.revision.cases.length}</strong></div><div><small>固定评分</small><strong>精确匹配</strong></div></div><p className="ns-lab-hash">任务集 {detail.revision.benchmark_id}<br />Revision {detail.revision.id}<br />Content SHA-256 {detail.revision.content_hash}<br />Scorer {detail.revision.scorer_version}</p>
        <div className="ns-lab-actions">{!detail.revision.archived && <Button asChild><Link to={`/model-lab?dataset=${encodeURIComponent(detail.revision.id)}`}>用此版本测评</Link></Button>}<Button disabled={busy} variant="outline" onClick={() => { const raw = detail.document_json || JSON.stringify(detail.document, null, 2); if (detailFormSafe) edit(detail.document); else { setDraft(null); setImportText(raw); setImportOpen(true); setImportChecked(true); setFileError(''); setNotice('此版本含需要保留原始精度的数字，已复制到完整 JSON 编辑器。请修改 version 后保存新版本；原版本不会被覆盖。'); } }}>复制为新版本</Button><Button disabled={busy} variant="outline" onClick={() => download(detail.document, detail.document_json)}>导出 JSON</Button>{detail.revision.kind === 'workspace_custom' && <Button disabled={busy} variant="ghost" onClick={() => void submit({ operation: 'archive_dataset', workspace_id: workspace, id: detail.revision.id, archived: !detail.revision.archived })}>{detail.revision.archived ? '恢复此版本' : '归档此版本'}</Button>}</div><p className="ns-lab-muted">版本内容不可覆盖。归档只会从新测评的可选列表中隐藏，不删除历史报告；恢复后可重新使用。</p>
        {detail.document.cases.map(task => <details key={task.id} className="ns-lab-details"><summary>{task.id} · {task.category} · {task.assertions.length} 组断言</summary><p>{task.task}</p><pre>func {task.function}(input {task.input_type}) {task.output_type}{'\n\n'}{task.faulty_source}</pre>{detailFormSafe ? <JsonView value={task.assertions} title="输入与预期结果" /> : <p className="ns-lab-muted">断言含高精度数字，请在下方完整原始 JSON 中查看，避免浏览器数值显示舍入。</p>}</details>)}<details className="ns-lab-details"><summary>完整导出规范（含任务、输入、预期与评分规则）</summary><pre>{detail.document_json || JSON.stringify(detail.document, null, 2)}</pre></details>
      </Surface> : !importOpen && <Surface className="ns-lab-panel"><EmptyState title="建立你的任务档案" description="新建空白任务集或导入 JSON。保存后，每个版本都有自己的内容哈希，并可用于重复测评。" action={<Button disabled={busy} onClick={() => edit()}>新建任务集</Button>} /><div className="ns-dataset-rule"><strong>从你定义的行为开始</strong><span>为一个 Go 函数准备任务说明、待修复的初始代码和断言。支持标量及一维切片；不支持其他语言或 Agent 工作流。</span></div></Surface>}
    </div></div>
  </LabShell>;
}
