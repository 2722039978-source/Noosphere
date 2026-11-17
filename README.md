<img src="frontend/public/noosphere-mark.png" alt="Noosphere" width="96">

# Noosphere

Noosphere 是面向 Windows 的 AI 项目工作台。打开一个本地目录，就能在同一工作区里用模型读代码、查资料、推进任务。重新打开项目，可以沿着原来的会话、任务记录和项目记忆继续做。

本地模型和云端 API 共用对话右下角的模型菜单。已经安装的本地引擎可以在应用里发现、启动和验证。配置好云端 API 后，也可以在当前会话中切过去。

[下载最新版](https://github.com/2722039978-source/Noosphere/releases/latest) · [更新记录](CHANGELOG.md) · [使用说明](LIMITATIONS.md) · [反馈问题](https://github.com/2722039978-source/Noosphere/issues)

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

「设置 → 应用信息」显示当前应用数据目录。「设置 → 会话备份」可导出和导入会话。更新或迁移时也可备份应用数据目录，以便保留配置和附件原件。

「设置 → 软件更新」可下载并校验新版安装包。下载完成后，根据提示退出 Noosphere 并运行安装器。

## 开发和构建

源码构建使用 Windows x64、PowerShell 7、Git 和 Visual Studio 的「使用 C++ 的桌面开发」工作负载。Node.js、CPython、Rust 和 Go 的版本记录在项目的工具链配置里。 [Node.js](.node-version) · [CPython](.python-version) · [Rust](rust-toolchain.toml) · [Go](backend/go.mod)

```powershell
pwsh -NoProfile -File scripts/setup.ps1 -InstallBrowser
pwsh -NoProfile -File scripts/dev.ps1
```

```powershell
pwsh -NoProfile -File scripts/build.ps1
pwsh -NoProfile -File scripts/package.ps1
```

`build.ps1` 将桌面程序构建到 `release/`。`package.ps1` 将 NSIS 安装器、便携 ZIP、构建信息和 SHA-256 校验文件生成到 `artifacts/<版本>/`。

| 目录 | 职责 |
| --- | --- |
| [frontend/src](frontend/src) | React / TypeScript 工作台界面，包含对话、任务、模型和项目页面。 |
| [desktop/src-tauri](desktop/src-tauri) | Tauri / Rust 桌面窗口、文件选择、服务进程管理和应用更新。 |
| [backend](backend) | Go 后端，负责模型调用、工作区、附件、任务和测评。 |
| [backend/python/codelens](backend/python/codelens) | Python CodeLens 源码解析和索引。 |

[Windows CI](https://github.com/2722039978-source/Noosphere/actions) · [MIT License](LICENSE) · [第三方资源](LIMITATIONS.md#设计来源与第三方资源)
