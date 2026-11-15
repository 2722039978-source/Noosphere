import { useEffect, useState } from 'react';
import { getCurrentWindow } from '@tauri-apps/api/window';
import { Minus, Square, Copy, X } from 'lucide-react';
import { Link, useLocation } from 'react-router-dom';
import { BrandMark } from './BrandMark';
import { Tooltip, TooltipContent, TooltipTrigger } from './ui/tooltip';

/** Native window controls share the archive palette instead of OS accent blue. */
export function DesktopTitleBar() {
  const { pathname } = useLocation();
  const [maximized,setMaximized]=useState(false),[error,setError]=useState('');
  const native=typeof window!=='undefined' && Boolean((window as unknown as {__TAURI_INTERNALS__?:{metadata?:{currentWindow?:{label?:string}}}}).__TAURI_INTERNALS__?.metadata?.currentWindow?.label);
  useEffect(() => {
    if(!native)return;
    const app=getCurrentWindow();
    let disposed=false, unlisten:(()=>void)|undefined;
    const refresh=()=>{void app.isMaximized().then(value=>{if(!disposed)setMaximized(value);}).catch(()=>{});};
    refresh();
    void app.onResized(refresh).then(stop=>{if(disposed)stop();else unlisten=stop;}).catch(()=>{});
    return ()=>{disposed=true;unlisten?.();};
  },[native]);
  const control=async(action:'minimize'|'maximize'|'close')=>{
    setError('');try{const app=getCurrentWindow();if(action==='minimize')await app.minimize();else if(action==='close')await app.close();else{await app.toggleMaximize();setMaximized(await app.isMaximized());}}catch{setError('窗口操作未完成，请重试。');}
  };
  return <><header className="ns-desktop-titlebar">
    <Tooltip><TooltipTrigger asChild><Link to="/home" className="ns-titlebar-brand" aria-label="功能介绍" aria-current={pathname === '/home' ? 'page' : undefined}><BrandMark className="size-6" /><span>Noosphere</span></Link></TooltipTrigger><TooltipContent side="bottom">功能介绍</TooltipContent></Tooltip>
    <div className="ns-desktop-drag" {...(native ? { 'data-tauri-drag-region': true } : {})} />
    {native && <div className="ns-desktop-controls"><button aria-label="最小化窗口" onClick={()=>void control('minimize')}><Minus size={14}/></button><button aria-label={maximized?'还原窗口':'最大化窗口'} onClick={()=>void control('maximize')}>{maximized?<Copy size={12}/>:<Square size={12}/>}</button><button aria-label="关闭窗口" onClick={()=>void control('close')}><X size={15}/></button></div>}
  </header>{error&&<p role="alert" className="ns-titlebar-error">{error}</p>}</>;
}
