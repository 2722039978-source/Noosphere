/**
 * 站点全部文案与数据 —— 真实 Noosphere 项目内容，非占位符。
 * 指标来源：NOOSPHERE_VISION.md 与各服务 README。
 */

// ── 导航 ──
export const NAV_LINKS = [
  { label: "首页", href: "#hero" },
  { label: "智能系统", href: "#systems" },
  { label: "技术架构", href: "#architecture" },
  { label: "产品矩阵", href: "#products" },
  { label: "未来计划", href: "#roadmap" },
] as const;

// ── 智能系统四大核心模块 ──
export interface SystemModule {
  codename: string;
  name: string;
  desc: string;
  /** 面板上跳动的核心数字 */
  stat: { value: number; suffix: string; label: string; digits?: number };
  tags: string[];
}

export const SYSTEM_MODULES: SystemModule[] = [
  {
    codename: "NEURAL CORE",
    name: "认知图谱引擎",
    desc: "基于 Tree-sitter AST 的多语言解析，预建调用、继承、依赖三重图谱。AI 不再逐行读代码，而是一次 O(1) 查询直达答案。",
    stat: { value: 99.6, suffix: "%", label: "上下文节省率", digits: 1 },
    tags: ["Tree-sitter", "NetworkX", "7 种语言"],
  },
  {
    codename: "MEMORY ENGINE",
    name: "记忆核心",
    desc: "自研 LSM-Tree 存储引擎，WAL 保障持久化。项目风格、命名规范、错误处理偏好——一次学习，永久记忆。",
    stat: { value: 85, suffix: "%", label: "代码一次过审率" },
    tags: ["LSM-Tree", "WAL", "零外部依赖"],
  },
  {
    codename: "HYBRID RETRIEVAL",
    name: "混合检索矩阵",
    desc: "HNSW 向量语义检索与 BM25 关键词检索双路并行，RRF 融合排序。语义与精确匹配，一个都不放过。",
    stat: { value: 800, suffix: " tok", label: "单次查询成本" },
    tags: ["HNSW", "BM25", "RRF 融合"],
  },
  {
    codename: "OPS SENTINEL",
    name: "运维感知系统",
    desc: "系统指标采集、日志异常检测、故障经验沉淀。每一次故障都变成组织记忆，下一次同类问题秒级匹配解决方案。",
    stat: { value: 80, suffix: "%", label: "故障排查时间缩减" },
    tags: ["指标采集", "异常检测", "情景记忆"],
  },
];

// ── HUD 仪表盘指标 ──
export interface GaugeMetric {
  label: string;
  en: string;
  value: number; // 0-100
  display: string;
}

export const GAUGES: GaugeMetric[] = [
  { label: "上下文压缩", en: "CTX COMPRESSION", value: 99.6, display: "99.6%" },
  { label: "检索命中率", en: "RETRIEVAL HIT", value: 92, display: "92.0%" },
  { label: "能源效率", en: "POWER EFFICIENCY", value: 87, display: "87.3%" },
  { label: "记忆持久化", en: "MEMORY PERSIST", value: 100, display: "100%" },
];

// ── 产品矩阵 ──
export interface Product {
  id: "codelens" | "nebula" | "devops";
  name: string;
  subtitle: string;
  desc: string;
  features: string[];
  port: number;
  lang: string;
  /** Three.js 展示台使用的几何体 */
  geometry: "lattice" | "torus" | "shard";
  accent: string;
}

export const PRODUCTS: Product[] = [
  {
    id: "codelens",
    name: "CodeLens",
    subtitle: "把「读代码」变成「查数据库」",
    desc: "342 个文件的项目，AI 只需 800 token 就能回答「谁调用了 authenticate」。调用链、影响分析、依赖图谱，全部预消化。",
    features: ["调用链追踪", "修改影响分析", "RAG 代码问答", "Git Diff 风险分析"],
    port: 8765,
    lang: "Python",
    geometry: "lattice",
    accent: "#00A8FF",
  },
  {
    id: "nebula",
    name: "Nebula",
    subtitle: "把「揣摩偏好」变成「注入标准」",
    desc: "扫描代码库，学习团队的命名规范、错误处理模式、框架偏好，自动注入每次 AI 对话。生成的代码天生「对味」。",
    features: ["风格自动学习", "System Prompt 注入", "混合语义检索", "嵌入式记忆引擎"],
    port: 8730,
    lang: "Go",
    geometry: "torus",
    accent: "#66CFFF",
  },
  {
    id: "devops",
    name: "DevOps",
    subtitle: "把「老员工的经验」变成「可检索的记忆」",
    desc: "「上次这个报错怎么解决的」不再需要翻聊天记录。故障诊断、解决方案、系统指标，全部沉淀为持久化记忆。",
    features: ["实时指标监控", "日志异常检测", "故障经验检索", "LLM 根因推理"],
    port: 8740,
    lang: "Go",
    geometry: "shard",
    accent: "#3B82C4",
  },
];

// ── 技术架构三层 ──
export const ARCH_LAYERS = [
  {
    tier: "LAYER 01",
    title: "结构化压缩",
    body: "代码不是平面文本。Tree-sitter 解析 → 实体提取 → 知识图谱 → 向量索引，四级流水线把 200K token 的原始代码压缩为 3K token 的结构化认知。",
  },
  {
    tier: "LAYER 02",
    title: "风格固化",
    body: "隐性规范显性化。命名习惯、错误处理、返回格式，从代码中提取为约束层，注入每一次生成。",
  },
  {
    tier: "LAYER 03",
    title: "经验沉淀",
    body: "运维知识不再随人员流动而蒸发。情景记忆 + 语义记忆双轨存储，让系统「记得」每一次故障的来龙去脉。",
  },
] as const;

// ── 未来计划 ──
export const ROADMAP = [
  { phase: "PHASE 01", title: "Docker 化与开源发布", status: "done" as const, desc: "三服务容器化，docker compose 一键启动，MIT 协议开源。" },
  { phase: "PHASE 02", title: "GitHub Actions CI", status: "active" as const, desc: "构建、测试、镜像发布全自动流水线。" },
  { phase: "PHASE 03", title: "共享数据协议", status: "planned" as const, desc: "Nebula ↔ CodeLens ↔ DevOps 摘要互通，三层认知融合。" },
  { phase: "PHASE 04", title: "MCP Server 模式", status: "planned" as const, desc: "接入 Claude Code、Cursor 等 AI 编程工具，成为标准认知层。" },
];

// ── Hero 数据流跑马灯 ──
export const TICKER_ITEMS = [
  "TREE-SITTER AST PARSING",
  "12,433 RELATIONS INDEXED",
  "LSM-TREE WAL SYNC",
  "HNSW VECTOR SEARCH",
  "RRF FUSION RANKING",
  "FAULT MEMORY RECALL",
  "8,521 ENTITIES MAPPED",
  "CONTEXT COMPRESSION 99.6%",
];
