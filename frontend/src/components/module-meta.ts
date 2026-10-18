/** 后端模块通用映射: 名称保留英文专有名词, 功能与状态中文化 */

/** 后端模块 key → 展示名 (保留品牌英文专有名词) */
export const MODULE_NAME: Record<string, string> = {
  gateway: "Gateway",
  nebula: "Nebula",
  codelens: "CodeLens",
  devops: "DevOps",
  ai: "AI",
};

/** 后端模块 key → 中文功能名 */
export const MODULE_FUNC: Record<string, string> = {
  gateway: "模型管理",
  nebula: "记忆引擎",
  codelens: "代码分析",
  devops: "系统健康",
  ai: "智能对话",
};

/** 模块状态 → 中文 */
export const MODULE_STATE: Record<string, string> = {
  running: "运行正常",
  starting: "启动中",
  stopped: "已停止",
  error: "异常",
  unknown: "未知",
};

/** 模块名称展示: 优先品牌英文名, 未知 key 原样返回 */
export function moduleName(key: string): string {
  return MODULE_NAME[key] ?? key;
}

/** 模块功能: 中文功能名, 未知 key 返回空串 */
export function moduleFunc(key: string): string {
  return MODULE_FUNC[key] ?? "";
}

/** 模块状态: 中文状态, 未知状态原样返回 */
export function moduleState(state?: string | null): string {
  return MODULE_STATE[state ?? ""] ?? state ?? "未知";
}