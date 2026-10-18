# Noosphere Desktop

本地 AI 桌面工作台（Windows 优先）。以 Tauri v2 为桌面壳，统一承载四个子系统：AI Gateway（多模型 LLM 网关）、Nebula（记忆引擎）、DevOps（运维 Agent）、CodeLens（代码智能分析）。

## Windows 桌面启动

双击 `Noosphere.lnk` 或 `Start-Noosphere.cmd` 启动已经构建的 `release/Noosphere.exe`。应用直接加载内置界面并管理本地后端，无需启动 Vite 或打开浏览器。首次默认进入三维档案工作台，可从五个真实模块入口进入业务；设置中的“工作台体验”可以调整默认入口、画质和启动过渡。

双击 `Create-Shortcuts.cmd` 可在项目目录和 Windows 桌面生成快捷方式。移动整个项目目录后，再运行一次来更新快捷方式目标。便携版需要保留 `release` 内的 `bin`、`codelens-python` 等资源。CodeLens 仍需要 Python 依赖：优先使用配置中的明确 Python 路径；默认模式会查找应用或项目的 `.venv`、`.local/venv`，然后尝试系统 Python。现有项目依赖已位于 `.local/venv`，它不包含在单独的 `release` 文件夹中。

完整构建：`powershell -NoProfile -File scripts/build.ps1`。增加 `-BundleNsis` 可生成安装包；不带该选项时仅更新便携版，旧安装包会被归档。构建记录和校验值位于 `release/build-info.json`。

本次视觉适配与验证记录见 [RhineLabUI 适配说明](docs/rhinelab-adaptation.md)。

聊天输入栏可开启“只读 Agent”，执行项目搜索、文件读取、已确认记忆检索和系统状态检查；DevOps 也提供任务入口。执行步骤与证据保存在工作区任务中心，支持取消和重开查看。用法与预算见 [可执行 Agent](docs/executable-agent.md)，发布验证见 [方案三验证记录](docs/EXECUTABLE_AGENT_VERIFICATION.md)。

第二阶段可在 Agent 下开启“操作提案”，预览文件修改、固定测试和服务启停，逐项确认执行并查看恢复记录。详见 [操作审批与恢复](docs/agent-reviewed-actions.md) 和 [第二阶段交付验证](docs/AGENT_ACTIONS_VERIFICATION.md)。

## 子系统

| 子系统 | 技术栈 | 职责 |
|--------|--------|------|
| AI Gateway | Go | 多 Provider LLM 网关（OpenAI / DeepSeek / Claude / Gemini / Ollama），流式 SSE、重试、Token 统计、调用日志 |
| Nebula | Go | 记忆引擎：LSM-Tree + HNSW 向量索引 + BM25 + RRF 混合检索 |
| DevOps Agent | Go | Windows 指标采集、运维工具、Agent Loop、AI 诊断 |
| CodeLens | Python (FastAPI) | Tree-sitter AST 代码分析（Go/Python/JS/TS 等），Go 适配器管理子进程并反向代理 |

## 目录结构

```
Noosphere/
├── frontend/                 # 前端 (React + Vite + TypeScript)
│   ├── src/
│   ├── index.html
│   ├── package.json
│   └── vite.config.ts
│
├── backend/                  # 后端 (Go)
│   ├── cmd/
│   │   └── server/
│   │       └── main.go
│   ├── internal/
│   │   ├── gateway/          # AI Gateway
│   │   ├── nebula/           # 记忆系统
│   │   ├── devops/           # 运维系统
│   │   └── codelens/         # 代码分析
│   ├── python/
│   │   └── codelens/         # CodeLens Python + FastAPI
│   ├── configs/
│   │   └── config.yaml
│   ├── go.mod
│   └── go.sum
│
├── desktop/                  # Tauri 桌面壳
│   ├── src-tauri/
│   └── package.json
│
├── scripts/                  # 启动/构建脚本
│   ├── dev.ps1
│   └── build.ps1
│
├── release/                  # 分发目录（仅打包产物，构建后生成）
├── docs/                     # 架构与历史文档
├── setup.bat                 # CodeLens Python 依赖安装
├── VERSION                   # 版本号唯一来源
├── .gitignore
├── README.md
└── LICENSE
```

## 开发

