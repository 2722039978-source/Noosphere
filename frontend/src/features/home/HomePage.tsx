import { lazy, Suspense, useCallback, useEffect, useRef, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { ArrowLeft, ArrowRight, ArrowUpRight, BookOpen, Code2, Focus, Layers, MessageSquare, Search, Settings2, X, Activity, type LucideIcon } from 'lucide-react';
import { WORKSPACE_MODULES, nextModuleIndex, searchModules, type WorkspaceModule } from '@/app/modules';
import { useBackend } from '@/hooks/useBackend';
import { useExperienceStore } from '@/stores/experience-store';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import StatusBar from '@/components/StatusBar';
import { BootOverlay } from './BootOverlay';
import { preloadWorkspace } from '@/app/workspace-loaders';
import { useArchiveReturn } from '@/app/ArchiveReturn';
import './archive.css';
const ArchiveCanvas = lazy(() => import('./ArchiveCanvas'));
let bootSeen = false;
let lastSelected = 0;

const featureCopy: Record<WorkspaceModule['id'], { title: string; description: string; action: string; Icon: LucideIcon }> = {
  assistant: { title: '对话与任务', description: '向模型提问、添加文件，或把当前对话转为任务，随时查看执行进度。', action: '开始对话', Icon: MessageSquare },
  codelens: { title: '代码分析', description: '在项目中查找代码和符号，打开回答所引用的源码。', action: '打开代码分析', Icon: Code2 },
  nebula: { title: '项目记忆', description: '保存对话里的偏好和约定，查看来源并确认 AI 提出的修改。', action: '打开记忆', Icon: BookOpen },
  devops: { title: '系统诊断', description: '查看本机资源和服务状态，诊断运行中的问题。', action: '打开系统诊断', Icon: Activity },
  gateway: { title: '本地模型', description: '发现本机的推理引擎和模型，直接在 Noosphere 内启动和连接。', action: '管理模型', Icon: Layers },
};

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
  const feature = featureCopy[module.id];
  const matches = searchModules(query);
  const searchTerm = query.trim().toLocaleLowerCase();
  const results = WORKSPACE_MODULES.filter(item => matches.includes(item)
    || (featureCopy[item.id].title + featureCopy[item.id].description).includes(searchTerm));
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
    if (path && (path === '/settings' || WORKSPACE_MODULES.some(item => item.to === path))) { event.preventDefault(); event.stopPropagation(); enter(path); }
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
      if (bootVisible) { if (event.key === 'Escape' || event.key === 'Enter') { event.preventDefault(); finishBoot(); } return; }
      if ((event.target as HTMLElement)?.closest('input, textarea, select, [contenteditable=true], [role=dialog]')) return;
      if (event.key === '/') { event.preventDefault(); setSearchOpen(true); }
      if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') { event.preventDefault(); setSelected((n) => nextModuleIndex(n, event.key === 'ArrowRight' ? 1 : -1)); setInspecting(false); }
      if (event.key === 'Escape') setInspecting(false);
      if (event.key === 'Enter' && !(event.target as HTMLElement)?.closest('button,a')) enter(module.to);
    };
    window.addEventListener('keydown', keyboard);
    return () => window.removeEventListener('keydown', keyboard);
  }, [searchOpen, module.to, enter, bootVisible, finishBoot, archiveReturn.phase]);
  const renderScene = sceneAllowed && graphics !== 'off' && !sceneFailed;
  return <>
    <div onClickCapture={followLink} {...(bootVisible || departing ? { inert: '' } : {})}
      className={`ns-archive-app ${renderScene ? 'has-scene' : 'is-static'} ${inspecting ? 'is-inspecting' : ''} ${departing ? 'is-departing' : ''}`}>
      {departing && <div className="ns-dossier-handoff is-departing" aria-hidden="true"><strong>{departing === '/settings' ? '设置' : feature.title}</strong></div>}
      <a className="ns-skip-link" href="#archive-content" onClick={event => { event.preventDefault(); document.getElementById('archive-content')?.focus(); }}>跳到主要内容</a>
      <div className="ns-archive-visual" aria-hidden="true">
        {renderScene ? <Suspense fallback={<span className="ns-scene-loading">正在载入三维场景…</span>}>
          <ArchiveCanvas selected={selected} onSelect={choose} highQuality={graphics === 'high' || (graphics === 'auto' && !autoLow)}
            inspecting={inspecting} adaptive={graphics === 'auto'} onFailure={failScene} onAutoQuality={lowerQuality} onReady={archiveReturn.ready} />
        </Suspense> : <div className="ns-static-orbits"><i /><i /><i /><b /></div>}
      </div>
      <header className="ns-archive-masthead">
        <div>
          <h1>功能介绍</h1>
          <p>连接本地模型，在对话中处理文件、代码和任务。</p>
        </div>
        <nav aria-label="介绍页操作">
          {module.id !== 'assistant' && <Link className="ns-intro-back" to="/chat"><ArrowLeft size={15} aria-hidden="true" />返回对话</Link>}
          <Button className="ns-intro-search" variant="outline" size="sm" onClick={() => setSearchOpen(true)}><Search aria-hidden="true" />搜索功能</Button>
          <Button asChild variant="ghost" size="icon-sm"><Link to="/settings" aria-label="设置"><Settings2 aria-hidden="true" /></Link></Button>
        </nav>
      </header>
      <main id="archive-content" tabIndex={-1} className="ns-archive-content">
        <section className="ns-file-callout" aria-labelledby="intro-feature-title">
          <div className="ns-file-title" aria-live="polite" aria-atomic="true">
            <div className="ns-feature-copy" key={module.id}>
              <span className="ns-feature-icon"><feature.Icon size={23} aria-hidden="true" /></span>
              <h2 id="intro-feature-title">{feature.title}</h2>
              <p>{feature.description}</p>
            </div>
          </div>
          <div className="ns-file-actions">
            <Link to={module.to} className="ns-access-file">{feature.action}<ArrowRight size={17} aria-hidden="true" /></Link>
            {renderScene && <Button variant="ghost" size="sm" aria-label={inspecting ? '收回三维预览' : '展开三维预览'} aria-pressed={inspecting}
              onClick={() => setInspecting(value => !value)}>{inspecting ? <X aria-hidden="true" /> : <Focus aria-hidden="true" />}{inspecting ? '收回预览' : '预览'}</Button>}
          </div>
        </section>
        <div className="ns-archive-bottom">
          <nav className="ns-archive-module-nav" aria-label="功能列表">
            {WORKSPACE_MODULES.map((item, index) => {
              const { title, Icon } = featureCopy[item.id];
              return <button key={item.id} type="button" onClick={() => choose(index)} aria-label={`选择${title}`} aria-pressed={index === selected}><Icon size={18} aria-hidden="true" /><span>{title}</span></button>;
            })}
          </nav>
          <div className="ns-archive-step">
            <Button variant="ghost" size="icon-sm" aria-label="上一个功能" onClick={() => choose(nextModuleIndex(selected, -1))}><ArrowLeft aria-hidden="true" /></Button>
            <Button variant="ghost" size="icon-sm" aria-label="下一个功能" onClick={() => choose(nextModuleIndex(selected, 1))}><ArrowRight aria-hidden="true" /></Button>
          </div>
        </div>
        {sceneFailed && <p className="ns-archive-meta" role="status">三维预览暂不可用，功能仍可打开。</p>}
      </main>
      <StatusBar backend={backend} />
      <Dialog open={searchOpen} onOpenChange={setSearchOpen}>
        <DialogContent className="ns-module-search">
          <DialogTitle>搜索功能</DialogTitle>
          <DialogDescription>输入功能名称，选择后直接打开。</DialogDescription>
          <Input autoFocus aria-label="搜索功能" placeholder="搜索对话、代码、记忆…" value={query} onChange={event => setQuery(event.target.value)} />
          <div className="ns-module-results">
            {results.map(item => {
              const result = featureCopy[item.id];
              return <Link key={item.id} to={item.to} onClick={() => setSearchOpen(false)}><result.Icon size={17} aria-hidden="true" /><div><strong>{result.title}</strong><small>{result.description}</small></div><ArrowUpRight size={16} aria-hidden="true" /></Link>;
            })}
            {!results.length && <p role="status">没有找到匹配的功能。</p>}
          </div>
        </DialogContent>
      </Dialog>
    </div>
    {bootVisible && <BootOverlay onFinish={finishBoot} />}
  </>;
}
