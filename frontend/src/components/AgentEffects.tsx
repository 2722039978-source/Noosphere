import { useState } from 'react';
import type { AgentServiceEffect } from '@/lib/api';
import { Button } from './ui/button';

const statusText: Record<string, string> = {
  applying: '执行中', applied: '已生效', restoring: '恢复中', restored: '已恢复',
  unknown: '操作结果待核查', obsolete: '服务状态已变化',
};
const stateText = (value: string) => value === 'running' ? '运行' : '停止';

export function AgentEffects({ items, allowRestore, busy, onRestore }: {
  items: AgentServiceEffect[];
  allowRestore: boolean;
  busy: boolean;
  onRestore: (effect: AgentServiceEffect) => Promise<void>;
}) {
  const [selected, setSelected] = useState('');
  return <section className="space-y-2">
    <h3 className="font-semibold">本机服务变更</h3>
    {items.map(item => <div key={item.id} className="rounded border border-border-subtle p-2">
      <p><strong>{item.name}</strong> · {stateText(item.before)} → {stateText(item.after)} · {statusText[item.status] ?? item.status}</p>
      {allowRestore && item.status === 'applied' && (selected === item.id
        ? <div className="flex gap-2">
            <Button disabled={busy} size="sm" onClick={() => { setSelected(''); void onRestore(item); }}>确认将 {item.name} 恢复为{stateText(item.before)}</Button>
            <Button disabled={busy} variant="outline" size="sm" onClick={() => setSelected('')}>取消</Button>
          </div>
        : <Button disabled={busy} variant="outline" size="sm" onClick={() => setSelected(item.id)}>恢复此服务</Button>)}
    </div>)}
  </section>;
}
