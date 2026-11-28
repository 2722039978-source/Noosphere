import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

interface EmptyStateProps {
  icon?: ReactNode;
  title: string;
  description?: string;
  action?: ReactNode;
  className?: string;
}

/** 统一空状态 */
export function EmptyState({ icon, title, description, action, className }: EmptyStateProps) {
  return (
    <div className={cn("ns-enter flex flex-col items-center justify-center py-14 text-center", className)}>
      {icon && (
        <div className="mb-4 flex size-12 items-center justify-center rounded-xl border border-border-soft bg-bg-elevated text-brand shadow-soft">
          {icon}
        </div>
      )}
      <p className="text-[14.5px] font-medium text-text-1">{title}</p>
      {description && (
        <p className="mt-1.5 max-w-sm text-[13px] leading-relaxed text-text-3">{description}</p>
      )}
      {action && <div className="mt-5">{action}</div>}
    </div>
  );
}

interface LoadingStateProps {
  label?: string;
  className?: string;
}

/** 统一加载状态 (轻量轨道脉冲) */
export function LoadingState({ label = "正在加载…", className }: LoadingStateProps) {
  return (
    <div className={cn("flex items-center justify-center gap-3 py-14 text-text-3", className)} role="status">
      <span className="ns-mini-orbit" aria-hidden="true"><i /><b /></span>
      <span className="text-[13px]">{label}</span>
    </div>
  );
}
