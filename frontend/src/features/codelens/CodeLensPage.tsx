import { useEffect, useRef, useState } from "react";
import { Code2, FileCode2, FolderOpen, ListFilter, Search, Sparkles, X } from "lucide-react";
import { useBackend } from "@/hooks/useBackend";
import { useUiStore } from "@/stores/ui-store";
import { useWorkspaceStore } from '@/stores/workspace-store';
import { Button } from "@/components/ui/button";
import { Surface } from "@/components/ui/surface";
import { PageHeader } from "@/components/ui/page-header";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { api, errMessage, type SourceLocation } from '@/lib/api';
import { SourceEvidenceButton } from '@/components/CodeLensEvidence';
import { ProjectAnalysisTools } from './ProjectAnalysisTools';
import './codelens-workspace.css';

interface IndexResult {
  chunk_strategy?: string;
  revision?: string;
  delta?: { updated_records: number; reused_records: number; deleted_records: number; embedded_chunks: number; graph_rebuilt: boolean; graph_ms: number };
  project_id?: string;
  indexed_chunks?: number;
  retrieval_ready?: boolean;
  project_root?: string;
  total_files?: number;
  total_lines?: number;
  total_entities?: number;
  languages?: Record<string, number>;
  indexed_at?: string;
  error?: string;
}

type CodeSearchHit = SourceLocation & { excerpt: string };

interface Entity {
  type: string;
  name: string;
  line: number;
}

interface AnalysisResult {
  entities?: Entity[];
  error?: string;
}

