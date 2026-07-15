"use client";

import { useEffect, useState } from "react";
import { SERVICES, type ServiceStatus } from "@/lib/services";

export type StatusMap = Record<string, ServiceStatus>;

/**
 * 轮询三个 Noosphere 后端服务的健康状态。
 * - 本机启动了服务（:8765 / :8730 / :8740）→ 显示真实在线状态
 * - 未启动 → 请求静默失败，状态标记为 offline，页面降级为演示数据
 */
export function useServiceStatus(intervalMs = 15000): {
  statuses: StatusMap;
  onlineCount: number;
} {
  const [statuses, setStatuses] = useState<StatusMap>(() =>
    Object.fromEntries(SERVICES.map((s) => [s.id, "checking"])),
  );

  useEffect(() => {
    let cancelled = false;

    const probe = async () => {
      const results = await Promise.all(
        SERVICES.map(async (svc) => {
          const ctrl = new AbortController();
          const timer = setTimeout(() => ctrl.abort(), 3000);
          try {
            const res = await fetch(`${svc.base}${svc.health}`, {
              signal: ctrl.signal,
              cache: "no-store",
            });
            return [svc.id, res.ok ? "online" : "offline"] as const;
          } catch {
            return [svc.id, "offline"] as const;
          } finally {
            clearTimeout(timer);
          }
        }),
      );
      if (!cancelled) setStatuses(Object.fromEntries(results));
    };

    probe();
    const timer = setInterval(probe, intervalMs);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [intervalMs]);

  const onlineCount = Object.values(statuses).filter((s) => s === "online").length;
  return { statuses, onlineCount };
}
