"use client";

import { useEffect, useState } from "react";
import { DEVOPS_METRICS_URL } from "@/lib/services";

export interface LiveMetrics {
  cpu: number;
  memory: number;
  disk: number;
}

/**
 * 拉取 DevOps Agent (:8740) 的真实系统指标。
 * 服务在线 → HUD 显示本机真实 CPU / 内存 / 磁盘占用；
 * 离线 → 返回 null，界面明确标注 SIMULATED 并使用演示数据。
 */
export function useLiveMetrics(intervalMs = 5000): LiveMetrics | null {
  const [metrics, setMetrics] = useState<LiveMetrics | null>(null);

  useEffect(() => {
    let cancelled = false;

    const pull = async () => {
      const ctrl = new AbortController();
      const timer = setTimeout(() => ctrl.abort(), 3500);
      try {
        const res = await fetch(DEVOPS_METRICS_URL, {
          signal: ctrl.signal,
          cache: "no-store",
        });
        if (!res.ok) throw new Error(String(res.status));
        const snap = await res.json();
        if (!cancelled) {
          setMetrics({
            cpu: snap?.cpu?.usage_percent ?? 0,
            memory: snap?.memory?.usage_percent ?? 0,
            disk: snap?.disk?.usage_percent ?? 0,
          });
        }
      } catch {
        if (!cancelled) setMetrics(null);
      } finally {
        clearTimeout(timer);
      }
    };

    pull();
    const timer = setInterval(pull, intervalMs);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [intervalMs]);

  return metrics;
}