日常打开方式：完整构建成功后，双击项目根目录 `Start-Noosphere.cmd`，或直接打开 `release/Noosphere.exe`。启动器使用自身所在目录定位程序，桌面程序使用绝对路径定位后端；不要求先打开终端或切换工作目录。

**最新加固记录（2026-09-28）**：本轮补齐项目级代码检索、结构化只读诊断、双层本地凭证、聊天 IndexedDB 迁移与备份，并修复索引和写入恢复问题。实际测试与发布状态见 [本轮交付记录](docs/HARDENING_2026-09-27.md)；[启动修复记录](docs/STARTUP_RELIABILITY.md) 保留为历史记录，不代表当前构建状态。

前置要求：Node.js 20+、Go 1.26.5（go.mod）、Rust 1.88+（当前 Tauri / HTTP 依赖锁文件要求）、Windows 10/11 SDK。前端自动测试使用 Node.js 24。安装和预检见 [开发指南](docs/DEVELOPMENT.md)。

2026-09-09：已接入 RhineLabUI 档案式界面，并完成请求取消、流式协议和持久化优化。实现范围、验证命令与限制见 [界面与流式稳定性记录](docs/RHINE_UI_STREAMING.md)。

```powershell
# 首次安装前端依赖和项目内 Tauri CLI
npm --prefix frontend ci
npm --prefix desktop ci

# 一键启动：构建后端、暂存资源、启动 Vite，再运行桌面开发程序
powershell -ExecutionPolicy Bypass -File scripts\dev.ps1

# 已有构建产物时跳过构建，直接启动
powershell -ExecutionPolicy Bypass -File scripts\dev.ps1 -SkipBuild
```

后端独立开发需先设置临时 `NOOSPHERE_BACKEND_TOKEN`，调用者使用同一 bearer token（监听 `127.0.0.1:8080`）；完整示例见开发指南。桌面模式自动管理凭证。

## 构建与分发

```powershell
# 全链路：Go 后端 → 前端 → Tauri 壳 → 组装 release/
powershell -ExecutionPolicy Bypass -File scripts\build.ps1

# 完整生成便携程序和 NSIS 安装包（使用项目内 Tauri CLI）
powershell -ExecutionPolicy Bypass -File scripts\build.ps1 -BundleNsis

# 仅更新开发用后端和前端产物，release/ 完全不变
powershell -ExecutionPolicy Bypass -File scripts\build.ps1 -SkipTauri
```

完整构建成功后，`release/` 包含程序、运行资源及本地数据。默认不生成安装包；只有 `-BundleNsis` 成功后才发布本次新安装包。旧程序及旧安装包会保存在 `release/legacy/`。已有 `config.yaml`、`models.yaml`、`data/`、`logs/` 不会被构建覆盖。

```
release/
├── Noosphere.exe             # Tauri 主程序
├── bin/
│   └── noosphere-backend.exe # Go 后端 sidecar
├── config.yaml               # 运行配置
├── config.defaults.yaml      # 首次启动使用的默认配置模板
├── models.yaml               # 模型配置（含加密 API Key，不入库）
├── codelens-python/          # CodeLens Python 服务
├── setup.bat                 # 首次运行安装 Python 依赖
└── Noosphere Desktop_*_x64-setup.exe   # NSIS 安装包（可选产物）
```

首次运行前，若需使用 CodeLens，请运行 `release\setup.bat` 安装 Python 依赖。

便携程序的数据保存在其 `config.yaml` 所在目录。安装版将首次运行配置、模型和数据放到 `%LOCALAPPDATA%\com.noosphere.desktop`，程序资源留在安装目录。可以在设置页查看实际数据目录；如需隔离测试，可用绝对路径环境变量 `NOOSPHERE_DATA_DIR` 指定另一目录。安装包不携带本机模型配置。

## 首次 CodeLens 依赖安装

CodeLens 依赖系统 Python 3.10+（fastapi / uvicorn / pydantic / tree-sitter）：

```powershell
setup.bat
```

## 文档

- [全栈架构](docs/ARCHITECTURE.md)
- [API 参考](docs/API.md)
- [开发指南](docs/DEVELOPMENT.md)
- [会话全文搜索与优化方案](docs/CHAT_SEARCH_PLAN.md)
- [历史演进](docs/HISTORY.md)
- `docs/Noosphere-Architecture.docx`：便于评审和归档的 Word 版架构说明

## License

见 [LICENSE](LICENSE)。
