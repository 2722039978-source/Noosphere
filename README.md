<img src="frontend/public/noosphere-mark.png" alt="Noosphere" width="96">

# Noosphere

Noosphere 是本地 AI 项目工作台。打开一个目录，就能在同一工作区里用模型读代码、查资料、推进任务。重新打开项目，可以沿着原来的会话、任务记录和项目记忆继续做。

本地模型和云端 API 共用对话右下角的模型菜单。已经安装的本地引擎可以在应用里发现、启动和验证。配置好云端 API 后，也可以在当前会话中切过去。

[下载稳定版](https://github.com/2722039978-source/Noosphere/releases/latest) · [更新记录](CHANGELOG.md) · [发行验收](CROSS_PLATFORM_ACCEPTANCE.md) · [反馈问题](https://github.com/2722039978-source/Noosphere/issues)

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

1. 打开 [GitHub Releases](https://github.com/2722039978-source/Noosphere/releases/latest)，下载适用的软件包。Windows 可运行安装器，或完整解压 ZIP 后启动 `Noosphere.exe`，便携版使用系统 WebView2 Runtime。Linux 可用 `sudo apt install ./Noosphere-1.0.0-linux-x64.deb` 安装 deb，或给 AppImage 添加执行权限后启动。
2. 点击左侧文件夹图标打开项目。可以直接选择检测到的启动目录，也可以浏览到另一个本地目录。
3. 从对话右下角选择模型。选中还没启动的本地模型时，可在弹窗中启动并验证。接入云端服务时，在「模型 → 高级管理 → 手动配置」填写连接信息，配置完成后从同一个模型菜单选择带「API」标识的模型。
4. 直接提问，或点击回形针添加附件。先从「这个项目的入口在哪里」或「总结这份文档的要求」开始。

1.0 正式发行面向 Windows x64 和 Linux x64。Linux 包在 Ubuntu 22.04 桌面环境验收。AppImage 普通启动需要 FUSE 和系统图形运行库，缺少 FUSE 时可用 `./Noosphere-1.0.0-linux-x64.AppImage --appimage-extract-and-run`。 [发行与验收状态](CROSS_PLATFORM_ACCEPTANCE.md)

## 更多工具

| 入口 | 用途 |
| --- | --- |
| 模型 | 扫描已安装的本地引擎，选择模型，查看服务状态和日志。高级管理可配置 API、默认模型和版本。 |
| 测评 | 用「模型试聊」向本地模型发一条短消息，查看连接和回答。用「正式测评」按指定任务集与参数运行受支持的本地模型，查看逐题结果并导出报告。 |
| 系统 | 查看 CPU、内存、磁盘和进程，按问题发起诊断，在对话侧栏回看任务过程。 |
| 功能介绍 | 点击窗口左上角的 Noosphere 图标，打开带三维动效的项目导览。 |

## 模型与数据

本地模型可使用已安装的 Ollama、LM Studio 或受支持的便携 llama.cpp 环境。安装包提供桌面程序和内置服务，模型权重通过对应引擎获取。云端对话可接入 DeepSeek、OpenAI、Claude、Gemini 和 OpenAI 兼容服务。使用云端模型时，对话和提供给模型的上下文会发送至所选 API。

模型选择依次使用会话绑定、工作区默认和全局默认。显式选择不可用时会报告原因，执行中的任务保留启动时的模型。DeepSeek、OpenAI、OpenAI 兼容服务和 Ollama 提供结构化工具适配，每个模型的对话和工具能力分别验证。应用只管理自己启动并拥有的模型进程。

每条消息最多添加 3 份附件，每份不超过 200 MiB。每个会话的原件与索引合计上限为 640 MiB，应用总上限为 2 GiB。文本和代码支持 UTF-8、带 BOM 的 UTF-16 与 GB18030。PDF 提取最多 100 页的文字层和 1 MiB 文本，图片与扫描 PDF 可先在本机识别。模型收到与问题相关的文字节选，引用记有文件名、页码或行号以及来源哈希。移除草稿或删除对应会话会清理附件。

CodeLens 的静态关系依赖解析和索引覆盖范围，文件变化后请更新索引。REA 当前可读取项目内文件的 PE 导入表和 Go 构建信息，文件上限 64 MiB。项目文件修改和结构化测试须逐项审批。目标文件或项目绑定变化时，受影响的提案需要重新审阅。应用恢复限于有记录的改动，后续外部编辑会保留冲突。项目测试以当前用户权限运行，可能产生文件及外部服务副作用。

本地测评使用受支持的 Ollama、llama.cpp 或 Windows 上可选的 OpenVINO。报告记录模型、任务集、参数和实际观测，结果适用于该次测试条件。系统性能指标按平台支持范围显示，缺失的指标保留为未知。 [OpenVINO 接入](scripts/openvino/USAGE.txt) · [原有测评细目](https://github.com/2722039978-source/Noosphere/blob/v1.0.0-rc.2/LIMITATIONS.md#本地模型测评)

## 更新备份与恢复

「设置 → 应用信息」显示当前应用数据目录。「设置 → 会话备份」可导出会话和已有文字节选。附件原件、工作区和任务库使用应用数据备份。升级前先导出会话 JSON，再退出应用并保留原数据目录和 WebView 用户资料。跨系统迁移时导入会话，重新绑定项目和模型，配置系统凭证并重建索引。旧审批只可回看。

「设置 → 软件更新」按安装方式、系统及架构选择更新包，校验大小和 SHA-256，并导出会话 JSON。退出应用后按提示安装或替换程序。旧版便携目录如有 `config.yaml`，先核对应用数据目录再迁移。

应用退出后，可用交付包资源目录的 `bin/noosphere-backend`（Windows 为 `bin/noosphere-backend.exe`）生成快照或从快照恢复。用「应用信息」显示的实际数据目录填写绝对路径。

```text
noosphere-backend -config "<原应用数据目录>/config.yaml" -backup-data
noosphere-backend -restore-data-from "<快照目录>" -restore-data-to "<尚不存在的新目录>"
```

快照保存配置、SQLite 及其伴随文件、附件原件和任务库。恢复先核对清单与 SHA-256，再写入新目录。可用 `NOOSPHERE_DATA_DIR` 指定恢复目录并核对数据。需要回到 v0.9.0 时使用升级前快照与对应的 WebView 资料或会话 JSON。API 凭证由 Windows Credential Manager 或 Linux Secret Service 管理。需要跨机重新授权时，可从保留的模型连接记录继续配置。

## 开发和构建

各目标使用对应架构的宿主、Git、锁定的 Node.js、Rust 和 Go 工具链，以及系统 WebView 构建依赖。Windows 入口使用 PowerShell 7 和 Visual Studio「使用 C++ 的桌面开发」工作负载。 [项目版本](scripts/build-config/VERSION) · [Node.js](scripts/build-config/.node-version) · [CPython](scripts/build-config/.python-version) · [Rust](desktop/rust-toolchain.toml) · [Go](backend/go.mod) · [目标清单](scripts/platform-targets.json)

```powershell
pwsh -NoProfile -File scripts/setup.ps1 -InstallBrowser
pwsh -NoProfile -File scripts/dev.ps1
```

```powershell
pwsh -NoProfile -File scripts/build.ps1
pwsh -NoProfile -File scripts/package.ps1
```

新的本地 Windows 构建将程序和运行组件归入 `release/app/`，从仓库或 `release/` 内的 `Start-Noosphere.cmd` 启动。`package.ps1` 将 NSIS 安装器、便携 ZIP、构建信息和 SHA-256 校验文件生成到 `artifacts/<版本>/`。

Linux 使用原生 Ubuntu 22.04 x64 构建环境，安装 GTK 3、WebKitGTK 4.1 开发包和系统打包工具。 [构建依赖与验收记录](CROSS_PLATFORM_ACCEPTANCE.md)

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

[桌面 CI](https://github.com/2722039978-source/Noosphere/actions) · [MIT License](LICENSES/MIT.txt)

## 第三方资源

Noosphere 的档案界面与三维场景参考并适配 RhineLabUI 的阵列、档案盒模型、材质、运动和灯光。RhineLabUI 的视觉参考来自《明日方舟》「莱茵生命：访问」终端。相关名称、标志、设定和设计的权利归各自权利人所有。 [RhineLabUI 上游](https://github.com/LBEILC/RhineLabUI) · [MIT 声明](frontend/public/assets/RhineLabUI-LICENSE.txt)

- REA 及依赖的许可证随内置分析组件分发。 [REA 上游](https://github.com/morluto/rea)
- 附件的 PDF 提取与文本解码。 [附件依赖许可](frontend/public/assets/Attachment-Dependencies-LICENSE.txt)
- MiSans 字体。 [版权说明](frontend/public/fonts/NOTICE.txt) · [字体许可](frontend/public/fonts/MiSans-license.pdf)
- 模型厂商图标。 [来源及商标](frontend/public/model-brands/NOTICE.txt) · [图标许可](frontend/public/model-brands/LICENSE.txt)