export function CodeLensPage() {
  const backend = useBackend();
  const { indexProject, analyzeFile } = backend;
  const workspaceId = useWorkspaceStore((s) => s.activeId);
  const workspaceProjectPath = useWorkspaceStore((s) => s.workspaces.find(item => item.id === s.activeId)?.project_path || '');
  const projectPath = useUiStore((s) => s.projectPath);
  // The Windows backend canonicalizes slash direction, case and trailing
  // separators. Those edits do not mean the user selected a different project.
  const projectIdentity = projectPath.trim().replace(/[\\/]+/g, '/').replace(/\/$/, '').toLowerCase();
  const setProjectPath = useUiStore((s) => s.setProjectPath);

  const [loading, setLoading] = useState(false);
  const [ingested, setIngested] = useState(false);
  const [indexResult, setIndexResult] = useState<IndexResult | null>(null);

  const [searchQuery, setSearchQuery] = useState("");
  const [searchResults, setSearchResults] = useState<CodeSearchHit[]>([]);
  const [searching, setSearching] = useState(false);
  const [searched, setSearched] = useState(false);
  const [searchError, setSearchError] = useState('');
  const [searchPartial, setSearchPartial] = useState(false);
  const [resultFilter, setResultFilter] = useState('');

  const [analyzeFileInput, setAnalyzeFileInput] = useState("");
  const [analysis, setAnalysis] = useState<AnalysisResult | null>(null);
  const [analyzing, setAnalyzing] = useState(false);
  const generation = useRef(0);
  useEffect(() => {
    generation.current++;
    setLoading(false); setIngested(false); setIndexResult(null);
    setSearchResults([]); setSearched(false); setSearching(false); setSearchError(''); setSearchPartial(false);
    setResultFilter('');
    setAnalysis(null); setAnalyzing(false);
    return () => { generation.current++; };
  }, [workspaceId, projectIdentity]);

  const handleIndex = async () => {
    if (loading || !projectPath.trim()) return;
    const epoch = generation.current;
    setLoading(true);
    setIngested(false);
    try {
      const r = (await indexProject(projectPath)) as IndexResult;
      if (generation.current !== epoch) return;
      setIndexResult(r);
      if (!r.error && r.retrieval_ready && r.project_id) {
        setIngested(true);
      }
    } catch (e) {
      if (generation.current === epoch) setIndexResult({ error: errMessage(e) });
    }
    if (generation.current === epoch) setLoading(false);
  };

  const handleSearch = async () => {
    if (!workspaceProjectPath || !searchQuery.trim() || searching) return;
    const epoch = generation.current;
    setSearching(true);
    setSearchError(''); setSearched(false);
    setSearchPartial(false);
    setResultFilter('');
    try {
      const r = await api.codelensProjectSearch(workspaceId, searchQuery.trim());
      if (generation.current !== epoch) return;
      setSearchResults(r.results ?? []);
      setSearchPartial(r.partial);
      setSearched(true);
    } catch (error) {
      if (generation.current !== epoch) return;
      setSearchError(errMessage(error));
      setSearchResults([]);
    }
    if (generation.current === epoch) setSearching(false);
  };

  const handleAnalyze = async () => {
    if (!workspaceProjectPath || !analyzeFileInput.trim() || analyzing) return;
    const epoch = generation.current;
    setAnalyzing(true);
    setAnalysis(null);
    try {
      const r = (await analyzeFile(workspaceProjectPath, analyzeFileInput)) as AnalysisResult;
      if (generation.current !== epoch) return;
      setAnalysis(r);
    } catch (e) {
      if (generation.current === epoch) setAnalysis({ error: errMessage(e) });
    }
    if (generation.current === epoch) setAnalyzing(false);
  };

  const filterTerm = resultFilter.trim().toLocaleLowerCase();
  const visibleResults = searchResults.map((hit, index) => ({ hit, index })).filter(({ hit }) => !filterTerm || `${hit.file}\n${hit.excerpt}`.toLocaleLowerCase().includes(filterTerm));

  return (
    <div className="ns-codelens-workspace flex-1 overflow-y-auto" data-workspace-page="codelens">
      <div className="ns-page-wrap ns-codelens-page">
        <PageHeader
          icon={<Code2 size={20} />}
          accent="var(--acc-codelens)"
          title="CodeLens"
          subtitle="在当前项目内搜索文字、追踪源码关系和分析编译产物"
        />

        <div className="ns-task-path" aria-label="代码工作流程"><strong>01 建立项目索引</strong><span aria-hidden="true">→</span><span>02 搜索代码</span><span aria-hidden="true">→</span><span>03 查看文件结构</span></div>
        <div className="ns-codelens-grid">
        {/* 项目索引 */}
        <Surface className="ns-codelens-stage" data-codelens-stage="index" role="region" aria-labelledby="codelens-index-title">
          <SectionTitle id="codelens-index-title" step="01" description="选择本地项目，建立可供代码搜索与 AI 对话使用的索引。" icon={<FolderOpen size={15} />} accent="text-acc-codelens">
            项目索引
          </SectionTitle>
          <form className="ns-codelens-form-row" onSubmit={event => { event.preventDefault(); void handleIndex(); }}>
            <div className="ns-codelens-input-wrap relative flex-1">
              <FolderOpen size={14} className="absolute top-1/2 left-3 -translate-y-1/2 text-text-3" />
              <Input
                id="codelens-project-path"
                value={projectPath}
                aria-label="项目目录"
                onChange={(e) => setProjectPath(e.target.value)}
                placeholder="项目绝对路径，如 D:\my-project"
                className="pl-9"
              />
            </div>
            <Button type="submit" disabled={loading || !projectPath.trim()} className="gap-1.5">
              <Sparkles size={14} /> {loading ? "索引中…" : "开始索引"}
            </Button>
          </form>
          <div className="ns-codelens-quick-fill"><Button type="button" size="sm" variant="ghost" disabled={loading || !workspaceProjectPath} onClick={() => { setProjectPath(workspaceProjectPath); document.getElementById('codelens-project-path')?.focus(); }}><FolderOpen size={13} aria-hidden="true" />使用工作区目录</Button>{!workspaceProjectPath && <span>当前工作区尚未绑定目录</span>}</div>

          {loading && <p role="status" className="mt-3 text-xs leading-6 text-text-2">索引任务在后台执行。可在项目侧栏的“任务”中查看进度或取消。</p>}
          {ingested && (
            <p className="mt-3 flex items-center gap-2 text-[13px] text-ok">
              <Sparkles size={13} /> 索引完成并已写入记忆引擎 — AI 助手现在可以回答代码相关问题。
            </p>
          )}
          {indexResult?.error && (
            <p role="alert" className="mt-3 rounded-none bg-err/10 px-4 py-2.5 text-[13px] text-err">
              {indexResult.error}
            </p>
          )}

          {indexResult && !indexResult.error && (
            <div className="ns-enter mt-4 flex flex-wrap items-center gap-8">
              <Stat label="文件数" value={indexResult.total_files?.toLocaleString() ?? '未提供'} />
              <Stat label="代码行" value={indexResult.total_lines?.toLocaleString() ?? '未提供'} />
              <Stat label="符号数" value={indexResult.total_entities?.toLocaleString() ?? '未提供'} />
            </div>
          )}
          {indexResult?.delta && <p className="mt-3 text-xs leading-6 text-text-3">按符号索引 · 更新 {indexResult.delta.updated_records} · 复用 {indexResult.delta.reused_records} · 删除 {indexResult.delta.deleted_records} · 重新计算向量 {indexResult.delta.embedded_chunks}{indexResult.delta.graph_rebuilt ? ` · 图构建 ${indexResult.delta.graph_ms} ms` : ' · 已跳过图重建'}{indexResult.revision && ` · 版本 ${indexResult.revision.slice(0, 12)}`}</p>}
          {indexResult?.languages && (
            <div className="mt-3 flex flex-wrap gap-2">
              {Object.entries(indexResult.languages).map(([lang, count]) => (
                <Badge key={lang} variant="info" className="text-xs">
                  {lang}: {count}
                </Badge>
              ))}
            </div>
          )}
        </Surface>

        {/* 代码搜索 */}
        <Surface className="ns-codelens-stage" data-codelens-stage="search" role="region" aria-labelledby="codelens-search-title">
          <SectionTitle id="codelens-search-title" step="02" description="检索函数、类型或关键词，展开结果查看代码片段。" icon={<Search size={15} />} accent="text-acc-codelens">
            代码搜索
          </SectionTitle>
          <form className="ns-codelens-form-row" onSubmit={event => { event.preventDefault(); void handleSearch(); }}>
            <div className="ns-codelens-input-wrap relative flex-1">
              <Search size={14} className="absolute top-1/2 left-3 -translate-y-1/2 text-text-3" />
              <Input
                value={searchQuery}
                aria-label="代码检索"
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder="搜索函数、类型、关键字…"
                className="pl-9"
              />
            </div>
            <Button type="submit" variant="secondary" disabled={!workspaceProjectPath || searching || !searchQuery.trim()}>
              {searching ? "搜索中…" : "搜索"}
            </Button>
          </form>
          {searching && <p role="status" className="ns-result-status">正在检索项目代码…</p>}
          {searchError && <p role="alert" className="ns-form-error">{searchError}</p>}
          {searchPartial && <p role="status" className="ns-result-status">本次搜索只覆盖部分文件或已达到结果上限。</p>}
          {searched && !searchResults.length && <p role="status" className="ns-result-status">没有找到匹配代码。可以更换关键词。</p>}
          {searchResults.length > 0 && (
            <div className="ns-enter mt-4 flex flex-col gap-2">
              <div className="ns-codelens-result-tools">
                <div className="ns-codelens-input-wrap relative"><ListFilter size={14} className="absolute top-1/2 left-3 -translate-y-1/2 text-text-3" aria-hidden="true" /><Input aria-label="筛选当前搜索结果" placeholder="筛选文件名或代码片段…" value={resultFilter} onChange={event => setResultFilter(event.target.value)} className="pl-9" /></div>
                <div className="ns-codelens-result-count"><span role="status" aria-live="polite">显示 {visibleResults.length} / {searchResults.length} 条</span>{resultFilter && <Button type="button" size="sm" variant="ghost" aria-label="清除搜索结果筛选" onClick={() => setResultFilter('')}><X size={13} aria-hidden="true" />清除</Button>}</div>
              </div>
              {!visibleResults.length && <p role="status" className="ns-result-status">当前筛选没有匹配结果；清除筛选可查看本次检索的全部记录。</p>}
              {visibleResults.map(({ hit: r, index }) => (
                <details key={`${r.file}:${r.start_line}:${index}`} className="ns-record-detail">
                  <summary><span className="font-mono text-acc-codelens">{r.file}</span><span className="text-text-2"> · 第 {r.start_line} 行</span></summary>
                  <div className="ns-detail-body"><pre className="overflow-x-auto whitespace-pre-wrap font-mono text-[13px]">{r.excerpt}</pre><div className="mt-4 flex flex-wrap gap-2"><SourceEvidenceButton workspaceId={workspaceId} location={r} label="打开源码引用" /><Button variant="outline" size="sm" onClick={() => { setAnalyzeFileInput(r.file); document.getElementById('file-analysis')?.scrollIntoView({ block: 'start' }); document.getElementById('analysis-path')?.focus({ preventScroll: true }); }}>在文件分析中打开</Button></div></div>
                </details>
              ))}
            </div>
          )}
        </Surface>

        {/* 文件分析 */}
        <Surface id="file-analysis" className="ns-codelens-stage" data-codelens-stage="analysis" role="region" aria-labelledby="codelens-analysis-title">
          <SectionTitle id="codelens-analysis-title" step="03" description="从检索结果打开文件，或输入相对路径，查看函数与类型的位置。" icon={<FileCode2 size={15} />} accent="text-acc-codelens">
            文件分析（AST）
          </SectionTitle>
          <form className="ns-codelens-form-row" onSubmit={event => { event.preventDefault(); void handleAnalyze(); }}>
            <div className="ns-codelens-input-wrap relative flex-1">
              <FileCode2 size={14} className="absolute top-1/2 left-3 -translate-y-1/2 text-text-3" />
              <Input
                value={analyzeFileInput}
                id="analysis-path" aria-label="分析文件路径"
                onChange={(e) => setAnalyzeFileInput(e.target.value)}
                placeholder="文件相对路径，如 cmd/server/main.go"
                className="pl-9"
              />
            </div>
            <Button type="submit" variant="secondary" disabled={!workspaceProjectPath || analyzing || !analyzeFileInput.trim()}>
              {analyzing ? "分析中…" : "分析"}
            </Button>
          </form>
          {analyzing && <p role="status" className="ns-result-status">正在读取文件结构…</p>}
          {analysis?.error && (
            <p role="alert" className="mt-3 rounded-none bg-err/10 px-4 py-2.5 text-[13px] text-err">
              {analysis.error}
            </p>
          )}
          {analysis?.entities?.length === 0 && <p role="status" className="ns-result-status">此文件未返回可展示的函数或类型。</p>}
          {analysis?.entities && (
            <div className="ns-enter mt-4 flex flex-col gap-2">
              {analysis.entities.map((e, i) => (
                <div key={i} className="flex items-center gap-2.5 text-[13px]">
                  <Badge
                    variant={e.type === "function" ? "info" : "secondary"}
                    className="text-xs"
                  >
                    {e.type}
                  </Badge>
                  <span className="font-mono">{e.name}</span>
                  <span className="text-xs text-text-3">第 {e.line} 行</span>
                </div>
              ))}
            </div>
          )}
        </Surface>
        </div>
        <ProjectAnalysisTools workspaceId={workspaceId} projectPath={workspaceProjectPath} />
      </div>
    </div>
  );
}

function SectionTitle({
  id, step, description,
  icon,
  accent,
  children,
}: {
  id: string;
  step: string;
  description: string;
  icon: React.ReactNode;
  accent?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="ns-section-heading">
      <span className="ns-section-number">{step}</span>
      <div><h2 id={id} className="flex items-center gap-2"><span className={accent}>{icon}</span>{children}</h2><p>{description}</p></div>
    </div>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-[20px] font-semibold text-text-1">{value}</div>
      <div className="mt-1 text-xs text-text-3">{label}</div>
    </div>
  );
}
