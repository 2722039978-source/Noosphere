# 桌面发行验收

## v1.0.1 开放模型库

PR #16 的 Windows x64 作业在等待 WebView2 CDP 时耗尽 45 秒，日志显示后端启动约耗时 54 秒。该结果说明 CDP 截止时间可能早于应用初始化完成。开发分支让 WebView2 分配调试端口，从独立用户资料目录读取端口，并核对本机 WebSocket 地址、监测进程提前退出、记录耗时。启动探测有 120 秒上限，后续打包应用页面和 IPC 断言继续执行。 [Windows 原始作业](https://github.com/2722039978-source/Noosphere/actions/runs/45861263617/job/140052595162)

三个 Unix 作业在本地引擎测试的 CustomLlama 平台支持断言处失败。开放模型库在这三个目标使用系统文件选择器、对应架构的运行时与 Unix 子进程管理。固定官方便携包的目标范围仍为 Windows x64。运行时是否安装、模型是否可加载及是否有可用加速设备需要分别验证。 [Linux x64](https://github.com/2722039978-source/Noosphere/actions/runs/45861263617/job/140052595018) · [macOS x64](https://github.com/2722039978-source/Noosphere/actions/runs/45861263617/job/140052594386) · [macOS ARM64](https://github.com/2722039978-source/Noosphere/actions/runs/45861263617/job/140052594974)

桌面交付矩阵包含 Windows x64、Linux x64、macOS x64 和 macOS ARM64。前端浏览器检查使用模拟 IPC 验证批量导入、运行时授权、精确模型切换、流式对话、附件能力限制及移除记录。真实驱动、量化、内存压力和模型精度需用对应宿主与实际权重验证。下表为已发布 v1.0.0 的历史证据。 [交付工作流](.github/workflows/ci.yml) · [模型兼容说明](MODEL_GUIDE.md)

### Windows 本机实测

在 Intel Arc A580（运行时报告 8098 MiB）上，使用固定版本 b12815 和 SmolLM2-135M-Instruct Q4_K_M 分别测试了 Vulkan 与 SYCL。两次成功运行均报告 31/31 层卸载，返回实际生成内容及流式结束标记，停止后释放监听端口。SYCL 首次冷启动在预热阶段超过测试的 120 秒等待时间，重试成功。CPU 另经桌面原生导入、按需安装、端口归属校验和 Go 网关流式对话验证。SmolVLM-256M Q8_0 搭配单独登记的官方 Q8_0 投影器，通过桌面附件上传 128×128 红色 PNG 后，流式回复为 “Red.”。不同驱动、模型和加速设备仍需分别验证。

## 1.0 实施与验收

`v1.0.0` 的正式发行目标为 Windows x64 和 Linux x64。下表记录实际分发文件的原生验收。 [正式发行](https://github.com/2722039978-source/Noosphere/releases/tag/v1.0.0)

| 目标 | 交付件 | 已取得的证据 |
| --- | --- | --- |
| Windows x64 | NSIS 安装器、完整便携 ZIP | 安装器通过从 v0.9.0 原位升级、卸载与重装。便携包通过原生 WebView2、Unicode 路径和真实 OpenVINO CPU 模型对话。用 v0.9.0 后端创建的数据核对了会话模型绑定、工作区、项目约定、任务、附件和隔离恢复。 [Windows 构建与验收](https://github.com/2722039978-source/Noosphere/actions/runs/45232545703/job/137895851301) · [HTTPS 模型接入](https://github.com/2722039978-source/Noosphere/actions/runs/45245263553) |
| Linux x64 | AppImage、deb | Ubuntu 22.04 上的 deb 通过 v0.9.0 原位升级及 Secret Service 凭证重启读回。两种包都通过 X11 离线启动和仅 Wayland 的 Weston 窗口检查。AppImage 使用 extract-and-run 验收。普通 FUSE 启动依赖宿主图形运行库。 [Linux 构建与验收](https://github.com/2722039978-source/Noosphere/actions/runs/45232545703/job/137895851881) · [HTTPS 模型接入](https://github.com/2722039978-source/Noosphere/actions/runs/45244554470/job/137937772601) |

项目任务的自动验证覆盖模型绑定、审批单次消费、外部编辑冲突、取消和重启恢复。真实模型工具调用与模型连接分别取证。项目测试以当前用户权限运行。 [使用和数据边界](README.md#模型与数据)

## 发行来源

两目标的包均从干净提交 `564ad1f93549b0a05d4cdfa6f19e5bbddfd6d5a0` 构建，保留各目标的原始 `build-info`。正式标签指向其子提交 `8a1ea43d2d3de77e0d0d94f0c0f39a84b5de5a3e`。两提交的源码树均为 `47eed7d21151c407605f617585ca5e73eb7c3ebf`。本页随主分支维护，标签保存对应安装文件的源码快照。 [构建提交](https://github.com/2722039978-source/Noosphere/commit/564ad1f93549b0a05d4cdfa6f19e5bbddfd6d5a0) · [标签提交](https://github.com/2722039978-source/Noosphere/commit/8a1ea43d2d3de77e0d0d94f0c0f39a84b5de5a3e)

发行目录的 `SHA256SUMS.txt` 覆盖四个包与构建清单。 [SHA-256 清单](https://github.com/2722039978-source/Noosphere/releases/download/v1.0.0/SHA256SUMS.txt)

## 构建依赖

Windows 使用 PowerShell 7、Visual Studio C++ 桌面工具链和 WebView2。Ubuntu 22.04 构建需要 GTK 3、`libwebkit2gtk-4.1-dev`、`libappindicator3-dev`、`librsvg2-dev`、`libxdo-dev`、`libfuse2`、`patchelf` 和 `xz-utils`。 [构建命令](README.md#开发和构建)

## 历史验收

早期候选的包范围、运行记录与当时的限制按对应版本保留。 [v1.0.0-rc.2 发行记录](https://github.com/2722039978-source/Noosphere/releases/tag/v1.0.0-rc.2) · [历史验收档案](https://github.com/2722039978-source/Noosphere/blob/4f9380b638873f66732212f83e12ff0d3e3a0f4d/CROSS_PLATFORM_ACCEPTANCE.md) · [v0.9.0 发行记录](https://github.com/2722039978-source/Noosphere/releases/tag/v0.9.0)
