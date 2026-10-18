import { useState } from 'react';
import { api, errMessage } from '@/lib/api';
import type { ChatMessage } from './chat-store';

export function MessageEvidence({ message, workspaceId, sessionId }: { message: ChatMessage; workspaceId?: string; sessionId?: string }) {
  const [state, setState] = useState('');
  const [busy, setBusy] = useState(false);
  async function save() {
    if (!workspaceId || !sessionId || busy) return;
    setBusy(true);
    try { await api.saveKnowledge(workspaceId, { content: message.content, category: 'solution', status: 'proposed', source: { kind: 'chat', session_id: sessionId, message_id: message.id } }); setState('已保存为待确认；请在 Nebula 核实内容后确认。'); }
    catch (error) { setState(`保存失败：${errMessage(error)}`); }
    finally { setBusy(false); }
  }
  return <div className="mt-3 text-xs leading-6 text-text-3">
    {message.contextWarnings?.map((warning, index) => <p role="status" key={index}>{warning}</p>)}
    {!!message.citations?.length && <details className="border border-border-subtle px-3 py-2">
      <summary className="cursor-pointer">本次提供给模型的依据 · {message.citations.length} 条 · {message.retrievalMode === 'semantic' ? '语义与关键词检索' : '关键词与哈希向量检索'}</summary>
      <p className="mt-2">引用保存的是回答时的来源快照；请结合回答中的 [R编号] 判断使用位置。</p>
      {message.citations.map(c => <details className="mt-2 border-t border-border-subtle pt-2" key={c.id}>
        <summary className="cursor-pointer break-all">[{c.id}] {c.kind === 'code' ? '代码' : '长期记忆'} · {c.title}{c.file && ` · L${c.start_line}–${c.end_line}`}{c.symbol && ` · ${c.symbol}`}{c.revision && ` · 版本 ${c.revision}`}</summary>
        {c.source_hash && <p className="break-all">源码版本 {c.source_hash}</p>}
        <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-words p-2 font-mono text-text-2">{c.excerpt}</pre>
      </details>)}
    </details>}
    {!message.streaming && message.content && workspaceId && sessionId && <button className="mt-2 underline underline-offset-4 disabled:opacity-50" disabled={busy || state.startsWith('已保存')} onClick={() => void save()}>{busy ? '正在保存…' : '整理为待确认记忆'}</button>}
    {state && <p role="status">{state}</p>}
  </div>;
}
