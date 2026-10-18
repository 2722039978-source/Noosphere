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
    <div className={cn("flex flex-col items-center justify-center py-16 text-center", className)}>
      {icon && (
        <div className="mb-4 flex size-12 items-center justify-center rounded-2xl bg-secondary text-text-2">
          {icon}
        </div>
      )}
      <p className="text-[14px] font-medium text-text-2">{title}</p>
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

/** 统一加载状态 (轻微呼吸) */
export function LoadingState({ label = "正在加载…", className }: LoadingStateProps) {
  return (
    <div className={cn("flex items-center justify-center gap-2.5 py-16 text-text-3", className)}>
      <span className="ns-breathe size-2 rounded-full bg-brand" />
      <span className="text-[13px]">{label}</span>
    </div>
  );
}