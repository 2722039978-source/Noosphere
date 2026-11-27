import { Moon, Sun } from "lucide-react";
import { Link } from 'react-router-dom';
import { useBackend } from "@/hooks/useBackend";
import { Button } from "@/components/ui/button";
import { Surface } from "@/components/ui/surface";
import { PageHeader } from "@/components/ui/page-header";
import { selectedChatModel } from '@/lib/model-capabilities';
import { useWorkspaceStore } from '@/stores/workspace-store';
import { useThemeStore } from "@/stores/theme-store";
import { ChatBackupPanel } from './ChatBackupPanel';
import { ExperienceSettings } from './ExperienceSettings';
import { UpdatePanel } from './UpdatePanel';
import { version } from '../../../package.json';


export function SettingsPage() {
  const backend = useBackend();
  const workspaceModel = useWorkspaceStore(state => state.workspaces.find(item => item.id === state.activeId)?.default_model_id);
  const defaultModel = selectedChatModel(backend.models, '', workspaceModel);
  const modelLabel = backend.modelsError || (!backend.modelsReady ? '正在读取模型…'
    : defaultModel ? (defaultModel.name || defaultModel.model_name) + (defaultModel.enabled === false ? '（已停用）' : '')
    : workspaceModel ? '工作区模型不可用' : '尚未连接模型');
  const theme = useThemeStore((s) => s.theme);
  const toggleTheme = useThemeStore((s) => s.toggle);
  return (
    <div className="flex-1 overflow-y-auto">
      <div id="settings-start" className="ns-page-wrap">
        <PageHeader title="设置" />
        <Surface className="flex items-center gap-4 px-6 py-5">
          <div className="min-w-0 flex-1">
            <h2 className="text-[15px] font-semibold text-text-1">外观</h2>
            <p className="mt-1 text-[13px] text-text-3">{theme === "light" ? "浅色模式" : "深色模式"}</p>
          </div>
          <Button variant="outline" onClick={toggleTheme}>
            {theme === "dark" ? <Sun aria-hidden="true" /> : <Moon aria-hidden="true" />}
            {theme === "dark" ? "切换浅色" : "切换深色"}
          </Button>
        </Surface>
        <Surface className="flex flex-wrap items-center gap-3 px-6 py-5">
          <div className="min-w-0 flex-1 basis-48">
            <h2 className="text-[15px] font-semibold text-text-1">模型连接</h2>
            <p className="mt-1 text-[13px] text-text-3">新会话默认 · {modelLabel}</p>
          </div>
          <Button asChild size="sm" variant="outline"><Link to="/model-hub">本地模型</Link></Button>
          <Button asChild size="sm" variant="outline"><Link to="/model-hub/config">管理连接</Link></Button>
        </Surface>
        <ExperienceSettings />
        <ChatBackupPanel />
        <UpdatePanel />

        {/* 应用信息 */}
        <Surface id="app-info" className="px-6 py-5">
          <h2 className="text-[15px] font-semibold text-text-1">应用信息</h2>
          <div className="mt-3 grid grid-cols-1 gap-x-6 gap-y-1 sm:grid-cols-2">
            <InfoRow label="版本" value={version} />
            <InfoRow
              label="后端状态"
              value={backend.startingBackend ? "启动中" : backend.health === "online" ? "在线" : backend.health === "checking" ? "检测中" : "离线"}
            />
            <InfoRow label="数据目录" value={backend.backendStatus.data_dir || "等待桌面服务提供路径"} />
          </div>
          {(backend.backendError || backend.health === "offline" || backend.health === "error" || backend.startingBackend) && (
            <div className="mt-4 flex flex-wrap items-center gap-3 border-t border-border-soft pt-4">
              <p role="status" className="min-w-0 flex-1 basis-52 break-words text-xs leading-relaxed text-err">{backend.startingBackend ? "正在启动本地后端，连接恢复后会自动刷新模型与记忆会话。" : backend.backendError || "暂时无法连接本地后端，请重试启动。"}</p>
              <Button variant="outline" size="sm" disabled={backend.startingBackend} onClick={() => void backend.restartBackend()}>{backend.startingBackend ? "启动中…" : "重试启动"}</Button>
            </div>
          )}
          {(backend.health === 'offline' || backend.health === 'error') && <Button asChild variant="outline" size="sm" className="mt-4"><Link to="/devops?view=diagnostics">打开诊断工具</Link></Button>}
          <details className="ns-record-detail mt-4"><summary>界面与字体</summary>
            <div className="ns-detail-body flex flex-wrap gap-5 text-xs text-text-2"><a className="underline underline-offset-4" href="/fonts/MiSans-license.pdf" target="_blank" rel="noreferrer">MiSans 字体许可 ↗</a><a className="underline underline-offset-4" href="/fonts/NOTICE.txt" target="_blank" rel="noreferrer">来源与署名 ↗</a></div>
          </details>
        </Surface>

      </div>
    </div>
  );
}

function InfoRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex min-w-0 items-start justify-between gap-4 border-b border-border-subtle py-2.5 text-[13px]">
      <span className="shrink-0 text-text-3">{label}</span>
      <span className="min-w-0 break-all text-right text-text-2">{value}</span>
    </div>
  );
}
