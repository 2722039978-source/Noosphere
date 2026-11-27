import { Channel } from '@tauri-apps/api/core';
import { FlaskConical, Send, Square } from 'lucide-react';
import { lazy, Suspense, useEffect, useLayoutEffect, useRef, useState, type FormEvent } from 'react';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';
import { LoadingState } from '@/components/ui/feedback';
import { createStreamRunner, type RunPhase, type RunStatus } from '@/features/chat/stream-runner';
import { useBackend } from '@/hooks/useBackend';
import { api, errMessage, type ChatMessageParam, type ModelConfig, type StreamChunk } from '@/lib/api';
import { chatCapabilityBlocker } from '@/lib/model-capabilities';
import { useWorkspaceStore } from '@/stores/workspace-store';
import { LocalModelPicker } from '@/features/gateway/LocalModelPicker';
import { useLocalEngines } from '@/features/gateway/local-engine';
import { isLocalModel, localEngineForModel, localModelChoices, modelConnectionKey, modelIsOnline, needsModelSetup, type ModelTarget } from '@/features/gateway/local-model-match';
import './model-check.css';

const ConnectionWizard = lazy(() => import('@/features/gateway/ModelConnectionWizard').then(module => ({ default: module.ModelConnectionWizard })));
const QUESTION_LIMIT = 1000;

interface TrialTurn {
  id: string;
  identity: string;
  question: string;
  answer: string;
  status?: RunStatus;
  error?: string;
}

const localTrialModel = (model: ModelConfig) => isLocalModel(model) && !chatCapabilityBlocker(model, false);

function shortPrompt(turns: TrialTurn[], question: string): ChatMessageParam[] {
  const recent: ChatMessageParam[] = [];
  let budget = 1500 - question.length;
  for (const turn of turns.slice(-3).reverse()) {
    if (turn.status !== 'completed' || !turn.answer) continue;
    const size = turn.question.length + turn.answer.length;
    if (size > budget) break;
    recent.unshift({ role: 'user', content: turn.question }, { role: 'assistant', content: turn.answer });
    budget -= size;
  }
  return [...recent, { role: 'user', content: question }];
}

