import { useEffect, useRef, useState } from "react";
import { useSearchParams } from 'react-router-dom';
import { BookOpen, Brain, Plus, Search, Sparkles } from "lucide-react";
import { useBackend } from "@/hooks/useBackend";
import { errMessage, type SearchHit } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Surface } from "@/components/ui/surface";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { EmptyState } from "@/components/ui/feedback";
import { cn } from "@/lib/utils";
import { KnowledgePanel } from './KnowledgePanel';
import './nebula-workspace.css';

const TYPE_VARIANT: Record<string, "default" | "warn" | "info" | "secondary"> = {
  working: "warn",
  episodic: "info",
  semantic: "default",
  procedural: "secondary",
};

const TYPE_LABEL: Record<string, string> = {
  working: "工作记忆",
  episodic: "情景记忆",
  semantic: "语义记忆",
  procedural: "程序记忆",
};

export function NebulaPage() {
  const [search] = useSearchParams();
  const history = search.get('view') === 'history';
  return (
    <div className="ns-nebula-scroll" data-workspace-page="nebula">
      <div className="ns-page-wrap ns-nebula-page">
        <header className="ns-page-header ns-memory-page-heading">
          <div className="min-w-0 flex-1"><h1>{history ? '历史记忆' : '记忆'}</h1></div>
        </header>
        {history ? <MemoryArchive /> : <KnowledgePanel />}
      </div>
    </div>
  );
}

