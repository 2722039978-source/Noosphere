import { useEffect, useRef, useState } from "react";
import { Plug, Plus, Trash2, Zap } from "lucide-react";
import { Link } from 'react-router-dom';
import { useBackend } from "@/hooks/useBackend";
import { Button } from "@/components/ui/button";
import { Surface } from "@/components/ui/surface";
import { PageHeader } from "@/components/ui/page-header";
import { Badge } from "@/components/ui/badge";
import { EmptyState } from "@/components/ui/feedback";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';
import { ModelForm } from "./ModelForm";
import { api, errMessage, type ModelConfig } from "@/lib/api";
import { cn } from "@/lib/utils";
import {ModelIdentityIcon} from '@/features/model-lab/ModelIdentityIcon';
import { resolveModelIdentity } from '@/features/model-lab/model-identity';
import { modelStatus } from '@/features/model-lab/model-status';
import { useChatStore, useChatPersistence } from '@/features/chat/chat-store';
import { activeWorkspaceSession } from '@/features/chat/chat-backup';
import { useWorkspaceStore } from '@/stores/workspace-store';
import './gateway-dialogs.css';


/** Provider → 模块色 */
function providerColor(p: string): string {
  if (p.includes("claude") || p.includes("anthropic")) return "text-acc-devops";
  if (p.includes("gemini") || p.includes("google")) return "text-acc-codelens";
  if (p.includes("ollama")) return "text-acc-nebula";
  return "text-acc-gateway";
}

