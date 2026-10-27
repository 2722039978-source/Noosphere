import { useEffect, useMemo, useRef, useState } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { ArrowRight, ArrowUpRight, ChevronDown, Cloud, Grid2X2, List, Monitor, MoreHorizontal, Plus, Search, X } from 'lucide-react';
import { useBackend } from '@/hooks/useBackend';
import { useWorkspaceStore } from '@/stores/workspace-store';
import { api, errMessage, type ModelConfig } from '@/lib/api';
import { useChatStore, useChatPersistence } from '@/features/chat/chat-store';
import { activeWorkspaceSession } from '@/features/chat/chat-backup';
import { ModelSectionNav } from './ModelSectionNav';
import { ModelIdentityIcon } from './ModelIdentityIcon';
import { ModelIdentitySummary } from './ModelIdentitySummary';
import { resolveModelIdentity } from './model-identity';
import { modelStatus } from './model-status';
import { hubGenerationText, loadHubPerformance, type HubPerformance } from './hub-performance';
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator } from '@/components/ui/dropdown-menu';
import { modelEngineering } from '@/lib/model-engineering-client';
import type { EnvironmentSnapshot, ModelDiscovery, ModelRevision } from '@/lib/model-engineering-types';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';
import { WorkspaceSignal } from '@/components/WorkspaceSignal';
import { ModelForm } from '@/features/gateway/ModelForm';
import { JsonView, LabError } from './shared';
import { HardwarePanel } from './HardwarePanel';
import { readHardware, hardwareCapacity } from './hardware-presentation';
import { filterHubModels, matchingRevision, modelDeployment, primaryHubModel, type DeploymentFilter } from './hub-presentation';
import './model-hub.css';

