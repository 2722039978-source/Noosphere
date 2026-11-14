import { useState } from "react";
import { invoke, isTauri } from "@tauri-apps/api/core";
import { Button } from "@/components/ui/button";
import { Surface } from "@/components/ui/surface";
import { exportChatBackup, flushChatPersistence, useChatPersistence } from "@/features/chat/chat-store";
import { useGenerationStore } from "@/features/chat/chat-runtime";

interface UpdateInfo {
  current_version: string;
  available_version: string | null;
  installer_available: boolean;
  downloaded_version: string | null;
}

interface DownloadedUpdate {
  version: string;
  installer: string;
}

export function UpdatePanel() {
  const [info, setInfo] = useState<UpdateInfo>();
  const [prepared, setPrepared] = useState<DownloadedUpdate>();
  const [busy, setBusy] = useState<"check" | "download">();
  const [message, setMessage] = useState("");
  const ready = useChatPersistence(state => state.ready);
  const generating = useGenerationStore(state => !!state.active);

  if (!isTauri()) return null;

  async function check() {
    setBusy("check");
    setMessage("");
    try {
      const next = await invoke<UpdateInfo>("update_check");
      setInfo(next);
      setMessage(next.available_version
        ? next.installer_available
          ? `可以更新到 ${next.available_version}。`
          : `${next.available_version} 尚未提供可校验的 Windows 安装包。`
        : next.current_version === next.downloaded_version
          ? `当前运行的是已下载的 ${next.current_version} 版。`
          : "当前已是最新正式版。");
    } catch (error) {
      setMessage(String(error));
    } finally {
      setBusy(undefined);
    }
  }

  async function download() {
    if (!info?.available_version) return;
    setBusy("download");
    setMessage("");
    try {
      if (!(await flushChatPersistence())) throw new Error("会话尚未保存，请重试后再下载更新。");
      const update = await invoke<DownloadedUpdate>("update_download", {
        expectedVersion: info.available_version,
        chatBackup: exportChatBackup(),
      });
      setPrepared(update);
      setMessage(`${update.version} 安装包已通过 SHA-256 校验，会话备份已保存。请打开下载目录，退出 Noosphere，再运行安装包。`);
    } catch (error) {
      setMessage(String(error));
    } finally {
      setBusy(undefined);
    }
  }

  return (
    <Surface className="px-6 py-5">
      <h2 className="text-[15px] font-semibold text-text-1">软件更新</h2>
      <p className="mt-2 text-[13px] text-text-2">从官方 GitHub Release 下载完整安装包。安装前请关闭 Noosphere。</p>
      <div className="mt-4 flex flex-wrap gap-3">
        <Button variant="outline" disabled={!!busy} onClick={() => void check()}>{busy === "check" ? "正在检查…" : "检查更新"}</Button>
        {info?.installer_available && (
          <Button variant="outline" disabled={!!busy || !ready || generating} onClick={() => void download()}>{busy === "download" ? "正在下载和校验…" : "下载更新并备份会话"}</Button>
        )}
        {(prepared || info?.downloaded_version) && (
          <Button variant="outline" onClick={() => {
            void invoke("update_show_folder").catch(error => setMessage(String(error)));
          }}>打开下载目录</Button>
        )}
      </div>
      {message && <p role="status" className="mt-3 text-xs text-text-2">{message}</p>}
      {prepared && <p className="mt-2 break-all text-xs text-text-3">{prepared.installer}</p>}
    </Surface>
  );
}