export function GatewayPage() {
  const backend = useBackend();
  const { models, deleteModel } = backend;
  const sessions = useChatStore(state => state.sessions);
  const activeId = useChatStore(state => state.activeId);
  const selectionPending = useChatStore(state => state.modelSelection);
  const persistenceReady = useChatPersistence(state => state.ready);
  const workspaceId = useWorkspaceStore(state => state.activeId);
  const workspaceDefault = useWorkspaceStore(state => state.workspaces.find(ws => ws.id === state.activeId)?.default_model_id);
  const session = activeWorkspaceSession(sessions, activeId, workspaceId);
  const sessionBusy = session?.messages.some(message => message.streaming);
  const [actionStatus, setActionStatus] = useState('');
  const [actionError, setActionError] = useState("");
  const [deleting, setDeleting] = useState<string | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<ModelConfig | null>(null);
  const [deleteError, setDeleteError] = useState('');
  const deleteInFlight = useRef(false);
  const deleteGeneration = useRef(0);
  const [showForm, setShowForm] = useState(false);
  const [editing, setEditing] = useState<ModelConfig>();
  const returnFocus = useRef<HTMLElement | null>(null);
  const addTrigger = useRef<HTMLButtonElement>(null);
  const cancelDelete = useRef<HTMLButtonElement>(null);
  const formGeneration = useRef(0);
  const openForm = (trigger: HTMLElement, model?: ModelConfig) => {
    returnFocus.current = trigger;
    formGeneration.current++;
    setEditing(model);
    setShowForm(!model);
  };
  const closeForm = () => {
    formGeneration.current++;
    setShowForm(false);
    setEditing(undefined);
  };
  const restoreFocus = (event: Event) => {
    event.preventDefault();
    const target = returnFocus.current;
    (target?.isConnected ? target : addTrigger.current)?.focus({ preventScroll: true });
  };
  const closeDelete = () => { deleteGeneration.current++; setDeleteTarget(null); };
  const remove = async () => {
    if (!deleteTarget || deleteInFlight.current) return;
    const target = deleteTarget;
    const generation = deleteGeneration.current;
    deleteInFlight.current = true;
    setDeleting(target.id); setDeleteError(''); setActionError(''); setActionStatus('');
    try {
      await deleteModel(target.id);
      invalidateCheck(target.id);
      if (generation === deleteGeneration.current) closeDelete();
      setActionStatus(`已删除 ${target.name} 的连接配置。受影响的会话需重新选择模型。`);
    } catch (error) {
      if (generation === deleteGeneration.current) setDeleteError(errMessage(error));
      else setActionError(`删除 ${target.name} 失败：${errMessage(error)}`);
    }
    finally { deleteInFlight.current = false; setDeleting(null); }
  };
  const [testing, setTesting] = useState<string | null>(null);
  const [switching, setSwitching] = useState(false);
  const [checks, setChecks] = useState<Record<string, { ok: boolean; message: string }>>({});
  const checkGeneration = useRef<Record<string, number>>({});
  function invalidateCheck(id: string) {
    checkGeneration.current[id] = (checkGeneration.current[id] ?? 0) + 1;
    setChecks(old => { const next = { ...old }; delete next[id]; return next; });
  }
  async function test(id: string) {
    if (testing) return;
    const generation = checkGeneration.current[id] ?? 0;
    setTesting(id);
    try {
      const result = await api.testModel(id);
      if (generation === (checkGeneration.current[id] ?? 0)) setChecks(old => ({ ...old, [id]: { ok: result.status === 'ok', message: result.status === 'ok' ? `已验证 · ${Math.round(result.latency_ms ?? 0)} ms` : connectionError(result.error || '未返回有效回复') } }));
    } catch (error) {
      if (generation === (checkGeneration.current[id] ?? 0)) setChecks(old => ({ ...old, [id]: { ok: false, message: connectionError(errMessage(error)) } }));
    }
    finally { setTesting(null); }
  }
  async function activate(model: ModelConfig, scope: 'session' | 'workspace' | 'global') {
    if (switching) return;
    setSwitching(true); setActionError(''); setActionStatus('');
    try {
      if (scope === 'session') {
        const store = useChatStore.getState();
        const target = activeWorkspaceSession(store.sessions, store.activeId, workspaceId);
        if (target) store.setActive(target.id);
        else store.newSession();
        if (!await useChatStore.getState().setModel(model.id)) throw new Error(useChatStore.getState().modelSelectionError || '模型切换未完成，请重试。');
        setActionStatus(`当前会话已切换到 ${model.name}。其他会话与默认设置保持不变。`);
      } else {
        await api.selectModel({ scope, workspace_id: workspaceId, session_id: '', model_id: model.id });
        setActionStatus(`${scope === 'global' ? '全局' : '工作区'}默认模型已更新为 ${model.name}，用于之后未绑定模型的新会话。`);
        await Promise.all([backend.loadModels(), useWorkspaceStore.getState().refresh()]);
      }
    }
    catch (error) { setActionError(errMessage(error)); }
    finally { setSwitching(false); }
  }

  useEffect(() => { void backend.loadModels(); }, [backend.loadModels]);


  return (
    <div className="flex-1 overflow-y-auto">
      <div className="ns-page-wrap">
        <PageHeader
          icon={<Zap size={20} />}
          accent="var(--acc-gateway)"
          title="Gateway"
          subtitle={`模型管理中心 · 大模型统一接入与配置（已接入 ${models.length} 个模型）`}
          actions={
            <Button ref={addTrigger} className="gap-1.5" aria-haspopup="dialog" aria-expanded={showForm} onClick={event => openForm(event.currentTarget)}>
              <Plus size={14} /> 添加模型
            </Button>
          }
        />

        <div className="ns-task-path"><strong>{models.length} 个模型配置</strong><span>/</span><span>全局默认：{models.find(model => model.is_default)?.name ?? '尚未设置'}</span><span>/</span><span>工作区默认：{workspaceDefault ? models.find(model => model.id === workspaceDefault)?.name ?? '模型不可用' : '继承全局默认'}</span></div>
        <p className="mb-4 text-xs text-text-3">当前会话：{session?.title ?? '尚未创建'} · {session?.modelId ? models.find(model => model.id === session.modelId)?.name ?? '模型不可用' : '首次请求时确定模型'}。“使用”只切换此会话；验证连接会发送一条简短推理请求。</p>
        {actionError && <p role="alert" className="ns-form-error">{actionError}</p>}
        {actionStatus && <p role="status" className="ns-result-status">{actionStatus} <Link to="/chat" className="underline underline-offset-4">打开 AI 对话</Link></p>}

        {models.length === 0 ? (
          <EmptyState
            icon={<Zap size={20} className="text-acc-gateway" />}
            title="尚未配置模型"
            description="支持 OpenAI 兼容接口、Claude、Gemini 与 Ollama，配置后即可启用 AI 对话。"
            action={
              <Button className="gap-1.5" aria-haspopup="dialog" onClick={event => openForm(event.currentTarget)}>
                <Plus size={14} /> 添加第一个模型
              </Button>
            }
          />
        ) : (
          <div className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,300px),1fr))] gap-4">
            {models.map((m) => {
              const color = providerColor(m.provider);
              const identity = resolveModelIdentity(m);
              const status = modelStatus(m, testing === m.id ? '正在验证' : checks[m.id]?.message, Boolean(sessionBusy && session?.modelId === m.id));
              return (
                <Surface key={m.id} className="ns-lift px-5 py-5">
                  <div className="flex items-start gap-3">
                    <span className={cn("flex size-10 shrink-0 items-center justify-center rounded-none bg-secondary", color)}>
                      <ModelIdentityIcon model={m}/>
                    </span>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2 text-[14px] font-medium">
                        <span className="truncate text-text-1">{m.name}</span>
                        {m.is_default && <Badge className="shrink-0 text-xs">全局默认</Badge>}
                        {m.id === session?.modelId && <Badge className="shrink-0 text-xs">当前会话</Badge>}
                      </div>
                      <div className="mt-1 text-xs text-text-3">{identity.family} · {identity.label}</div>
                    </div>
                    <button
                      onClick={event => { if (deleteInFlight.current) return; returnFocus.current = event.currentTarget; deleteGeneration.current++; setDeleteError(''); setDeleteTarget(m); }}
                      disabled={deleting !== null}
                      aria-haspopup="dialog"
                      aria-label={`删除模型 ${m.name}`}
                      title="删除模型"
                      className="cursor-pointer p-1 text-text-3 hover:text-err"
                    >
                      <Trash2 size={15} />
                    </button>
                  </div>

                  <div className="mt-4 flex flex-col gap-1.5 font-mono text-xs text-text-3">
                    <div className="flex items-center gap-2">
                      <Plug size={12} className="shrink-0" />
                      <span className="truncate">{m.base_url}</span>
                    </div>
                    <div>模型：{m.model_name || "—"}</div>
                  </div>

                  <div className="mt-4 flex items-center gap-2">
                    <Badge variant={status.state === 'failed' ? 'err' : status.state === 'verified' ? 'ok' : status.state === 'running' || status.state === 'checking' ? 'info' : 'secondary'} className="text-xs">{status.label}</Badge>
                    <span className="text-xs text-text-3">{status.detail}</span>
                  </div>
                  <div className="ns-provider-actions"><Button variant="outline" size="sm" disabled={testing !== null || m.enabled === false} onClick={() => void test(m.id)}>{testing === m.id ? '连接测试中…' : '测试连接'}</Button><Button variant="ghost" size="sm" aria-haspopup="dialog" onClick={event => openForm(event.currentTarget, m)}>编辑配置</Button><Button variant="outline" size="sm" disabled={!persistenceReady || switching || !!selectionPending || sessionBusy || m.enabled === false || session?.modelId === m.id} onClick={() => void activate(m, 'session')}>{session?.modelId === m.id ? '当前使用' : switching ? '验证中…' : '使用'}</Button>{!m.is_default && <Button variant="ghost" size="sm" disabled={switching || !!selectionPending || m.enabled === false} onClick={() => void activate(m, 'global')}>设为全局默认</Button>}{workspaceDefault !== m.id && <Button variant="ghost" size="sm" disabled={switching || !!selectionPending || m.enabled === false} onClick={() => void activate(m, 'workspace')}>设为工作区默认</Button>}</div>
                  {checks[m.id] && <p role={checks[m.id].ok ? 'status' : 'alert'} className={checks[m.id].ok ? 'ns-result-status' : 'ns-form-error'}>{checks[m.id].message}</p>}
                </Surface>
              );
            })}
          </div>
        )}
      </div>
      <Dialog open={showForm || Boolean(editing)} onOpenChange={open => { if (!open) closeForm(); }}>
        <DialogContent className={cn('ns-gateway-dialog', editing && 'ns-gateway-drawer')} onCloseAutoFocus={restoreFocus}>
          <DialogTitle>{editing ? '编辑模型接入' : '添加模型'}</DialogTitle>
          <DialogDescription>{editing ? '更新此连接配置。密钥留空会保留本机已保存的密钥。' : '添加本地模型或 API 连接。保存后可验证连接，再选择用于当前会话。'}</DialogDescription>
          {(showForm || editing) && (() => {
            const generation = formGeneration.current;
            const editedID = editing?.id;
            return <ModelForm key={`${editedID ?? 'new'}-${generation}`} initial={editing} onSaved={() => {
              if (editedID) invalidateCheck(editedID);
              if (generation === formGeneration.current) closeForm();
            }} onCancel={closeForm} />;
          })()}
        </DialogContent>
      </Dialog>
      <Dialog open={Boolean(deleteTarget)} onOpenChange={open => { if (!open) closeDelete(); }}>
        <DialogContent role="alertdialog" className="ns-gateway-dialog ns-gateway-confirm" aria-busy={deleting !== null} onCloseAutoFocus={restoreFocus} onOpenAutoFocus={event => { event.preventDefault(); cancelDelete.current?.focus({ preventScroll: true }); }}>
          <DialogTitle>删除模型配置？</DialogTitle>
          <DialogDescription>将删除「{deleteTarget?.name}」的连接配置。使用该配置的会话需重新选择模型。</DialogDescription>
          {deleteError && <p role="alert" className="ns-form-error">删除失败：{deleteError}。可重试或取消。</p>}
          <div className="ns-gateway-confirm-actions"><Button ref={cancelDelete} variant="outline" disabled={deleting !== null} onClick={closeDelete}>取消</Button><Button variant="destructive" disabled={deleting !== null} onClick={() => void remove()}>{deleting ? '删除中…' : '确认删除'}</Button></div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
function connectionError(message: string) {
  if (/401|403/.test(message)) return '连接失败：密钥无效或无访问权限，请编辑配置更新密钥后重试。';
  if (/402|insufficient.*balance/i.test(message)) return '连接失败：账户余额不足，请充值后重新测试。';
  if (/429/.test(message)) return '连接失败：请求过于频繁，请稍后重试。';
  if (/404/.test(message)) return '连接失败：接口或模型不存在，请检查地址与模型名称。';
  return `连接失败：${message}`;
}