/** Gateway configs are immediately usable; optional identity and hardware never gate chat. */
export function ModelHubPage() {
  const location = useLocation();
  const { models: configs, modelsReady, modelsError, loadModels, deleteModel } = useBackend();
  const workspaceID = useWorkspaceStore(state => state.activeId);
  const sessions = useChatStore(state => state.sessions), activeID = useChatStore(state => state.activeId);
  const chatReady = useChatPersistence(state => state.ready), selectionPending = useChatStore(state => state.modelSelection);
  const currentSession = activeWorkspaceSession(sessions, activeID, workspaceID);
  const sessionBusy = currentSession?.messages.some(message => message.streaming);
  const workspaceDefault = useWorkspaceStore(state => state.workspaces.find(w => w.id === state.activeId)?.default_model_id ?? '');
  const [revisions, setRevisions] = useState<ModelRevision[]>([]), [environment, setEnvironment] = useState<EnvironmentSnapshot | null>(null);
  const [revisionCursor, setRevisionCursor] = useState('');
  const [metadataError, setMetadataError] = useState<unknown>(), [selectedID, setSelectedID] = useState('');
  const [filter, setFilter] = useState<DeploymentFilter>('all'), [query, setQuery] = useState('');
  const [sort, setSort] = useState<'name' | 'provider'>('name'), [view, setView] = useState<'list' | 'grid'>('list');
  const [addOpen, setAddOpen] = useState(false), [detailsOpen, setDetailsOpen] = useState(false);
  const [editing, setEditing] = useState(false), [deleting, setDeleting] = useState<ModelConfig>();
  const [actionError, setActionError] = useState(''), [actionStatus, setActionStatus] = useState(''), [busy, setBusy] = useState(false);
  const [checks, setChecks] = useState<Record<string,string>>({});
  const checkEpoch = useRef<Record<string,number>>({});
  const [performanceState, setPerformanceState] = useState<{key:string; data:HubPerformance|null; loading:boolean; error:string}>({key:'',data:null,loading:false,error:''});
  const returnFocus = useRef<HTMLElement | null>(null), cancelDelete = useRef<HTMLButtonElement>(null), addTrigger = useRef<HTMLButtonElement>(null);
  const formGeneration = useRef(0), deleteGeneration = useRef(0);
  const preferred = primaryHubModel(configs, workspaceDefault);
  const model = currentSession?.modelId ? configs.find(item => item.id === currentSession.modelId) : preferred;
  const detailModel = configs.find(item => item.id === selectedID) ?? model;
  const revision = model ? matchingRevision(model, revisions) : undefined;
  const performanceKey = `${workspaceID}:${revision?.id ?? ''}`;
  const performance = performanceState.key === performanceKey ? performanceState.data : null;
  const performanceLoading = Boolean(revision && (performanceState.key !== performanceKey || performanceState.loading));
  const performanceError = performanceState.key === performanceKey ? performanceState.error : '';
  const identity = model ? resolveModelIdentity(model) : null;
  const status = model ? modelStatus(model, checks[model.id],Boolean(sessionBusy && currentSession?.modelId === model.id)) : null;
  const visible = useMemo(() => filterHubModels(configs, filter, query, sort), [configs, filter, query, sort]);
  const hardware = readHardware(environment?.hardware);
  const epoch = useRef(0);
  useEffect(() => { void loadModels(); }, [loadModels]);
  useEffect(() => {
    const controller = new AbortController();
    setPerformanceState({key:performanceKey,data:null,loading:Boolean(revision),error:''});
    if (revision) void loadHubPerformance(workspaceID,revision,controller.signal)
      .then(data => {if(!controller.signal.aborted)setPerformanceState({key:performanceKey,data,loading:false,error:''});})
      .catch(error => {if(!controller.signal.aborted)setPerformanceState({key:performanceKey,data:null,loading:false,error:errMessage(error)});});
    return () => controller.abort();
  }, [workspaceID,revision,performanceKey]);
  useEffect(() => { if (chatReady && currentSession && !selectionPending) void useChatStore.getState().syncModel(currentSession.id); }, [chatReady, currentSession?.id, workspaceID, selectionPending]);
  const restoreFocus = (event: Event) => { event.preventDefault(); (returnFocus.current?.isConnected ? returnFocus.current : addTrigger.current)?.focus({preventScroll:true}); };
  const closeDetails = () => {formGeneration.current++;setDetailsOpen(false);};
  const closeAdd = () => {formGeneration.current++;setAddOpen(false);};
  const closeDelete = () => {deleteGeneration.current++;setDeleting(undefined);};
  const openDetails = (item?: ModelConfig, edit = false, trigger?: HTMLElement | null) => { if(trigger) returnFocus.current=trigger; formGeneration.current++;setSelectedID(item?.id ?? ''); setEditing(edit); setDetailsOpen(true); };
  const clearCheck = (id:string) => {checkEpoch.current[id]=(checkEpoch.current[id]??0)+1;setChecks(old=>{const next={...old};delete next[id];return next;});};
  const useModel = async (item: ModelConfig) => {
    if (busy || selectionPending || item.enabled === false || sessionBusy || !chatReady) return;
    setActionError(''); setActionStatus('');
    const store=useChatStore.getState();
    const target = activeWorkspaceSession(store.sessions,store.activeId,workspaceID);
    if (target) store.setActive(target.id);
    else store.newSession();
    if (await useChatStore.getState().setModel(item.id)) {setChecks(old=>({...old,[item.id]:'已验证 · 连接成功'}));setActionStatus(`当前会话已切换到 ${item.name || item.model_name}`);}
    else {clearCheck(item.id);setActionError(useChatStore.getState().modelSelectionError || '模型切换未完成，请重试');}
  };
  const setDefault = async (item: ModelConfig, scope:'workspace'|'global') => {
    if(busy)return;setBusy(true);setActionError('');setActionStatus('');
    try { await api.selectModel({scope,workspace_id:workspaceID,session_id:'',model_id:item.id});setActionStatus(`${scope==='workspace'?'工作区':'全局'}默认模型已更新；已有会话绑定保持不变`);await Promise.all([loadModels(),useWorkspaceStore.getState().refresh()]); }
    catch(error){setActionError(errMessage(error));}finally{setBusy(false);}
  };
  const testConnection = async (item: ModelConfig) => {
    if(busy)return;setBusy(true);setChecks(old=>({...old,[item.id]:'正在测试连接…'}));
    const generation=checkEpoch.current[item.id]??0;
    try {const result=await api.testModel(item.id);if(generation===(checkEpoch.current[item.id]??0))setChecks(old=>({...old,[item.id]:result.status==='ok'?'已验证 · 连接成功':`连接失败：${result.error || '请检查服务、权限与模型配置'}`}));}
    catch(error) {if(generation===(checkEpoch.current[item.id]??0))setChecks(old=>({...old,[item.id]:`连接失败：${errMessage(error)}`}));}finally{setBusy(false);}
  };
  const removeModel = async () => {
    if(!deleting||busy)return;setBusy(true);setActionError('');
    const generation=deleteGeneration.current,target=deleting;
    try {await deleteModel(target.id);clearCheck(target.id);if(generation===deleteGeneration.current)closeDelete();setActionStatus('模型配置已删除；已有会话将明确显示模型不可用');}
    catch(error){setActionError(errMessage(error));}finally{setBusy(false);}
  };
  useEffect(() => {
    const generation = ++epoch.current;
    void modelEngineering({ operation: 'list_models', limit: 100 }).then(result => { if (epoch.current === generation) { setRevisions(result.models); setRevisionCursor(result.next_cursor); } }).catch(error => { if (epoch.current === generation) setMetadataError(error); });
    void modelEngineering({ operation: 'environment' }).then(result => { if (epoch.current === generation) setEnvironment(result.environment); }).catch(error => { if (epoch.current === generation) setMetadataError(error); });
    return () => { epoch.current++; };
  }, []);
  useEffect(() => {
    const search = document.getElementById('hub-model-search');
    if (location.hash === '#hub-model-search') search?.focus();
    const shortcut = (event: KeyboardEvent) => {
      if (event.key !== '/' || event.ctrlKey || event.metaKey || event.altKey || (event.target as HTMLElement).closest('input,textarea,select,[contenteditable=true],[role=dialog]')) return;
      event.preventDefault(); search?.focus();
    };
    window.addEventListener('keydown', shortcut); return () => window.removeEventListener('keydown', shortcut);
  }, [location.hash]);
  const loadMoreRevisions = async () => {
    const generation = epoch.current;
    const result = await modelEngineering({ operation: 'list_models', cursor: revisionCursor, limit: 100 });
    if (epoch.current !== generation) return;
    setRevisions(old => [...old, ...result.models.filter(item => !old.some(existing => existing.id === item.id))]);
    setRevisionCursor(result.next_cursor);
  };
  const counts = { all: configs.length, local: configs.filter(m => modelDeployment(m) === 'local').length, api: configs.filter(m => modelDeployment(m) === 'api').length };
  const formTicket=formGeneration.current, editedID=detailModel?.id;
  const missingPerformanceReason = !model ? '尚未配置模型；暂无测评数据。' : model.provider !== 'ollama' ? '此接入尚无原生测评适配；暂无测评数据。' : revision ? '此精确版本暂无真实测评数据；可在 Model Lab 发起测评。' : '尚未注册精确模型版本；暂无测评数据。';
  return <div className="ns-hub-scroll"><div className="ns-model-hub">
    <div className="ns-hub-heading"><p className="ns-hub-breadcrumb"><img src="/noosphere-mark.png" alt="" /> <span>›</span> 工作台 <span>/</span> MODULE 07 <span>/</span> MODEL HUB</p><h1>Model Hub</h1><p>本地模型版本档案 · 复用 Gateway 连接，保留精确版本与部署证据</p></div>
    <div className="ns-hub-orbit"><WorkspaceSignal /></div>
    <ModelSectionNav variant="hub" />
    {actionError && <p role="alert" className="ns-hub-inline-error">{actionError}</p>}
    {(actionStatus || selectionPending) && <p role="status" className="ns-hub-evidence-note">{selectionPending ? '正在验证模型连接，确认后切换目标会话…' : actionStatus}</p>}
    <section className="ns-hub-feature" aria-label="主模型">
      <div className="ns-hub-feature-main"><span className="ns-hub-ordinal">01</span><div className="ns-hub-feature-copy"><span className="ns-hub-eyebrow">MAIN MODEL</span><h2>{model?.name || model?.model_name || (currentSession?.modelId ? '当前会话模型不可用' : modelsReady ? '添加你的第一个模型' : '正在读取模型连接…')}</h2>
        <div className="ns-hub-tags">{model ? <><span>{identity?.family || model.provider}</span>{(revision?.parameter_size || model.metadata?.parameter_size) && <span>{revision?.parameter_size || model.metadata?.parameter_size}</span>}{(revision?.quantization || model.metadata?.quantization) && <span>{revision?.quantization || model.metadata?.quantization}</span>}<span>{identity?.label}</span><span className="ns-hub-default">{currentSession?.modelId === model.id ? '当前会话' : model.id === workspaceDefault ? '工作区默认' : model.is_default ? '全局默认' : '已配置'}</span></> : <span>GATEWAY / MODEL ACCESS</span>}</div>
        <p className="ns-hub-model-description">{model ? `${model.name || model.model_name} · 通过 ${model.provider} 连接，选择后即可开始对话。` : '连接本机推理服务或 API，让模型进入你的工作空间。'}</p>
        <div className="ns-hub-primary-actions">{model && model.enabled !== false ? <Link className="ns-hub-primary" to="/chat"><ArrowRight size={17} />开始对话</Link> : model ? <button className="ns-hub-primary" disabled title="此模型连接已停用，请在 Gateway 启用">连接已停用</button> : <button className="ns-hub-primary" onClick={event => {returnFocus.current=event.currentTarget;formGeneration.current++;setAddOpen(true);}}><Plus size={17} />添加模型</button>}<button className="ns-hub-more" onClick={event => openDetails(model,false,event.currentTarget)} aria-label="查看主模型详情"><MoreHorizontal size={19} /></button></div>
        <dl className="ns-hub-specs"><div><dt>CONTEXT</dt><dd title="当前部署上下文；不是模型架构的理论上限">{model?.context_length ? `${(model.context_length/1024).toLocaleString()}K` : '未提供'}</dd></div><div><dt>PARAMETERS</dt><dd>{revision?.parameter_size || model?.metadata?.parameter_size || '未提供'}</dd></div><div><dt>QUANTIZATION</dt><dd>{revision?.quantization || model?.metadata?.quantization || (identity?.deployment==='api'?'不适用':'未提供')}</dd></div><div><dt>STATUS</dt><dd className="ns-hub-config-status" title={status?.detail} data-disabled={model?.enabled === false} data-state={status?.state}>{status?.label || '待配置'}</dd></div></dl>
      </div></div>
      <aside className="ns-hub-feature-metrics"><div><h3>PERFORMANCE</h3><dl><div><dd className={!performance ? "ns-hub-metric-missing" : undefined}>{performanceLoading ? "正在读取…" : performanceError ? "数据读取失败" : hubGenerationText(performance)}</dd><dt>生成速度{performance ? "（单样本）" : ""}</dt></div><div><dd className="ns-hub-metric-missing" title={performance?.ttftMissingReason}>{performance ? "未采集" : "暂无测评数据"}</dd><dt>首 Token 延迟</dt></div></dl><p className="ns-hub-metric-note" role={performanceError ? "alert" : undefined}>{performanceError ? `性能记录读取失败：${performanceError}` : performanceLoading ? "正在读取此工作区的真实测评记录。" : performance ? <>{performance.note} <Link to={`/evaluation-reports/${encodeURIComponent(performance.reportID)}`}>查看报告 ↗</Link><br/>{performance.ttftMissingReason}</> : missingPerformanceReason}</p></div><div><h3>HARDWARE</h3><dl><div><dd className={!hardware?.gpus[0]?.total_vram_bytes || identity?.deployment==='api'?'ns-hub-metric-missing':undefined}>{identity?.deployment==='api'?'不适用':hardware?.gpus[0]?.total_vram_bytes ? <>{(hardware.gpus[0].total_vram_bytes / 1024 ** 3).toFixed(1)} <small>GiB</small></> : '未采集'}</dd><dt>本机显存总容量</dt></div><div><dd className={!hardware?.memory.installed_bytes || identity?.deployment==='api'?'ns-hub-metric-missing':undefined}>{identity?.deployment==='api'?'不适用':hardware?.memory.installed_bytes ? <>{(hardware.memory.installed_bytes / 1024 ** 3).toFixed(0)} <small>GiB</small></> : '未采集'}</dd><dt>本机已安装内存</dt></div></dl>{identity?.deployment==='api'&&<p className="ns-hub-metric-note">API 服务不提供本地模型硬件指标。</p>}</div><button onClick={event => openDetails(model,false,event.currentTarget)}>VIEW DETAILS <ArrowUpRight size={13} /></button></aside>
    </section>
    <section className="ns-hub-catalog" aria-labelledby="hub-catalog-title"><div className="ns-hub-catalog-toolbar"><h2 id="hub-catalog-title">使用模型</h2><div className="ns-hub-filters" aria-label="部署方式筛选">{([['all', '全部'], ['local', '本地'], ['api', 'API']] as const).map(([value, label]) => <button key={value} aria-pressed={filter === value} onClick={() => setFilter(value)}>{label}<span>{counts[value]}</span></button>)}</div>
      <label className="ns-hub-search"><Search size={17} aria-hidden="true" /><input id="hub-model-search" value={query} onChange={e => setQuery(e.target.value)} placeholder="搜索模型名称或协议…" aria-label="搜索模型" />{query && <button aria-label="清除搜索" onClick={() => setQuery('')}><X size={14} /></button>}</label>
      <label className="ns-hub-sort"><span className="sr-only">模型排序</span><select aria-label="模型排序" value={sort} onChange={e => setSort(e.target.value as 'name' | 'provider')}><option value="name">按模型名称</option><option value="provider">按接入协议</option></select><ChevronDown size={13} /></label>
      <div className="ns-hub-view" aria-label="模型视图"><button aria-label="网格视图" aria-pressed={view === 'grid'} onClick={() => setView('grid')}><Grid2X2 size={18} /></button><button aria-label="列表视图" aria-pressed={view === 'list'} onClick={() => setView('list')}><List size={18} /></button></div><button ref={addTrigger} className="ns-hub-add" onClick={event => {returnFocus.current=event.currentTarget;formGeneration.current++;setAddOpen(true);}}><Plus size={17} />添加模型</button></div>
      {modelsError && <p className="ns-hub-inline-error" role="alert">模型连接读取失败：{modelsError} <button onClick={() => void loadModels()}>重试</button></p>}
      <div className={`ns-hub-models is-${view}`}>{view === 'list' && <div className="ns-hub-table-head"><span>模型名称 / 别名</span><span>类型</span><span>部署方式</span><span>来源 / 地址</span><span>参数 / 量化</span><span>状态</span><span>操作</span></div>}
        {visible.map(item => <HubModelRow key={item.id} model={item} revision={matchingRevision(item, revisions)} defaultLabel={item.id === workspaceDefault ? "工作区默认" : item.is_default ? "全局默认" : ""} selected={currentSession?.modelId === item.id} disabled={busy || !!selectionPending || !!sessionBusy || !chatReady} running={Boolean(sessionBusy && currentSession?.modelId===item.id)} check={checks[item.id]} onUse={() => void useModel(item)} onDetails={trigger => openDetails(item,false,trigger)} onEdit={trigger => openDetails(item,true,trigger)} onTest={() => void testConnection(item)} onDefault={scope => void setDefault(item,scope)} onDelete={trigger => {returnFocus.current=trigger;deleteGeneration.current++;setDeleting(item);}} />)}
        {!visible.length && <div className="ns-hub-empty" role="status">{!modelsReady ? '正在读取 Gateway 模型连接…' : configs.length ? '没有符合筛选条件的模型。' : '尚未配置模型连接。添加模型后即可在这里选择并开始对话。'}{configs.length > 0 && <button onClick={() => { setFilter('all'); setQuery(''); }}>清除筛选</button>}</div>}
      </div><p className="ns-hub-evidence-note">“已配置”表示保存了连接；“已验证”表示最近一次连接成功；“当前使用”表示会话绑定；仅实际推理时显示“正在运行”。没有对应版本的性能记录时显示“暂无测评数据”，API 硬件指标显示“不适用”。</p>
    </section>
    <Dialog open={addOpen} onOpenChange={open=>{if(!open)closeAdd();}}><DialogContent onCloseAutoFocus={restoreFocus} className="ns-hub-dialog" aria-describedby="hub-add-description"><DialogTitle>添加模型</DialogTitle><DialogDescription id="hub-add-description">保存 Gateway 连接，在当前工作台直接选择使用。</DialogDescription>{addOpen && <ModelForm key={formTicket} onSaved={() => {if(formTicket===formGeneration.current)closeAdd();}} onCancel={closeAdd} />}</DialogContent></Dialog>
    <Dialog open={detailsOpen} onOpenChange={open=>{if(!open)closeDetails();}}><DialogContent onCloseAutoFocus={restoreFocus} className="ns-hub-dialog ns-hub-drawer" aria-describedby="hub-details-description"><DialogTitle>{editing ? '编辑模型' : detailModel?.name || '模型与本机详情'}</DialogTitle><DialogDescription id="hub-details-description">模型档案 / {editing ? '编辑连接配置' : '连接、模型身份与本机硬件；显式发现操作才会读取本地模型身份'}</DialogDescription>{detailsOpen && (editing && detailModel ? <ModelForm key={`${detailModel.id}-${formTicket}`} initial={detailModel} onSaved={() => {if(editedID)clearCheck(editedID);if(formTicket===formGeneration.current)closeDetails();}} onCancel={closeDetails}/> : <><ModelIdentityDetails key={detailModel?.id ?? 'none'} configs={configs} initialID={detailModel?.id ?? ''} revisions={revisions} revisionCursor={revisionCursor} onLoadMore={loadMoreRevisions} onRegistered={item => setRevisions(old => [item, ...old.filter(m => m.id !== item.id)])} /><LabError error={metadataError} /><HardwarePanel environment={environment} load={false} /></>)}</DialogContent></Dialog>
    <Dialog open={!!deleting} onOpenChange={open => {if(!open)closeDelete();}}><DialogContent role="alertdialog" onCloseAutoFocus={restoreFocus} onOpenAutoFocus={event => {event.preventDefault();cancelDelete.current?.focus();}}><DialogTitle>删除模型配置？</DialogTitle><DialogDescription>将删除「{deleting?.name}」的连接配置。使用该配置的会话需要重新选择模型。</DialogDescription><div className="flex justify-end gap-2"><Button ref={cancelDelete} variant="outline" onClick={closeDelete}>取消</Button><Button disabled={busy} onClick={() => void removeModel()}>确认删除</Button></div></DialogContent></Dialog>
  </div></div>;
}

