import type { BackendApi } from "../hooks/useBackend";
import { StatusDot } from "@/components/ui/status-dot";
import { Link } from "react-router-dom";

export default function StatusBar({ backend }: { backend: BackendApi }) {
  const { health, backendStatus, backendError, startingBackend, restartBackend } = backend;
  const online = health === "online";
  const offline = health === "offline" || health === "error";
  const label = startingBackend ? "启动中" : online ? "在线" : offline ? "离线" : "检测中";
  return (
    <footer className="ns-statusbar">
      <span className="flex shrink-0 items-center gap-2"><StatusDot status={online ? "ok" : offline ? "err" : "warn"} /><span aria-live="polite">后端{label}</span><span className="ns-status-english">/ {online ? "CONNECTED" : offline ? "OFFLINE" : "CONNECTING"}</span>{backendStatus.restarts > 0 && <span className="hidden text-warn sm:inline">重启 {backendStatus.restarts} 次</span>}</span>
      {backendError && <Link to="/settings" className="min-w-0 flex-1 truncate text-err underline-offset-2 hover:underline" title={backendError} aria-label={`查看后端错误：${backendError}`}>{backendError}</Link>}
      {(offline || startingBackend || backendError) && <button type="button" className="h-7 shrink-0 border-l border-border-soft pl-3 text-text-1 disabled:opacity-50" disabled={startingBackend} onClick={() => void restartBackend()}>{startingBackend ? "启动中…" : "重试启动"}</button>}
      {!backendError && !startingBackend && <span className="ns-footer-signature">NOOSPHERE <b>DESKTOP</b><i aria-hidden="true" /></span>}
      {online && <span className="hidden sm:inline">BUILD 0.5.0</span>}
    </footer>
  );
}
