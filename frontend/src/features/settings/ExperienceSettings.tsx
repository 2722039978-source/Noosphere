import { Link } from 'react-router-dom';
import { useExperienceStore, type GraphicsMode } from '@/stores/experience-store';
import { Surface } from '@/components/ui/surface';
export function ExperienceSettings() {
  const { graphics, boot, update } = useExperienceStore();
  return <Surface className="ns-experience-settings px-6 py-5">
    <div><h2 className="text-[15px] font-semibold">档案预览</h2><p className="mt-1 text-[13px] text-text-3">启动时恢复最近项目与对话。可以从这里浏览三维档案。</p></div>
    <Link to="/home" className="text-sm underline underline-offset-4">打开动效主页</Link>
    <div className="ns-preference-row"><label htmlFor="graphics-mode">三维画质<small>自动模式根据实际帧间隔调整；三维静止后休眠，界面保持轻量动效</small></label><select id="graphics-mode" value={graphics} onChange={(event) => update({ graphics: event.target.value as GraphicsMode })}><option value="auto">自动调整</option><option value="high">精细 · 清晰光影</option><option value="low">节能 · 保留清晰度</option><option value="off">关闭 · 二维档案</option></select></div>
    <div className="ns-preference-row"><label htmlFor="boot-motion">档案预览过渡</label><input id="boot-motion" type="checkbox" checked={boot} onChange={(event) => update({ boot: event.target.checked })} /></div>
  </Surface>;
}
