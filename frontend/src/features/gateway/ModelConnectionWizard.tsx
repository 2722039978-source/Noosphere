import { useEffect, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Box, FolderOpen, RefreshCw } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { api, errMessage, type LibraryChange, type LocalEngine, type LocalEngineAction, type ModelConfig, type ModelDiagnostic } from '@/lib/api';
import { useBackend } from '@/hooks/useBackend';
import { useWorkspaceStore } from '@/stores/workspace-store';
import { useGenerationStore } from '@/features/chat/chat-runtime';
import { ConnectionDiagnostics } from './ConnectionDiagnostics';
import { useEnginePreferences, useLocalEngines } from './local-engine';
import { localEngineName, localModelChoices, modelChoiceStatus, modelConnectionKey, modelNameKey, modelIsOnline, portableConflict, type ModelTarget } from './local-model-match';
import { ImportModels, LibraryModelDetails, RuntimeManager } from './ModelLibrary';
import { Input } from '@/components/ui/input';
import './model-connection.css';

const installation: Record<string, string> = { ollama: 'https://ollama.com/download', lm_studio: 'https://lmstudio.ai/download' };

async function discoveredModelId(key: string) {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(key));
  return 'local-' + Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, '0')).join('');
}

export function ModelConnectionWizard({ preferred, onUse, allowAutoStart = true, standalone = false, initialTarget }: {
  preferred?: ModelTarget & { id?: string };
  onUse: (model: ModelConfig) => void | Promise<void>;
  allowAutoStart?: boolean;
  standalone?: boolean;
  initialTarget?: string;
}) {
  const { models, modelsReady, addModel } = useBackend();
  const workspacePath = useWorkspaceStore(state => state.workspaces.find(workspace => workspace.id === state.activeId)?.project_path ?? '');
  const generating = useGenerationStore(state => state.active !== null);
  const [directory, setDirectory] = useState('');
  const discovery = useLocalEngines(directory || workspacePath, true);
  const [selectedKey, setSelectedKey] = useState(() => preferred ? modelConnectionKey(preferred) : initialTarget ?? '');
  const [search, setSearch] = useState('');
  const queryClient = useQueryClient();
  const library = useQuery({ queryKey: ['model-library'], queryFn: api.modelLibrary, staleTime: 30000, retry: false });
  const [working, setWorking] = useState('');
  const [auto, setAuto] = useState(true);
  const [result, setResult] = useState<ModelDiagnostic>();
  const [error, setError] = useState('');
  const [log, setLog] = useState<{ path: string; content: string }>();
  const inFlight = useRef(false);
  const mounted = useRef(false);
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);

  const choices = localModelChoices(models, discovery.engines, preferred?.id);
  const enabled = choices.filter(item => item.config?.enabled !== false);
  const selection = selectedKey ? choices.find(item => item.key === selectedKey) : enabled.find(item => modelIsOnline(item)) ?? enabled[0];
  const engine = selection?.engine;
  const custom = engine?.id === 'custom_llama';
  const entry = custom ? library.data?.models?.find(model => model.id === selection?.target.model_name) : undefined;
  const online = modelIsOnline(selection);
  const modelName = selection?.target.model_name;
  const sameEngineConflict = custom && engine.state === 'online' && !online ? engine : undefined;
  const canStart = Boolean(engine?.can_start ?? engine?.installed) && (!custom || entry?.status === 'ready');
  const conflict = online ? undefined : sameEngineConflict ?? portableConflict(discovery.engines, engine);
  const switching = Boolean(conflict?.managed && conflict.state === 'online');
  const waitingForOther = conflict?.state === 'starting';
  const externalConflict = Boolean(conflict && !switching && !waitingForOther);
  const needsStart = !!engine && !online;
  const actionLabel = needsStart ? switching ? '切换并启动' : '启动并使用' : standalone ? '开始对话' : '使用模型';
  const checking = discovery.isPending && discovery.isFetching;
  const conflictName = conflict?.models.find(model => model.loaded)?.name ?? (conflict && localEngineName(conflict));
  const matched = choices.filter(item => [item.name, item.target.model_name, item.engine?.name].some(value => value?.toLowerCase().includes(search.trim().toLowerCase())));

  async function perform(action: () => Promise<void>) {
    if (inFlight.current) return;
    inFlight.current = true;
    setError(''); setResult(undefined);
    try { await action(); }
    catch (problem) { if (mounted.current) setError(errMessage(problem)); }
    finally {
      inFlight.current = false;
      if (mounted.current) setWorking('');
    }
  }

  async function changeLibrary(change: LibraryChange) {
    setWorking('正在更新本机模型库…');
    const next = await api.changeModelLibrary(change);
    queryClient.setQueryData(['model-library'], next);
    await queryClient.invalidateQueries({ queryKey: ['local-engines'] });
    return next;
  }

  async function operate(target: LocalEngine, action: LocalEngineAction) {
    setWorking(action === 'stop' ? '正在停止模型…' : action === 'switch' ? '正在切换并加载模型…' : '正在启动模型…');
    const next = await discovery.operate(target, action, target.id === engine?.id ? modelName : undefined);
    if (action === 'stop') {
      const preferences = useEnginePreferences.getState();
      if (target.id !== 'custom_llama') preferences.revoke(target);
      else if (selection?.config) preferences.grant(selection.config, target, false);
    }
    if (action !== 'stop' && next.state !== 'online') throw new Error(next.error || '服务尚未就绪。请查看日志后重试。');
    return next;
  }

  async function useModel() {
    if (!selection || generating || !modelsReady || selection.config?.enabled === false) return;
    await perform(async () => {
      let liveEngine = engine;
      let liveModel = selection.model;
      if (custom && online) {
        const refreshed = await discovery.refetch();
        if (refreshed.error) throw refreshed.error;
        liveEngine = refreshed.data?.engines.find(item => item.id === 'custom_llama');
        liveModel = liveEngine?.models.find(item => item.id === modelName);
        if (!liveEngine?.managed || liveEngine.state !== 'online' || !liveModel?.loaded || liveModel.needs_restart) {
          throw new Error('当前模型运行状态已变化，请重新选择并启动。');
        }
      }
      if (engine && !online) {
        const started = await operate(engine, switching ? 'switch' : 'start');
        liveEngine = started;
        const requested = modelNameKey(selection.target.provider, selection.target.model_name);
        liveModel = started.models.find(item => modelNameKey(started.provider, item.id) === requested);
        if (!liveModel || custom && (!started.managed || !liveModel.loaded)) {
          throw new Error('服务已在线，但未发现所选模型。请刷新列表。');
        }
      }
      if (!mounted.current) return;
      setWorking('正在验证模型…');
      const latest = selection.config ? undefined : await api.getModels();
      const existing = selection.config ?? localModelChoices(latest?.models ?? [], []).find(item => item.key === selection.key)?.config;
      if (existing?.enabled === false) throw new Error('此模型已停用，可在高级管理中启用。');
      const base: ModelConfig = existing ?? {
        id: await discoveredModelId(selection.key), name: selection.name, ...selection.target, enabled: true, deployment: 'local',
      };
      const config: ModelConfig = custom ? { ...base, name: liveModel?.name || entry?.name || selection.name, context_length: entry?.context || entry?.suggested_context || base.context_length,
        metadata: { ...base.metadata, source: 'llama.cpp', architecture: entry?.architecture ?? base.metadata?.architecture,
          features: { ...base.metadata?.features, vision: liveModel?.vision == null ? undefined : liveModel.vision ? 'supported' : 'unsupported' } },
        protocol_options: { tools: false, ...base.protocol_options } }
        : !existing && liveModel?.vision != null ? { ...base, metadata: { source: engine?.id,
          features: { vision: liveModel.vision ? 'supported' : 'unsupported' } } } : base;
      const changed = !existing || custom && (config.name !== existing.name ||
        config.context_length !== existing.context_length || config.metadata?.source !== existing.metadata?.source ||
        config.metadata?.architecture !== existing.metadata?.architecture || config.metadata?.features?.vision !== existing.metadata?.features?.vision ||
        config.protocol_options?.tools !== existing.protocol_options?.tools);
      // Session binding performs its own inference check. The isolated trial has no binding.
      if (changed || !allowAutoStart) {
        const checked = changed ? await api.probeModel(config) : await api.testModel(config.id);
        if (!mounted.current) return;
        if (checked.status !== 'ok' || checked.inference !== 'succeeded') {
          setResult(checked);
          return;
        }
      }
      if (changed) await addModel({ ...config });
      if (!mounted.current) return;
      await onUse(config);
      if (liveEngine && canStart && allowAutoStart) useEnginePreferences.getState().grant(config, liveEngine, auto);
    });
  }

  return <div className="ns-model-connection">
    <ImportModels models={library.data?.models ?? []} run={perform} change={changeLibrary} busy={!!working} onImport={id => {
      setSearch('');
      const target = discovery.engines.find(item => item.id === 'custom_llama');
      if (target) setSelectedKey(modelConnectionKey({ ...target, model_name: id }));
    }} />
    <div className="ns-model-list-heading">
      {!standalone && <h3>本机模型</h3>}
      <Button variant="ghost" size="sm" disabled={discovery.isFetching || !!working} onClick={() => { void discovery.refetch(); void library.refetch(); }}><RefreshCw size={14} aria-hidden="true" />重新扫描</Button>
    </div>
    {checking && <p role="status" className="text-xs text-text-2">正在查找本地模型…</p>}
    {discovery.isError && <p role="alert" className="text-xs text-err">{errMessage(discovery.error)}</p>}
    {library.isError && <p role="alert" className="text-xs text-err">{errMessage(library.error)}</p>}
    {library.data?.warnings?.map((warning, i) => <p key={i} role="status" className="text-xs text-text-2">{warning}</p>)}
    {choices.length > 0 && <Input type="search" aria-label="搜索模型" placeholder="搜索模型" value={search} onChange={event => setSearch(event.target.value)} />}
    {search && !matched.length && <p role="status" className="text-xs text-text-2">没有匹配的模型。</p>}
    {choices.length > 0 && <div className="ns-local-model-list" aria-label="本地模型列表">
      {matched.map(item => <button key={item.key} type="button" disabled={!!working} aria-pressed={selection?.key === item.key} onClick={() => {
        setSelectedKey(item.key); setResult(undefined); setError(''); setLog(undefined);
      }}>
        <Box size={17} aria-hidden="true" />
        <span>{item.name}</span>
        <small data-online={modelIsOnline(item) && item.config?.enabled !== false}>{modelChoiceStatus(item)}</small>
      </button>)}
    </div>}
    {!checking && !choices.length && <p className="text-sm text-text-2">请选择 GGUF 文件，或启动 Ollama、LM Studio 后重新扫描。</p>}
    {!checking && selectedKey && !selection && <p role="alert" className="text-xs text-err">未找到所选模型，请重新扫描或选择列表中的模型。</p>}
    {entry && library.data && <LibraryModelDetails key={JSON.stringify(entry)} model={entry} library={library.data} run={perform} change={changeLibrary} busy={!!working || generating || !!selection?.model?.loaded || engine?.state === 'starting'} onForget={() => setSelectedKey('')} />}
    <RuntimeManager key={entry?.id ?? ''} library={library.data} required={!!entry && entry.status.startsWith('runtime_')} busy={!!working || generating || !!discovery.engines.find(item => item.id === 'custom_llama' && item.managed)} run={perform} change={changeLibrary} />
    {selection && <div className="ns-model-use">
      {switching && <p>切换时会先停止 {conflictName}。</p>}
      {waitingForOther && <p>{conflictName} 正在启动，完成后可以切换。</p>}
      {externalConflict && <p>请先在原应用中停止 {conflictName} 服务，再重新扫描。</p>}
      {engine?.error && !online && !working && <p>{engine.error}</p>}
      {selection.config?.enabled === false && <p>此模型已停用，可在高级管理中启用。</p>}
      <div className="ns-model-use-actions">
        {engine && canStart && allowAutoStart && <label><input type="checkbox" checked={auto} disabled={!!working} onChange={event => setAuto(event.target.checked)} />下次自动启动</label>}
        <Button disabled={!!working || checking || !modelsReady || generating || selection.config?.enabled === false ||
          engine?.state === 'starting' || needsStart && (!canStart || externalConflict || waitingForOther)} onClick={() => void useModel()}>{actionLabel}</Button>
      </div>
    </div>}
    {engine && (engine.state === 'starting' || /正在(切换并加载|启动)模型/.test(working)) &&
      <Button variant="outline" size="sm" className="justify-self-start" onClick={() => {
        void discovery.operate(engine, 'stop', modelName).catch(problem => setError(errMessage(problem)));
      }}>取消启动</Button>}
    {working && <p role="status" className="text-xs text-text-2">{working}</p>}
    {generating && <p className="text-xs text-text-2">对话正在运行，结束后可切换模型。</p>}
    {result && <ConnectionDiagnostics result={result} />}
    {error && <p role="alert" className="ns-form-error">{error}</p>}
    <details className="ns-record-detail">
      <summary>高级选项</summary>
      <div className="ns-detail-body grid gap-3 text-xs text-text-2">
        <Button className="justify-self-start" variant="outline" size="sm" disabled={!!working} onClick={() => void perform(async () => {
          const path = await api.pickProjectFolder();
          if (path && mounted.current) { setDirectory(path); setSelectedKey(''); setLog(undefined); }
        })}><FolderOpen size={14} aria-hidden="true" />选择模型目录…</Button>
        {engine && <div className="grid gap-2">
          <p className="break-all">{engine.base_url}{engine.version ? ' · ' + engine.version : ''}</p>
          {engine.directory && <p className="break-all">{engine.directory}</p>}
          <div className="flex flex-wrap gap-2">
            {engine.managed && (!custom || selection?.model?.loaded) && allowAutoStart && <>
              <Button size="sm" variant="outline" disabled={!!working || generating} onClick={() => void perform(async () => { await operate(engine, 'stop'); })}>停止服务</Button>
              <Button size="sm" variant="outline" disabled={!!working || generating} onClick={() => void perform(async () => { await operate(engine, 'restart'); })}>重启服务</Button>
            </>}
            {engine.log_path && <Button size="sm" variant="outline" disabled={!!working} onClick={() => void perform(async () => {
              setWorking('正在读取日志…'); setLog(await api.localEngineLog(engine.id));
            })}>查看日志</Button>}
          </div>
          {log && <><p className="break-all">{log.path}</p><pre className="max-h-44 overflow-auto whitespace-pre-wrap">{log.content || '暂无日志'}</pre></>}
        </div>}
        {discovery.engines.filter(item => item.id in installation && !item.models.length).map(item => <div key={item.id} className="flex flex-wrap items-center justify-between gap-2">
          <span>{item.name}</span>
          {!item.installed ? <a className="text-brand underline underline-offset-4" href={installation[item.id]} target="_blank" rel="noreferrer">安装</a>
            : (item.can_start ?? item.installed) && item.state !== 'online' ? <Button size="sm" variant="outline" disabled={!!working || generating || item.state === 'starting'} onClick={() => void perform(async () => { await operate(item, 'start'); })}>启动服务</Button>
            : <span>{item.error || '未发现模型'}</span>}
        </div>)}
      </div>
    </details>
  </div>;
}
