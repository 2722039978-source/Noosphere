import { cn } from "@/lib/utils";

export type DotStatus = "ok" | "warn" | "err" | "idle";

const DOT_COLOR: Record<DotStatus, string> = {
  ok: "bg-ok",
  warn: "bg-warn",
  err: "bg-err",
  idle: "bg-text-3",
};

const PULSE_COLOR: Record<DotStatus, string> = {
  ok: "rgba(52, 211, 153, 0.4)",
  warn: "rgba(232, 179, 75, 0.4)",
  err: "rgba(241, 102, 122, 0.4)",
  idle: "rgba(245, 245, 247, 0.25)",
};

/** 状态点: 颜色承担功能语义, 可选柔和脉冲 */
export function StatusDot({
  status,
  pulse = false,
  className,
}: {
  status: DotStatus;
  pulse?: boolean;
  className?: string;
}) {
  return (
    <span
      className={cn(
        "inline-block size-[9px] shrink-0 rounded-full",
        DOT_COLOR[status],
        pulse && "ns-pulse",
        className
      )}
      style={pulse ? ({ "--ns-pulse-color": PULSE_COLOR[status] } as React.CSSProperties) : undefined}
    />
  );
}
