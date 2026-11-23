# 当前边界

## 平台与模型接入

当前稳定下载为 Windows x64 的 v0.9.0。已公开的 1.0 候选版覆盖 Windows x64、Linux x64、macOS arm64 和 macOS x64。两种 Mac 的包均为 ad-hoc 预览。Windows/Linux ARM64、macOS universal、RPM、Flatpak/Snap、商店版和 Lite 未纳入本轮交付。 [四目标预览包](https://github.com/2722039978-source/Noosphere/releases/tag/v1.0.0-rc.1) · [跨平台验收](CROSS_PLATFORM_ACCEPTANCE.md)

模型可以运行在本机服务中，也可以通过 API 接入。使用远程 Provider 时，对话、附件及提供给模型的项目上下文会发送到对应服务。源码和便携包均不附带模型权重。

Gateway 提供 Ollama、OpenAI-compatible、OpenAI、DeepSeek、Anthropic / Claude、Gemini 等对话适配。结构化 Agent 工具目前支持 DeepSeek、OpenAI、OpenAI-compatible 和 Ollama。具体模型是否正确响应协议和工具调用，需要通过实际连接验证。

Model Hub 分别显示对话、Agent 工具和原生测评的能力状态。`supported` 表示适配范围，`available` 表示当前配置可进入对应流程，`verification` 表示验证状态。保存配置、连接成功和模型正在推理是不同状态。

模型选择依次使用当前会话绑定、工作区默认和全局默认。已有会话保留自己的绑定；明确选择的模型不可用时会报告错误。切换模型由后端确认后生效，执行中的 Agent 保持原模型。

## 对话附件

每条消息最多添加 3 个文件，每个文件不超过 200 MB（209,715,200 字节）。原件和索引合计每个会话最多 640 MiB，整个应用最多 2 GiB。文本和代码支持 UTF-8、带 BOM 的 UTF-16 和 GB18030。PDF 支持最多 100 页的文字层，每份最多提取 1 MB 文本。图片与扫描 PDF 可先在本机 OCR，再粘贴文字或添加为文本文件。

附件只读取用户选择或拖入的文件。原件与工作区数据库配套保存，按项目目录和会话隔离。发送时，模型收到为当前问题选取的文字节选，引用保留文件名、SHA-256 和页码或行号。对于磁盘保存的文本，读取节选时核对同一批原始字节的 SHA-256。使用远程模型时，节选随对话请求发送。发送后可以按需引用上次附件。

移除或取消草稿会清理其数据。未使用草稿 24 小时后到期，在应用启动或下一次附件操作时清理。已发送的文件随会话保留，删除会话时清理对应附件。会话 JSON 备份保留文件引用与已用节选。备份原件时请使用应用数据备份，或退出应用后一起保留 `workspace.sqlite`、其对应 WAL 文件和 `chat-attachment-files` 目录。

## 本地模型测评

原生测评适配本机 Ollama、可核验的本机 llama.cpp 和可选的 OpenVINO GenAI。llama.cpp 核验回环服务、固定模型及本地 GGUF；OpenVINO 核验本机 IR 文件集、词法器、模板与 CPU/GPU 运行配置。文件哈希描述文件身份，不能验证进程内存中的权重。按文件身份、大小和修改时间缓存的哈希无法检测保留这些信息的修改。外部常驻服务自行管理模型加载。OpenVINO 的 Windows x64 安装入口、锁文件和参数说明见 [USAGE.txt](scripts/openvino/USAGE.txt)。

内置 `local-text-v1` / `local.text.v1` 提供 8 道中文指令遵循和 8 道 JSON 结构化输出短任务。评分检查明确的目标答案或 JSON 内容与结构，不执行模型输出，不使用模型裁判。文本测评不需要 Go 或 CodeLens；仍需满足本机资源、模型身份、参数和模型服务的准备要求。这些任务不能推断开放式对话质量、长上下文、工具调用或安全能力。

`reviewer-core-v1` 覆盖中英文成对指令、约束对话、固定小仓库问答、短输入检索、良性请求与授权边界，以及文本函数选择。`reviewer-context-4k-v1` 和 `reviewer-context-8k-v1` 用固定事实分别验证不同配置容量下的检索。实际输入 Token 数取决于服务模板和词法器。llama.cpp 在扩充套件中检查格式化输入、输出预算与实际上下文，记录原生返回的提示词计数。`reviewer-tools-v1` 向支持的 llama.cpp 服务发真实工具调用请求，只运行固定无副作用的测试工具。纯文本模拟调用及未受支持的传输不会计通过。开放式对话质量需要人工评阅。良性拒答任务覆盖指定行为，广义安全能力保留未测。

编程工作流 `codelens.codetask.go.v1` 面向约束明确的 Go 函数任务。模型输出经过提取和校验，由 Go 工具链编译，在 wazero / WASI 中执行并与预期结果比较。此工作流仍需要 CodeLens 与可信 Go 工具链。

自定义任务集支持每集 **1–50 个案例**、每例 **1–100 组断言**，JSON 文档与桌面请求上限为 **64 KiB**。函数仅支持一个入参、一个返回值，类型为 `bool`、`string`、`int`、`int64`、`float64` 及其一维切片；禁止 import、宿主脚本与自定义评分代码。评分按声明类型精确匹配，各例的全部断言通过才计为通过。超出浏览器安全整数范围的 `int64` 断言需要使用原始 JSON 编辑或导入。

任务集保存为不可变版本。修改时创建新版本，旧报告、已启动任务的恢复和重试继续引用原始版本。现有 `simulation-demo-v1` 发布策略只适用于内置任务；自定义任务的发布标准需要单独制定。其他语言、复杂项目任务和完整 Agent 能力尚未纳入此任务格式。

## 报告与指标

每份报告记录具体模型版本、任务集、参数和运行环境。小型任务集与受限执行环境提供的是这些条件下的观察，结果不代表通用模型排名或生产可用性承诺。

单模型基线使用同一版本独立执行两轮，描述该版本的表现与重复性。轮次差异不构成跨模型升级或晋级依据。双版本报告需要结合正确性、失败类型以及有效成对样本数量阅读；基础设施失败和未完成任务保留各自状态。

耗时和 Token 指标来自宿主记录或模型引擎的实际返回值。缺失的首 Token 延迟、加载耗时、CPU、内存及显存信息保留未知原因；总响应耗时无法直接确定首 Token 延迟，模型文件大小也无法确定显存占用。硬件采样可能无法可靠归因到单个模型，API 模型的本地硬件指标标为不适用。

新版 llama.cpp 扩充文本套件从请求起点记录首个非空思考或回答增量，以及首段最终答案的时间。Windows 对本机连接对应的服务进程按约 500 ms 采样工作集和专用 GPU 分配。它们是进程级采样峰值，可能含同进程其他工作，也可能漏过采样间隔内的瞬时峰值。使用代理或不提供计数器的平台时覆盖率会下降。硬件建议以权重文件和用户给出的 KV 缓存、上下文及运行余量估算，实测报告单独显示。

HTML 报告导出包含报告选定的全部案例、模型版本、冻结参数、输出和证据引用。缺失或截断的证据会标明，历史报告使用保存时的版本。导出内容可能包含项目代码与路径，公开分享前请检查。

能力画像只汇总本次任务集实际覆盖的类别，并保留样本数及未评分数量。SVG 分享图卡是这些结果的摘要。JSON 实验包保存实验条件和案例证据，不包含模型权重；导入后先核对本机已登记的模型、任务版本和参数，再由用户明确开始新的测评。缺少兼容条件时不会自动下载模型或执行导入内容。跨硬件和引擎版本的重跑不保证逐字一致，文件哈希也不构成对测评过程真实性的第三方认证。

Release Center 的模拟与真实灰度有独立界面、API 和记录。真实灰度在用户启用的工作区专用入口向双臂之一发送正式模型请求。根据墙钟窗口、请求数、错误率、延迟和绑定请求的人工反馈进行门禁判断。异常时暂停候选流量，用户可恢复或回滚，事件保存当时的观测证据和摘要。此入口面向本机工作区验证。日常对话和 Agent 按原有的会话模型规则运行。

## CodeLens、REA 与 Agent

CodeLens 使用 Tree-sitter 提取定义与静态关系，并保留文件、行号和 SHA-256。候选调用目标未经编译器类型检查；依赖配置、动态调用、解析错误和索引覆盖范围会影响结果。来源文件变化后需要更新索引，引用读取会核对保存的来源版本。

REA 当前接入项目内产物的 **PE 导入表**与 **Go 构建信息**读取，文件上限为 **64 MiB**。保存的原始记录、摘要和 SHA-256 可供对话引用与复查。当前接入范围不包含 REA 的全部分析工具，元数据读取不能独立解释程序的完整运行行为。

只读 Agent 在允许的项目目录和工具范围内查找资料，保存步骤与证据。任务最多进行六次模型调用、十二次工具调用，执行期限为六分钟，单次模型请求期限为两分钟。文件修改、受支持的测试及服务操作通过操作提案逐项确认。模型回答与建议需要结合对应工具记录核对。

候选版绑定操作提案与任务、项目目录绑定、文件身份和内容版本。项目解绑或目标变化后，需要重新审阅受影响的操作。恢复针对本应用记录的修改，遇到后续编辑会保留冲突。外部编辑器仍可能在最后一次版本核对和写入之间抢占。经批准的项目测试使用用户权限，可能写入其他文件或联系外部服务。应用能结束受管进程，测试副作用需要按项目实际行为处理。

Nebula 的记忆检索模式与质量取决于配置的嵌入服务和实际资料。检索结果属于参考依据，代码或工具输出中的文字也可能不完整或不可信。

## 构建、数据与自动检查

### 数据备份和恢复

桌面会话保存在 WebView 用户资料中。SQLite、工作区、任务、记忆和附件原件保存在应用数据目录中。系统凭证由系统凭证库管理。会话 JSON 包含对话及保存的引用节选，导入后可在导入的工作区重新绑定模型和项目。完整搬迁要同时保留会话或原 WebView 资料、应用数据和系统凭证的迁移安排。

在 1.0 候选中，可在应用完全退出后，用包内 `bin/noosphere-backend.exe`（Windows）或 `bin/noosphere-backend`（Linux/macOS 资源目录）生成离线数据快照。使用「设置 → 应用信息」显示的应用数据目录，将下列示例替换为绝对路径。命令会打印生成的快照路径。

```text
noosphere-backend -config "<原应用数据目录>/config.yaml" -backup-data
noosphere-backend -restore-data-from "<快照目录>" -restore-data-to "<尚不存在的新目录>"
```

快照包含配置、SQLite 及其伴随文件、附件原件与保存的任务数据。恢复先校验清单和 SHA-256，再写到新目录。需要检查恢复结果时，用 `NOOSPHERE_DATA_DIR` 指向新目录启动候选版。保留原目录、备份与会话 JSON，检查会话、模型配置、项目约定、任务和附件后再决定是否使用恢复目录。跨系统恢复时重新选择项目路径、配置系统凭证并重建派生索引。恢复出来的审批保留为历史。

候选版在变更后端数据前创建快照，在新存储打开后完成升级记录。升级中断时原快照保留。回到 v0.9.0 时使用升级前快照，并以独立 WebView 资料或导出的会话恢复桌面对话。

桌面程序与 `bin/`、`codelens-python/`、`codelens-runtime/`、`rea-runtime/` 等资源一起交付。CodeLens 使用按目标锁定的私有 CPython 与 wheel，REA 使用包内 Node.js 24.22.2 与 REA 6.8.0。Windows 使用 WebView2，Linux 使用 WebKitGTK，macOS 使用系统 WKWebView。开发态可使用配置的解释器，正式桌面在私有运行时缺失时提示修复安装。

正式桌面写数据到 Tauri 解析的本机用户目录，设置页显示实际路径。Windows 旧便携应用继续使用 EXE 旁已有的配置目录，`NOOSPHERE_DATA_DIR` 可指定独立的绝对路径。Linux AppImage 和 macOS .app 的安装资源按只读使用。升级前保存配置、服务数据和会话。跨系统仅迁移会话等兼容记录，绝对路径、引擎运行目录、凭证、派生索引和审批授权需在新安装处理。 [备份说明](README.md#更新备份与恢复)

模型配置使用加密存储，主密钥交给 Windows Credential Manager、macOS Keychain 或 Linux Secret Service。凭证存储未运行、锁定、无权限或损坏时，云端持久化会报告失败。无凭证的本地模型仍可使用。曾保存但当前无法解密的记录会保留，重新授权时需要输入凭证。

0.9.0 在 `encrypted_api_key` 保存密文，并兼容读取旧 `api_key`。降级到 0.8.x 需要恢复升级前的 `models.yaml` 及对应的系统凭证。旧程序会忽略新版字段。

Windows 继续使用 WebView2 IPC 和独立账户安装器验收。各平台的模拟服务和原生运行记录分别列出。LM Studio 通过已运行的 HTTP 服务连接，服务启停由 LM Studio 管理。Ollama 的停止和重启仅针对本次应用持有的进程。现有便携 llama.cpp/SYCL 和 OpenVINO 安装配置限 Windows x64。

Intel Mac 的本地引擎验收使用 Ollama CPU，要求 macOS 14 或更新。macOS 13.5 是桌面私有 Node 24 的候选版本门槛。LM Studio 的 macOS 版本要求 Apple Silicon。 [Ollama macOS 要求](https://docs.ollama.com/macos) · [LM Studio 系统要求](https://lmstudio.ai/docs/app/system-requirements)

项目测试要求工作区信任、已批准的结构化操作及对应工具链。被批准的测试代码以当前用户权限运行，可能更改文件和外部服务。进程组用于回收正常子进程，无法约束主动脱离进程组的程序。受限 Go 编程测评需要独立的约束与验收，当前仅 Windows 启用。Linux/macOS 的系统诊断服务写操作和部分进程/GPU 指标暂不可用。Apple 统一内存依照实际观测记录。

## 设计来源与第三方资源

Noosphere 的档案界面与三维场景参考并适配 [LBEILC / RhineLabUI](https://github.com/LBEILC/RhineLabUI) 的实现，包括阵列、档案盒模型、材质、运动和灯光。上游版权声明及 MIT 全文保存在 [RhineLabUI-LICENSE.txt](frontend/public/assets/RhineLabUI-LICENSE.txt)。

RhineLabUI 的视觉参考来自《明日方舟》「莱茵生命：访问」终端界面。相关原作名称、标志、设定与视觉设计的权利归各自权利人所有；Noosphere 与原作品、RhineLabUI 作者及相关品牌没有官方关联或背书关系。模型档案的静态建筑美术按参考方向单独生成。

- [REA](https://github.com/morluto/rea)：产物分析组件，使用锁定的 `rea-agents` 版本；上游及其依赖许可证随运行组件分发。
- 附件 PDF 提取与文本解码的版权及许可见 [Attachment-Dependencies-LICENSE.txt](frontend/public/assets/Attachment-Dependencies-LICENSE.txt)。
- MiSans：字体版权说明见 [NOTICE.txt](frontend/public/fonts/NOTICE.txt)，许可见 [MiSans-license.pdf](frontend/public/fonts/MiSans-license.pdf)。
- 模型厂商图标：来源与商标说明见 [NOTICE.txt](frontend/public/model-brands/NOTICE.txt)，许可见 [LICENSE.txt](frontend/public/model-brands/LICENSE.txt)。
- Noosphere 自有代码使用 [MIT License](LICENSE)。第三方代码、模型、字体、图标及其他素材继续适用各自的许可与版权声明。
