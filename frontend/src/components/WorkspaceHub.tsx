import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Folder, FolderOpen, Plus } from 'lucide-react';
import { api, errMessage } from '@/lib/api';
import { useBackend } from '@/hooks/useBackend';
import { useWorkspaceStore, type WorkspaceInput } from '@/stores/workspace-store';
import { useChatStore } from '@/features/chat/chat-store';
import { Button } from './ui/button';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from './ui/dialog';
import { Input } from './ui/input';
import './workspace-hub.css';

export function ProjectPickerDialog({ open, onOpenChange, onProjectSaved }: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onProjectSaved: () => void;
}) {
  const activeId = useWorkspaceStore(state => state.activeId);
  function opened(id: string) {
    useWorkspaceStore.getState().select(id);
    useChatStore.getState().restoreWorkspaceSession(id);
    onOpenChange(false);
    onProjectSaved();
  }
  return <Dialog open={open} onOpenChange={onOpenChange}>
    {open && <DialogContent className="ns-workspace-dialog">
      <DialogTitle>打开项目</DialogTitle>
      <DialogDescription>选择要在此工作区使用的文件夹。</DialogDescription>
      <ProjectPanel key={activeId} onOpen={opened} />
    </DialogContent>}
  </Dialog>;
}

const EMPTY: WorkspaceInput = { name: '', project_path: '', default_model_id: '' };

function ProjectPanel({ onOpen }: { onOpen: (id: string) => void }) {
  const { models } = useBackend();
  const workspaces = useWorkspaceStore(state => state.workspaces);
  const activeId = useWorkspaceStore(state => state.activeId);
  const [candidate, setCandidate] = useState('');
  const [draft, setDraft] = useState<WorkspaceInput>(workspaces.find(workspace => workspace.id === activeId) ?? EMPTY);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const mounted = useRef(true);
  const inFlight = useRef(false);
  useEffect(() => {
    mounted.current = true;
    void api.projectLaunchCandidate().then(path => { if (mounted.current) setCandidate(path ?? ''); }, () => {});
    return () => { mounted.current = false; };
  }, []);

  async function perform(action: () => Promise<void>) {
    if (inFlight.current) return;
    inFlight.current = true;
    setBusy(true);
    setError('');
    try { await action(); }
    catch (problem) { if (mounted.current) setError(errMessage(problem)); }
    finally {
      inFlight.current = false;
      if (mounted.current) setBusy(false);
    }
  }

  function openFolder(path?: string) {
    void perform(async () => {
      const folder = path ?? await api.pickProjectFolder();
      if (!folder || !mounted.current) return;
      const workspace = await useWorkspaceStore.getState().openProject(folder);
      if (mounted.current) onOpen(workspace.id);
    });
  }

  function save(event: FormEvent) {
    event.preventDefault();
    void perform(async () => {
      const previousDefault = workspaces.find(workspace => workspace.id === draft.id)?.default_model_id ?? '';
      const workspace = await useWorkspaceStore.getState().save({ ...draft, name: draft.name.trim(), project_path: draft.project_path.trim(), default_model_id: previousDefault });
      if (draft.default_model_id !== previousDefault) {
        if (mounted.current) setDraft({ ...draft, id: workspace.id });
        await api.selectModel({ scope: 'workspace', workspace_id: workspace.id, session_id: '', model_id: draft.default_model_id });
        await useWorkspaceStore.getState().refresh();
      }
      if (mounted.current) onOpen(workspace.id);
    });
  }

  return <div className="space-y-3">
    <Button className="w-full justify-start" disabled={busy} onClick={() => openFolder()}><FolderOpen aria-hidden="true" />选择文件夹…</Button>
    {candidate && <button type="button" disabled={busy} className="ns-project-candidate" onClick={() => openFolder(candidate)}><Folder size={16} aria-hidden="true" /><span><strong>打开检测到的项目</strong><small title={candidate}>{candidate}</small></span></button>}
    {busy && <p role="status" className="text-xs text-text-2">正在打开项目…</p>}
    {error && <p role="alert" className="ns-form-error">{error}</p>}
    <details className="ns-record-detail">
      <summary>工作区设置</summary>
      <form onSubmit={save} className="ns-detail-body grid gap-3">
        <div className="flex items-end gap-2">
          <label className="min-w-0 flex-1 text-xs">当前工作区<select className="ns-workspace-select mt-1" value={draft.id ?? ''} disabled={busy} onChange={event => { setError(''); setDraft(workspaces.find(item => item.id === event.target.value) ?? EMPTY); }}>
            <option value="">新工作区</option>{workspaces.map(workspace => <option key={workspace.id} value={workspace.id}>{workspace.name}</option>)}
          </select></label>
          <Button type="button" variant="outline" disabled={busy} onClick={() => { setDraft(EMPTY); setError(''); }}><Plus size={14} />新建</Button>
        </div>
        <label className="text-xs">工作区名称<Input required maxLength={85} value={draft.name} disabled={busy} className="mt-1" onChange={event => setDraft({ ...draft, name: event.target.value })} /></label>
        <label className="text-xs">工作区项目目录<Input value={draft.project_path} disabled={busy} placeholder="可选 · 本机绝对路径" className="mt-1" onChange={event => setDraft({ ...draft, project_path: event.target.value })} /></label>
        <label className="text-xs">工作区默认模型<select className="ns-workspace-select mt-1" value={draft.default_model_id} disabled={busy} onChange={event => setDraft({ ...draft, default_model_id: event.target.value })}>
          <option value="">使用全局默认</option>
          {models.filter(model => model.enabled !== false).map(model => <option key={model.id} value={model.id}>{model.name || model.model_name}</option>)}
          {draft.default_model_id && !models.some(model => model.id === draft.default_model_id && model.enabled !== false) && <option value={draft.default_model_id}>当前模型不可用</option>}
        </select></label>
        <Button type="submit" disabled={busy || !draft.name.trim()}>{draft.id ? '保存工作区' : '创建并切换'}</Button>
      </form>
    </details>
  </div>;
}
