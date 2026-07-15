# ◈ DevOps Agent — 运维智能体

> **让每一次故障，都成为经验。** v0.2.0 | Go | Apple Design

---

## 目录

- [1. 产品概述](#1-产品概述)
- [2. 前端仪表盘](#2-前端仪表盘)
- [3. 模块架构](#3-模块架构)
- [4. 工具调用框架](#4-工具调用框架)
- [5. 指标采集模块](#5-指标采集模块)
- [6. 日志异常分析](#6-日志异常分析)
- [7. 故障记忆存储](#7-故障记忆存储)
- [8. REST API](#8-rest-api)
- [9. 快速开始](#9-快速开始)
- [10. 技术栈与项目结构](#10-技术栈与项目结构)

---

## 1. 产品概述

面向服务器运维场景的智能 Agent —— 系统指标实时采集、工具动态调用、日志异常检测、故障记忆持久化，四位一体。

```
运维: "服务器 CPU 飙升，帮我看看"
Agent: "CPU 95%, 进程 java(12345) 占用 5.2核。3天前同类故障：GC频繁→调整JVM堆。建议检查GC日志。"
      ↓ 30 秒定位 + 历史方案直接可用
```

---

## 2. 前端仪表盘

### 2.1 设计语言

从莱茵生命工业风升级为 **Apple Design**——玻璃拟态面板、SF 系统字体、弹性弹簧动画、浅色渐变背景。

| 要素 | 旧 | 新 |
|------|----|----|
| 背景 | 六边形 Canvas + 扫描线 | 浅色渐变 `#F2F2F7` |
| 面板 | 扁平矩形 2px 边框 | 玻璃拟态 `blur(20px)` |
| 字体 | 等宽为主 | SF Pro 三级体系 |
| 圆角 | 2px | 8–24px |
| 动画 | linear / ease | 弹簧 `cubic-bezier(0.34,1.56,0.64,1)` |
| 光标 | 自定义 SVG | 系统标准 |

**配色**：`#007AFF` 蓝 / `#00C7BE` 青 / `#34C759` 绿 / `#FF9500` 橙 / `#FF3B30` 红

### 2.2 页面布局

```
HEADER · 玻璃导航栏
HERO · "让每一次故障，都成为经验" + 4 个 SVG 环形指标图
MONITOR · 4 列卡片: CPU | 内存 | 磁盘 | 故障记录
MAIN ──┬── 运维操作终端 [诊断|工具|日志|故障] 四段切换
       └── 进程 TOP 15 + 事件日志 + 系统参数
ARCH · 2 个 SVG 架构视图
FOOTER
```

### 2.3 Hero 环形指标图

4 个 SVG 环形图实时展示健康状态：

| 环 | 颜色 | 数据源 | 阈值 |
|----|------|--------|------|
| CPU | `#007AFF` | `cpu.usage_percent` | >70% 橙, >90% 红 |
| 内存 | `#34C759` | `memory.usage_percent` | 同上 |
| 磁盘 | `#FF9500` | `disk.usage_percent` | 同上 |
| 健康 | 动态 | CPU<90 && 内存<90 | 否则变红 |

环用 `stroke-dashoffset` 做弧线动画，800ms 弹性过渡。

### 2.4 进程 CPU 显示

**问题**：Windows `Get-Process` 的 `$_.CPU` 是累计 CPU 秒数（不是百分比），直接显示会出现 "500%"。

**解决**：前端增量差值算法。

```
第 N 次:  快照 { PID → cpuSec, time }
第 N+1 次: 真实CPU% = (本次cpuSec - 上次cpuSec) / 间隔秒数 × 100
           单核等效%  = min(真实CPU%, 100)
           占用核数   = 真实CPU% / 100
```

进程列表展示两列：

| 列 | 示例 | 说明 |
|----|------|------|
| 多核占用 | `2.3核` / `14.5核` | <1核 绿, 1–多核 蓝, >70%总核 红 |
| 单核等效 | `87%` | 上限 100%, 进度条基准 |

### 2.5 六种元素联动

联动基于轻量级 `linkageBus`（pub/sub 事件总线）：

| 触发 | 事件 | 效果 |
|------|------|------|
| 悬停指标卡片 | `card:hover` | SVG 对应节点发光 + 邻居卡片变暗 |
| 悬停环形图 | 内联 | 对应指标卡片高亮上浮 |
| 数据刷新 | `data:refreshing→refreshed` | 数字弹性跳动, 3 列错开 120ms |
| 诊断完成 | `diagnosis:completed` | 相关故障条目高亮 |
| 滚动 | scroll 事件 | 顶部 2px 渐变进度条 |
| 工具执行 | `tool:executed` | 事件日志自动追加 |

### 2.6 动画体系

| 动画 | 曲线 | 时长 |
|------|------|------|
| 数字跳动 | ease-out | 600ms |
| 环形图 | spring | 800ms |
| 进度条 | spring | 800ms |
| 卡片悬浮 | spring | 400ms |
| 标签切换 | smooth | 350ms |
| 列表入场 | spring | 400ms + stagger |

### 2.7 文件

```
web/
├── index.html    SPA 主页面 (25.7 KB)
├── devops.css    Apple 玻璃拟态系统 (36.9 KB)
└── devops.js     联动 + 增量CPU + 弹簧动画 (28.0 KB)
```

---

## 3. 模块架构

```
┌──────────────────────────────────────────────────────┐
│                DevOps Agent 运维智能体                  │
├──────────┬──────────┬──────────┬──────────┬──────────┤
│  agent/  │  tools/  │ metrics/ │analyzer/ │  web/    │
│  核心引擎 │ 工具调用  │ 指标采集  │ 日志分析  │ 前端仪表盘 │
├──────────┴──────────┼──────────┴──────────┼──────────┤
│       memory/       │       api/         │ Nebula   │
│    Nebula 记忆集成   │    REST API        │  Agent   │
└─────────────────────┴────────────────────┴──────────┘
```

### 3.1 agent/ — 核心引擎

`DevOpsAgent` 持有 `ToolRegistry` + `*engine.Engine`。`Diagnose()` 是核心入口：分析查询 → 推荐工具 → 采集指标 → 搜索历史故障 → 生成报告。

### 3.2 tools/ — 运维工具实现

每个工具是 `func(args map[string]any) (*ToolResult, error)`。危险操作标记 `RequireConfirm: true`。Windows 走 PowerShell，Linux 走 bash/systemctl。

### 3.3 metrics/ — 系统指标采集

`SystemCollector` 实现 `Collector` 接口。5 秒缓存 + `sync.RWMutex`。`runtime.GOOS` 判断走 Windows WMI 还是 Linux procfs。

### 3.4 analyzer/ — 日志异常分析

规则引擎 + LLM 双层分析。12 条预置规则覆盖 OOM、磁盘满、连接拒绝/超时、服务崩溃、CPU 过高等。

### 3.5 memory/ — Nebula 记忆集成

`SaveFault()` 双写：情景记忆(完整故障 JSON) + 语义记忆(提炼解决方案)。`SearchSimilarFaults()` 用混合检索（向量+关键词 RRF 融合）匹配历史案例。

---

## 4. 工具调用框架

### 4.1 设计

```go
type ToolDef struct {
    Name, Description string
    Category          ToolCategory   // system / log / service / network
    Parameters        []ToolParam
    RiskLevel         string         // low / medium / high / critical
    RequireConfirm    bool
}
```

### 4.2 工具清单

| 分类 | 工具 | 风险 |
|------|------|------|
| 🖥️ 系统 | `exec_command`, `get_system_info`, `list_processes` | 高/低/低 |
| 📋 日志 | `search_logs`, `parse_error_log`, `tail_log` | 低 |
| ⚙️ 服务 | `check_service`, `restart_service`, `list_services` | 低/严重/低 |
| 🌐 网络 | `check_port` | 低 |

### 4.3 推荐机制

关键词匹配评分：每个匹配 +20 分，上限 80。例如 "cpu" → 推荐 `list_processes` + `get_system_info`。

### 4.4 调用时序

```
Client → POST /tools/execute → API → Registry.Get(name) → ToolFunc(args) → ToolResult
```

### 4.5 前端快捷命令

6 个预设模板（系统信息 / CPU Top / 内存 Top / 端口检查 / 服务列表 / 错误解析），选择后自动填充工具名和 JSON 参数。

### 4.6 扩展

添加新工具：实现 `ToolFunc` → 在 `tools/` 注册 → `main.go` 调用 → `keywords` 添加匹配词。

---

## 5. 指标采集模块

### 5.1 指标结构

```go
type MetricsSnapshot struct {
    CPU       CPUMetrics    // usage_percent, cores
    Memory    MemMetrics    // total_gb, used_gb, usage_percent
    Disk      DiskMetrics   // total_gb, used_gb, usage_percent
    Processes []ProcInfo    // pid, name, cpu(累计秒), memory_mb
}
```

### 5.2 平台适配

| 指标 | Windows | Linux |
|------|---------|-------|
| CPU | `Win32_Processor` | `/proc/stat` |
| 内存 | `Win32_OperatingSystem` | `/proc/meminfo` |
| 磁盘 | `Win32_LogicalDisk` | `df -BG` |
| 进程 | `Get-Process` | `ps aux` |

### 5.3 进程 CPU 注意

`ProcInfo.CPU` 字段（JSON: `cpu_percent`）是**累计 CPU 秒数**，不是百分比。前端通过增量差值算法（见 §2.4）将其转换为真实占用率并双维度展示。

### 5.4 API 示例

```bash
curl http://localhost:8740/api/v1/devops/metrics
# → { cpu: { usage_percent: 45.2, cores: 16 }, memory: {...}, processes: [...] }
```

---

## 6. 日志异常分析

### 6.1 12 条检测规则

| 规则 | 严重度 | 类别 |
|------|--------|------|
| OOM_Killer | critical | memory |
| DiskFull | critical | disk |
| ServiceCrash | critical | crash |
| DBConnectionFail | critical | service |
| FileDescriptorExhaustion | critical | service |
| ConnectionRefused | high | network |
| ConnectionTimeout | high | network |
| DNSError | high | network |
| HighCPU | high | cpu |
| TLSCertError | high | security |
| PermissionDenied | medium | security |
| RateLimit | medium | service |

### 6.2 使用

```
POST /api/v1/devops/logs/analyze  { path, level }
POST /api/v1/devops/logs/search   { path, keyword, tail_lines }
```

---

## 7. 故障记忆存储

### 7.1 模型

| 记忆类型 | TTL | 存储 | 内容 |
|----------|-----|------|------|
| 情景记忆 | 30天 | KV+向量+倒排 | 完整故障 JSON, tag: fault |
| 语义记忆 | 永久 | KV+向量 | 提炼的解决方案, tag: solution |

情景记忆 → 整合引擎 → 语义记忆（模拟人脑记忆巩固）

### 7.2 核心方法

```go
SaveFault(record)           // 双写情景+语义
SearchSimilarFaults(query)  // 混合检索 (RRF)
SearchSolutions(query)      // 语义记忆搜索
MarkResolved(id, solution)  // 标记已解决
```

---

## 8. REST API

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/v1/devops/status` | GET | Agent 状态 + 工具统计 |
| `/api/v1/devops/metrics` | GET | 系统指标快照 |
| `/api/v1/devops/diagnose` | POST | 智能诊断 |
| `/api/v1/devops/tools` | GET | 工具列表 |
| `/api/v1/devops/tools/execute` | POST | 执行工具 |
| `/api/v1/devops/logs/analyze` | POST | 日志分析 |
| `/api/v1/devops/logs/search` | POST | 日志搜索 |
| `/api/v1/devops/faults` | GET | 故障统计 |
| `/api/v1/devops/faults/search` | POST | 混合检索故障 |

静态文件：`/web/` 前缀挂载 `./web` 目录，根路径 `/` 自动重定向。

---

## 9. 快速开始

```bash
# 双击 start.bat，或：
cd "DevOps Agent"
.\devops-server.exe --port 8740
# → http://localhost:8740/web/
```

```bash
# 系统指标
curl http://localhost:8740/api/v1/devops/metrics

# 智能诊断
curl -X POST http://localhost:8740/api/v1/devops/diagnose \
  -H "Content-Type: application/json" \
  -d '{"query":"CPU 飙高","server_name":"prod-01"}'

# 执行工具
curl -X POST http://localhost:8740/api/v1/devops/tools/execute \
  -H "Content-Type: application/json" \
  -d '{"name":"list_processes","args":{"sort_by":"cpu","top_n":5}}'

# 搜索故障
curl -X POST http://localhost:8740/api/v1/devops/faults/search \
  -H "Content-Type: application/json" \
  -d '{"query":"CPU 飙升","limit":5}'
```

---

## 10. 技术栈与项目结构

| 模块 | 技术 |
|------|------|
| 后端语言 | Go 1.26.5 |
| HTTP | net/http（标准库，零框架） |
| 记忆引擎 | Nebula Agent (LSM + HNSW + BM25) |
| 系统指标 | PowerShell/WMI + procfs |
| 前端 | 原生 HTML/CSS/JS · Apple 设计语言 |
| LLM | DeepSeek V4 Pro（可替换） |

```
DevOps Agent/
├── agent/       核心引擎 + 工具注册
├── tools/       运维工具实现
├── metrics/     系统指标采集 (Win+Linux)
├── analyzer/    日志异常分析 (12条规则)
├── memory/      Nebula 故障存储
├── api/         REST 路由
├── cmd/devops-server/   入口
├── web/         Apple 前端仪表盘
├── docs/        本文档
├── start.bat    一键启动
└── go.mod
```

---

> **DevOps Agent** — *采集一次指标，记住每次故障，越用越高效。*
