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
| 阻塞 | 正式包签名、公证与最终业务验收。四目标包、CI、签名环境 | 已取得下方各版本的原生候选证据，包括四目标包内后端的真实远程推理。正式包需要获授权的 Apple Developer ID、公证配置及成品复验。 |

按模型与数据边界、审批与项目流程、源码回归、候选打包、最终包原生验收推进。签名后的实际文件参与最终验收及哈希记录。历史检查见下文。

### 数值版本 1.0.0 的预览验收

以下预览验收从提交 `564ad1f93549b0a05d4cdfa6f19e5bbddfd6d5a0`、源码树 `47eed7d21151c407605f617585ca5e73eb7c3ebf` 生成。包与日志保存在 Actions，公开下载仍为 rc.2。签名后的正式包需要独立哈希及验收。 [预览构建](https://github.com/2722039978-source/Noosphere/actions/runs/45232545703) · [正式源码 PR](https://github.com/2722039978-source/Noosphere/pull/9)

| 目标 | 实际预览包证据 |
| --- | --- |
| Windows x64 | NSIS 通过公开 0.9.0 原位升级、卸载与重装。便携 ZIP 在 Unicode 路径的 WebView2 中连接真实 OpenVINO CPU 模型并获得回答。用公开 0.9.0 后端创建的工作区、模型、约定、任务与附件验证升级、阻挡写入及隔离恢复。ZIP 的 SHA-256 为 `fc18629249d4dff39da3131b3527b893461e403d239527b283f9f12d40b2fa9b`。本地证据保存于 `test/release-1.0/windows-1.0.0-preview-upgrade/` 和 `.local/model-canary/`。 [Windows 包与日志](https://github.com/2722039978-source/Noosphere/actions/runs/45232545703/job/137895851301) |
| Linux x64 | Ubuntu 22.04 的 deb 通过 0.9.0 真实安装升级和合成凭证重启读回。deb 与 AppImage 通过搬迁、X11 离线启动和纯 Weston Wayland 窗口及缓冲区检查。AppImage 以 extract-and-run 启动。AppImage 与 deb 的 SHA-256 依次为 `53ac848d4df4dd87cfe55080f195c6e7d16ea7b0cc764909ff2ef40173cc5421` 与 `371faa5a62eb0ba84f3796a93e5f6d30c82c40efd8267f1d5d4d24aaace70deb`。 [Linux 包与日志](https://github.com/2722039978-source/Noosphere/actions/runs/45232545703/job/137895851881) |
| macOS x64 | Intel 原生 `.app` 归档与 DMG 通过搬迁启动、私有运行时和进程清理。独立系统 Keychain 通过合成凭证保存、重启读回及锁定后保留密文。该次构建使用 ad-hoc 签名。 [Intel Mac 包与日志](https://github.com/2722039978-source/Noosphere/actions/runs/45232545703/job/137895851698) |
| macOS arm64 | `macos-15` 两次在分配 runner 前取消。同一源码在 macOS 26.7.2 Apple Silicon 上生成 ad-hoc `.app` 归档及 DMG。通过搬迁启动、DMG 安装、私有进程清理及独立 Keychain 保存、重启读回与锁定检查。归档的 SHA-256 为 `5a3a55de21ac965789cdf4895f2178c6890412ff6613fccbb5cd840188d0b877`。 [原队列](https://github.com/2722039978-source/Noosphere/actions/runs/45232545703) · [Apple Silicon 包与日志](https://github.com/2722039978-source/Noosphere/actions/runs/45241333984) |

工作流的 Apple Silicon 构建使用 `macos-26`，目标架构为 arm64。macOS 13.5 仍是待在目标设备复验的最低系统候选。此前的 `macos-15` 原生记录保留在对应的历史版本下。

Windows x64、Linux x64、macOS arm64 与 macOS x64 的上述实际包内后端，均通过标准 HTTPS OpenAI 兼容 Chat Completions 接入独立云端 runner 上的 llama.cpp b12359 CPU 与 SmolLM2-135M-Instruct Q4_K_M。四目标均确认上游错误凭证返回 401，正确凭证获得真实推理。Windows 首次运行时测试脚本写入 CRLF，触发后端对握手换行的检查。改用原生字节握手后，从同一 ZIP 对同一个远程服务复测通过。测试仅使用合成输入及临时凭证，完成后关闭服务并移除凭证。成品桌面交互、流式响应及远程工具往返各有独立验收项。 [Linux 和两种 Mac](https://github.com/2722039978-source/Noosphere/actions/runs/45244554470) · [Windows 复测](https://github.com/2722039978-source/Noosphere/actions/runs/45245263553)

Linux 的普通 AppImage 启动需要 FUSE 和桌面图形运行库。补充检查在最小化 Ubuntu 镜像上缺少 `libEGL.so.1`，原生 FUSE 路径暂未验收通过。 [FUSE 补充检查](https://github.com/2722039978-source/Noosphere/actions/runs/45247088174)

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

## 构建和发行命令

构建命令及工具链见 [开发和构建](README.md#开发和构建)。Ubuntu 22.04 构建需要 `libwebkit2gtk-4.1-dev`、GTK 3、`libappindicator3-dev`、`librsvg2-dev`、`libxdo-dev`、`libfuse2`、`patchelf` 与 `xz-utils`。Mac 使用对应架构的原生 Xcode。Windows `package.ps1` 支持 `-OutputRoot <新目录>`。

Unix 包输出到 `artifacts/<版本>/<目标>/`。用 `node scripts/desktop.mjs aggregate <目标产物根目录> <新输出目录>` 汇总。正式分发使用 `--formal-tag v<版本>` 校验匹配标签的四目标源码、最终文件的 SHA-256，以及两种 Mac 的 Developer ID 签名、公证和 stapling 记录。

## 历史验收

- v0.9.0 记录 Windows 安装与 WebView2、Ubuntu 22.04 X11 和两种 Mac 预览包。 [v0.9.0 标签构建](https://github.com/2722039978-source/Noosphere/actions/runs/44788436392) · [v0.9.0 下载与支持范围](https://github.com/2722039978-source/Noosphere/releases/tag/v0.9.0)
- 早期候选、Windows 详细构建指纹和原生记录随原始版本永久链接保留。 [Windows 候选详细记录](https://github.com/2722039978-source/Noosphere/blob/1b5ca890306cb21d54005c6e3005138dbfdd72ab/CROSS_PLATFORM_ACCEPTANCE.md#本地-windows-候选历史证据) · [v0.8.0–v0.9.0 历史](https://github.com/2722039978-source/Noosphere/blob/1b5ca890306cb21d54005c6e3005138dbfdd72ab/CROSS_PLATFORM_ACCEPTANCE.md#080-至-090-的候选历史证据)
