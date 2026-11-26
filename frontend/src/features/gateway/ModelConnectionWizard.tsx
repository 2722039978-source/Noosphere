import { useEffect, useRef, useState } from 'react';
import { Box, FileBox, FolderOpen, RefreshCw } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { api, errMessage, type LocalEngine, type LocalEngineAction, type ModelConfig, type ModelDiagnostic } from '@/lib/api';
import { useBackend } from '@/hooks/useBackend';
import { useWorkspaceStore } from '@/stores/workspace-store';
import { useGenerationStore } from '@/features/chat/chat-runtime';
import { ConnectionDiagnostics } from './ConnectionDiagnostics';
import { useEnginePreferences, useLocalEngines } from './local-engine';
import { localEngineName, localModelChoices, modelChoiceStatus, modelConnectionKey, modelNameKey, portableConflict, type ModelTarget } from './local-model-match';
import './model-connection.css';

const installation: Record<string, string> = { ollama: 'https://ollama.com/download/windows', lm_studio: 'https://lmstudio.ai/download' };

async function discoveredModelId(key: string) {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(key));
  return 'local-' + Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, '0')).join('');
}

export function ModelConnectionWizard({ preferred, onUse, allowAutoStart = true, standalone = false }: {
  preferred?: ModelTarget & { id?: string };
  onUse: (model: ModelConfig) => void | Promise<void>;
  allowAutoStart?: boolean;
  standalone?: boolean;
}) {
  const { models, modelsReady, addModel } = useBackend();
  const workspacePath = useWorkspaceStore(state => state.workspaces.find(workspace => workspace.id === state.activeId)?.project_path ?? '');
  const generating = useGenerationStore(state => state.active !== null);
  const [directory, setDirectory] = useState('');
  const discovery = useLocalEngines(directory || workspacePath, true);
  const [selectedKey, setSelectedKey] = useState(() => preferred ? modelConnectionKey(preferred) : '');
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
  const selection = selectedKey ? choices.find(item => item.key === selectedKey) : enabled.find(item => item.engine?.state === 'online') ?? enabled[0];
  const engine = selection?.engine;
  const online = engine?.state === 'online';
  const canStart = Boolean(engine?.can_start ?? engine?.installed);
  const conflict = online ? undefined : portableConflict(discovery.engines, engine);
  const switching = Boolean(conflict?.managed && conflict.state === 'online');
  const waitingForOther = conflict?.state === 'starting';
  const externalConflict = Boolean(conflict && !switching && !waitingForOther);
  const needsStart = !!engine && !online;
  const actionLabel = needsStart ? switching ? '切换并启动' : '启动并使用' : standalone ? '开始对话' : '使用模型';
  const checking = discovery.isPending && discovery.isFetching;
  const conflictName = conflict && localEngineName(conflict);

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

  async function operate(target: LocalEngine, action: LocalEngineAction, model?: string) {
    setWorking(action === 'stop' ? '正在停止模型…' : action === 'switch' ? '正在切换并加载模型…' : '正在启动模型…');
    const next = await discovery.operate(target, action, model);
    if (action === 'stop') useEnginePreferences.getState().revoke(target);
    if (action !== 'stop' && next.state !== 'online') throw new Error(next.error || '服务尚未就绪。请查看日志后重试。');
    return next;
  }

  async function useModel() {
    if (!selection || generating || !modelsReady || selection.config?.enabled === false) return;
    await perform(async () => {
      if (engine && !online) {
        const started = await operate(engine, switching ? 'switch' : 'start', selection.target.model_name);
        const requested = modelNameKey(selection.target.provider, selection.target.model_name);
        if (!started.models.some(item => modelNameKey(started.provider, item.id) === requested)) {
          throw new Error('服务已在线，但未发现所选模型。请刷新列表。');
        }
      }
      if (!mounted.current) return;
      setWorking('正在验证模型…');
      const latest = selection.config ? undefined : await api.getModels();
      const existing = selection.config ?? localModelChoices(latest?.models ?? [], []).find(item => item.key === selection.key)?.config;
      if (existing?.enabled === false) throw new Error('此模型已停用，可在高级管理中启用。');
      const config: ModelConfig = existing ?? {
        id: await discoveredModelId(selection.key), name: selection.name, ...selection.target, enabled: true, deployment: 'local',
      };
      const checked = existing ? await api.testModel(existing.id) : await api.probeModel(config);
      if (!mounted.current) return;
      if (checked.status !== 'ok' || checked.inference !== 'succeeded') {
        setResult(checked);
        return;
      }
      if (!existing) await addModel({ ...config });
      if (!mounted.current) return;
      await onUse(config);
      if (engine && canStart && allowAutoStart) useEnginePreferences.getState().grant(config, engine, auto);
    });
  }

  return <div className="ns-model-connection">
    <div className="ns-model-list-heading">
      {!standalone && <h3>本机模型</h3>}
      <Button variant="ghost" size="sm" disabled={discovery.isFetching || !!working} onClick={() => void discovery.refetch()}><RefreshCw size={14} aria-hidden="true" />重新扫描</Button>
    </div>
    {checking && <p role="status" className="text-xs text-text-2">正在查找本地模型…</p>}
    {discovery.isError && <p role="alert" className="text-xs text-err">{errMessage(discovery.error)}</p>}
    {choices.length > 0 && <div className="ns-local-model-list" aria-label="本地模型列表">
      {choices.map(item => <button key={item.key} type="button" disabled={!!working} aria-pressed={selection?.key === item.key} onClick={() => {
        setSelectedKey(item.key); setResult(undefined); setError(''); setLog(undefined);
      }}>
        <Box size={17} aria-hidden="true" />
        <span>{item.name}</span>
        <small data-online={item.engine?.state === 'online' && item.config?.enabled !== false}>{modelChoiceStatus(item)}</small>
      </button>)}
    </div>}
    {!checking && !choices.length && <p className="text-sm text-text-2">未发现本地模型。可在高级选项中选择模型目录或查看运行环境。</p>}
    {!checking && selectedKey && !selection && <p role="alert" className="text-xs text-err">未找到所选模型，请重新扫描或选择列表中的模型。</p>}
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
        <Button className="justify-self-start" variant="outline" size="sm" disabled={!!working} onClick={() => void perform(async () => {
          setWorking('正在读取模型…');
          const file = await api.pickLocalModelFile();
          if (!file || !mounted.current) return;
          const updated = await api.registerCustomModel(file);
          setDirectory(updated.directory ?? '');
          setSelectedKey('');
          setLog(undefined);
          await discovery.refetch();
          setWorking('');
        })}><FileBox size={14} aria-hidden="true" />选择模型文件（.gguf）…</Button>
        {engine && <div className="grid gap-2">
          <p className="break-all">{engine.base_url}{engine.version ? ' · ' + engine.version : ''}</p>
          {engine.directory && <p className="break-all">{engine.directory}</p>}
          <div className="flex flex-wrap gap-2">
            {engine.managed && allowAutoStart && <>
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
