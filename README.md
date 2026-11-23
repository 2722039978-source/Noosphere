<img src="frontend/public/noosphere-mark.png" alt="Noosphere" width="96">

# Noosphere

Noosphere 是本地 AI 项目工作台。打开一个目录，就能在同一工作区里用模型读代码、查资料、推进任务。重新打开项目，可以沿着原来的会话、任务记录和项目记忆继续做。

本地模型和云端 API 共用对话右下角的模型菜单。已经安装的本地引擎可以在应用里发现、启动和验证。配置好云端 API 后，也可以在当前会话中切过去。

[下载稳定版](https://github.com/2722039978-source/Noosphere/releases/latest) · [下载 1.0 候选版](https://github.com/2722039978-source/Noosphere/releases) · [更新记录](CHANGELOG.md) · [使用说明](LIMITATIONS.md) · [跨平台验收](CROSS_PLATFORM_ACCEPTANCE.md) · [反馈问题](https://github.com/2722039978-source/Noosphere/issues)

## 用 Noosphere 做什么

### 接手一个项目，找到代码入口

在「代码 → 项目索引」为当前目录建立索引，搜索定义、查看静态关系，沿文件名和行号回到源码。在对话里可以问「这个项目从哪里启动？配置是在哪一步加载的？请给出对应文件和行号。」代码工具也支持读取项目内 Windows 可执行文件的 PE 导入表和 Go 构建信息。

### 阅读资料，核对回答里的出处

把需求文档、日志、代码或有文字层的 PDF 拖进对话。比如同时添加两版需求，问「哪些验收条件变了？请按条列出，并标出出处。」Noosphere 会提取与问题相关的片段，保留文件名、页码或行号。本地模型也可以使用这些文本片段。

### 把对话变成可继续的任务

从输入框的「+」选择「转为任务」，也可以从会话侧栏新建任务。比如先让模型定位测试失败的原因，再查看操作提案，决定是否修改文件或运行测试。历史任务以对话形式显示目标、步骤、审批和结果，可以回到记录继续追问。

### 保存值得长期保留的项目约定

可以在对话中提出「记住这个项目的构建命令和提交前要跑的检查」。确认记忆候选后，约定会保存在当前工作区。「记忆」页可以查看来源、编辑、暂停或删除。想改写一条记忆时，可以在对话里先比较前后内容，再确认保存。

## 开始使用

1. 打开 [GitHub Releases](https://github.com/2722039978-source/Noosphere/releases/latest)，下载 Windows x64 安装器。也可以解压便携 ZIP 后运行 `Noosphere.exe`，便携版使用系统中的 WebView2 Runtime。
2. 点击左侧文件夹图标打开项目。可以直接选择检测到的启动目录，也可以浏览到另一个本地目录。
3. 从对话右下角选择模型。选中还没启动的本地模型时，可在弹窗中启动并验证。接入云端服务时，在「模型 → 高级管理 → 手动配置」填写连接信息，配置完成后从同一个模型菜单选择带「API」标识的模型。
4. 直接提问，或点击回形针添加附件。先从「这个项目的入口在哪里」或「总结这份文档的要求」开始。

稳定版发行页列出当前可下载的软件包和支持范围。1.0 候选发行页提供 Windows x64、Linux x64、macOS arm64 和 macOS x64 的预览包。两种 Mac 架构的候选包采用 ad-hoc 签名。macOS 用 ⌘O 打开项目、⌘Q 退出。关闭主窗口会隐藏工作台，点击 Dock 图标可恢复窗口。 [稳定版](https://github.com/2722039978-source/Noosphere/releases/latest) · [1.0 候选版](https://github.com/2722039978-source/Noosphere/releases/tag/v1.0.0-rc.2) · [跨平台验收](CROSS_PLATFORM_ACCEPTANCE.md)

## 更多工具

| 入口 | 用途 |
| --- | --- |
| 模型 | 扫描已安装的本地引擎，选择模型，查看服务状态和日志。高级管理可配置 API、默认模型和版本。 |
| 测评 | 用「模型试聊」向本地模型发一条短消息，查看连接和回答。用「正式测评」按指定任务集与参数运行受支持的本地模型，查看逐题结果并导出报告。 |
| 系统 | 查看 CPU、内存、磁盘和进程，按问题发起诊断，在对话侧栏回看任务过程。 |
| 功能介绍 | 点击窗口左上角的 Noosphere 图标，打开带三维动效的项目导览。 |

## 模型与数据

本地模型可使用已安装的 Ollama、LM Studio 或受支持的便携 llama.cpp 环境。安装包提供桌面程序和内置服务，模型权重通过对应引擎获取。云端对话可接入 DeepSeek、OpenAI、Claude、Gemini 和 OpenAI 兼容服务。使用云端模型时，对话和提供给模型的上下文会发送至所选 API。

每条消息可添加 3 份附件，每份最多 200 MB。文本、代码和有文字层的 PDF 可以直接添加，图片和扫描 PDF 可先转成文字。模型协议、附件格式和测评任务集的详细说明见 [使用说明](LIMITATIONS.md)。

## 更新备份与恢复

「设置 → 应用信息」显示当前应用数据目录。「设置 → 会话备份」可导出会话和已有文字节选。附件原件、工作区和任务库使用应用数据备份。升级前先导出会话 JSON，再退出应用并保留原数据目录和 WebView 用户资料。跨系统迁移时导入会话，重新绑定项目和模型，配置系统凭证并重建索引。旧审批只可回看。

1.0 候选的「设置 → 软件更新」按安装方式、系统及架构选择正式更新包，校验大小和 SHA-256，并导出会话 JSON。退出应用后按提示运行 NSIS、安装 deb、用 Finder 替换应用，或将 AppImage、便携 ZIP 解包到自己的目录。旧版便携目录如有 `config.yaml`，先核对应用数据目录并备份，再迁移配置与数据，避免打开另一份空资料。快照与恢复步骤见 [使用说明](LIMITATIONS.md#构建数据与自动检查)。

## 开发和构建

各目标使用对应架构的宿主、Git、锁定的 Node.js、Rust 和 Go 工具链，以及系统 WebView 构建依赖。Windows 入口使用 PowerShell 7 和 Visual Studio「使用 C++ 的桌面开发」工作负载。 [Node.js](.node-version) · [CPython](.python-version) · [Rust](rust-toolchain.toml) · [Go](backend/go.mod) · [目标清单](scripts/platform-targets.json)

```powershell
pwsh -NoProfile -File scripts/setup.ps1 -InstallBrowser
pwsh -NoProfile -File scripts/dev.ps1
```

```powershell
pwsh -NoProfile -File scripts/build.ps1
pwsh -NoProfile -File scripts/package.ps1
```

`build.ps1` 将桌面程序构建到 `release/`。`package.ps1` 将 NSIS 安装器、便携 ZIP、构建信息和 SHA-256 校验文件生成到 `artifacts/<版本>/`。

Linux 使用原生 Ubuntu 22.04 x64 构建环境，安装 GTK 3、WebKitGTK 4.1 开发包和系统打包工具。macOS 使用对应 CPU 架构的 Xcode Command Line Tools。下面的命令以 Linux 为例，macOS 将目标参数改为 `macos-arm64` 或 `macos-x64`。各命令和具体依赖见 [验收记录](CROSS_PLATFORM_ACCEPTANCE.md)。

```sh
node scripts/desktop.mjs setup linux-x64
node scripts/desktop.mjs doctor linux-x64
node scripts/desktop.mjs dev linux-x64
node scripts/desktop.mjs test linux-x64
node scripts/desktop.mjs package linux-x64
```

完整桌面包包含解析和产物分析所用的私有 Python、Node 及依赖。项目测试使用用户确认的项目工具链。本地推理仍需安装对应引擎和模型权重。

| 目录 | 职责 |
| --- | --- |
| [frontend/src](frontend/src) | React / TypeScript 工作台界面，包含对话、任务、模型和项目页面。 |
| [desktop/src-tauri](desktop/src-tauri) | Tauri / Rust 桌面窗口、文件选择、服务进程管理和应用更新。 |
| [backend](backend) | Go 后端，负责模型调用、工作区、附件、任务和测评。 |
| [backend/python/codelens](backend/python/codelens) | Python CodeLens 源码解析和索引。 |

[桌面 CI](https://github.com/2722039978-source/Noosphere/actions) · [MIT License](LICENSE) · [第三方资源](LIMITATIONS.md#设计来源与第三方资源)
