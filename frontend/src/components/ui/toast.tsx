import { createContext, useCallback, useContext, useState, type ReactNode } from "react";
import { cn } from "@/lib/utils";

interface ToastItem {
  id: number;
  message: string;
  variant: "info" | "ok" | "err";
}

interface ToastApi {
  toast: (message: string, variant?: ToastItem["variant"]) => void;
}

const ToastCtx = createContext<ToastApi>({ toast: () => {} });

/** 全局 toast 提示 (轻量自研, 挂载于 AppProviders) */
export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([]);

  const toast = useCallback((message: string, variant: ToastItem["variant"] = "info") => {
    const id = Date.now() + Math.random();
    setItems((prev) => [...prev, { id, message, variant }]);
    setTimeout(() => setItems((prev) => prev.filter((t) => t.id !== id)), 3200);
  }, []);

  return (
    <ToastCtx.Provider value={{ toast }}>
      {children}
      {/* Toast viewport */}
      <div className="pointer-events-none fixed bottom-10 left-1/2 z-[100] flex -translate-x-1/2 flex-col items-center gap-2">
        {items.map((t) => (
          <div
            key={t.id}
            className={cn(
              "animate-in fade-in slide-in-from-bottom-2 rounded-lg border px-3.5 py-2 text-xs shadow-lg",
              t.variant === "ok" && "border-ok/30 bg-bg-raised text-ok",
              t.variant === "err" && "border-err/30 bg-bg-raised text-err",
              t.variant === "info" && "border-border bg-bg-raised text-text-1"
            )}
          >
            {t.message}
          </div>
        ))}
      </div>
    </ToastCtx.Provider>
  );
}

export function useToast() {
  return useContext(ToastCtx);
}
