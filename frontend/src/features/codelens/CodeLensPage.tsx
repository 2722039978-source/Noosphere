import { useEffect, useRef, useState } from "react";
import { Code2, FileCode2, FolderOpen, Search, Sparkles } from "lucide-react";
import { useBackend } from "@/hooks/useBackend";
import { useUiStore } from "@/stores/ui-store";
import { useWorkspaceStore } from '@/stores/workspace-store';
import { Button } from "@/components/ui/button";
import { Surface } from "@/components/ui/surface";
import { PageHeader } from "@/components/ui/page-header";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { errMessage } from '@/lib/api';

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

interface CodeSearchHit {
  file: string;
  line: number;
  content: string;
}

interface Entity {
  kind: string;
  name: string;
  line: number;
}

interface AnalysisResult {
  entities?: Entity[];
  error?: string;
}

export function CodeLensPage() {
  const backend = useBackend();
  const { indexProject, searchCode, analyzeFile } = backend;
  const workspaceId = useWorkspaceStore((s) => s.activeId);
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

  const [analyzeFileInput, setAnalyzeFileInput] = useState("");
  const [analysis, setAnalysis] = useState<AnalysisResult | null>(null);
  const [analyzing, setAnalyzing] = useState(false);
  const generation = useRef(0);
  useEffect(() => {
    generation.current++;
    setLoading(false); setIngested(false); setIndexResult(null);
    setSearchResults([]); setSearched(false); setSearching(false); setSearchError('');
    setAnalysis(null); setAnalyzing(false);
    return () => { generation.current++; };
  }, [workspaceId, projectIdentity]);

  const handleIndex = async () => {
    if (loading) return;
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
    if (!searchQuery.trim() || searching) return;
    const epoch = generation.current;
    setSearching(true);
    setSearchError(''); setSearched(false);
    try {
      const r = (await searchCode(projectPath, searchQuery)) as { results?: CodeSearchHit[] };
      if (generation.current !== epoch) return;
      setSearchResults(r.results || []);
      setSearched(true);
    } catch (error) {
      if (generation.current !== epoch) return;
      setSearchError(errMessage(error));
      setSearchResults([]);
    }
    if (generation.current === epoch) setSearching(false);
  };

  const handleAnalyze = async () => {
    if (!analyzeFileInput.trim()) return;
    const epoch = generation.current;
    setAnalyzing(true);
    try {
      const r = (await analyzeFile(projectPath, analyzeFileInput)) as AnalysisResult;
      if (generation.current !== epoch) return;
      setAnalysis(r);
    } catch (e) {
      if (generation.current === epoch) setAnalysis({ error: errMessage(e) });
    }
    if (generation.current === epoch) setAnalyzing(false);
  };

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="ns-page-wrap">
        <PageHeader
          icon={<Code2 size={20} />}
          accent="var(--acc-codelens)"
          title="CodeLens"
          subtitle="代码智能工作区 · 基于 Tree-sitter 的代码分析，索引结果自动写入记忆引擎"
        />

        <div className="ns-task-path" aria-label="代码工作流程"><strong>01 建立项目索引</strong><span>→</span><span>02 搜索代码</span><span>→</span><span>03 查看文件结构</span></div>
        {/* 项目索引 */}
        <Surface className="px-6 py-6">
          <SectionTitle step="01" description="选择本地项目，建立可供代码搜索与 AI 对话使用的索引。" icon={<FolderOpen size={15} />} accent="text-acc-codelens">
            项目索引
          </SectionTitle>
          <div className="mt-4 flex gap-2.5">
            <div className="relative flex-1">
              <FolderOpen size={14} className="absolute top-1/2 left-3 -translate-y-1/2 text-text-3" />
              <Input
                value={projectPath}
                aria-label="项目目录"
                onChange={(e) => setProjectPath(e.target.value)}
                placeholder="项目绝对路径，如 D:\my-project"
                className="pl-9"
              />
            </div>
            <Button onClick={handleIndex} disabled={loading || !projectPath} className="gap-1.5">
              <Sparkles size={14} /> {loading ? "索引中…" : "开始索引"}
            </Button>
          </div>

          {loading && <p role="status" className="mt-3 text-xs text-text-2">索引任务在后台执行，切换页面不会取消；进度、结果和取消操作可在顶部“工作区与任务”查看。</p>}
          {ingested && (
            <p className="mt-3 flex items-center gap-2 text-[13px] text-ok">
              <Sparkles size={13} /> 索引完成并已写入记忆引擎 — AI 助手现在可以回答代码相关问题。
            </p>
          )}
          {indexResult?.error && (
            <p className="mt-3 rounded-none bg-err/10 px-4 py-2.5 text-[13px] text-err">
              {indexResult.error}
            </p>
          )}

          {indexResult && !indexResult.error && (
            <div className="ns-enter mt-4 flex flex-wrap items-center gap-8">
              <Stat label="文件数" value={String(indexResult.total_files ?? 0)} />
              <Stat label="代码行" value={(indexResult.total_lines ?? 0).toLocaleString()} />
              <Stat label="符号数" value={String(indexResult.total_entities ?? 0)} />
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
        <Surface className="px-6 py-6">
          <SectionTitle step="02" description="检索函数、类型或关键词，展开结果查看代码片段。" icon={<Search size={15} />} accent="text-acc-codelens">
            代码搜索
          </SectionTitle>
          <div className="mt-4 flex gap-2.5">
            <div className="relative flex-1">
              <Search size={14} className="absolute top-1/2 left-3 -translate-y-1/2 text-text-3" />
              <Input
                value={searchQuery}
                aria-label="代码检索"
                onChange={(e) => setSearchQuery(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && handleSearch()}
                placeholder="搜索函数、类型、关键字…"
                className="pl-9"
              />
            </div>
            <Button variant="secondary" onClick={handleSearch} disabled={searching || !searchQuery.trim()}>
              {searching ? "搜索中…" : "搜索"}
            </Button>
          </div>
          {searching && <p role="status" className="ns-result-status">正在检索项目代码…</p>}
          {searchError && <p role="alert" className="ns-form-error">{searchError}</p>}
          {searched && !searchResults.length && <p role="status" className="ns-result-status">没有找到匹配代码。请确认项目已建立索引，或更换关键词。</p>}
          {searchResults.length > 0 && (
            <div className="ns-enter mt-4 flex flex-col gap-2">
              {searchResults.map((r, i) => (
                <details key={i} className="ns-record-detail">
                  <summary><span className="font-mono text-acc-codelens">{r.file}</span><span className="text-text-2"> · 第 {r.line} 行</span></summary>
                  <div className="ns-detail-body"><pre className="overflow-x-auto whitespace-pre-wrap font-mono text-[13px]">{r.content}</pre><Button variant="outline" size="sm" className="mt-4" onClick={() => { setAnalyzeFileInput(r.file); document.getElementById('file-analysis')?.scrollIntoView({ block: 'start' }); document.getElementById('analysis-path')?.focus({ preventScroll: true }); }}>在文件分析中打开</Button></div>
                </details>
              ))}
            </div>
          )}
        </Surface>

        {/* 文件分析 */}
        <Surface id="file-analysis" className="px-6 py-6">
          <SectionTitle step="03" description="从检索结果打开文件，或输入相对路径，查看函数与类型的位置。" icon={<FileCode2 size={15} />} accent="text-acc-codelens">
            文件分析（AST）
          </SectionTitle>
          <div className="mt-4 flex gap-2.5">
            <div className="relative flex-1">
              <FileCode2 size={14} className="absolute top-1/2 left-3 -translate-y-1/2 text-text-3" />
              <Input
                value={analyzeFileInput}
                id="analysis-path" aria-label="分析文件路径"
                onChange={(e) => setAnalyzeFileInput(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && handleAnalyze()}
                placeholder="文件相对路径，如 cmd/server/main.go"
                className="pl-9"
              />
            </div>
            <Button variant="secondary" onClick={handleAnalyze} disabled={analyzing || !analyzeFileInput.trim()}>
              {analyzing ? "分析中…" : "分析"}
            </Button>
          </div>
          {analysis?.error && (
            <p className="mt-3 rounded-none bg-err/10 px-4 py-2.5 text-[13px] text-err">
              {analysis.error}
            </p>
          )}
          {analysis?.entities && (
            <div className="ns-enter mt-4 flex flex-col gap-2">
              {analysis.entities.map((e, i) => (
                <div key={i} className="flex items-center gap-2.5 text-[13px]">
                  <Badge
                    variant={e.kind === "function" ? "info" : "secondary"}
                    className="text-xs"
                  >
                    {e.kind}
                  </Badge>
                  <span className="font-mono">{e.name}</span>
                  <span className="text-xs text-text-3">第 {e.line} 行</span>
                </div>
              ))}
            </div>
          )}
        </Surface>
      </div>
    </div>
  );
}

function SectionTitle({
  step, description,
  icon,
  accent,
  children,
}: {
  step: string;
  description: string;
  icon: React.ReactNode;
  accent?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="ns-section-heading">
      <span className="ns-section-number">{step}</span>
      <div><h2 className="flex items-center gap-2"><span className={accent}>{icon}</span>{children}</h2><p>{description}</p></div>
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
