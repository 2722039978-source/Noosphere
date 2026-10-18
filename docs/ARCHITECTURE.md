# Noosphere 全栈架构

Noosphere 是一个 Windows 优先的本地 AI 桌面工作台。当前实现采用 Tauri v2 作为桌面壳，React 负责交互界面，Go sidecar 提供统一业务 API，Python 子进程承担 Tree-sitter 代码分析。架构优化的核心结论是：保留现有 `frontend`、`backend`、`desktop` 边界，将共享状态、接口入口、安全边界和构建产物各自收敛到唯一责任层。

## 系统拓扑

```text
┌───────────────────────────────────────────────────────────────┐
│ Tauri Desktop                                                 │
│  ┌─────────────────────────────────────────────────────────┐  │
│  │ React + Vite + TypeScript                               │  │
│  │ App Shell · Lazy Routes · BackendProvider · Zustand     │  │
│  └──────────────────────────┬──────────────────────────────┘  │
│                             │ Tauri commands                   │
│  ┌──────────────────────────▼──────────────────────────────┐  │
│  │ Rust command bridge and Go sidecar lifecycle            │  │
│  └──────────────────────────┬──────────────────────────────┘  │
└─────────────────────────────┼─────────────────────────────────┘
                              │ 127.0.0.1:8080
┌─────────────────────────────▼─────────────────────────────────┐
│ Go backend                                                    │
│ Gateway · Nebula · DevOps · CodeLens adapter                  │
│ Request ID · Recovery · CORS allowlist · Graceful shutdown    │
└───────────────┬──────────────────┬────────────────────────────┘
                │                  │ child process
                │                  ▼
                │        Python FastAPI + Tree-sitter
                ▼
      Local WAL · SST · HNSW index
```

## 目录边界

| 目录 | 所有权 | 主要技术 | 不应承载的内容 |
|---|---|---|---|
| `frontend` | WebView 界面与客户端状态 | React 18、Vite、TypeScript、Tailwind CSS 4、Radix、Zustand | 后端业务规则、密钥、持久化引擎 |
| `backend` | 统一业务服务与本地数据能力 | Go、标准库 HTTP、自研 LSM/HNSW、Python CodeLens worker | 桌面窗口管理、前端展示状态 |
| `desktop` | 桌面生命周期与系统权限边界 | Tauri 2、Rust | 业务页面、重复的 Go 服务逻辑 |
| `scripts` | 可复现的开发与构建编排 | PowerShell | 运行时数据与源码副本 |
| `release` | 构建生成的可分发产物 | EXE、配置、sidecar | 手工维护的源文件 |
| `docs` | 架构、API、开发与变更记录 | Markdown、DOCX | 可执行代码与运行日志 |

## 前端架构

`AppProviders` 是全局依赖入口。`BackendProvider` 只挂载一次，负责后端健康状态、模块状态、模型、记忆会话和系统指标；页面通过 `useBackend` 消费共享状态，不再各自创建十秒轮询。路由页面使用 `React.lazy` 按工作区拆包，首次启动只加载应用外壳与当前页面。

界面采用三层导航模型：全局工作区导航、当前功能的上下文导航、主工作区。桌面宽屏展示三栏；窄屏把全局导航和会话历史放入独立对话框抽屉，主工作区保持完整宽度。视觉令牌集中在 `frontend/src/index.css`，组件继续使用现有 shadcn/Radix 源码和 Lucide 图标。

状态分工如下：

- `BackendProvider`：远端或 sidecar 状态、异步操作和统一轮询。
- `useChatStore`：本地对话、流式消息和持久化会话。
- `useUiStore`：当前路由与用户选择的项目路径。
- `useThemeStore`：暗色或浅色主题及本地持久化。

## 后端架构

`backend/cmd/server` 仅负责配置加载、依赖装配、路由注册和服务器生命周期。业务实现位于 `backend/internal`：

- `gateway` 统一模型配置、调用、流式输出与统计。
- `nebula` 管理 LSM、向量索引、混合检索和记忆服务。
- `devops` 组合系统工具、指标采集与诊断 Agent。
- `codelens` 管理 Python worker 并代理代码分析请求。
- `middleware` 处理请求 ID、日志、崩溃恢复和来源控制。
- `core` 维护模块生命周期快照。

HTTP 服务器设置请求头和请求体读取超时，同时不给 SSE 流式响应设置固定写入截止时间。CORS 只接受配置中的本地开发与 Tauri 来源；无 `Origin` 的本地 sidecar、命令行和进程间请求继续可用。

## 核心数据流

### AI 对话

1. 用户在 React 对话输入区提交问题。
2. 前端从 Nebula 检索最多三条相关代码上下文。
3. 前端通过 Tauri Channel 调用 `gateway_chat_stream`。
4. Rust bridge 与 Go Gateway 协作处理模型流。
5. token 持续写入 `useChatStore`，完成后清除流式状态。

### CodeLens 索引

1. 用户选择项目路径并启动索引。
2. Go CodeLens adapter 将请求转发给 Python FastAPI worker。
3. Tree-sitter 分析文件并返回实体、语言和行数统计。
4. 项目摘要写入 Nebula 语义记忆，供后续 AI 对话检索。

## 安全边界

- Go 服务默认只监听 `127.0.0.1`。
- 浏览器来源由 `server.allowed_origins` 精确控制，不使用通配符。
- Tauri WebView 启用 CSP，仅允许本地资源、IPC、开发服务器和本地 sidecar。
- 模型密钥不进入前端持久化状态，发布配置与运行日志不纳入版本控制。
- DevOps 工具执行仍应在每个工具内部校验参数与操作范围。

## 演进约束

新增工作区时应同时提供独立路由页面、明确的 backend service 边界、API 类型和最小状态所有权。不要在页面组件中再创建全局轮询，也不要把开发机绝对路径、API Key、日志或构建产物写入源码。
