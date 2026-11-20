# 桌面跨平台验收记录

记录日期为 2026-11-20（America/Los_Angeles）。每次复验记录版本、源码提交及工作区指纹、原生产物 SHA-256、目标 OS/CPU、测试命令及日志位置。状态使用 **已完成**、**待验证**、**待实施**、**阻塞** 和 **待核实**。源码适配、原生测试包和正式发行分别判断。

## 目标和证据矩阵

| 目标 | 构建环境与最低系统 | 计划分发 | 源码与构建 | 原生运行与正式发行 |
| --- | --- | --- | --- | --- |
| Windows x64 | `windows-2025` x64/MSVC；现有兼容下限为带 WebView2 的 Windows 10 x64 | NSIS、完整便携 ZIP | 已完成 0.9.0 源码测试、构建、打包和哈希复核 | 本地 0.9.0 便携包通过 WebView2 和真实 Bonsai SYCL 推理验收。隔离安装、卸载与重装按 CI 记录判定 |
| Linux x64 | `ubuntu-22.04` x64，glibc 2.35 构建基线，WebKitGTK 4.1 | AppImage、deb | 原生源码测试和 deb 构建通过，AppImage 在 linuxdeploy 解析私有 CPython 的 Tcl 依赖时失败 | 已将私有库路径限于打包进程，待复验 AppImage、deb/升级、离线 X11、Wayland 和真实模型 |
| macOS arm64 | `macos-15` Apple Silicon，原生 arm64；候选最低 macOS 13.5 | .app 归档、DMG | 原生源码测试、预览包生成和搬迁后的私有组件解析通过 | 搬迁启动时 Tauri 资源定位失败。已让测试从规范路径启动，待复验。正式发行阻塞于 Developer ID Application 和公证权限 |
| macOS x64 | `macos-15-intel` Intel，原生 x86_64；桌面运行包候选最低 macOS 13.5，Ollama CPU 本地模型验收最低 macOS 14 | .app 归档、DMG | 原生源码测试通过 | 原生包、真实 Ollama CPU 和最低系统待验证。正式发行阻塞于 Developer ID Application 和公证权限 |

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

## 当前工作区与本地证据

跨平台首轮是在 `255e1af95c18ae84d212e831942a33440a43ec38` 的开发工作区验证的。本轮 `0.9.0` 候选基于 `main / v0.8.1 / 057ab0e100a581b59d99861403738f9520447182` 整合。下方 `0.8.0` 产物仅记录首轮证据。

0.9.0 候选复核修正了凭证降级兼容。新版密文使用独立 YAML 字段，旧客户端不会将其读作 API Key。发布 GitHub 正式版需要先取得原生验收结果，并在受保护的环境中配置 macOS Developer ID、公证和 stapling。

- **已完成**。Windows `a520bac8df26163309cd61791c730da9d33a177f` 的干净 0.9.0 便携包通过七组 WebView2 原生检查和真实 Bonsai SYCL 闭环。该包和配套 NSIS 的哈希及命令保存在本地 `.local/release-0.9.0-evidence/RETEST.md`。修复 CI 固定端口读取后，便携包通过端口策略复测。隔离安装结果以 [候选 CI](https://github.com/2722039978-source/Noosphere/pull/6/checks) 为准。
- **待验证**。第二轮 Windows CI 已使用固定调试端口，安装后的 WebView2 仍无法连接。CI 改为以验证过的 Medium 完整性运行现有安装、卸载重装和便携包验收，保留测试专用浏览器配置和 loopback 调试。正式二进制的调试行为未改变。 [Microsoft WebView2 权限说明](https://learn.microsoft.com/en-us/microsoft-edge/webview2/concepts/security) · [WRY 复验方式](https://github.com/tauri-apps/wry/issues/1782)
- **待验证**。Linux 的打包日志确认 `_tkinter` 依赖 `libtcl9.0.so`，两者都在私有 CPython 中。打包限定库搜索路径后，需从两个最终包分别复跑私有解析和离线启动。**待核实**。AppImage 构建日志报告部分 Ubuntu 系统库的版权文件没有自动找到，须对最终包实际分发的库核对其版权与许可文件。
- **待验证**。Apple Silicon 的 `.app` 与 DMG 通过私有运行时搬迁和挂载解析，桌面启动需要继续复验。测试将临时目录解析为规范路径，保留 Tauri 对符号链接启动路径的安全检查。最低系统、Dock/Cmd+Q、Keychain 和真实模型需分别取得两种架构的原生证据。

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
