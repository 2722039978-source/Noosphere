# Noosphere

**在 Windows 桌面上管理项目对话、分析代码，并比较本地模型的表现。**

[![CI](https://github.com/2722039978-source/Noosphere/actions/workflows/ci.yml/badge.svg)](https://github.com/2722039978-source/Noosphere/actions/workflows/ci.yml)

打开最近使用的项目与会话，让 Agent 通过 CodeLens 查找源码依据，使用 REA 查看可执行文件信息。在 Model Hub 接入模型，在 Model Lab 保存版本、运行 Go 任务测评，并从报告查看结果、失败案例与性能数据。点击侧栏顶部的 Noosphere 标识可以进入三维动效主页。

## 截图

Model Hub：模型连接、版本与状态。截图使用独立的示例数据。

![Model Hub](docs/screenshots/model-hub.png)

<details>
<summary>查看 Model Lab</summary>

![Model Lab：模型版本、任务集与推理配置](docs/screenshots/model-lab.png)

</details>

## 下载

- [Windows x64 便携版 v0.5.0（2026-11-03）](https://github.com/2722039978-source/Noosphere/releases/tag/v0.5.0-20261103)：下载页面提供 ZIP 和 SHA-256 校验文件。
- [全部发布版本](https://github.com/2722039978-source/Noosphere/releases)。最新源码可按下方步骤构建。

解压完整便携包后运行其中的 `Noosphere.exe`，保留同目录资源。使用 CodeLens 前安装 Python 3.10+，运行包内的 `setup.bat` 安装依赖。

## 快速开始

从源码构建需要 Windows、PowerShell 7、Node.js 24 与 npm、Go 1.26.5（见 [go.mod](backend/go.mod)）、Rust 1.88+ MSVC 工具链、Microsoft C++ Build Tools 的“使用 C++ 的桌面开发”组件与 Windows SDK，以及 Python 3.10+、WebView2 Runtime。

```powershell
git clone https://github.com/2722039978-source/Noosphere.git
cd Noosphere
npm --prefix frontend ci
npm --prefix desktop ci
python -m venv .local/venv
.local/venv/Scripts/python.exe -m pip install -r backend/python/codelens/requirements.txt
pwsh -NoProfile -ExecutionPolicy Bypass -File scripts/preflight.ps1 -RequireDesktop
pwsh -NoProfile -ExecutionPolicy Bypass -File scripts/build.ps1
```

构建完成后双击 `Start-Noosphere.cmd`。从侧栏“模型”添加并测试模型连接，打开项目后即可开始对话。双击 `Create-Shortcuts.cmd` 可以创建桌面快捷方式；开发模式使用 `scripts/dev.ps1`。

公开核心测试检查 evalrunner 校验、Gateway 模型选择、模型版本比较与报告聚合：

```powershell
pwsh -NoProfile -ExecutionPolicy Bypass -File scripts/check-core.ps1
```

## 链接

- [当前边界、数据目录与第三方资源](LIMITATIONS.md)
- [问题反馈](https://github.com/2722039978-source/Noosphere/issues) · [Pull Requests](https://github.com/2722039978-source/Noosphere/pulls) · [CI 检查](https://github.com/2722039978-source/Noosphere/actions)
- [MIT License](LICENSE) · [设计来源与致谢](LIMITATIONS.md#设计来源与第三方资源)
- [RhineLabUI](https://github.com/LBEILC/RhineLabUI) · [REA](https://github.com/morluto/rea)