function HubModelRow({ model, revision, defaultLabel, selected, disabled, running, check, onUse, onDetails, onEdit, onTest, onDefault, onDelete }: { model: ModelConfig; revision?: ModelRevision; defaultLabel: string; selected: boolean; disabled: boolean; running:boolean; check?:string; onUse:()=>void; onDetails:(trigger:HTMLElement|null)=>void; onEdit:(trigger:HTMLElement|null)=>void; onTest:()=>void; onDefault:(scope:'workspace'|'global')=>void; onDelete:(trigger:HTMLElement|null)=>void }) {
  const more=useRef<HTMLButtonElement>(null);
  const openingDialog=useRef(false);
  const openDialog=(open:(trigger:HTMLElement|null)=>void)=>{openingDialog.current=true;open(more.current);};
  const deployment = modelDeployment(model), Icon = deployment === 'local' ? Monitor : Cloud;
  const identity=resolveModelIdentity(model),status=modelStatus(model,check,running);
  let endpoint = model.base_url; try { endpoint = new URL(model.base_url).host; } catch { /* retain configured address */ }
  return <article className="ns-hub-model-row" data-selected={selected}><button className="ns-hub-model-name" onClick={event=>onDetails(event.currentTarget)} aria-label={`查看模型 ${model.name || model.model_name}`}><span className="ns-hub-model-icon"><ModelIdentityIcon model={model}/></span><span><strong>{model.name || model.model_name}</strong><small>{defaultLabel && <b>{defaultLabel}</b>}{model.model_name}</small></span></button><span className="ns-hub-row-protocol" title={identity.vendor}>{identity.family}</span><span><span className="ns-hub-deployment" data-local={deployment === 'local'}><Icon size={17} />{deployment === 'local' ? '本地' : deployment === 'api' ? 'API' : '未知'}</span></span><span className="ns-hub-endpoint"><span>{model.provider}</span><small title={model.base_url}>{endpoint}</small></span><span className="ns-hub-row-identity">{revision?.parameter_size || model.metadata?.parameter_size || '未提供'}<small>{revision?.quantization || model.metadata?.quantization || (deployment==='api'?'不适用':'未提供')}</small></span><span className="ns-hub-row-status" data-state={status.state} data-disabled={model.enabled === false}>{status.label}<small role={check ? 'status' : undefined}>{selected ? '当前会话 · ' : ''}{check || status.detail}</small></span><span className="ns-hub-row-actions"><button disabled={disabled || model.enabled===false || selected} onClick={onUse}>{selected?'当前使用':'使用'}</button><DropdownMenu onOpenChange={open=>{if(open)openingDialog.current=false;}}><DropdownMenuTrigger asChild><button ref={more} aria-label={`${model.name || model.model_name} 更多操作`}><MoreHorizontal size={17}/></button></DropdownMenuTrigger><DropdownMenuContent align="end" onCloseAutoFocus={event=>{if(openingDialog.current)event.preventDefault();}}><DropdownMenuItem onSelect={()=>openDialog(onDetails)}>模型详情</DropdownMenuItem><DropdownMenuItem onSelect={()=>openDialog(onEdit)}>编辑配置</DropdownMenuItem><DropdownMenuItem disabled={disabled||model.enabled===false} onSelect={onTest}>测试连接</DropdownMenuItem><DropdownMenuSeparator/><DropdownMenuItem disabled={disabled||model.enabled===false} onSelect={()=>onDefault('workspace')}>设为工作区默认</DropdownMenuItem><DropdownMenuItem disabled={disabled||model.enabled===false} onSelect={()=>onDefault('global')}>设为全局默认</DropdownMenuItem><DropdownMenuSeparator/><DropdownMenuItem onSelect={()=>openDialog(onDelete)}>删除模型</DropdownMenuItem></DropdownMenuContent></DropdownMenu></span></article>;
}

