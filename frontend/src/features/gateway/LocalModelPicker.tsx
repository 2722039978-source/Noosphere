import { Box, Search } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import type { ModelConfig } from '@/lib/api';
import { cn } from '@/lib/utils';
import { isLocalModel, modelChoiceStatus, modelConnectionKey, needsModelSetup, type LocalModelChoice } from './local-model-match';

const BROWSE = 'browse';

export function LocalModelPicker({ choices, current, disabled, label, className, onSelect, onBrowse }: {
  choices: LocalModelChoice[];
  current?: ModelConfig;
  disabled?: boolean;
  label: string;
  className?: string;
  onSelect: (choice: LocalModelChoice) => void;
  onBrowse: () => void;
}) {
  const key = current?.enabled !== false && current ? modelConnectionKey(current) : '';
  const selected = choices.find(choice => choice.key === key);
  if (!choices.length) return <Button type="button" variant="ghost" size="sm" className={className} disabled={disabled} onClick={onBrowse}><Box aria-hidden="true" />连接模型</Button>;

  return <Select disabled={disabled} value={selected && !needsModelSetup(selected) ? selected.key : ''} onValueChange={value => {
    if (value === BROWSE) onBrowse();
    else {
      const choice = choices.find(item => item.key === value);
      if (choice) onSelect(choice);
    }
  }}>
    <SelectTrigger size="sm" aria-label={label} title={selected?.name} className={cn('min-w-0 [&_[data-slot=select-value]]:truncate', className)}>
      <Box aria-hidden="true" />
      <SelectValue placeholder={selected?.name ?? (current?.enabled === false ? '模型已停用' : '选择模型')}>{selected?.name}</SelectValue>
    </SelectTrigger>
    <SelectContent align="end" className="max-w-[calc(100vw-24px)]">
      <SelectGroup>
        {choices.map(choice => {
          const status = modelChoiceStatus(choice);
          const showStatus = ['未启动', '启动中', '未找到引擎', '暂不可用', '已停用'].includes(status);
          const detail = isLocalModel(choice.target) ? (showStatus ? status : '') : (showStatus ? 'API · ' + status : 'API');
          return <SelectItem key={choice.key} value={choice.key} disabled={choice.config?.enabled === false} className="whitespace-normal">
            {choice.name}
            {detail && <span className="ml-2 whitespace-nowrap text-xs text-text-3">{detail}</span>}
          </SelectItem>;
        })}
      </SelectGroup>
      <SelectItem value={BROWSE} className="mt-1 border-t border-border-soft"><span className="inline-flex items-center gap-2 text-brand"><Search size={14} aria-hidden="true" />查找本地模型…</span></SelectItem>
    </SelectContent>
  </Select>;
}
