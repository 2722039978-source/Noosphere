# Noosphere

**在 Windows 桌面上管理项目对话、分析代码，并比较本地模型的表现。**

[![CI](https://github.com/2722039978-source/Noosphere/actions/workflows/ci.yml/badge.svg)](https://github.com/2722039978-source/Noosphere/actions/workflows/ci.yml)

打开最近使用的项目与会话，让 Agent 通过 CodeLens 查找源码依据，使用 REA 查看可执行文件信息。在 Model Hub 接入模型，在 Model Lab 运行可复现的本机测评并分享报告。Release Center 提供模拟与工作区专用的真实灰度入口。点击侧栏顶部的 Noosphere 标识可以进入三维动效主页。

## 功能分区

Noosphere 将日常使用划分为 **AI 对话、CodeLens、Nebula、DevOps、模型** 五个工作区；模型工作区进一步连接 Model Hub、Model Lab、任务集档案、测评报告与 Release Center。各模块围绕项目上下文协作，也可以从对应页面单独使用。

### AI 对话：围绕项目继续工作

- **项目与会话管理**：按工作区组织对话，回到最近使用的项目与会话，通过会话检索找回之前的讨论。
- **流式回答与模型选择**：接收流式输出；模型选择按当前会话绑定、工作区默认、全局默认的顺序生效，已有会话保留自己的模型绑定。
- **带来源的项目上下文**：将 CodeLens 的代码片段、已确认的长期记忆和产物分析记录用于对话，在回答旁查看引用与来源信息。
- **Agent 任务与操作预览**：Agent 可以在项目范围内列出、搜索和读取源码，检索上下文、追踪静态关系，并保留执行步骤与证据。文件修改、受支持的测试和服务操作以提案形式交由用户逐项确认。
- **对话备份**：设置页提供会话备份导出与导入；导入合并时保留已有同 ID 会话，避免静默覆盖本地记录。

适合用来继续项目讨论、定位实现位置，或让 Agent 先收集依据，再决定是否执行变更。Agent 有调用次数与执行时间限制，并非无限制的自主终端。

### CodeLens：代码索引、检索与来源追踪

CodeLens 由 Python 分析服务与 Go 接入层共同提供能力，使用 Tree-sitter 提取源码定义、导入和静态关系。

- **项目索引与代码检索**：把项目源码整理为可检索片段，供代码工作台、对话与 Agent 使用。
- **结构分析**：查看代码实体与静态关系，按符号或文件路径查找相关定义。
- **可核对的证据**：保留文件路径、行号和 SHA-256，读取引用时核对来源版本；源码变化后需要重新索引。
- **REA 产物分析**：读取项目内可执行产物的 PE 导入表与 Go 构建信息，保存原始记录、摘要及文件哈希，供后续引用和复查。

静态调用候选没有经过编译器类型检查，不能替代完整的类型分析或运行时调试。当前 REA 接入也不等于包含其全部逆向分析工具。

### Nebula：长期记忆与资料召回

Nebula 用于保存与检索本机资料，并将可用的项目知识接回对话。

- **记忆会话**：创建记忆会话、存储内容、搜索历史记录并查看元数据。
- **混合检索**：检索引擎包含 BM25 文本检索与向量检索，通过 RRF 合并结果；实际语义检索能力取决于嵌入服务配置。
- **项目知识整理**：将约定、决策、偏好和解决方案保存为工作区记忆，区分待确认、已确认及被替代等状态。
- **有效性检查**：记录来源、修订版本与可选到期时间，向对话提供上下文时排除未确认、过期或来源已变化的项目记忆。

历史记忆会话与经过整理的项目知识是不同用途的记录。检索命中只是参考依据，仍应结合当前源码和工具结果判断。

### DevOps：本机资源观测与诊断

- **资源面板**：查看 CPU、内存、磁盘和进程数量，支持暂停自动采样与手动刷新。
- **模块状态**：查看后端模块健康状态，区分服务不可用与正在等待数据。
- **诊断工具**：结合进程、连接、服务状态等工具信息开展系统诊断，查看工具调用与结果。
- **项目 Agent**：在项目上下文中调查问题；受支持的服务启动、停止等变更可通过操作提案审核。

该模块面向本机环境排查，不应当作已完成生产验证的无人值守运维平台。

### Model Hub：模型连接与能力档案

Model Hub 集中管理本机模型服务和远程 API 连接，让“可以聊天”“可以调用工具”“可以原生测评”分别可见。

- **连接管理**：添加、编辑和测试模型配置，查看模型版本与连接状态。
- **多 Provider 对话**：Gateway 提供 Ollama、OpenAI-compatible、OpenAI、DeepSeek、Anthropic / Claude、Gemini 和 OpenVINO 等适配。
- **能力分开展示**：分别展示对话、Agent 工具与原生测评能力及验证状态，避免把连接成功误认为所有功能均可用。
- **衔接测评**：对满足条件的本机模型发现并登记版本，继续进入 Model Lab。

结构化 Agent 工具目前适配 DeepSeek、OpenAI、OpenAI-compatible 和 Ollama，具体模型仍需实际验证。原生测评核验本机 Ollama、llama.cpp 和 OpenVINO 的模型版本与配置。Windows 可选的 OpenVINO 安装与设备选择见 [OpenVINO 使用说明](scripts/openvino/USAGE.txt)。

### Model Lab：可追溯的本机模型测评

Model Lab 围绕“模型版本、任务集、推理参数、运行环境”组织实验，保存结果供复查。

- **版本与参数记录**：登记模型版本并保存推理配置，让报告对应到具体实验条件。
- **运行前准备检查**：检查模型服务、资源预算、CodeLens 和 Go / WASI 执行环境等条件，呈现无法启动的原因。
- **单模型基线**：同一版本独立执行两轮，复查任务表现与重复性。
- **受限 Go 任务执行**：当前工作流为 `codelens.codetask.go.v1`，模型输出经过提取、校验与 Go 编译后，在 wazero / WASI 中运行，再与预期结果比较。
- **内置与自定义任务**：中文指令和 JSON、成对中英文、约束对话、固定仓库问答、4k/8k 检索、良性拒答边界、原生工具调用及 Go 修复按套件分开。可另建带固定断言的 Go 任务版本。
- **能力画像和实验包**：查看逐题输出、分类分母、缺失原因及实际推理指标。可导出 HTML、SVG 分享图卡和 JSON 实验包，核对本机模型、参数与任务版本后复跑。
- **硬件建议**：按模型权重、设备容量、上下文和 KV 缓存估算，能读取当前工作区同一版本的实测记录。支持的本机 llama.cpp 文本测评还记录首 Token 和 Windows 进程的资源采样峰值。

自定义任务集当前支持每集 1–50 个案例、每例 1–100 组断言，总 JSON 请求不超过 64 KiB，函数签名与评分方式也有限制。结果只描述相应任务和环境下的表现，不构成通用模型排名；缺失的指标会保留未知状态。分享报告前请检查其中的代码、路径与输出。

### Release Center：模拟与真实灰度

模拟入口使用持久化演练数据。真实灰度由用户在指定工作区启用专用会话，在稳定版本与候选版本之间分流实际模型请求。风险门禁读取墙钟窗口、请求结果、延迟和人工质量反馈，支持自动暂停与回滚。事件保存每次决定的观测与哈希。普通对话和 Agent 沿用各自的模型选择。

## 推荐使用流程

### 先围绕项目开展对话

1. 在“模型”中添加连接并测试，确认所需的对话或工具能力。
2. 打开项目，在 CodeLens 建立索引，查看关键代码片段与来源。
3. 回到 AI 对话，基于项目上下文提问；需要调查时使用 Agent，并查看任务记录与引用。
4. 对文件、测试或服务操作先检查提案，再逐项确认；将值得保留的项目约定整理为已确认记忆。

### 再测量本机模型表现

1. 在 Model Hub 接入满足原生测评条件的本机模型，登记模型版本。
2. 在 Model Lab 选择版本、任务集与推理配置，先完成准备检查。
3. 运行单版本基线，结合逐题结果、失败类型和有效样本阅读报告。
4. 导出 HTML 报告、SVG 图卡或 JSON 实验包。需要验证候选发布时打开 Release Center。

## 下载与安装

在 [GitHub Releases](https://github.com/2722039978-source/Noosphere/releases) 下载 Windows x64 正式安装器 `Noosphere-<版本>-windows-x64-setup.exe`，按向导选择目录。程序会启动内置 Go、CodeLens 和 REA。安装器可离线安装缺失的 WebView2 Runtime，应用所需的 Visual C++ 运行库随包提供。普通用户无需安装 WebView2 SDK、Python、Go、Node.js、Rust、C++ Build Tools 或 Windows SDK。

便携版为 `Noosphere-<版本>-windows-x64.zip`。解压全部文件，运行 `Noosphere.exe`，并保留旁边的资源目录。ZIP 使用系统已有的 WebView2 Runtime，机器缺少 Runtime 时请使用正式安装器。旧版 `v0.6.0-20261114` 的 ZIP 仍采用手工安装 Python 的方式。

从侧栏“模型”添加并测试模型连接，再打开项目开始对话或在 CodeLens 建索引。安装包提供应用和内置服务；本地推理模型、可选 OpenVINO、动态 Go 修复评测及 Agent 代跑项目测试使用各自的模型服务或语言工具。

可用 `Get-FileHash -Algorithm SHA256 -LiteralPath <下载文件>` 对照同一版本的 `SHA256SUMS.txt`。从 Windows 设置的“已安装的应用”卸载 Noosphere 时，默认保留应用数据。

## 开发和构建

使用 Windows x64、PowerShell 7、Git、npm、rustup 和 Visual Studio 的“使用 C++ 的桌面开发”工作负载（包含 x64 MSVC、Windows SDK 和 VC Redistributable 文件）。项目版本来源为根目录的 `VERSION`。Node、CPython、Rust 和 Go 版本分别由 [`.node-version`](.node-version)、[`.python-version`](.python-version)、[`rust-toolchain.toml`](rust-toolchain.toml) 和 [`backend/go.mod`](backend/go.mod) 指定。开发和桌面验证还需要系统 WebView2 Runtime。

```powershell
git clone https://github.com/2722039978-source/Noosphere.git
cd Noosphere
pwsh -NoProfile -File scripts/setup.ps1 -InstallBrowser
pwsh -NoProfile -File scripts/doctor.ps1 -Profile Package
pwsh -NoProfile -File scripts/dev.ps1
```

`setup.ps1` 安装锁定的 npm 依赖，并在 `.local/venv` 安装 CodeLens 开发依赖。`-InstallBrowser` 下载浏览器回归测试使用的 Chromium。只开发前端可用 `setup.ps1 -FrontendOnly`。`doctor.ps1` 支持 `Frontend`、`Backend`、`Desktop`、`Package` 检查范围。

```powershell
pwsh -NoProfile -File scripts/build.ps1
pwsh -NoProfile -File scripts/package.ps1
pwsh -NoProfile -File scripts/test.ps1 -Rust -Browser -PackagedRoot release
```

`build.ps1` 更新本地 `release/`，可通过 `Start-Noosphere.cmd` 启动。`package.ps1` 生成 `artifacts/<版本>/` 下的 NSIS 安装器、便携 ZIP、`build-info.json` 和 `SHA256SUMS.txt`。常规 CI 构建归档候选产物。与 `VERSION` 一致的 `v<版本>` 标签在构建和验收成功后触发 GitHub Release。

## 更新、备份与恢复

在“设置 → 软件更新”检查官方完整安装包。下载完成后会校验 SHA-256 并保存会话导出。按面板提示退出 Noosphere，再运行已校验的安装器。安装后重新打开设置核对版本和实际数据目录。便携版可以解压到新目录，再从该目录启动。旧便携目录及其 `config.yaml` 应保留，直至确认所需数据已迁移。

应用数据位置以设置页显示为准。默认安装数据根为 `%LOCALAPPDATA%\com.noosphere.desktop`，开发模式有独立数据目录。旧便携版仍识别 EXE 同目录的 `config.yaml`，维护者也可用绝对路径 `NOOSPHERE_DATA_DIR`。数据根保存配置、`models.yaml`、`data/`、运行日志和 `upgrade-backups/`；会话正文由 WebView2 IndexedDB 保存，可通过设置面板导出和导入。

更新遇到故障时，先退出桌面，再保留现有数据目录的副本。可从已保存的安装包回到旧版本。需要恢复备份时，根据版本备份清单确定原始路径，保留当前数据后恢复对应快照，并通过会话备份面板导入 JSON。SQLite 数据库和同版本 WAL 需要一起处理。跨 Windows 账号迁移后可能需要重新输入模型密钥，主钥保存在原账号的 Windows 凭据管理器中。源码索引和向量缓存可以用原项目重建，配置、会话、项目记忆和审批记录应保留。

启动故障先查看设置显示的数据目录中的 `desktop-services.log` 及 Go 服务日志。若连窗口也没有出现，可使用安装器补齐 WebView2 Runtime，并检查 Windows 事件查看器。报告故障时附版本号和对应日志，避免附带私有模型配置、会话、代码索引或原始文件修改记录。

## 模块化架构

桌面端使用 **Tauri 2 / Rust** 承载 **React / TypeScript** 界面，由 Go 后端组织模型调用、工作区、记忆、Agent 和测评服务，Python CodeLens 负责源码解析。下面按实际目录划分，便于阅读和扩展：

| 层级 / 模块 | 主要职责 | 源码位置 |
| --- | --- | --- |
| 前端工作区 | 页面路由、对话、代码、记忆、诊断与模型工程界面 | [frontend/src/app](frontend/src/app)、[frontend/src/features](frontend/src/features) |
| 桌面运行层 | 原生窗口、运行组件管理、本地认证与命令桥接 | [desktop/src-tauri/src](desktop/src-tauri/src) |
| Go 服务入口 | 服务启动、生命周期、路由与后台任务装配 | [backend/cmd/server](backend/cmd/server) |
| Gateway | Provider 适配、流式对话、模型选择与能力检查 | [backend/internal/gateway](backend/internal/gateway) |
| 工作区与 Agent | 项目状态、会话模型绑定、受限工具与操作提案 | [workspaces](backend/internal/workspaces)、[agent](backend/internal/agent) |
| CodeLens | 项目索引、静态来源追踪、Python 服务桥接与 REA 接入 | [Go 接入层](backend/internal/codelens)、[Python 分析服务](backend/python/codelens) |
| Nebula 与 Knowledge | 记忆存储、文本 / 向量检索、项目知识与引用有效性检查 | [nebula](backend/internal/nebula)、[knowledge](backend/internal/knowledge) |
| DevOps 与硬件采集 | 系统指标、诊断工具、主机环境与硬件信息 | [devops](backend/internal/devops)、[hostinfo](backend/internal/hostinfo) |
| 模型工程 | 版本、任务集、测评、沙箱执行、模拟发布和工作区真实灰度 | [modellab](backend/internal/modellab)、[modelstore](backend/internal/modelstore)、[evalrunner](backend/internal/evalrunner)、[modelops](backend/internal/modelops)、[livecanary](backend/internal/livecanary) |
| 构建与检查 | 工具链检查、开发启动、生产构建、安装器与 ZIP、原生集成验证 | [scripts](scripts)、[CI](.github/workflows/ci.yml) |

前端模块入口定义在 [modules.ts](frontend/src/app/modules.ts)，页面映射见 [routes.tsx](frontend/src/app/routes.tsx)。这里的模块化指代码与职责划分，不表示每个模块都是可独立安装的插件。

## 数据与使用边界

- **Windows 优先、持续开发中**：界面、接口和数据结构仍可能变化，升级前请备份重要配置、对话与实验数据。
- **本地工作台不等于全程离线**：使用远程 Provider 时，对话、附件与提供给模型的项目上下文会发送到对应服务；源码和便携包不附带模型权重。
- **数据目录可查看**：实际目录以设置页显示为准；绝对路径环境变量 `NOOSPHERE_DATA_DIR` 可指定独立数据目录。
- **凭证与导出要分开保管**：模型凭证通过界面管理，不要写入公开示例或 Issue。报告和备份可能包含私人项目内容。
- **验证范围**：Windows workflow 执行源码测试和真实桌面 IPC 验收，模型端点由测试提供。无开发工具的 Windows 镜像、真实模型推理和具体硬件仍需单独验证。
- **任务产物留在本机**：历史文档、工作记录、开发缓存和验收输出可统一放在 Git 忽略的 `test/`。公开源码及便携版保留启动所需文件。

完整的模型适配、任务格式、指标解释、Agent 限制和第三方资源说明见 [LIMITATIONS.md](LIMITATIONS.md)。

## 链接

- [当前边界、数据目录与第三方资源](LIMITATIONS.md)
- [问题反馈](https://github.com/2722039978-source/Noosphere/issues) · [Pull Requests](https://github.com/2722039978-source/Noosphere/pulls) · [CI 检查](https://github.com/2722039978-source/Noosphere/actions)
- [MIT License](LICENSE) · [设计来源与致谢](LIMITATIONS.md#设计来源与第三方资源)
- [RhineLabUI](https://github.com/LBEILC/RhineLabUI) · [REA](https://github.com/morluto/rea)
