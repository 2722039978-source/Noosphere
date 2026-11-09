import { useEffect, useState, type ReactNode } from 'react';
import { api, errMessage, type ArtifactRecord, type SourceExcerpt, type SourceLocation } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogTitle, DialogTrigger } from '@/components/ui/dialog';

function EvidenceDrawer({ label, title, description, children }: {
  label: string;
  title: string;
  description: string;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  return <Dialog open={open} onOpenChange={setOpen}>
    <DialogTrigger asChild><Button variant="outline" size="sm" type="button" className="max-w-full whitespace-normal text-left">{label}</Button></DialogTrigger>
    <DialogContent className="!left-auto !right-0 !top-0 !h-dvh !w-full !max-w-[min(100vw,44rem)] !translate-x-0 !translate-y-0 content-start !rounded-none overflow-y-auto border-l border-border-soft">
      <DialogTitle className="break-all pr-10">{title}</DialogTitle>
      <DialogDescription>{description}</DialogDescription>
      {open && children}
    </DialogContent>
  </Dialog>;
}

export function SourceEvidenceButton({ workspaceId, location, label }: { workspaceId: string; location: SourceLocation; label?: string }) {
  const endLine = Math.min(location.end_line, location.start_line + 199);
  return <EvidenceDrawer label={label ?? `${location.file}:${location.start_line}`} title={location.file} description={`源码 · 第 ${location.start_line} 至 ${location.end_line} 行`}>
    <SourceEvidence key={`${workspaceId}:${location.file}:${location.start_line}:${endLine}:${location.source_hash}`} workspaceId={workspaceId} location={{ ...location, end_line: endLine }} />
  </EvidenceDrawer>;
}

function SourceEvidence({ workspaceId, location }: { workspaceId: string; location: SourceLocation }) {
  const { file, start_line, end_line, source_hash } = location;
  const [result, setResult] = useState<SourceExcerpt>();
  const [error, setError] = useState('');
  useEffect(() => {
    let active = true;
    void api.codelensSource(workspaceId, { file, start_line, end_line, source_hash }).then(value => { if (active) setResult(value); }, cause => { if (active) setError(errMessage(cause)); });
    return () => { active = false; };
  }, [workspaceId, file, start_line, end_line, source_hash]);
  if (error) return <p role="alert" className="text-err">{error}</p>;
  if (!result) return <p role="status">正在核对源码版本…</p>;
  return <>
    {location.source_hash && <p className="break-all text-xs text-text-3">引用版本 {location.source_hash}</p>}
    {result.source_hash && <p className="break-all text-xs text-text-3">当前版本 {result.source_hash}</p>}
    {!result.matches_version ? <p role="status">文件内容已变化。可以更新索引并重新追踪。</p> :
      <pre className="max-h-[72vh] overflow-auto whitespace-pre-wrap break-words border border-border-soft p-3 text-xs">{result.excerpt}</pre>}
  </>;
}

export function ArtifactEvidenceButton({ workspaceId, id, path, record }: { workspaceId: string; id: string; path: string; record?: ArtifactRecord }) {
  return <EvidenceDrawer label="打开 REA 记录" title={path} description="REA 编译产物分析">
    <ArtifactEvidence key={`${workspaceId}:${id}`} workspaceId={workspaceId} id={id} initial={record} />
  </EvidenceDrawer>;
}

function ArtifactEvidence({ workspaceId, id, initial }: { workspaceId: string; id: string; initial?: ArtifactRecord }) {
  const [result, setResult] = useState(initial);
  const [error, setError] = useState('');
  const [showRaw, setShowRaw] = useState(false);
  useEffect(() => {
    if (initial) return;
    let active = true;
    void api.codelensGetArtifact(workspaceId, id).then(value => { if (active) setResult(value); }, cause => { if (active) setError(errMessage(cause)); });
    return () => { active = false; };
  }, [workspaceId, id, initial]);
  if (error) return <p role="alert" className="text-err">{error}</p>;
  if (!result) return <p role="status">正在读取 REA 记录…</p>;
  return <>
    <dl className="space-y-2 break-all text-xs text-text-2">
      <div><dt className="font-semibold">工具</dt><dd>{result.tool}</dd></div>
      <div><dt className="font-semibold">文件 SHA-256</dt><dd>{result.source_hash}</dd></div>
      <div><dt className="font-semibold">分析时间</dt><dd>{new Date(result.observed_at).toLocaleString()}</dd></div>
    </dl>
    <Button variant="outline" size="sm" type="button" onClick={() => setShowRaw(value => !value)}>{showRaw ? '返回分析结果' : '查看原始 JSON'}</Button>
    <pre className="max-h-[66vh] overflow-auto whitespace-pre-wrap break-words border border-border-soft p-3 text-xs">{JSON.stringify(showRaw ? result.evidence : result.evidence.normalized_result, null, 2)}</pre>
  </>;
}