function MemoryArchive() {
  const backend = useBackend();
  const { sessions, loadSessions, createSession, storeMemory, searchMemories } = backend;
  const [activeId, setActiveId] = useState("");
  const [memoryInput, setMemoryInput] = useState("");
  const [searchQuery, setSearchQuery] = useState("");
  const [searchResults, setSearchResults] = useState<SearchHit[]>([]);
  const [searched, setSearched] = useState(false);
  const [creating, setCreating] = useState(false);
  const [storing, setStoring] = useState(false);
  const [searching, setSearching] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const alive = useRef(true);
  const selection = useRef(0);

  useEffect(() => {
    alive.current = true;
    void loadSessions();
    return () => { alive.current = false; selection.current++; };
  }, [loadSessions]);

  const selectSession = (id: string) => {
    selection.current++;
    setActiveId(id); setSearchResults([]); setSearched(false);
    setError(''); setNotice(''); setStoring(false); setSearching(false);
  };

  const handleCreate = async () => {
    if (creating) return;
    setCreating(true); setError(''); setNotice('');
    try {
      const id = await createSession("session-" + Date.now());
      if (alive.current) { selectSession(id); setNotice('记忆会话已创建。'); }
    } catch (e) { if (alive.current) setError(errMessage(e)); }
    finally { if (alive.current) setCreating(false); }
  };

  const handleStore = async () => {
    if (!activeId || !memoryInput.trim() || storing) return;
    const epoch = selection.current;
    const submitted = memoryInput;
    setStoring(true); setError(''); setNotice('');
    try {
      await storeMemory(activeId, submitted);
      if (!alive.current || epoch !== selection.current) return;
      setMemoryInput(current => current === submitted ? '' : current);
      setSearchResults([]); setSearched(false); setNotice('已存储到当前记忆会话。');
      void loadSessions();
    } catch (e) {
      if (alive.current && epoch === selection.current) setError(errMessage(e));
    } finally {
      if (alive.current && epoch === selection.current) setStoring(false);
    }
  };

  const handleSearch = async () => {
    if (!activeId || !searchQuery.trim() || searching) return;
    const epoch = selection.current;
    setSearching(true); setSearched(false); setError(''); setNotice('');
    try {
      const r = await searchMemories(activeId, searchQuery);
      if (!alive.current || epoch !== selection.current) return;
      setSearchResults(r.results || []);
      setSearched(true);
    } catch (e) {
      if (alive.current && epoch === selection.current) {
        setError(errMessage(e)); setSearchResults([]);
      }
    } finally {
      if (alive.current && epoch === selection.current) setSearching(false);
    }
  };

  const activeSession = sessions.find((s) => s.id === activeId);

  return (
    <div className="ns-memory-history-layout">
      <aside className="ns-memory-sidebar" aria-label="记忆会话列表">
        <div className="flex items-center gap-2 px-5 pt-5 pb-3"><BookOpen size={14} className="text-acc-nebula" /><span className="text-xs font-medium text-text-3">记忆会话</span></div>
        <div className="px-3 pb-3"><Button variant="outline" size="sm" disabled={creating} className="w-full justify-start gap-2" onClick={() => void handleCreate()}><Plus size={14} />{creating ? '创建中…' : '新建会话'}</Button></div>
        <div className="ns-memory-session-list">
          {sessions.map(s => <button key={s.id} aria-pressed={activeId === s.id} onClick={() => selectSession(s.id)} className={cn('flex cursor-pointer items-center justify-between px-3 py-2 text-left text-[13px]', activeId === s.id ? 'bg-secondary text-text-1' : 'text-text-2 hover:bg-secondary/50')}><span className="truncate">{s.name}</span><span className="ml-2 shrink-0 text-xs text-text-3">{s.memory_count}</span></button>)}
          {!sessions.length && <p className="px-3 py-2 text-xs text-text-3">暂无会话</p>}
        </div>
      </aside>
      <div className="ns-nebula-legacy-main">
        <Surface className="px-5 py-4">
          <h2 className="mb-4 text-[16px] font-semibold">记忆存储与检索</h2>
          <div className="flex gap-2.5">
            <Input
              placeholder="输入要存储的记忆内容…"
              aria-label="历史记忆内容"
              value={memoryInput}
              onChange={(e) => setMemoryInput(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && !e.nativeEvent.isComposing && void handleStore()}
              className="flex-1"
            />
            <Button
              variant="secondary"
              size="sm"
              className="gap-1.5"
              onClick={handleStore}
              disabled={storing || !activeId || !memoryInput.trim()}
            >
              <Brain size={14} /> {storing ? '存储中…' : '存储'}
            </Button>
          </div>
          <div className="mt-3 flex gap-2.5 border-t border-border-subtle pt-3">
            <Input
              placeholder={activeId ? "语义搜索记忆…" : "请先选择记忆会话"}
              aria-label="搜索历史记忆"
              value={searchQuery}
              disabled={searching}
              onChange={(e) => setSearchQuery(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && !e.nativeEvent.isComposing && void handleSearch()}
              className="flex-1"
            />
            <Button
              variant="secondary"
              size="sm"
              className="gap-1.5"
              onClick={handleSearch}
              disabled={searching || !activeId || !searchQuery.trim()}
            >
              <Search size={14} /> {searching ? '搜索中…' : '搜索'}
            </Button>
          </div>
          {error && <p role="alert" className="ns-form-error">{error}</p>}
          {(notice || storing || searching) && <p role="status" className="ns-legacy-feedback">{storing ? '正在存储记忆…' : searching ? '正在检索记忆…' : notice}</p>}
        </Surface>

        <div className="flex flex-col gap-3">
          {activeSession && (
            <div className="flex items-center gap-2 text-xs text-text-3">
              <BookOpen size={13} />
              {activeSession.name} · {activeSession.memory_count} 条记忆
            </div>
          )}

          {searched && searchResults.length === 0 && (
            <EmptyState
              icon={<Search size={18} />}
              title={`没有匹配「${searchQuery}」的记忆`}
              description="尝试更换关键词，或先存储一些记忆内容。"
            />
          )}

          {searchResults.map((r, i) => {
            const rec = r.record;
            return (
              <Surface key={i} className="px-5 py-4">
                <div className="flex gap-3">
                  <Sparkles size={14} className="mt-1 shrink-0 text-acc-nebula" />
                  <div className="min-w-0 flex-1">
                    <div className="mb-2 flex items-center gap-2">
                      <Badge variant={TYPE_VARIANT[rec?.type ?? ""] ?? "default"} className="text-xs">
                        {TYPE_LABEL[rec?.type ?? ""] ?? rec?.type ?? "语义记忆"}
                      </Badge>
                      <span className="font-mono text-xs text-text-3">
                        相关度 {typeof r.score === "number" ? r.score.toFixed(3) : r.score}
                      </span>
                      {rec?.importance != null && (
                        <span className="ml-auto text-xs text-text-3">
                          重要度 {Number(rec.importance).toFixed(2)}
                        </span>
                      )}
                    </div>
                    <div className="text-[14px] leading-relaxed break-words whitespace-pre-wrap text-text-1">
                      {rec?.content || "—"}
                    </div>
                    {rec?.metadata && Object.keys(rec.metadata).length > 0 && (
                      <details className="ns-record-detail mt-4"><summary>记录详情 · 元数据</summary><div className="ns-detail-body flex flex-wrap gap-2">
                        {Object.entries(rec.metadata).map(([k, v]) => (
                          <Badge key={k} variant="secondary" className="text-xs">
                            {k}: {String(v)}
                          </Badge>
                        ))}
                      </div></details>
                    )}
                  </div>
                </div>
              </Surface>
            );
          })}

          {!searched && (
            <p className="px-1 py-3 text-[13px] text-text-3">
              {activeId ? "输入关键词查看这段历史会话的记忆。" : "选择一段记忆会话。"}
            </p>
          )}
        </div>
      </div>
    </div>
  );
}