function ModelIdentityDetails({ configs, initialID, revisions, revisionCursor, onLoadMore, onRegistered }: { configs: ModelConfig[]; initialID: string; revisions: ModelRevision[]; revisionCursor: string; onLoadMore: () => Promise<void>; onRegistered: (model: ModelRevision) => void }) {
  const [id, setID] = useState(initialID), [discovery, setDiscovery] = useState<ModelDiscovery | null>(null), [busy, setBusy] = useState(false), [error, setError] = useState<unknown>();
  const epoch = useRef(0), selected = useRef(id); selected.current = id;
  useEffect(() => () => { epoch.current++; }, []);
  const config = configs.find(item => item.id === id);
  const discover = async () => { if (busy || !id) return; const generation = epoch.current, requestID = id; setBusy(true); setError(undefined); setDiscovery(null); try { const result = await modelEngineering({ operation: 'discover', gateway_model_id: id }); if (generation === epoch.current && selected.current === requestID) setDiscovery(result); } catch (e) { if (generation === epoch.current) setError(e); } finally { if (generation === epoch.current) setBusy(false); } };
  const register = async (hash: string) => { if (busy) return; const generation = epoch.current; setBusy(true); setError(undefined); try { const result = await modelEngineering({ operation: 'register_model', gateway_model_id: id, discovered_identity_hash: hash }); if (generation === epoch.current) onRegistered(result); } catch (e) { if (generation === epoch.current) setError(e); } finally { if (generation === epoch.current) setBusy(false); } };
  const more = async () => { if (busy || !revisionCursor) return; const generation = epoch.current; setBusy(true); setError(undefined); try { await onLoadMore(); } catch (e) { if (generation === epoch.current) setError(e); } finally { if (generation === epoch.current) setBusy(false); } };
  return <section className="ns-hub-identity-details">{config && <ModelIdentitySummary model={config}/>}<div className="ns-lab-section"><h2>模型版本档案</h2><Link to="/gateway">管理连接与凭据 <ArrowUpRight size={13} /></Link></div><LabError error={error} /><label className="ns-lab-field">Gateway 连接<select value={id} disabled={busy} onChange={e => { setID(e.target.value); setDiscovery(null); }}>{configs.map(item => <option key={item.id} value={item.id}>{item.name} · {item.provider}</option>)}</select></label>{config && <p className="ns-lab-muted">{config.model_name} · {config.base_url}</p>}{config?.provider === 'ollama' ? <Button disabled={busy || config.enabled === false} onClick={() => void discover()}>{busy ? '正在读取模型身份…' : '发现模型版本'}</Button> : <p className="ns-lab-notice">当前原生测评仅支持 Ollama；此连接可用于对话，尚无精确身份与驻留验证适配。</p>}{discovery && <div className="ns-lab-discovery"><p>Ollama {discovery.engine_version}</p>{!discovery.models.length && <p>未发现本地模型。</p>}{discovery.models.map(item => <article className="ns-lab-record" key={item.metadata_hash}><div className="ns-lab-section"><h3>{item.name}</h3><Button size="sm" disabled={busy || !item.local_verified || !!item.ineligibility_reason} onClick={() => void register(item.metadata_hash)}>注册此版本</Button></div><p>{item.parameter_size || '参数量未知'} · {item.quantization || '量化未知'} · {hardwareCapacity(item.size_bytes)}</p>{item.ineligibility_reason && <p>{item.ineligibility_reason}</p>}<JsonView value={item} title="模型身份、能力及限制" /></article>)}</div>}<details className="ns-lab-details"><summary>已注册的不可变版本（{revisions.length}）</summary>{revisions.map(item => <JsonView key={item.id} value={item} title={item.model_name} />)}{!revisions.length && <p>尚无模型版本档案。</p>}{revisionCursor && <Button variant="outline" disabled={busy} onClick={() => void more()}>{busy ? '正在加载版本…' : '加载更多版本'}</Button>}</details></section>;
}
