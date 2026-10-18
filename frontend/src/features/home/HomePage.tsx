import { lazy, Suspense, useCallback, useEffect, useRef, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { ArrowLeft, ArrowRight, ArrowUpRight, Focus, Search, Settings2, X } from 'lucide-react';
import { WORKSPACE_MODULES, nextModuleIndex, searchModules } from '@/app/modules';
import { useBackend } from '@/hooks/useBackend';
import { useExperienceStore } from '@/stores/experience-store';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import StatusBar from '@/components/StatusBar';
import { BootOverlay } from './BootOverlay';
import { WorkspaceSignal } from '@/components/WorkspaceSignal';
import { WorkspaceHub } from '@/components/WorkspaceHub';
import { preloadWorkspace } from '@/app/workspace-loaders';
import { useArchiveReturn } from '@/app/ArchiveReturn';
import './archive.css';
const ArchiveCanvas = lazy(() => import('./ArchiveCanvas'));
let bootSeen = false;
let lastSelected = 0;

export function HomePage() {
  const navigate = useNavigate();
  const backend = useBackend();
  const { graphics, boot } = useExperienceStore();
  const archiveReturn = useArchiveReturn();
  const returningOnMount = useRef(Boolean(archiveReturn.sourcePath));
  const [selected, setSelected] = useState(() => {
    const source = WORKSPACE_MODULES.findIndex(item => archiveReturn.sourcePath?.startsWith(item.to));
    return source >= 0 ? source : lastSelected;
  });
  const [departing, setDeparting] = useState<string | null>(null);
  const departure = useRef<ReturnType<typeof setTimeout>>();
  const [searchOpen, setSearchOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [inspecting, setInspecting] = useState(false);
  const [sceneFailed, setSceneFailed] = useState(false);
  const [autoLow, setAutoLow] = useState(false);
  const [sceneAllowed, setSceneAllowed] = useState(false);
  const [bootVisible, setBootVisible] = useState(() => boot && !bootSeen && !returningOnMount.current && !matchMedia('(prefers-reduced-motion: reduce)').matches);
  const finishBoot = useCallback(() => { bootSeen = true; setBootVisible(false); }, []);
  const failScene = useCallback(() => setSceneFailed(true), []);
  const lowerQuality = useCallback(() => setAutoLow(true), []);
  const choose = useCallback((index: number) => { setSelected(index); setInspecting(false); }, []);
  const module = WORKSPACE_MODULES[selected];
  const service = backend.modules[module.service];
  const results = searchModules(query);
  useEffect(() => { lastSelected = selected; }, [selected]);
  useEffect(() => {
    if (!returningOnMount.current) return;
    bootSeen = true;
    if (archiveReturn.phase === 'idle') document.getElementById('archive-content')?.focus({ preventScroll: true });
  }, [archiveReturn.phase]);
  useEffect(() => {
    if (archiveReturn.phase !== 'loading') return;
    const media = matchMedia('(min-width: 900px) and (prefers-reduced-motion: no-preference)');
    const readyIfStatic = () => { if (!media.matches || graphics === 'off' || sceneFailed) archiveReturn.ready(); };
    readyIfStatic();
    media.addEventListener('change', readyIfStatic);
    return () => media.removeEventListener('change', readyIfStatic);
  }, [archiveReturn.phase, archiveReturn.ready, graphics, sceneFailed]);
  useEffect(() => () => clearTimeout(departure.current), []);
  const enter = useCallback((path: string) => {
    if (departure.current) return;
    void preloadWorkspace(path);
    if (matchMedia('(prefers-reduced-motion: reduce)').matches) { navigate(path); return; }
    const index = WORKSPACE_MODULES.findIndex(item => item.to === path);
    if (index >= 0) setSelected(index);
    setSearchOpen(false); setInspecting(true); setDeparting(path);
    departure.current = setTimeout(() => navigate(path, { state: { archiveHandoff: true } }), 760);
  }, [navigate]);
  const followLink = (event: React.MouseEvent<HTMLDivElement>) => {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    const link = (event.target as HTMLElement).closest<HTMLAnchorElement>('a[href]');
    const path = link?.getAttribute('href')?.replace(/^#/, '');
    if (path && /^\/(chat|gateway|codelens|nebula|devops|settings)$/.test(path)) { event.preventDefault(); event.stopPropagation(); enter(path); }
  };
  useEffect(() => {
    const media = matchMedia('(min-width: 900px) and (prefers-reduced-motion: no-preference)');
    const update = () => setSceneAllowed(media.matches && !bootVisible);
    // Let the lightweight boot finish before WebGL initialization can occupy the main thread.
    const timer = setTimeout(update, 80); media.addEventListener('change', update);
    return () => { clearTimeout(timer); media.removeEventListener('change', update); };
  }, [bootVisible]);
  useEffect(() => {
    const keyboard = (event: KeyboardEvent) => {
      if (archiveReturn.phase !== 'idle' || searchOpen || event.isComposing || event.ctrlKey || event.altKey || event.metaKey) return;
      if ((event.target as HTMLElement)?.closest('input, textarea, select, [contenteditable=true], [role=dialog]')) return;
      if (bootVisible) { if (event.key === 'Escape' || event.key === 'Enter') { event.preventDefault(); finishBoot(); } return; }
      if (event.key === '/') { event.preventDefault(); setSearchOpen(true); }
      if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') { event.preventDefault(); setSelected((n) => nextModuleIndex(n, event.key === 'ArrowRight' ? 1 : -1)); setInspecting(false); }
      if (event.key === 'Escape') setInspecting(false);
      if (event.key === 'Enter' && !(event.target as HTMLElement)?.closest('button,a')) enter(module.to);
    };
    window.addEventListener('keydown', keyboard);
    return () => window.removeEventListener('keydown', keyboard);
  }, [searchOpen, module.to, enter, bootVisible, finishBoot, archiveReturn.phase]);
  const renderScene = sceneAllowed && graphics !== 'off' && !sceneFailed;
  return <><div onClickCapture={followLink} {...(bootVisible || departing ? { inert: '' } : {})} className={`ns-archive-app ${renderScene ? 'has-scene' : 'is-static'} ${inspecting ? 'is-inspecting' : ''} ${departing ? 'is-departing' : ''}`}>
    {departing && <div className="ns-dossier-handoff is-departing" aria-hidden="true"><span>FILE / {module.number}</span><strong>{module.name}</strong><small>WORKSPACE / CONNECTED</small></div>}
    <a className="ns-skip-link" href="#archive-content" onClick={(event) => { event.preventDefault(); document.getElementById('archive-content')?.focus(); }}>跳到主要内容</a>
    <div className="ns-archive-visual" aria-hidden="true">{renderScene ? <Suspense fallback={<span className="ns-scene-loading">正在准备档案…</span>}><ArchiveCanvas selected={selected} onSelect={choose} highQuality={graphics === 'high' || (graphics === 'auto' && !autoLow)} inspecting={inspecting} adaptive={graphics === 'auto'} onFailure={failScene} onAutoQuality={lowerQuality} onReady={archiveReturn.ready} /></Suspense> : <div className="ns-static-orbits"><i /><i /><i /><span>N<span>／</span>S</span></div>}</div>
    <header className="ns-archive-masthead"><Link to="/home" className="ns-archive-brand" aria-label="Noosphere 工作台"><strong>NOOSPHERE</strong><small>SYNTHESIZE INFORMATION</small><span>ANALYSIS <b>OS</b></span></Link><nav aria-label="档案操作"><WorkspaceHub /><Button variant="ghost" onClick={() => setSearchOpen(true)}><Search data-icon="inline-start" /><span>模块检索</span><kbd>/</kbd></Button><Link className="ns-direct-chat" to="/chat">AI 对话 <ArrowUpRight size={14} /></Link><Button asChild variant="ghost" size="icon"><Link to="/settings" aria-label="设置"><Settings2 /></Link></Button></nav></header>
    <div className="ns-archive-signal"><WorkspaceSignal /></div>
    <main id="archive-content" tabIndex={-1} className="ns-archive-content">
      <section className="ns-file-callout" aria-label="选中模块" style={{ '--module-accent': module.accent } as React.CSSProperties}>
        <div className="ns-file-eyebrow">PERSONAL WORKSPACE <span>/</span> {module.category}</div>
        <div className="ns-file-number">FILE NUMBER: <span key={module.number}>NS–00{module.number.slice(-1)}</span><ArrowUpRight size={23} /></div>
        <div className="ns-file-rule"><i /></div>
        <div className="ns-file-title" aria-live="polite" aria-atomic="true"><h1 key={module.id}>{module.name}</h1><span>{module.english}</span></div>
        <p className="ns-file-description">{module.description}</p>
        <div className="ns-file-capabilities">{module.capabilities.map((text) => <span key={text}>{text}</span>)}</div>
        <div className="ns-file-actions"><Link to={module.to} className="ns-access-file"><span>进入{module.name}<small>ACCESS WORKSPACE</small></span><ArrowRight size={24} /></Link>{renderScene && <Button variant="ghost" size="icon" aria-label={inspecting ? '收回档案' : '展开档案预览'} aria-pressed={inspecting} onClick={() => setInspecting(!inspecting)}>{inspecting ? <X /> : <Focus />}</Button>}</div>
        <div className="ns-module-health"><i data-online={service?.state === 'running'} />{service?.state === 'running' ? '服务就绪' : backend.health === 'online' ? '等待服务' : '等待本地服务'}<span>LOCAL / {module.service.toUpperCase()}</span></div>
      </section>
      <div className="ns-archive-bottom"><div className="ns-archive-ordinal"><span>WORKSPACE / SELECT</span><div><b key={module.number}>{module.number}</b><i>/</i><span>05</span></div></div><nav className="ns-archive-module-nav" aria-label="主导航"><Link className="sr-only" to="/home">工作台</Link>{WORKSPACE_MODULES.map((item, index) => <button key={item.id} onClick={() => choose(index)} aria-label={`选择${item.name}`} aria-pressed={index === selected}><span>{item.number}</span><strong>{item.name}</strong><small>{item.category}</small></button>)}<Link className="sr-only" to="/settings">设置</Link></nav><div className="ns-archive-step"><Button variant="ghost" size="icon" aria-label="上一个档案" onClick={() => choose(nextModuleIndex(selected, -1))}><ArrowLeft /></Button><Button variant="ghost" size="icon" aria-label="下一个档案" onClick={() => choose(nextModuleIndex(selected, 1))}><ArrowRight /></Button></div></div>
    </main>
    <div className="ns-archive-meta"><span>{sceneFailed ? '三维预览暂不可用 · 所有模块仍可访问' : graphics === 'off' ? '二维模式' : autoLow ? '已自动降低画质' : 'LOCAL FIRST · YOUR KNOWLEDGE, CONNECTED'}</span><span><kbd>←</kbd> <kbd>→</kbd> 切换模块 <i>/</i> <kbd>ENTER</kbd> 进入</span></div>
    <StatusBar backend={backend} />
    <Dialog open={searchOpen} onOpenChange={setSearchOpen}><DialogContent className="ns-module-search" aria-describedby="module-search-description"><DialogTitle>工作档案索引</DialogTitle><DialogDescription id="module-search-description">查找模块名称、用途或功能，直接进入工作区。</DialogDescription><Input autoFocus aria-label="检索模块" placeholder="搜索模块与功能…" value={query} onChange={(event) => setQuery(event.target.value)} /><div className="ns-module-results">{results.map((item) => <Link key={item.id} to={item.to} onClick={() => setSearchOpen(false)}><span>{item.number}</span><div><strong>{item.name}</strong><small>{item.category} / {item.english}</small></div><ArrowUpRight size={18} /></Link>)}{!results.length && <p role="status">没有找到匹配模块，试试“代码”“记忆”或“模型”。</p>}</div></DialogContent></Dialog>
  </div>{bootVisible && <BootOverlay onFinish={finishBoot} />}</>;
}
