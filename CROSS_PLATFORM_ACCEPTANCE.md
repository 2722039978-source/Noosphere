# 桌面发行验收

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
