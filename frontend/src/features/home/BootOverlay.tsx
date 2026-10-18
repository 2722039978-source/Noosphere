import { useEffect } from 'react';
import { ArrowUpRight } from 'lucide-react';
export function BootOverlay({ onFinish }: { onFinish: () => void }) {
  useEffect(() => { const timer = setTimeout(onFinish, 1900); return () => clearTimeout(timer); }, [onFinish]);
  return <div className="ns-boot" aria-label="工作台启动" role="dialog" aria-modal="true">
    <span className="ns-boot-corner">PERSONAL INTELLIGENCE SYSTEM</span>
    <div className="ns-boot-core"><div className="ns-orbital-mark" aria-hidden="true"><i /><i /><i /><b /></div><span>WELCOME TO</span><strong>NOOSPHERE</strong><p>你的知识，你的工作空间。</p><div className="ns-boot-progress" /></div>
    <button autoFocus onClick={onFinish}>进入工作台 <ArrowUpRight size={16} /> <kbd>ENTER</kbd></button>
  </div>;
}
