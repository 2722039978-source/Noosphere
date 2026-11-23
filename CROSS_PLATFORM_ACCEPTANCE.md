# 桌面跨平台验收记录

每次复验记录版本、源码提交及工作区指纹、原生产物 SHA-256、目标 OS/CPU、测试命令及日志位置。源码实现、自动检查、原生运行、真实模型和最终发行包分别取证。

## 1.0 实施与验收

2026-11-23（America/Los_Angeles）从当时的 `main` 与 `v0.9.0` 提交 `0f6cdef68f6296aeebf1eee6e120db07dfacef57` 建立 `develop/noosphere-1.0`。工作树原有的 `.codex/environments/environment.toml` 属于本地配置。

| 状态 | 工作与涉及模块 | 当前证据、依赖及验收条件 |
| --- | --- | --- |
| 已完成 | 0.9.0 基线、目标与历史资产核对 | [主干 CI](https://github.com/2722039978-source/Noosphere/actions/runs/44781864724) 和 [标签 CI](https://github.com/2722039978-source/Noosphere/actions/runs/44788436392) 成功。标签 CI 四目标及汇总通过。公开资产包含 Windows x64、Linux x64、macOS arm64，Intel x64 没有公开资产。Linux、Apple Silicon 是预览。 [0.9.0 发行记录](https://github.com/2722039978-source/Noosphere/releases/tag/v0.9.0) |
| 已完成 | 模型连接、绑定和配置冻结的源码整改与自动回归。Gateway、Providers、Workspaces | 模拟 IPC、Provider HTTP 和 Go 测试覆盖 A→B→A、会话及工作区默认、显式停用、入队后编辑连接、协议参数与凭证错误。Windows 的现有本地 OpenVINO 可真实生成。每目标的真实云与真实模型工具见下方矩阵。 |
| 已完成 | 文本附件版本核对、索引与来源提示。Attachment、Workspaces、CodeLens、对话界面 | 用等长正文替换回归核对原件 SHA-256。模拟 IPC 覆盖项目及记忆来源。Linux 和 macOS 的拖放、输入、原生目录选择仍待目标机。 |
| 已完成 | 项目任务、审批、测试与恢复的源码整改及 Windows 后端集成。Agent、Workspaces、任务界面 | 10/10 通过，真实 Python 项目测试和文件恢复使用模拟 Provider 的结构化工具请求。覆盖取消、重启、会话与项目绑定失效、冲突以及单次审批。另有 1.0 候选后端与真实本地模型的只读工具往返证据。 |
| 已完成 | 后端数据升级、离线备份和隔离恢复。SQLite、附件文件、备份入口 | 公开 v0.9.0 后端写入真实 SQLite/WAL 与附件，RC 读取并生成快照。阻挡 SQLite 写入时，升级拒绝就绪，保留旧版本与原快照。解除锁后复用快照重试成功，v0.9.0 可读取恢复目录。证据在 `test/release-1.0/data-upgrade-final-backend.log` 和 `data-upgrade-retry.log`。 |
| 待验证 | 私有运行时、窗口交互和生命周期。桌面 Rust、Go 后端、Python/Node、桌面验证脚本 | 四目标验证搬迁、离线启动、解析、服务异常与进程归属。目标包验证目录选择、输入法、键盘、拖放、剪贴板及显示设置。 |
| 阻塞 | 正式包签名、公证与最终业务验收。四目标包、CI、签名环境 | 已取得下方各版本的原生候选证据。正式包需要获授权的 Apple Developer ID、公证配置及成品复验。四目标的实际云端连接仍需能成功响应的授权服务。 |

按模型与数据边界、审批与项目流程、源码回归、候选打包、最终包原生验收推进。签名后的实际文件参与最终验收及哈希记录。历史检查见下文。

### 数值版本 1.0.0 的预览验收

以下预览验收从提交 `564ad1f93549b0a05d4cdfa6f19e5bbddfd6d5a0`、源码树 `47eed7d21151c407605f617585ca5e73eb7c3ebf` 生成。包与日志保存在 Actions，公开下载仍为 rc.2。签名后的正式包需要独立哈希及验收。 [预览构建](https://github.com/2722039978-source/Noosphere/actions/runs/45232545703) · [正式源码 PR](https://github.com/2722039978-source/Noosphere/pull/9)

| 目标 | 实际预览包证据 |
| --- | --- |
| Windows x64 | NSIS 通过公开 0.9.0 原位升级、卸载与重装。便携 ZIP 在 Unicode 路径的 WebView2 中连接真实 OpenVINO CPU 模型并获得回答。用公开 0.9.0 后端创建的工作区、模型、约定、任务与附件验证升级、阻挡写入及隔离恢复。ZIP 的 SHA-256 为 `fc18629249d4dff39da3131b3527b893461e403d239527b283f9f12d40b2fa9b`。本地证据保存于 `test/release-1.0/windows-1.0.0-preview-upgrade/` 和 `.local/model-canary/`。 [Windows 包与日志](https://github.com/2722039978-source/Noosphere/actions/runs/45232545703/job/137895851301) |
| Linux x64 | Ubuntu 22.04 的 deb 通过 0.9.0 真实安装升级和合成凭证重启读回。deb 与 AppImage 通过搬迁、X11 离线启动和纯 Weston Wayland 窗口及缓冲区检查。AppImage 以 extract-and-run 启动。AppImage 与 deb 的 SHA-256 依次为 `53ac848d4df4dd87cfe55080f195c6e7d16ea7b0cc764909ff2ef40173cc5421` 与 `371faa5a62eb0ba84f3796a93e5f6d30c82c40efd8267f1d5d4d24aaace70deb`。 [Linux 包与日志](https://github.com/2722039978-source/Noosphere/actions/runs/45232545703/job/137895851881) |
| macOS x64 | Intel 原生 `.app` 归档与 DMG 通过搬迁启动、私有运行时和进程清理。独立系统 Keychain 通过合成凭证保存、重启读回及锁定后保留密文。该次构建使用 ad-hoc 签名。 [Intel Mac 包与日志](https://github.com/2722039978-source/Noosphere/actions/runs/45232545703/job/137895851698) |
| macOS arm64 | `macos-15` 两次在分配 runner 前取消。同一源码另在 `macos-26` Apple Silicon 上运行原生预览验证。两种 runner 记录分别保留。 [原队列](https://github.com/2722039978-source/Noosphere/actions/runs/45232545703) · [Apple Silicon 补充运行](https://github.com/2722039978-source/Noosphere/actions/runs/45241333984) |

工作流的 Apple Silicon 构建使用 `macos-26`，目标架构为 arm64。macOS 13.5 仍是待在目标设备复验的最低系统候选。此前的 `macos-15` 原生记录保留在对应的历史版本下。

### 已公开的 1.0.0-rc.2

四目标 CI 构建提交为 `a5dbe10c65ea8ba989cc265b5dcb697800f04b2e`。发行标签指向 `54729ea4bc1be4e0eb8c90b03f8b888dcf98bce7`，两者的源码树相同。公开的八个包连同清单和构建信息共 15 项，已核对大小、SHA-256 和匿名下载。 [候选发行](https://github.com/2722039978-source/Noosphere/releases/tag/v1.0.0-rc.2) · [四目标 CI](https://github.com/2722039978-source/Noosphere/actions/runs/45213950945)

| 目标 | 候选包已经取得的证据 |
| --- | --- |
| Windows x64 | NSIS 实际安装、v0.9.0 升级、卸载重装及原生验证通过。发布 ZIP 另在 WebView2 中从 Unicode 测试目录启动，接入现有 OpenVINO CPU 模型获得真实中文问题的回答，核对工作区和会话绑定。 |
| Linux x64 | Ubuntu 22.04 X11 完成公开 v0.9.0 deb 原位升级，旧工作区保留。deb 和以 extract-and-run 启动的 AppImage 通过搬迁离线启动。已安装后端使用独立 GNOME Keyring 保存合成凭证并在重启后读回。 |
| macOS arm64、x64 | 两种架构的 ad-hoc `.app` 归档和 DMG 完成原生启动及受管进程回收。Dock 恢复修复通过模拟 IPC 回归，签名成品窗口交互仍待原生桌面检查。 |

公开 rc.2 的两种 Mac 归档在原生 runner 上用独立 Keychain 创建并重启读回合成凭证。Keychain 锁定后，后端报告锁定并保留原密文。Ubuntu 的 rc.2 deb 在纯 Wayland 会话创建窗口并提交缓冲区，rc.2 AppImage 因锁定 Tauri 的 GTK 启动钩子强制 X11 而退出。正式源码在启动 GTK 前处理纯 Wayland 会话，后续从重新打包的 deb 和 AppImage 验证实际窗口。 [rc.2 原生补充验证](https://github.com/2722039978-source/Noosphere/actions/runs/45231206194) · [固定 Tauri 钩子](https://raw.githubusercontent.com/tauri-apps/tauri/tauri-cli-v2.11.4/crates/tauri-bundler/src/bundle/linux/appimage/linuxdeploy-plugin-gtk.sh)

### 已公开的 1.0.0-rc.1

候选发行包含四个目标的八个分发文件。发布资产来自 PR CI 检出提交 `01f2519ac5ba1b047377b9330d512a4a032279fb`，标签指向合并提交 `69ecdc5273f955f2d6826717bf0ecfa091bef2df`。两者 Git 树相同。GitHub 返回的 15 个资产大小和 SHA-256 均与验收件一致。标签和主干 CI 也已通过。 [候选下载](https://github.com/2722039978-source/Noosphere/releases/tag/v1.0.0-rc.1) · [产物构建 CI](https://github.com/2722039978-source/Noosphere/actions/runs/45178383595) · [主干 CI](https://github.com/2722039978-source/Noosphere/actions/runs/45186612945) · [标签 CI](https://github.com/2722039978-source/Noosphere/actions/runs/45188818439)

| 目标 | 已公开包的原生证据 |
| --- | --- |
| Windows x64 | NSIS 通过公开 v0.9.0 原位升级、卸载重装和用户目录标记保留。发布 ZIP 在 WebView2 中通过现有 OpenVINO CPU 模型的实际对话，以及 v0.9.0 会话、附件和项目数据升级与隔离恢复。 |
| Linux x64 | AppImage 和 deb 通过私有运行时、搬迁后 X11 离线启动和进程回收。 |
| macOS arm64、x64 | 两种架构的 ad-hoc 归档及 DMG 均通过搬迁后启动和受管进程回收。 |

Windows x64 的同一候选 ZIP 内后端（SHA-256 `540e79abfa34cafa3d48b2b129a89d86728366f62f8ffa090753ffba6b17ff45`）另通过真实本地只读 Agent。Windows 11 `10.0.26300`、Intel Arc A580、PrismML llama.cpp SYCL `b10787-73bfe6cc4` 和 Bonsai 2 27B `PTQ1_0` 使用 OpenAI 兼容工具协议。模型发出 `project_read_file`，读取中文路径下的临时 Git 项目，收到工具结果后在第二次响应中核对出文件原值，带行号和正文哈希的证据随任务保存。这项验收在后端执行，桌面审批与写入仍按独立记录验收。记录在 `.local/model-canary/windows-bonsai-agent-45178383595/`。

同一后端和模型在隔离 Git 项目另通过修改及验证。Bonsai 读取 `label.js` 并提出文件改动与 Node 测试。验收核对前后哈希和 NTFS 文件身份，确认未批准时文件未变。同任务测试提前批准与重复批准均返回 HTTP 409。批准后读取到实际修改，随后独立批准 `node --test`，运行退出码 `0` 且断言通过。首次使用 `.cjs` 作为读取目标时超出本次 Agent 支持的源文件范围，未执行操作。两次证据分别在 `.local/model-canary/windows-bonsai-reviewed-actions-45178383595-attempt2/` 和 `.local/model-canary/windows-bonsai-reviewed-actions-45178383595/`。

### 正式签名与发行

GitHub Actions 的 `desktop-release-signing` 环境允许从正式目标标签 `v1.0.0` 执行签名作业。发布负责人在该环境的 Secrets 中配置 `APPLE_CERTIFICATE`（以 Base64 编码的 Developer ID Application `.p12`）、`APPLE_CERTIFICATE_PASSWORD`、`APPLE_SIGNING_IDENTITY`、`APPLE_ID`、`APPLE_PASSWORD`（公证用的 app 专用密码）和 `APPLE_TEAM_ID`。签名脚本在临时 Keychain 使用这些值，作业结束后清理。 [签名环境设置](https://github.com/2722039978-source/Noosphere/settings/environments/28270863494/edit)

数值版 `VERSION` 与 `v1.0.0` 标签一致时，从该标签运行 Desktop delivery，启用 `formal`，也可运行 `gh workflow run ci.yml --ref v1.0.0 -f formal=true`。正式四目标汇总要求两种 Mac 的 Developer ID 签名、公证和 stapling 证据。下载工作流的 `noosphere-four-target-candidate` 产物，原生业务复验使用其中的实际包和 `SHA256SUMS.txt`。 [GitHub Actions](https://github.com/2722039978-source/Noosphere/actions/workflows/ci.yml)

公证上传后查看 CI 日志中的提交 ID。等待超时或收到拒绝时，发布负责人可通过已授权的 `notarytool` 配置用 `xcrun notarytool info <提交ID> --keychain-profile <配置名>` 核对状态，必要时以 `xcrun notarytool log <提交ID> --keychain-profile <配置名>` 查看拒绝原因。应用和 DMG 都要获得 `Accepted` 并通过 stapling 校验。

签名工作流完成后，用 `gh run download <运行ID> -n noosphere-four-target-candidate -D <成品目录>` 下载汇总产物。核对 `delivery-info.json` 的 `formal-packaging`、标签、源码树和四目标构建提交，再对 `SHA256SUMS.txt` 中列出的每个实际文件计算 SHA-256。按下方原生交接复验并在 `test/release-1.0/v1.0.0/<目标>/` 保存系统、测试结果、包哈希和日志。对通过验收的文件创建发行草稿，并核对 GitHub 资产列表中的名称、大小和摘要。草稿中的四目标文件与验收记录对应后再公开，最后确认 `/releases/latest`、预期标签与下载资产。

### 本地 Windows 候选历史证据

Windows 候选包从干净检出 `1e11ff985ad5443b8e77946894afda87d9c240a3` 构建。后续提交增加 macOS 版本字段和 Linux 许可正文处理。四目标的下一次汇总需要从同一份源码构建四个目标。保留本次 Windows 包的原始提交、哈希及验收记录。

| 目标 | 已取得的 1.0 证据 | 待验证与阻塞 |
| --- | --- | --- |
| Windows x64 | NSIS 和完整 ZIP 已生成。已验证包内外构建信息及最终 SHA-256。Windows 11 x64、WebView2 157.0.4341.60 下的搬迁 ZIP 通过 7 组原生检查。私有 Python 3.14.7 解析 7 种源码类型，Node 24.22.2 与 REA 6.8.0 在移除宿主解释器搜索路径后运行。公开 v0.9.0 到该 ZIP 的 WebView 会话及项目数据升级通过，v0.9.0 从隔离恢复目录回看通过。 | 当前用户有已有 Noosphere 数据。NSIS 安装与就地升级脚本须在干净标准用户环境运行。高 DPI、真实输入法和剪贴板仍需人工复验。安装器 Authenticode 为 `NotSigned`。 |
| Linux x64 | Go 服务及进程管理回归交叉编译通过。已准备 AppImage 与 deb 的 Unicode 路径、离线、崩溃、候选版本排序和许可校验。 | 缺原生 Ubuntu x64 环境。本轮包、X11/Wayland、Secret Service、安装升级和真实模型待验证。 |
| macOS arm64 | Go 服务及进程管理回归交叉编译通过。已准备预览归档、DMG、数字 plist 版本、公证和最终 DMG 安装复验。 | 缺 Apple Silicon 原生环境及受授权签名和公证配置。本轮包、Keychain、Dock/Cmd+O/Q、13.5 候选下限、升级和真实模型待验证。 |
| macOS x64 | Go 服务及进程管理回归交叉编译通过。使用独立 Intel 原生目标与包名。 | 缺 Intel Mac 原生环境及受授权签名和公证配置。同样需要独立的成品、Keychain、交互、最低系统、升级和真实 CPU 模型运行记录。 |

Windows 最终候选文件在 `artifacts/1.0.0-rc.1/`。构建指纹为 `72227065f17b4679123feb71c054e38af41d34d7248788ac249ebcbb5ad3c765`。

| 文件 | 字节 | SHA-256 |
| --- | ---: | --- |
| `Noosphere-1.0.0-rc.1-windows-x64-setup.exe` | 379892358 | `117cc6d3cf2edcd34b7b593e595d59a5af70568f2064d432926163d170ce1335` |
| `Noosphere-1.0.0-rc.1-windows-x64.zip` | 124461818 | `a1085322c4d407b623d2a94a5131b985d83ebad56abe74bdcebd9774e93e4c35` |
| `build-info.json` | 3533 | `089b7b5e7449c543dbddc3bbd3e0968d81ca4f5f75c6120a1196bf66312178cc` |

源码与模拟验证通过 Go 全量测试及 `go vet`、52 项 Rust 单元测试（1 项可选集成未运行）、28 项前端单元测试、4 组模拟 IPC 浏览器流程和 4 项发行脚本回归。候选后端 SHA-256 `dd6ba6f918b5912c44425593c6e5f03452b2c4d9478c064789379aabe8597793` 与 ZIP 内一致，10/10 Agent 回归含真实 Python 测试。日志在 `test/release-1.0/` 的 `go-test.log`、`rust-tests.log`、`candidate-frontend-tests.log`、`candidate-agent-test.log`、`windows-package.log` 和 `windows-native-webview.log`。

同一 ZIP 在 Windows 原生 WebView2 中通过 900×600、模拟 2× 像素比、低动效、中文文本、输入法组合期间 Enter 防误发送和 Shift+Enter 换行。证据见 `windows-motion-scale.log`。Windows 系统级缩放、实际输入法及与其他应用的剪贴板仍需独立桌面验收。

`desktop-upgrade-final.log` 记录公开 v0.9.0 写入真实 WebView2 对话、原始附件和记忆后，用候选 ZIP 替换程序并读取原资料的结果。同系统的会话 JSON 导入也生成独立工作区，旧项目绑定、模型标识和审批授权留在原记录。用 v0.9.0 程序打开恢复的新目录与备份的 WebView 资料，原对话和项目记录可回看。一次退出后立即运行的备份命令返回错误，当时未捕获标准错误。手动重试成功，补齐日志后三次复验通过（`data-upgrade-exact-backend*.log`）。

| 平台、引擎和模型 | 协议 | 模型和任务验收 |
| --- | --- | --- |
| Windows x64，OpenVINO GenAI 2026.4.1.0 CPU，Qwen3-0.6B-int4-ov | 本机 OpenAI 兼容 Chat Completions | 候选 ZIP 的原生界面打开 Unicode 项目、发现并验证外部服务、保存默认模型，发送中文问题并收到真实回答。候选后端另通过流式响应。退出桌面时外部服务继续运行。真实工具往返待验证。证据在 `.local/model-canary/windows-openvino-zip-webview2-canary.json` 和 `windows-openvino-package-canary.json`。 |
| Windows x64，DeepSeek `deepseek-v4-pro` 与 `deepseek-flash` | HTTPS OpenAI 兼容连接探测 | 两次真实请求由上游返回 HTTP 401。需可用的授权凭证确认生成和工具往返。 |
| Linux x64、macOS arm64、macOS x64，llama.cpp b12359 CPU，SmolLM2-135M-Instruct Q4_K_M | 本机 OpenAI 兼容 Chat Completions | 公开 rc.2 的三个原生包内后端通过真实连接、生成及会话绑定。在各平台跟踪的包来自公开发行并事先验证 SHA-256。桌面交互、此模型的工具能力及签名后的包仍待复验。 [三目标真实模型日志](https://github.com/2722039978-source/Noosphere/actions/runs/45233010357) |
| Linux x64、macOS arm64、macOS x64，云端 | 授权服务的 HTTPS API | 待验证。使用现有接入方式配置可用的测试凭证，在原生桌面保存连接并核对真实对话和所支持的工具往返。 |

### 1.0 原生交接

每个平台在独立测试账号或测试机准备同一份小项目和测试数据。项目包含可搜索的定义、文本和有文字层的 PDF，以及一项固定的项目测试。先用 v0.9.0 写入工作区、会话、项目约定、任务与附件并退出，再用候选验证各项数据和来源。保留升级前快照和独立 WebView 资料。用新目录恢复前一版本并重复核对。在无法安全继续的外部编辑场景记录冲突和当前文件。可用会话演示真实工具调用，但只有模型确实返回结构化调用、收到工具结果并继续时，才记为真实 Agent 通过。

| 目标与前提 | 在目标机器上的操作 | 通过证据 |
| --- | --- | --- |
| Windows x64，独立标准用户，WebView2，公开 0.9.0 NSIS | `pwsh -NoProfile -File scripts/verify-installer.ps1 -Installer <RC.exe> -PreviousInstaller <0.9.0.exe> -PreviousVersion 0.9.0 -PreviousSHA256 0de517f1566517cd4e36b3631a5b7a14def883e7bf988cb1245d55b6f851f2d7`。解包到包含空格及中文的目录，运行 `node scripts/verify-desktop.cjs <便携目录>`。 | 升级后安装注册版本、启动、数据保留、重复安装和卸载均通过。核对会话、附件、系统凭证、缩放、中文输入、拖放与低动效。 |
| Linux x64，Ubuntu 22.04 X11 与 Wayland，原生桌面和 Secret Service | 按下方构建命令打包。运行 `node scripts/verify-native-unix.mjs artifacts/<版本>/linux-x64`，离线用 [CI 网络命名空间入口](.github/workflows/ci.yml)。在干净测试机 `sudo apt install ./<0.9.0.deb>`，保存测试数据后安装候选 deb。单独验证 AppImage FUSE 启动、替换及数据保留。 | 输出两个最终包 SHA-256。X11 与 Wayland 分别记录窗口、目录选择、Secret Service、Unicode、离线启动、模型和核心任务；deb 记录旧版到候选的升级及恢复。 |
| macOS arm64，Apple Silicon 原生桌面、Keychain、已授权签名环境 | 用原生目标 `macos-arm64` 构建和运行 `node scripts/verify-native-unix.mjs artifacts/<版本>/macos-arm64`。签名、Submit、Staple 和成品检查由 `scripts/desktop-ci-sign.mjs` 和已配置的受保护 CI 环境办理。 | 最终归档和 DMG 内复制出的 .app 通过签名、Gatekeeper、私有组件、启动和清理。另取 13.5 候选最低系统、Keychain、Finder 安装升级、Dock、Cmd+O/Q、真实模型证据。 |
| macOS x64，Intel 原生桌面、Keychain、已授权签名环境 | 使用 `macos-x64` 重做以上过程。最低桌面候选测试 13.5，实际 Ollama CPU 选用引擎支持的系统及现有模型权重。 | 独立 Intel 哈希、签名和系统记录。分开记录桌面下限和模型引擎下限。 |

每个目标在包含空格及 Unicode 的路径复跑，并记录系统版本、架构、窗口系统及 WebView、应用与引擎版本、模型 ID、API 协议、工具调用状态、源码提交、产物哈希与失败原因。将可公开的证据保存到对应 CI 日志及工作区 `test/release-1.0/<目标>/`，截图和日志仅保留测试数据。聚合命令校验来源与实际分发文件，并生成 `delivery-info.json`，记录各包真实构建提交与汇总所用源码树。签名构建与本表业务验收均通过后，再按仓库授权发正式版。

macOS 凭证升级用获授权的成品复验。先在旧 ad-hoc 包中保存测试凭证，退出后安装 Developer ID 签名成品，核对原凭证读回或可诊断的 Keychain 拒绝。拒绝时核对原密文和旧数据仍在，再按界面提示重新授权连接。 [Apple 代码签名要求](https://developer.apple.com/documentation/technotes/tn3127-inside-code-signing-requirements/) · [Apple Keychain 与签名要求](https://developer.apple.com/library/archive/technotes/tn2206/)

## 0.9.0 发布后的状态

| 目标 | 0.9.0 已取得的证据 | 分发状态 |
| --- | --- | --- |
| Windows x64 | 标签 CI 安装器、便携包和原生 WebView2 通过。发行说明记录了本地 Bonsai SYCL 验收。 | 公共 NSIS、完整便携 ZIP |
| Linux x64 | Ubuntu 22.04 标签 CI 通过。deb 和 AppImage 均验证搬迁后仅回环网络下的 X11 启动及清理。 | 公共 AppImage、deb 预览；安装升级、Wayland 和真实模型待验证 |
| macOS arm64 | ARM64 runner 原生构建，搬迁后的归档及 DMG 内应用完成服务启动和清理。 | 公共 ad-hoc 预览；正式签名、公证、最低系统和完整业务待验证 |
| macOS x64 | Intel runner 的构建、归档、DMG 原生启动检查和汇总通过。 | CI 候选；无公共下载，正式签名及原生业务待验证 |

[0.9.0 发行说明与资产](https://github.com/2722039978-source/Noosphere/releases/tag/v0.9.0) · [0.9.0 标签 CI](https://github.com/2722039978-source/Noosphere/actions/runs/44788436392)

## 0.9.0 候选历史矩阵

以下矩阵保存 2026-11-20 的候选阶段记录。已复验的状态见上表。

| 目标 | 构建环境与最低系统 | 计划分发 | 源码与构建 | 原生运行与正式发行 |
| --- | --- | --- | --- | --- |
| Windows x64 | `windows-2025` x64/MSVC；现有兼容下限为带 WebView2 的 Windows 10 x64 | NSIS、完整便携 ZIP | 已完成 0.9.0 源码测试、构建、打包和哈希复核 | 本地 0.9.0 便携包通过 WebView2 和真实 Bonsai SYCL 推理验收。隔离安装、卸载与重装按 CI 记录判定 |
| Linux x64 | `ubuntu-22.04` x64，glibc 2.35 构建基线，WebKitGTK 4.1 | AppImage、deb | 原生源码测试和 deb 构建通过，AppImage 在 linuxdeploy 解析私有 CPython 的 Tcl 依赖时失败 | 已将私有库路径限于打包进程，待复验 AppImage、deb/升级、离线 X11、Wayland 和真实模型 |
| macOS arm64 | `macos-15` Apple Silicon，原生 arm64；候选最低 macOS 13.5 | .app 归档、DMG | 原生源码测试、预览包生成和搬迁后的私有组件解析通过 | 搬迁启动时 Tauri 资源定位失败。已让测试从规范路径启动，待复验。正式发行阻塞于 Developer ID Application 和公证权限 |
| macOS x64 | `macos-15-intel` Intel，原生 x86_64；桌面运行包候选最低 macOS 13.5，Ollama CPU 本地模型验收最低 macOS 14 | .app 归档、DMG | 原生源码测试、预览包生成和搬迁后的私有组件解析通过 | 搬迁启动时同样出现 Tauri 资源定位失败，待规范路径复验。真实 Ollama CPU 和最低系统待验证。正式发行阻塞于 Developer ID Application 和公证权限 |

目标契约记录在 [platform-targets.json](scripts/platform-targets.json)。macOS 13.5 受锁定 Node 24 的官方预构建要求约束。最终下限还取 Tauri、Go、CPython、所有 wheel、Node 和 REA 中的最高要求。构建检查 Mach-O 部署版本，仍需分别在候选最低版本启动最终包。macOS 15 CI 只能证明对应 runner 的结果。 [Node 24.22.2 构建要求](https://github.com/nodejs/node/blob/v24.22.2/BUILDING.md) · [GitHub 托管 runner 规格](https://docs.github.com/en/actions/reference/runners/github-hosted-runners)

Intel Mac 使用原生 x64 **Ollama CPU** 验收本地模型，系统为 macOS 14 Sonoma 或更新。记录 Ollama 版本、模型标签及摘要、CPU 推理、经 Gateway 生成和会话切换，以及退出后被管理的实例已停止。macOS 13.5 候选环境只复验桌面基础功能和私有运行时。LM Studio 的 macOS 版本要求 Apple Silicon。 [Ollama macOS 要求](https://docs.ollama.com/macos) · [LM Studio 系统要求](https://lmstudio.ai/docs/app/system-requirements)

## 构建与测试命令

Windows 在 PowerShell 7 中运行。默认包输出目录为 `artifacts/<版本>/`。可用 `-OutputRoot <新目录>` 隔离本地试包，同版本已有产物须另选目录。

```powershell
pwsh -NoProfile -File scripts/setup.ps1 -InstallBrowser
pwsh -NoProfile -File scripts/test.ps1
pwsh -NoProfile -File scripts/package.ps1
```

Linux 安装 `libwebkit2gtk-4.1-dev`、GTK 3 开发依赖、`libappindicator3-dev`、`librsvg2-dev`、`libxdo-dev`、`libfuse2`、`patchelf`、`xz-utils` 和编译工具。macOS 使用与目标匹配的原生 Xcode 工具链。构建器版本见根工具链文件及 `backend/go.mod`。以下命令的目标可替换为 `macos-arm64` 或 `macos-x64`。

```sh
node scripts/desktop.mjs setup linux-x64
node scripts/desktop.mjs doctor linux-x64
node scripts/desktop.mjs test linux-x64
node scripts/desktop.mjs package linux-x64
```

Unix 包输出到 `artifacts/<版本>/<目标>/`。用 `node scripts/desktop.mjs aggregate <目标产物根目录> <新输出目录>` 校验并汇总四目标产物。正式校验追加 `--formal-tag v<版本>`，要求四目标同一干净、数字版本的标签源码，macOS 还要有签名、公证和 stapling 记录。GitHub CI 产物供下载验证。汇总、上传到 CI、创建正式发布是独立的步骤。

## 核心能力复验

| 项目 | 当前可用证据 | 仍需验证 |
| --- | --- | --- |
| 平台生命周期与私有运行时 | Windows 既有 Job Object，Unix 使用父进程控制管道和受管进程组。锁定的私有 Python、Node、REA 随目标组装 | 搬到包含空格和 Unicode 的路径，清空宿主工具链搜索路径，断网启动。用 Tree-sitter 真解析，再用 REA 读取已知 Go 产物并核对 SHA-256。杀死宿主后检查 Go、worker 和常规子孙进程 |
| 项目与桌面 | Windows 原有 COM 文件夹选择器，Unix 原生对话框接入。macOS 关闭窗口后保留会话与服务，Dock 请求显示原窗口 | 手动选目录、打开文件管理器、Cmd+O/Cmd+Q、拖放、中文输入法、剪贴板、缩放、高 DPI、WebGL 与低动效降级。Linux 记录 X11/Wayland，macOS 在最终签名包验证 |
| T11/T12 | Browser 回归使用模拟 IPC，Windows 源码测试使用本地 HTTP fixture | 各目标记录真实出站 endpoint/model_id、Base URL 拼接、无默认模型、工作区和会话绑定、执行中任务模型冻结、流停止、附件原件与引用、索引、记忆编辑、历史任务、审批、冲突和恢复 |
| 系统凭证 | Windows Credential Manager 保留既有存储名称，macOS Keychain 和 Linux Secret Service 源码接入。项目不需要 CGO | 原生分场景验证不存在、锁定、拒绝、服务不可用、损坏及签名后升级。用无凭证本地模型验证服务不可用时仍能进入主界面 |
| 本地推理 | Ollama 使用直接子进程及监听者核验，LM Studio 连接已有 HTTP 服务。Windows SYCL 保留独立 profile | 每目标至少真实启动一个本地模型、发送生成请求、切换模型并清理自有引擎。验证端口冲突及手动/systemd/launchd 服务保持运行。Unix 上宿主硬崩溃后的引擎进程回收待实施 |
| 授权测试与文件动作 | Windows 结构化测试和文件恢复有真实执行证据。Unix 进程清理、外部项目工具链发现及批准后执行已进入源码 | 原生运行可信 Go/npm/Python 测试。无工具链给出缺失状态。验证拒绝、并发批准、取消、重启不重放、符号链接/junction、大小写和 Unicode、项目解绑及外部编辑冲突 |
| 更新与复原 | 目标选择、来源限制、大小和 SHA-256、下载后备份与安装提示已接入 | 从上一版本验证 NSIS、deb、DMG 的升级与恢复。AppImage 在用户可写目录换包。比较升级前后数据、会话和附件。系统签名另行验证 |

项目测试使用项目之外、由用户安装并信任的 Go、Python 或 Node，通过批准记录执行固定参数数组。自定义工具链的绝对路径可通过 `NOOSPHERE_TEST_GO`、`NOOSPHERE_TEST_PYTHON`、`NOOSPHERE_TEST_NODE` 显式指定。进程回收覆盖正常进程组。被批准的测试代码可写外部文件或访问外部服务。现有文件内容在原子替换前复核，复核与替换之间仍存在外部程序并发编辑窗口，新建文件采用原子不覆盖机制。受限 Go 编程测评在 Linux/macOS 保持关闭，等价编译与执行约束待实施。

## 0.8.0 至 0.9.0 的候选历史证据

跨平台首轮是在 `255e1af95c18ae84d212e831942a33440a43ec38` 的开发工作区验证的。本轮 `0.9.0` 候选基于 `main / v0.8.1 / 057ab0e100a581b59d99861403738f9520447182` 整合。下方 `0.8.0` 产物仅记录首轮证据。

0.9.0 候选复核修正了凭证降级兼容。新版密文使用独立 YAML 字段，旧客户端不会将其读作 API Key。发布 GitHub 正式版需要先取得原生验收结果，并在受保护的环境中配置 macOS Developer ID、公证和 stapling。

- **已完成**。Windows `a520bac8df26163309cd61791c730da9d33a177f` 的干净 0.9.0 便携包通过七组 WebView2 原生检查和真实 Bonsai SYCL 闭环。该包和配套 NSIS 的哈希及命令保存在本地 `.local/release-0.9.0-evidence/RETEST.md`。修复 CI 固定端口读取后，便携包通过端口策略复测。隔离安装结果以 [候选 CI](https://github.com/2722039978-source/Noosphere/pull/6/checks) 为准。
- **待验证**。第二轮 Windows CI 已使用固定调试端口，安装后的 WebView2 仍无法连接。CI 改为以验证过的 Medium 完整性运行现有安装、卸载重装和便携包验收，保留测试专用浏览器配置和 loopback 调试。正式二进制的调试行为未改变。 [Microsoft WebView2 权限说明](https://learn.microsoft.com/en-us/microsoft-edge/webview2/concepts/security) · [WRY 复验方式](https://github.com/tauri-apps/wry/issues/1782)
- **待验证**。Linux 的打包日志确认 `_tkinter` 依赖 `libtcl9.0.so`，两者都在私有 CPython 中。打包限定库搜索路径后，需从两个最终包分别复跑私有解析和离线启动。**待核实**。AppImage 构建日志报告部分 Ubuntu 系统库的版权文件没有自动找到，须对最终包实际分发的库核对其版权与许可文件。
- **待验证**。Apple Silicon 与 Intel 的 `.app` 和 DMG 通过私有运行时搬迁和挂载解析，桌面启动需要继续复验。测试将临时目录解析为规范路径，保留 Tauri 对符号链接启动路径的安全检查。最低系统、Dock/Cmd+Q、Keychain 和真实模型需分别取得两种架构的原生证据。

- **已完成**。对照 `main` 核对 tracked 与 untracked 路径。至少 142 个未跟踪文件与 `main` 内容一致。T11/T12、Model Lab、连接向导和 Windows 发布文件中的大量项目因此是已有成果。将来合入以 `main` 已有版本为基础，逐文件审阅本轮真正新增的代码，保留后续分支对 Provider 的兼容修复。
- **已完成**。合入 `main` 中缺失的 OpenAI 采样参数处理和 Responses 工具回退。Windows `go test ./internal/gateway/providers` 通过，测试包含实际回环请求与字段断言。
- **已完成**。Windows `npm --prefix frontend run build` 通过，Vite 报一个大于 500 kB 的 chunk 警告。`node ../scripts/verify-browser.cjs` 从 `frontend/` 运行通过，覆盖模型切换、绑定、停止、记忆和引用交互。该套件使用浏览器和模拟 IPC。
- **已完成**。Windows `python scripts/test-agent-release.py --backend <本轮构建的 backend.exe>` 为 9/9，包含真实 Python 项目测试和批准后执行、外部编辑与恢复。Linux/macOS 三目标编译检查已完成，执行结果待原生验证。
- **已完成**。Windows Go 使用 `CGO_ENABLED=0 go test -p 2 -timeout 8m -count=1 ./...` 完成全包测试。Rust 本机 `cargo test --locked --offline` 通过 51 项，1 项真实 Go 模拟集成测试默认忽略。前端 `node --test tests/*.test.mjs` 通过 223 项。
- **已完成**。构建工作流 YAML 通过 `actionlint`，构建脚本通过 Node 语法检查。按 Linux x64 和 macOS 两种架构下载并核对各 19 个 Python wheel 的锁文件哈希。对应目标上的安装、解析和签名后运行仍待验证。
- **已完成**。最终 Windows ZIP 解包后进行了真实 Bonsai 2 27B SYCL 验收。发现和启动引擎、监听进程身份核验、Gateway 会话绑定、真实推理和停止均通过。llama.cpp 版本为 `b10787-73bfe6cc4`，模型 ID 为 `bonsai-local-27b`，实际生成接口为 `http://127.0.0.1:18787/v1/chat/completions`，答复 `4`，`finish_reason=stop`。关闭后端口释放。证据为 `real-model-final-zip.log`、`real-model-final-zip/model-probe.json` 和截图。构建过程中的第一次启动触发低内存保护并回收进程，单独记录在 `real-model-windows.log`。Ollama Windows 备选测试未运行，下载阶段遇到 TLS 证书吊销检查错误。
- **已完成**。Windows 用 `pwsh -NoProfile -File scripts/package.ps1 -OutputRoot artifacts/cross-platform-win-final-20261120-070736` 生成 0.8.0 候选包。搬迁后清空宿主解释器搜索路径，CodeLens 3.14.7 实际解析七种语言，Node 24.22.2 与 REA 6.8.0 对包内 Go 后端完成产物分析及哈希核对。包内外 build-info 一致。NSIS 的 Authenticode 检查为 `NotSigned`。
- **已完成**。`node scripts/verify-desktop.cjs <解包目录>` 对最终便携 ZIP 的原生 WebView2 桌面通过七组检查。涵盖真实索引、会话/任务/记忆、200 MiB 文本和 PDF 附件、拖放与引用、回环模拟 Provider 流式和审批、后端崩溃恢复、正常退出及进程清理。证据在产物父目录的 `native-webview.log` 和 `screenshots/`。

候选工作区 HEAD 是 `255e1af95c18ae84d212e831942a33440a43ec38`，`source_dirty=true`，实际构建源指纹为 `0fb1d8aae109099b251f66950bb59acd406dc9661b010abc1835e8d070f5e80f`。本地位置为 `artifacts/cross-platform-win-final-20261120-070736/0.8.0/`。

| Windows 最终文件 | 字节数 | SHA-256 |
| --- | ---: | --- |
| `Noosphere-0.8.0-windows-x64-setup.exe` | 379879310 | `ed41aea2fd669ee5290e1ce7102520cb4d3e5279feba72d49d6020fac0420612` |
| `Noosphere-0.8.0-windows-x64.zip` | 124435587 | `f3ac767f063bb4f3acf58648755794bf81a6d9cd653a9a2a110e98241c98c8cc` |
| `build-info.json` | 3512 | `8c9a4a133c3eea5790ef5f73d894f971e76bf9d40d4382e16a5cbc578a62d4b9` |

构建日志和本次包所用后端的 9/9 审批实测分别在产物父目录的 `package.log`、`backend-approval.log`。其他源码验证和环境限制日志在 `.local/cross-platform-acceptance/`。Windows 是本地唯一已生成目标，四目标汇总命令按预期报缺少目标。

本地对比文件为 `workspace-inventory.txt`、`tracked-vs-main.txt`、`untracked-vs-main.tsv`。建议合入已经校验的源码、锁、验收脚本及第三方许可。模型权重、应用数据和凭证、临时日志、编译缓存、旧发行包及个人工作环境文件保留在本地。剩余差异先查 Windows 基线门禁和项目功能测试，再形成可审阅提交。

## 发行记录要求

下载来源校验、签名前文件校验和最终发行物校验各自独立。macOS 为主程序、Go、Python、Node、动态库及其他 Mach-O 设置 Developer ID 签名，启用 Hardened Runtime。提交公证后分别检查最终 .app 与 DMG 的签名及 stapling，再从 DMG 安装验证。Linux/Windows 本地测试包继续使用现有政策。SHA-256 用于完整性验证。系统身份签名和公证仍由相应系统校验。

发行应生成同一干净源码下的四目标最终包，并校验 Windows 安装、Linux 和 macOS 原生运行及 macOS 签名和公证证据。保留可追溯的版本和哈希记录。
