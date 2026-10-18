import { useExperienceStore, type GraphicsMode } from '@/stores/experience-store';
import { Surface } from '@/components/ui/surface';
export function ExperienceSettings() {
  const { graphics, boot, startPage, update } = useExperienceStore();
  return <Surface className="ns-experience-settings px-6 py-5">
    <div><h2 className="text-[15px] font-semibold">工作台体验</h2><p className="mt-1 text-[13px] text-text-3">档案入口与视觉性能，按你的工作习惯调整。</p></div>
    <div className="ns-preference-row"><label htmlFor="start-page">默认入口<small>进入应用时首先打开的工作区</small></label><select id="start-page" value={startPage} onChange={(event) => update({ startPage: event.target.value as 'archive' | 'chat' })}><option value="archive">三维档案工作台</option><option value="chat">AI 对话</option></select></div>
    <div className="ns-preference-row"><label htmlFor="graphics-mode">三维画质<small>自动模式根据实际帧间隔调整；三维静止后休眠，界面保持轻量动效</small></label><select id="graphics-mode" value={graphics} onChange={(event) => update({ graphics: event.target.value as GraphicsMode })}><option value="auto">自动调整</option><option value="high">精细 · 清晰光影</option><option value="low">节能 · 保留清晰度</option><option value="off">关闭 · 二维档案</option></select></div>
    <div className="ns-preference-row"><label htmlFor="boot-motion">启动过渡<small>每次启动仅播放一次，可随时跳过</small></label><input id="boot-motion" type="checkbox" checked={boot} onChange={(event) => update({ boot: event.target.checked })} /></div>
  </Surface>;
}
