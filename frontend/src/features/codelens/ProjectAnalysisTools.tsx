import { useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { api, errMessage, type ArtifactRecord, type ArtifactTool, type SourceTrace } from '@/lib/api';
import { ArtifactEvidenceButton, SourceEvidenceButton } from '@/components/CodeLensEvidence';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Surface } from '@/components/ui/surface';
import { useEvidenceDraft } from './evidence-draft';
import { artifactCitation } from './project-evidence';

const STATUS = { resolved: '已确定目标', candidate: '候选目标', unresolved: '无法确定' };
const RELATION = { call: '调用', calls: '调用', import: '导入', imports: '导入', route: '路由' } as Record<string, string>;

export function ProjectAnalysisTools({ workspaceId, projectPath }: { workspaceId: string; projectPath: string }) {
  return <div className="ns-codelens-evidence-grid" key={`${workspaceId}:${projectPath}`}>
    <SourceTraceTool workspaceId={workspaceId} enabled={!!projectPath} />
    <ArtifactToolPanel workspaceId={workspaceId} projectPath={projectPath} />
  </div>;
}

function SourceTraceTool({ workspaceId, enabled }: { workspaceId: string; enabled: boolean }) {
  const [query, setQuery] = useState('');
  const [trace, setTrace] = useState<SourceTrace>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  async function run() {
    if (busy || !enabled || !query.trim()) return;
    setBusy(true);
    setTrace(undefined);
    setError('');
    try { setTrace(await api.codelensTrace(workspaceId, query.trim())); }
    catch (cause) { setError(errMessage(cause)); }
    finally { setBusy(false); }
  }

  return <Surface className="ns-codelens-stage" aria-labelledby="source-trace-title">
    <h2 className="text-base font-semibold" id="source-trace-title">源码追踪</h2>
    <p className="mt-2 text-xs leading-6 text-text-2">输入函数名、文件或路径，查看定义、导入与调用关系。</p>
    <form className="ns-codelens-form-row" onSubmit={event => { event.preventDefault(); void run(); }}>
      <Input className="flex-1" aria-label="追踪符号或路径" value={query} onChange={event => setQuery(event.target.value)} placeholder="例如 registerWorkspaceWorkers" maxLength={300} />
      <Button type="submit" disabled={!enabled || !query.trim() || busy}>{busy ? '追踪中…' : '追踪'}</Button>
    </form>
    {!enabled && <p className="mt-3 text-xs text-text-3">先在项目侧栏绑定目录。</p>}
    {error && <p role="alert" className="ns-form-error mt-3">{error}</p>}
    {trace && <div className="mt-4 space-y-3 text-xs">
      <p role="status">{trace.matches.length} 个位置 · {trace.relations.length} 条关系{trace.revision && ` · 版本 ${trace.revision.slice(0, 12)}`}</p>
      {trace.warnings.map(warning => <p className="text-text-2" key={warning}>{warning}</p>)}
      <ul className="space-y-2" aria-label="匹配的源码位置">
        {trace.matches.map(match => <li key={`${match.file}:${match.start_line}:${match.symbol}`} className="flex flex-wrap items-center gap-2">
          {match.symbol && <strong className="break-all">{match.symbol}</strong>}
          <SourceEvidenceButton workspaceId={workspaceId} location={match} />
        </li>)}
      </ul>
      <ul className="space-y-3" aria-label="源码关系">
        {trace.relations.map((relation, index) => <li className="space-y-2 border-t border-border-soft pt-3" key={`${relation.source.file}:${relation.source.start_line}:${index}`}>
          <p><strong>{RELATION[relation.kind] ?? relation.kind}</strong> · {STATUS[relation.status]}{relation.reason && ` · ${relation.reason}`}</p>
          <div className="flex flex-wrap items-center gap-2"><SourceEvidenceButton workspaceId={workspaceId} location={relation.source} />
            {relation.target && <><span aria-label="目标">→</span><SourceEvidenceButton workspaceId={workspaceId} location={relation.target} /></>}
          </div>
          {relation.candidates?.map(candidate => <SourceEvidenceButton key={`${candidate.file}:${candidate.start_line}`} workspaceId={workspaceId} location={candidate} />)}
        </li>)}
      </ul>
    </div>}
  </Surface>;
}

function ArtifactToolPanel({ workspaceId, projectPath }: { workspaceId: string; projectPath: string }) {
  const [path, setPath] = useState('');
  const [tool, setTool] = useState<ArtifactTool>('inspect-pe-imports');
  const [result, setResult] = useState<ArtifactRecord>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const active = useRef(true);
  const navigate = useNavigate();
  useEffect(() => { active.current = true; return () => { active.current = false; }; }, []);

  async function run() {
    if (busy || !projectPath || !path.trim()) return;
    setBusy(true);
    setError('');
    setResult(undefined);
    try {
      const record = await api.codelensInspectArtifact(workspaceId, path.trim(), tool);
      if (active.current) setResult(record);
    } catch (cause) {
      if (active.current) setError(errMessage(cause));
    } finally {
      if (active.current) setBusy(false);
    }
  }

  return <Surface className="ns-codelens-stage" aria-labelledby="artifact-analysis-title">
    <h2 className="text-base font-semibold" id="artifact-analysis-title">产物分析 <span className="ml-2 text-xs text-text-3">REA</span></h2>
    <p className="mt-2 text-xs leading-6 text-text-2">输入项目内的可执行文件路径，读取 PE 导入表或 Go 构建信息。</p>
    <form className="mt-4 space-y-3" onSubmit={event => { event.preventDefault(); void run(); }}>
      <Input aria-label="产物相对路径" value={path} onChange={event => setPath(event.target.value)} placeholder="例如 release/Noosphere.exe" maxLength={1024} />
      <div className="flex flex-wrap items-center gap-2">
        <select className="min-h-9 min-w-0 flex-1 border border-border-soft bg-bg-raised px-2 text-xs" aria-label="产物分析类型" value={tool} onChange={event => setTool(event.target.value as ArtifactTool)}>
          <option value="inspect-pe-imports">PE 导入表</option>
          <option value="inspect-go-binary">Go 构建信息</option>
        </select>
        <Button type="submit" disabled={!projectPath || !path.trim() || busy}>{busy ? '分析中…' : '分析产物'}</Button>
      </div>
    </form>
    {!projectPath && <p className="mt-3 text-xs text-text-3">先在项目侧栏绑定目录。</p>}
    {error && <p role="alert" className="ns-form-error mt-3">{error}</p>}
    {result && <div className="mt-4 space-y-3 text-xs" aria-label="产物分析结果">
      <p role="status" className="break-all">已保存 {result.path}</p>
      <p className="break-all text-text-3">SHA-256 {result.source_hash}</p>
      <div className="flex flex-wrap gap-2">
        <ArtifactEvidenceButton workspaceId={workspaceId} id={result.id} path={result.path} record={result} />
        <Button variant="secondary" type="button" size="sm" onClick={() => {
          useEvidenceDraft.getState().attach({ citation: artifactCitation(workspaceId, result), projectPath });
          navigate('/chat');
        }}>带入对话</Button>
      </div>
    </div>}
  </Surface>;
}
