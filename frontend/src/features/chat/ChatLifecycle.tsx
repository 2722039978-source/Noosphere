import { useEffect, useState } from "react";
import { isTauri } from "@tauri-apps/api/core";
import { getCurrentWindow } from "@tauri-apps/api/window";
import { Button } from "@/components/ui/button";
import { flushChatPersistence, isChatPersistenceDirty, useChatPersistence } from "./chat-store";
import { chatRunner, prepareChatShutdown, retryCancellation, useGenerationStore } from "./chat-runtime";

/** Mounted once above routes: persist and cancel before the native window is destroyed. */
export function ChatLifecycle() {
  const error = useChatPersistence((state) => state.error);
  const cancelError = useGenerationStore((state) => state.cancelError);
  const [closeError, setCloseError] = useState("");
  useEffect(() => {
    const beforeUnload = (event: BeforeUnloadEvent) => {
      chatRunner.flush();
      void flushChatPersistence();
      if (isChatPersistenceDirty()) { event.preventDefault(); event.returnValue = ""; }
    };
    window.addEventListener("beforeunload", beforeUnload);
    let disposed = false;
    let closing = false;
    let unlisten: (() => void) | undefined;
    if (isTauri()) {
      const windowHandle = getCurrentWindow();
      void windowHandle.onCloseRequested(async (event) => {
        event.preventDefault();
        if (closing) return;
        closing = true;
        try {
          if (await prepareChatShutdown()) await windowHandle.destroy();
          else setCloseError("退出前保存或取消请求未完成，请重试后关闭窗口。");
        } catch { setCloseError("暂时无法关闭窗口，请重试。"); }
        finally { closing = false; }
      }).then((cleanup) => { if (disposed) cleanup(); else unlisten = cleanup; }, () => setCloseError("窗口关闭监听不可用，请先确认会话已保存。"));
    }
    return () => { disposed = true; unlisten?.(); window.removeEventListener("beforeunload", beforeUnload); };
  }, []);
  if (!error && !cancelError && !closeError) return null;
  return (
    <div role="alert" className="flex shrink-0 flex-wrap items-center justify-center gap-3 border-b border-err/30 bg-bg-raised px-4 py-2 text-xs text-err">
      <span>{error || cancelError?.message || closeError}</span>
      <Button size="sm" variant="outline" onClick={() => {
        chatRunner.flush();
        void Promise.all([flushChatPersistence(),retryCancellation()]).then(([saved,cancelled]) => { if (saved && cancelled) setCloseError(""); });
      }}>重试</Button>
    </div>
  );
}
