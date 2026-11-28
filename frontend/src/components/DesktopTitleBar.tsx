import { useEffect, useState } from 'react';
import { getCurrentWindow } from '@tauri-apps/api/window';
import { Minus, Square, Copy, X, Folder } from 'lucide-react';
import { Link, useLocation } from 'react-router-dom';
import { BrandMark } from './BrandMark';
import { api } from '@/lib/api';
import { useWorkspaceStore } from '@/stores/workspace-store';
import { useGenerationStore } from '@/features/chat/chat-runtime';
import { useUiStore } from '@/stores/ui-store';
import { Tooltip, TooltipContent, TooltipTrigger } from './ui/tooltip';

/** Native window controls share the archive palette instead of OS accent blue. */
export function DesktopTitleBar() {
  const { pathname } = useLocation();
  const [maximized, setMaximized] = useState(false);
  const [error, setError] = useState('');
  const [nativeTitlebar, setNativeTitlebar] = useState(false);
  const workspaceName = useWorkspaceStore(state => state.workspaces.find(item => item.id === state.activeId)?.name ?? '');
  const workspaceReady = useWorkspaceStore(state => state.hydrated);
  const generating = useGenerationStore(state => state.active);
  const inHome = pathname === '/home';
  const native = typeof window !== 'undefined' && Boolean((window as unknown as { __TAURI_INTERNALS__?: { metadata?: { currentWindow?: { label?: string } } } }).__TAURI_INTERNALS__?.metadata?.currentWindow?.label);

  useEffect(() => {
    if (!native) return;
    let active = true;
    void api.desktopPlatform().then(platform => {
      if (active) setNativeTitlebar(platform.native_titlebar);
    }).catch(() => {});
    return () => { active = false; };
  }, [native]);

  useEffect(() => {
    if (!native || nativeTitlebar) return;
    const app = getCurrentWindow();
    let disposed = false, unlisten: (() => void) | undefined;
    const refresh = () => { void app.isMaximized().then(value => { if (!disposed) setMaximized(value); }).catch(() => {}); };
    refresh();
    void app.onResized(refresh).then(stop => { if (disposed) stop(); else unlisten = stop; }).catch(() => {});
    return () => { disposed = true; unlisten?.(); };
  }, [native, nativeTitlebar]);

  const control = async (action: 'minimize' | 'maximize' | 'close') => {
    setError('');
    try {
      const app = getCurrentWindow();
      if (action === 'minimize') await app.minimize();
      else if (action === 'close') await app.close();
      else { await app.toggleMaximize(); setMaximized(await app.isMaximized()); }
    } catch {
      setError('窗口操作未完成，请重试。');
    }
  };

  return <>
    <header className="ns-desktop-titlebar" data-in-home={inHome}>
      <Tooltip>
        <TooltipTrigger asChild>
          <Link to="/home" className="ns-titlebar-brand" aria-label="功能介绍" aria-current={inHome ? 'page' : undefined}>
            <span className="ns-titlebar-mark-wrap">
              <BrandMark className="size-[22px]" />
              <i className="ns-titlebar-orbit" aria-hidden="true" />
            </span>
            <span className="ns-titlebar-wordmark">Noosphere</span>
            <span className="ns-titlebar-tag" aria-hidden="true">{inHome ? 'ARCHIVE' : 'WORKBENCH'}</span>
          </Link>
        </TooltipTrigger>
        <TooltipContent side="bottom">{inHome ? '当前在功能介绍' : '打开三维功能介绍'}</TooltipContent>
      </Tooltip>

      {!inHome && workspaceReady && workspaceName && (
        <div className="ns-titlebar-context">
          <i className="ns-titlebar-sep" aria-hidden="true" />
          <button
            type="button"
            className="ns-titlebar-workspace"
            title={`切换项目：${workspaceName}`}
            onClick={() => useUiStore.getState().setProjectPickerOpen(true)}
          >
            <Folder size={13} aria-hidden="true" />
            <span className="truncate">{workspaceName}</span>
          </button>
          {generating && (
            <Link to="/chat" className="ns-titlebar-live" title="返回生成中的对话">
              <i aria-hidden="true" />
              <span>{generating.phase === 'streaming' ? '输出中' : '思考中'}</span>
            </Link>
          )}
        </div>
      )}

      <div className="ns-desktop-drag" {...(native && !nativeTitlebar ? { 'data-tauri-drag-region': true } : {})} />

      {native && !nativeTitlebar && (
        <div className="ns-desktop-controls">
          <button aria-label="最小化窗口" onClick={() => void control('minimize')}><Minus size={14} /></button>
          <button aria-label={maximized ? '还原窗口' : '最大化窗口'} onClick={() => void control('maximize')}>{maximized ? <Copy size={12} /> : <Square size={12} />}</button>
          <button aria-label="关闭窗口" onClick={() => void control('close')}><X size={15} /></button>
        </div>
      )}
    </header>
    {error && <p role="alert" className="ns-titlebar-error">{error}</p>}
  </>;
}
