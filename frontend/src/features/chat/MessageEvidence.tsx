import { useState } from 'react';
import { api, errMessage } from '@/lib/api';
import { useWorkspaceStore } from '@/stores/workspace-store';
import type { ChatMessage } from './chat-store';
import { ArtifactEvidenceButton, SourceEvidenceButton } from '@/components/CodeLensEvidence';

export function MessageEvidence({ message, workspaceId, sessionId }: { message: ChatMessage; workspaceId?: string; sessionId?: string }) {
  const [state, setState] = useState('');
  const [busy, setBusy] = useState(false);
  async function save() {
    if (!workspaceId || !sessionId || busy) return;
    const workspace = useWorkspaceStore.getState();
    const projectPath = workspace.workspaces.find(item => item.id === workspaceId)?.project_path ?? '';
    if (workspace.activeId !== workspaceId || (message.sourceProjectPath ?? '') !== projectPath) {
      setState('此回复的项目来源与当前工作区不匹配。');
      return;
    }
    setBusy(true);
    try {
      const item = await api.saveKnowledge(workspaceId, {
        content: message.content, category: 'solution', status: 'proposed', expected_project_path: projectPath,
        source: { kind: 'chat', session_id: sessionId, message_id: message.id, excerpt: message.content.slice(0, 1000) },
      });
      setState(item.status === 'proposed' ? '已保存为待确认，可在记忆区查看。' : '已关联现有记忆。');
    }
    catch (error) { setState(`保存失败：${errMessage(error)}`); }
    finally { setBusy(false); }
  }
  return <div className="mt-3 text-xs leading-6 text-text-3">
    {message.contextWarnings?.map((warning, index) => <p role="status" key={index}>{warning}</p>)}
    {!!message.citations?.length && <details className="border border-border-subtle px-3 py-2">
      <summary className="cursor-pointer">本次提供给模型的依据 · {message.citations.length} 条</summary>
      {message.citations.map(c => <details className="mt-2 border-t border-border-subtle pt-2" key={c.id}>
        <summary className="cursor-pointer break-all">[{c.id}] {c.kind === 'code' ? '代码' : c.kind === 'artifact' ? '编译产物' : c.kind === 'attachment' ? '附件' : '长期记忆'} · {c.title}{c.page ? ` · 第 ${c.page} 页` : c.start_line ? ` · L${c.start_line}–${c.end_line}` : ''}{c.symbol && ` · ${c.symbol}`}{c.revision && ` · 版本 ${c.revision}`}</summary>
        {c.source_hash && <p className="break-all">文件 SHA-256 {c.source_hash}</p>}
        {!message.evidenceSnapshotOnly && workspaceId === c.workspace_id && c.artifact_id && <ArtifactEvidenceButton workspaceId={workspaceId} id={c.artifact_id} path={c.title} />}
        {!message.evidenceSnapshotOnly && workspaceId === c.workspace_id && c.file && c.start_line && c.end_line && <SourceEvidenceButton workspaceId={workspaceId} label="打开源码引用" location={{ file: c.file, start_line: c.start_line, end_line: c.end_line, source_hash: c.source_hash }} />}
        <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-words p-2 font-mono text-text-2">{c.excerpt}</pre>
      </details>)}
    </details>}
    {!message.streaming && (!message.status || message.status === 'completed') && message.content && workspaceId && sessionId && <button className="mt-2 underline underline-offset-4 disabled:opacity-50" disabled={busy || state.startsWith('已')} onClick={() => void save()}>{busy ? '正在保存…' : '整理为待确认记忆'}</button>}
    {state && <p role="status">{state}</p>}
  </div>;
}
