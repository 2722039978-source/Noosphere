/**
 * 与本地 Noosphere 三服务的连接配置。
 * 官网运行在 :3000，与三个后端端口互不冲突；
 * 服务未启动时所有请求优雅降级，页面自动切换为模拟数据。
 */

export interface ServiceEndpoint {
  id: "gateway" | "codelens" | "nebula" | "devops";
  name: string;
  codename: string;
  port: number;
  /** 服务根地址（可用 NEXT_PUBLIC_* 覆盖，便于部署到非本机环境） */
  base: string;
  /** 健康检查路径 */
  health: string;
  /** 控制台入口 */
  console: string;
}

export const SERVICES: ServiceEndpoint[] = [
  {
    id: "gateway",
    name: "AI Gateway",
    codename: "GATEWAY",
    port: 8800,
    base: process.env.NEXT_PUBLIC_GATEWAY_URL ?? "http://localhost:8800",
    health: "/health",
    console: "http://localhost:8800",
  },
  {
    id: "codelens",
    name: "CodeLens",
    codename: "STRUCTURE",
    port: 8765,
    base: process.env.NEXT_PUBLIC_CODELENS_URL ?? "http://localhost:8765",
    health: "/api/v1/health",
    console: "http://localhost:8765",
  },
  {
    id: "nebula",
    name: "Nebula",
    codename: "MEMORY",
    port: 8730,
    base: process.env.NEXT_PUBLIC_NEBULA_URL ?? "http://localhost:8730",
    health: "/health",
    console: "http://localhost:8730",
  },
  {
    id: "devops",
    name: "DevOps",
    codename: "SENTINEL",
    port: 8740,
    base: process.env.NEXT_PUBLIC_DEVOPS_URL ?? "http://localhost:8740",
    health: "/health",
    console: "http://localhost:8740/web/",
  },
];

/** DevOps 实时指标端点（在线时 HUD 展示真实 CPU / 内存数据） */
export const DEVOPS_METRICS_URL = `${SERVICES[3].base}/api/v1/devops/metrics`;

export type ServiceStatus = "checking" | "online" | "offline";
