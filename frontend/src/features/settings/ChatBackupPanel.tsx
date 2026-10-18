import { useRef, useState } from 'react';
import { Button } from '@/components/ui/button';
import { Surface } from '@/components/ui/surface';
import { exportChatBackup, importChatBackup, useChatPersistence } from '@/features/chat/chat-store';
import { useGenerationStore } from '@/features/chat/chat-runtime';

export function ChatBackupPanel() {
  const input = useRef<HTMLInputElement>(null);
  const ready = useChatPersistence((state) => state.ready);
  const generating = useGenerationStore((state) => !!state.active);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const disabled = !ready || generating || busy;
  return <Surface className="px-6 py-5">
    <h2 className="text-[15px] font-semibold text-text-1">会话备份</h2>
    <p className="mt-2 text-[13px] text-text-2">会话按条保存到本机数据库。导入会合并新会话，同 ID 的本机会话优先保留；旧版本迁移时保留原始备份。</p>
    <div className="mt-4 flex flex-wrap gap-3">
      <Button variant="outline" disabled={disabled} onClick={() => {
        try {
          const blob = new Blob([exportChatBackup()], { type: 'application/json' });
          const url = URL.createObjectURL(blob);
          const link = document.createElement('a'); link.href = url;
          link.download = `noosphere-conversations-${new Date().toISOString().slice(0,10)}.json`;
          link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000);
          setMessage('备份已导出。文件包含对话内容，请妥善保存。');
        } catch (error) { setMessage(String(error)); }
      }}>导出会话</Button>
      <Button variant="outline" disabled={disabled} onClick={() => input.current?.click()}>导入会话</Button>
      <input ref={input} className="sr-only" type="file" accept="application/json,.json" aria-label="选择会话备份" onChange={async (event) => {
        const file = event.target.files?.[0]; event.target.value = ''; if (!file) return;
        setBusy(true);
        try {
          const result = await importChatBackup(await file.text());
          setMessage(result.recoveredWorkspaces ? `会话已合并并保存。已恢复 ${result.recoveredWorkspaces} 个导入工作区，可在顶部切换；项目目录和默认模型需重新设置。` : '会话已合并并保存。');
        } catch (error) { setMessage(String(error)); }
        finally { setBusy(false); }
      }} />
    </div>
    {message && <p role="status" className="mt-3 text-xs text-text-2">{message}</p>}
  </Surface>;
}
