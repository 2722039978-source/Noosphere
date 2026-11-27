import { useEffect, useRef, useState } from 'react';
import { api, errMessage, type ChatAttachment } from '@/lib/api';
import { attachmentMaxBytes } from './chat-backup';

const maxFiles = 3;
const chunkBytes = 4 * 1024 * 1024;

type AttachmentState =
  | { phase: 'reading'; progress: number }
  | { phase: 'processing' }
  | { phase: 'ready'; attachment: ChatAttachment }
  | { phase: 'failed'; message: string; retry: boolean };

type Scope = { workspace_id: string; session_id: string; project_path: string };

interface DraftEntry {
  id: string;
  file: File;
  scope: Scope;
  key: string;
  controller: AbortController | null;
  dispatched: boolean;
  state: AttachmentState;
}

function readFile(file: Blob, signal: AbortSignal, progress: (loaded: number) => void): Promise<ArrayBuffer> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    const abort = () => reader.abort();
    reader.onload = () => reader.result instanceof ArrayBuffer
      ? resolve(reader.result)
      : reject(new Error('附件读取结果无效'));
    reader.onerror = () => reject(new Error('无法读取文件，请检查文件权限或重新选择'));
    reader.onabort = () => reject(new Error('附件读取已取消'));
    reader.onprogress = event => {
      if (event.lengthComputable && event.total > 0) progress(event.loaded);
    };
    reader.onloadend = () => signal.removeEventListener('abort', abort);
    if (signal.aborted) { reject(new Error('附件读取已取消')); return; }
    signal.addEventListener('abort', abort, { once: true });
    reader.readAsArrayBuffer(file);
  });
}

export function useChatAttachments(workspaceId: string, sessionId: string, projectPath: string) {
  const key = JSON.stringify([workspaceId, sessionId, projectPath]);
  const entries = useRef(new Map<string, DraftEntry>());
  const [files, setFiles] = useState<DraftEntry[]>([]);
  const [feedback, setFeedback] = useState<{ key: string; message: string } | null>(null);
  const current = files.filter(entry => entry.key === key);
  const publish = () => setFiles([...entries.current.values()].map(entry => ({ ...entry })));
  const show = (message: string) => setFeedback({ key, message });

  useEffect(() => () => {
    for (const [id, entry] of entries.current) {
      if (entry.key !== key) continue;
      entries.current.delete(id);
      entry.controller?.abort();
      if (entry.dispatched) void api.discardChatAttachment(workspaceId, sessionId, id).catch(() => {});
    }
  }, [key, workspaceId, sessionId]);

  async function attempt(entry: DraftEntry) {
    const controller = new AbortController();
    entry.controller = controller;
    const update = (state: AttachmentState) => {
      if (entries.current.get(entry.id) !== entry || controller.signal.aborted) return;
      entry.state = state;
      publish();
    };
    update({ phase: 'reading', progress: 0 });
    try {
      const { name, size } = entry.file;
      let attachment: ChatAttachment | undefined;
      for (let offset = 0; offset < size; offset += chunkBytes) {
        const end = Math.min(offset + chunkBytes, size);
        const bytes = await readFile(entry.file.slice(offset, end), controller.signal, loaded =>
          update({ phase: 'reading', progress: Math.floor((offset + loaded) / size * 100) }));
        if (controller.signal.aborted) return;
        if (end === size) update({ phase: 'processing' });
        entry.dispatched = true;
        const result = await api.stageChatAttachment({ ...entry.scope, id: entry.id, name, offset, size }, bytes);
        if (result.received !== end) throw new Error('附件传输未确认，请重试');
        attachment = result.attachment;
      }
      if (!attachment || attachment.id !== entry.id || attachment.name !== name || attachment.size !== size ||
          !/^[0-9a-f]{64}$/.test(attachment.source_hash)) throw new Error('附件确认不完整，请重试');
      update({ phase: 'ready', attachment });
    } catch (error) {
      const status = typeof error === 'object' && error && 'status' in error ? Number(error.status) : 0;
      update({ phase: 'failed', message: errMessage(error), retry: ![400, 404, 413, 415].includes(status) });
    } finally {
      if (entry.controller === controller) entry.controller = null;
    }
  }

  function add(picked: File[]) {
    if (!sessionId) { show('会话尚未就绪，请稍后添加附件。'); return; }
    show('');
    const scope = { workspace_id: workspaceId, session_id: sessionId, project_path: projectPath };
    const selected = [...entries.current.values()].filter(entry => entry.key === key);
    for (const file of picked) {
      if (selected.some(entry => entry.file.name === file.name && entry.file.size === file.size && entry.file.lastModified === file.lastModified)) continue;
      if (selected.length >= maxFiles) { show('每条消息最多添加 3 份文件。'); break; }
      const maxBytes = /\.(png|jpe?g)$/i.test(file.name) ? 8 * 1024 * 1024 : attachmentMaxBytes;
      const invalid = file.size === 0 || file.size > maxBytes;
      const entry: DraftEntry = {
        id: crypto.randomUUID(), file, scope, key, controller: null, dispatched: false,
        state: invalid
          ? { phase: 'failed', retry: false, message: file.size === 0 ? '文件为空。' : `文件超过每份 ${maxBytes / 1024 / 1024} MB 上限。` }
          : { phase: 'reading', progress: 0 },
      };
      entries.current.set(entry.id, entry);
      selected.push(entry);
      if (!invalid) void attempt(entry);
    }
    publish();
  }

  function remove(id: string) {
    const entry = entries.current.get(id);
    if (!entry || entry.key !== key) return;
    entries.current.delete(id);
    entry.controller?.abort();
    publish();
    if (entry.dispatched) {
      void api.discardChatAttachment(workspaceId, sessionId, id)
        .catch(() => show('附件清理暂未确认，未使用的草稿会自动过期。'));
    }
  }

  function retry(id: string) {
    const entry = entries.current.get(id);
    if (entry?.key === key && entry.state.phase === 'failed' && entry.state.retry && !entry.controller) void attempt(entry);
  }

  return {
    files: current,
    error: feedback?.key === key ? feedback.message : '',
    blocked: current.some(entry => entry.state.phase !== 'ready'),
    attachments: current.flatMap(entry => entry.state.phase === 'ready' ? [entry.state.attachment] : []),
    add, remove, retry, show,
    markSent() {
      for (const entry of current) entries.current.delete(entry.id);
      show('');
      publish();
    },
  };
}
