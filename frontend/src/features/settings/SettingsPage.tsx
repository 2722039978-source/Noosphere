import { useEffect, useState } from "react";
import { Moon, Plus, Sun, Trash2, Zap } from "lucide-react";
import { useBackend } from "@/hooks/useBackend";
import { Button } from "@/components/ui/button";
import { Surface } from "@/components/ui/surface";
import { PageHeader } from "@/components/ui/page-header";
import { Badge } from "@/components/ui/badge";
import { ModelForm } from "@/features/gateway/ModelForm";
import { errMessage } from "@/lib/api";
import { useThemeStore } from "@/stores/theme-store";
import { ChatBackupPanel } from './ChatBackupPanel';
import { ExperienceSettings } from './ExperienceSettings';


export function SettingsPage() {
  const backend = useBackend();
  const theme = useThemeStore((s) => s.theme);
  const toggleTheme = useThemeStore((s) => s.toggle);
  const { models, loadModels, deleteModel } = backend;
  const [actionError, setActionError] = useState("");
  const [deleting, setDeleting] = useState<string | null>(null);
  const remove = async (id: string) => { if (deleting) return; setDeleting(id); setActionError(""); try { await deleteModel(id); } catch (error) { setActionError(errMessage(error)); } finally { setDeleting(null); } };
  const [showForm, setShowForm] = useState(false);

  useEffect(() => {
    loadModels();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);


  return (
    <div className="flex-1 overflow-y-auto">
      <div className="ns-page-wrap">
        <PageHeader title="设置" subtitle="应用信息与 AI 模型接入配置" />
        <ExperienceSettings />
        <ChatBackupPanel />

        {/* 应用信息 */}
        <Surface className="px-6 py-5">
          <h2 className="text-[15px] font-semibold text-text-1">应用信息</h2>
          <div className="mt-3 grid grid-cols-1 gap-x-6 gap-y-1 sm:grid-cols-2">
            <InfoRow label="版本" value="0.5.0" />
            <InfoRow
              label="后端状态"
              value={backend.startingBackend ? "启动中" : backend.health === "online" ? "在线" : backend.health === "checking" ? "检测中" : "离线"}
            />
            <InfoRow label="接口地址" value="127.0.0.1:8080" />
            <InfoRow label="数据目录" value={backend.backendStatus.data_dir || "等待桌面服务提供路径"} />
          </div>
          {(backend.backendError || backend.health === "offline" || backend.health === "error" || backend.startingBackend) && (
            <div className="mt-4 flex flex-wrap items-center gap-3 border-t border-border-soft pt-4">
              <p role="status" className="min-w-0 flex-1 basis-52 break-words text-xs leading-relaxed text-err">{backend.startingBackend ? "正在启动本地后端，连接恢复后会自动刷新模型与记忆会话。" : backend.backendError || "暂时无法连接本地后端，请重试启动。"}</p>
              <Button variant="outline" size="sm" disabled={backend.startingBackend} onClick={() => void backend.restartBackend()}>{backend.startingBackend ? "启动中…" : "重试启动"}</Button>
            </div>
          )}
        </Surface>

        <Surface className="flex items-center gap-4 px-6 py-5">
          <div className="min-w-0 flex-1">
            <h2 className="text-[15px] font-semibold text-text-1">外观</h2>
            <p className="mt-1 text-[13px] text-text-3">档案界面 · {theme === "light" ? "暖灰浅色" : "石墨深色"}</p>
          </div>
          <Button variant="outline" onClick={toggleTheme}>
            {theme === "dark" ? <Sun aria-hidden="true" /> : <Moon aria-hidden="true" />}
            {theme === "dark" ? "浅色模式" : "深色模式"}
          </Button>
        </Surface>

        <Surface className="px-6 py-5">
          <h2 className="text-[15px] font-semibold text-text-1">界面与字体</h2>
          <p className="mt-2 text-[13px] leading-relaxed text-text-2">视觉参考本地 RhineLabUI，保留 Noosphere 的模块与工作流。MiSans 字体由小米提供，原版文件随应用本地加载。</p>
          <div className="mt-3 flex flex-wrap gap-5 text-xs text-text-2"><a className="underline underline-offset-4" href="/fonts/MiSans-license.pdf" target="_blank" rel="noreferrer">MiSans 字体许可 ↗</a><a className="underline underline-offset-4" href="/fonts/NOTICE.txt" target="_blank" rel="noreferrer">字体来源与署名 ↗</a></div>
        </Surface>

        {/* AI 模型接入 */}
        <Surface className="px-6 py-5">
          <div className="flex flex-wrap items-center gap-2.5">
            <h2 className="text-[15px] font-semibold text-text-1">AI 模型接入</h2>
            <Badge variant="secondary" className="text-xs">{models.length}</Badge>
            <Button size="sm" className="ml-auto gap-1.5" onClick={() => setShowForm((v) => !v)}>
              <Plus size={14} /> {showForm ? "取消" : "添加模型"}
            </Button>
          </div>

          {actionError && <p role="alert" className="ns-form-error">{actionError}</p>}
          {showForm && <ModelForm onSaved={() => setShowForm(false)} onCancel={() => setShowForm(false)} />}

          <div className="mt-4">
            {models.length === 0 ? (
              <div className="flex flex-col items-center py-8 text-center">
                <Zap size={18} className="mb-2 text-acc-gateway" />
                <p className="text-[13px] text-text-3">尚未配置模型，添加后即可启用 AI 对话。</p>
              </div>
            ) : (
              <div className="flex flex-col gap-2">
                {models.map((m) => (
                  <div key={m.id} className="flex items-center gap-3 rounded-none bg-secondary px-4 py-3">
                    <Zap size={14} className="shrink-0 text-acc-gateway" />
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2 text-[13.5px] font-medium">
                        <span className="truncate text-text-1">{m.name}</span>
                        {m.is_default && <Badge className="shrink-0 text-xs">默认</Badge>}
                      </div>
                      <div className="truncate font-mono text-xs text-text-3">
                        {m.provider} · {m.model_name} · {m.base_url}
                      </div>
                    </div>
                    <button
                      onClick={() => void remove(m.id)}
                      disabled={deleting !== null}
                      aria-label={`删除模型 ${m.name}`}
                      title="删除"
                      className="cursor-pointer p-1 text-text-3 hover:text-err"
                    >
                      <Trash2 size={14} />
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>
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