export function ModelCheckPage() {
  const { models, modelsReady, modelsError, loadModels } = useBackend();
  const available = models.filter(localTrialModel);
  const workspacePath = useWorkspaceStore(state => state.workspaces.find(item => item.id === state.activeId)?.project_path ?? '');
  const { engines } = useLocalEngines(workspacePath, modelsReady);
  const [choice, setChoice] = useState('');
  const [setup, setSetup] = useState<{ preferred?: ModelTarget & { id?: string } } | null>(null);
  const [draft, setDraft] = useState('');
  const [turns, setTurns] = useState<TrialTurn[]>([]);
  const [phase, setPhase] = useState<RunPhase | null>(null);
  const [stopError, setStopError] = useState('');
  const alive = useRef(false);
  const scroller = useRef<HTMLDivElement>(null);
  const nearBottom = useRef(true);
  const composer = useRef<HTMLTextAreaElement>(null);
  const choices = localModelChoices(available, engines, choice);
  const model = choice ? available.find(item => item.id === choice) : choices.length === 1 ? choices[0].config : undefined;
  const engine = localEngineForModel(engines, model);
  const modelStopped = !!engine && !modelIsOnline(choices.find(item => model && item.key === modelConnectionKey(model)));
  const identity = model ? [model.id, model.provider, model.base_url, model.model_name].join('|') : '';
  const currentTurns = turns.filter(turn => turn.identity === identity);
  const [runner] = useState(() => createStreamRunner({
    start: async (run, messages, receive) => {
      const channel = new Channel<StreamChunk>();
      channel.onmessage = receive;
      await api.chatStream(run.requestId, messages, channel, run.modelId, 'local', '', undefined, true);
    },
    cancel: api.cancelChat,
    active: run => { if (alive.current) setPhase(run?.phase ?? null); },
    append: (run, delta) => {
      if (alive.current) setTurns(current => current.map(turn => turn.id === run.messageId ? { ...turn, answer: turn.answer + delta } : turn));
    },
    finish: (run, status, error, content) => {
      if (alive.current) setTurns(current => current.map(turn => turn.id === run.messageId
        ? { ...turn, answer: content ?? turn.answer, status, error } : turn));
    },
    cancelError: (_id, error) => { if (alive.current) setStopError('停止请求未确认。' + errMessage(error)); },
  }));

  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      void runner.cancel().catch(() => {});
    };
  }, [runner]);
  useEffect(() => {
    void runner.cancel().catch(() => {});
    setTurns([]);
    setStopError('');
  }, [identity, runner]);
  useLayoutEffect(() => {
    const area = scroller.current;
    if (area && nearBottom.current) area.scrollTop = area.scrollHeight;
  }, [turns, phase]);

  function send(event?: FormEvent) {
    event?.preventDefault();
    const question = draft.trim();
    if (!question || !model || phase) return;
    if (modelStopped) { setSetup({ preferred: model }); return; }
    const id = crypto.randomUUID();
    const messages = shortPrompt(currentTurns, question);
    const accepted = runner.start({
      requestId: id, sessionId: '', messageId: id, modelId: model.id,
      begin: () => {
        nearBottom.current = true;
        setTurns(current => [...current.slice(-29), { id, identity, question, answer: '' }]);
      },
      prepare: async () => messages,
    });
    if (accepted) {
      setDraft('');
      setStopError('');
    }
  }

  function connect(selected: ModelConfig) {
    if (!localTrialModel(selected)) throw new Error('请选择本机模型。');
    setChoice(selected.id);
    setSetup(null);
    requestAnimationFrame(() => composer.current?.focus());
  }

  return <div className="ns-model-check" data-workspace-page="model-check">
    <section className="ns-model-check-panel" aria-label="模型试聊页面">
      <header className="ns-model-check-heading">
        <div>
          <h1>模型试聊</h1>
          <p>试聊记录仅保留在此页面。</p>
        </div>
      </header>
      <div className="ns-model-check-toolbar">
        <LocalModelPicker label="试聊模型" choices={choices} current={model} disabled={!!phase || !modelsReady}
          className="ns-model-check-selector" onBrowse={() => setSetup({ preferred: model })} onSelect={selected => {
            if (!selected.config || needsModelSetup(selected)) setSetup({ preferred: selected.config ?? selected.target });
            else setChoice(selected.config.id);
          }} />
        {modelStopped && <Button size="sm" variant="outline" disabled={!!phase || engine.state === 'starting'} onClick={() => setSetup({ preferred: model })}>启动模型</Button>}
      </div>
      {modelsError && <div className="ns-model-check-feedback" role="alert">{modelsError}
        <Button variant="ghost" size="sm" onClick={() => void loadModels()}>重新读取模型</Button>
      </div>}
      <div className="ns-model-check-dialogue">
        <div className="ns-model-check-turns" ref={scroller} aria-label="试聊记录" onScroll={event => {
          const element = event.currentTarget;
          nearBottom.current = element.scrollHeight - element.scrollTop - element.clientHeight < 80;
        }}>
          {!currentTurns.length && <div className="ns-model-check-empty">
            <FlaskConical size={23} aria-hidden="true" />
            <p>{!modelsReady ? '正在读取模型…' : !model ? '选择模型后开始试聊。' : '发一条消息开始试聊。'}</p>
          </div>}
          {currentTurns.map(turn => <article className="ns-model-check-turn" key={turn.id}>
            <p className="ns-model-check-question"><span className="sr-only">你的问题 </span>{turn.question}</p>
            <div className="ns-model-check-answer">
              {turn.answer && <p><span className="sr-only">模型 </span>{turn.answer}</p>}
              {!turn.status && !turn.answer && <p role="status">等待模型回应…</p>}
              {turn.status === 'cancelled' && <p role="status">已停止试聊</p>}
              {turn.status === 'failed' && <div role="alert" className="ns-model-check-feedback">
                {turn.error || '模型回应中断，请重试。'}
                <Button variant="ghost" size="sm" disabled={!!phase} onClick={() => { setDraft(turn.question); composer.current?.focus(); }}>填回问题</Button>
              </div>}
              {turn.status === 'completed' && !turn.answer && <p role="status">模型未返回正文。</p>}
            </div>
          </article>)}
        </div>
        <form className="ns-model-check-composer" onSubmit={send}>
          <textarea ref={composer} rows={2} maxLength={QUESTION_LIMIT} value={draft} aria-label="输入试聊问题"
            placeholder="输入简短的问题…" onChange={event => setDraft(event.target.value)}
            onKeyDown={event => {
              if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
                event.preventDefault();
                send();
              }
            }} />
          {stopError && <p className="ns-model-check-feedback" role="alert">{stopError}</p>}
          <div className="ns-model-check-actions">
            <Button type="button" size="sm" variant="ghost" disabled={!!phase || !turns.length}
              onClick={() => { setTurns([]); setStopError(''); composer.current?.focus(); }}>清空试聊</Button>
            {phase ? <Button type="button" size="sm" variant="outline" onClick={() => void runner.cancel().catch(() => {})}>
              <Square aria-hidden="true" />停止试聊
            </Button> : <Button type="submit" size="sm" disabled={!model || !draft.trim()} className="ns-model-check-send">
              <Send aria-hidden="true" />发送试聊
            </Button>}
          </div>
        </form>
      </div>
    </section>
    <Dialog open={!!setup} onOpenChange={open => { if (!open) setSetup(null); }}>
      {setup && <DialogContent className="ns-model-connection-dialog">
        <DialogTitle>选择模型</DialogTitle>
        <DialogDescription className="sr-only">选择本次试聊使用的本地模型。</DialogDescription>
        <Suspense fallback={<LoadingState label="正在读取本机模型…" />}>
          <ConnectionWizard preferred={setup.preferred} allowAutoStart={false} onUse={connect} />
        </Suspense>
      </DialogContent>}
    </Dialog>
  </div>;
}
