<img src="frontend/public/noosphere-mark.png" alt="Noosphere" width="96">

# Noosphere

Noosphere 是 Windows 上的 AI 项目工作台。打开一个项目，在对话里使用本地模型或云端 API，带着文件、源码和项目记忆继续工作。

[下载最新版](https://github.com/2722039978-source/Noosphere/releases/latest) · [更新记录](CHANGELOG.md) · [反馈问题](https://github.com/2722039978-source/Noosphere/issues)

## 开始使用

1. 在 GitHub Releases 下载 Windows x64 安装器。也可以解压便携 ZIP 并运行 `Noosphere.exe`。便携版使用系统中的 WebView2 Runtime。
2. 点击左侧文件夹图标打开项目。Noosphere 会识别可用的启动目录，重新打开同一目录会回到对应工作区。
3. 点击对话右下角的模型菜单。已配置的云端模型标注为「API」。选择尚未启动的本地模型后，可在弹窗中启动并验证连接。云端地址和密钥在「模型 → 高级管理 → 手动配置」中填写。
4. 用回形针选择文件，也可以直接拖入对话。文本、代码与有文字层的 PDF 会提取与问题相关的片段，并保留文件名、行号或页码。

## 工作台

| 入口 | 可以做什么 |
| --- | --- |
| 对话 | 查看会话历史和任务，切换模型，引用附件、项目代码和已确认的记忆。 |
| 代码 | 为项目建立索引、搜索定义、追踪来源，也可用 REA 查看 Windows 可执行产物。 |
| 记忆 | 从对话形成记忆候选，确认后存为项目记忆。可查看来源、编辑、暂停或删除，也可在对话里比较改写结果。 |
| 模型 | 扫描本地运行环境，启动或切换模型，查看服务状态与日志。高级管理支持配置 API、默认模型和版本。 |
| 测评 | 用独立的短对话试聊，或用任务集和指定参数运行正式测评，查看逐题结果并导出报告。 |
| 系统 | 查看 CPU、内存、磁盘和进程，按问题发起诊断，并从对话侧栏回看任务。 |

用输入框的「+」将对话转为任务，也可以在侧栏新建。打开历史任务会显示目标、过程、审批和结果。文件写入、运行测试和服务操作按提案审批。

点击窗口左上角的 Noosphere 图标可以打开带三维动效的功能介绍。

## 模型与数据

本地模型可使用已安装的 Ollama、LM Studio 或受支持的便携 llama.cpp 环境。安装包包含应用和内置服务，模型权重通过相应引擎获取。

每条消息可添加 3 份附件，每份最多 8 MB。扫描 PDF 或图片可先转换为文字，再添加到对话。使用云端模型时，供模型使用的对话和上下文会发送至所选 API。

实际数据目录可在「设置 → 应用信息」中查看。「会话备份」支持导入和导出，「软件更新」会在下载完成后校验安装包。支持的协议、测评任务集、数据目录和第三方资源见 [使用说明](LIMITATIONS.md)。

## 从源码构建

开发环境为 Windows x64，使用 PowerShell 7、Git、Node.js、CPython、Go、Rust 和 Visual Studio 的「使用 C++ 的桌面开发」工作负载。工具版本由项目中的锁文件指定。

```powershell
pwsh -NoProfile -File scripts/setup.ps1 -InstallBrowser
pwsh -NoProfile -File scripts/dev.ps1
pwsh -NoProfile -File scripts/build.ps1
pwsh -NoProfile -File scripts/package.ps1
```

`build.ps1` 将桌面程序构建到 `release/`。`package.ps1` 将 NSIS 安装器、便携 ZIP、构建信息和 SHA-256 校验文件生成到 `artifacts/<版本>/`。

前端由 [React / TypeScript](frontend/src) 实现，原生窗口与本地服务由 [Tauri / Rust](desktop/src-tauri/src) 管理，[Go 后端](backend/cmd/server) 负责模型调用、工作区和测评，[Python CodeLens](backend/python/codelens) 负责源码解析。

[CI](https://github.com/2722039978-source/Noosphere/actions) · [MIT License](LICENSE) · [第三方资源](LIMITATIONS.md#设计来源与第三方资源)
