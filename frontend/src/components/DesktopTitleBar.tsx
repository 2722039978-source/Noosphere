import { useEffect, useState } from 'react';
import { getCurrentWindow } from '@tauri-apps/api/window';
import { Minus, Square, Copy, X } from 'lucide-react';

/** Native window controls share the archive palette instead of OS accent blue. */
export function DesktopTitleBar() {
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
  if(!native)return null;
  const control=async(action:'minimize'|'maximize'|'close')=>{
    setError('');try{const app=getCurrentWindow();if(action==='minimize')await app.minimize();else if(action==='close')await app.close();else{await app.toggleMaximize();setMaximized(await app.isMaximized());}}catch{setError('窗口操作未完成，请重试。');}
  };
  return <><header className="ns-desktop-titlebar"><div className="ns-desktop-drag" data-tauri-drag-region><img src="/noosphere-mark.png" alt="" draggable={false}/><span data-tauri-drag-region>Noosphere</span></div><div className="ns-desktop-controls"><button aria-label="最小化窗口" onClick={()=>void control('minimize')}><Minus size={14}/></button><button aria-label={maximized?'还原窗口':'最大化窗口'} onClick={()=>void control('maximize')}>{maximized?<Copy size={12}/>:<Square size={12}/>}</button><button aria-label="关闭窗口" onClick={()=>void control('close')}><X size={15}/></button></div></header>{error&&<p role="alert" className="ns-titlebar-error">{error}</p>}</>;
}
