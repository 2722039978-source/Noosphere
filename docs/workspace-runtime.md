# 工作区与任务：第一阶段

本阶段把 AI 对话、CodeLens 索引和诊断 Agent 的执行记录统一交给后端管理。Windows 桌面入口、RhineLabUI 配色和主页往返动效沿用当前版本。

## 使用方法

1. 从桌面 `Noosphere` 快捷方式或 `release/Noosphere.exe` 启动。
2. 点击顶部显示工作区名称的按钮，打开“工作区与任务”。可创建工作区、设置项目目录和默认模型。
3. 在 CodeLens 中建立索引。后端保存项目绑定，更换目录后需要重新索引。
4. 在 AI 对话中提问；会话按工作区显示，模型选择器的“默认”优先使用工作区设置。
5. 索引和诊断发起后可以切换页面。在“工作区与任务”查看阶段、结果，或取消未完成任务。

未标记工作区的旧会话仍显示在“本地工作区”。首次升级保留 CodeLens 已输入的目录，但需要重新索引才能建立后端工作区与项目的关联。现有 Nebula 数据及会话正文不会被本次升级删除。

导入包含未知工作区编号的会话备份时，会为这些会话恢复独立的“导入工作区”，可从顶部选择。新恢复的工作区项目目录和默认模型留空，需要重新设置，避免错误使用本机其他项目的上下文。

## 执行与保存

- 工作区、任务、事件及完成结果保存在配置的 `app.data_dir/workspace.sqlite`，采用 SQLite WAL。
- 会话正文仍沿用 IndexedDB；本阶段没有把旧会话正文迁移进 SQLite。
- 每个工作区的任务窗口显示最近 100 条记录；只拉取轻量信息，选中任务后再读取结果。结果单条上限 2 MiB。
- 代码索引最多同时执行 1 个，诊断最多 2 个；各自队列最多 32 个。对话有另外 2 个名额。
- 隐藏任务窗口会停止窗口轮询；活动任务使用自适应轮询，事件按游标增量读取。
- 取消会传递到模型连接、诊断操作和索引解析。Python 索引在文件/调用图步骤之间协作退出，取消不会把迟到结果绑定到另一个目录。
- 页面切换不会取消后台索引或诊断。退出应用会结束执行，重启后把未完成记录标为“已中断”，需要手动重新发起。
- 本阶段没有新增自动重试、自动续跑、跨设备同步或自动修改系统的权限。

## 验证入口

- `frontend`: `npm test`、`npm run build`。
- 浏览器回归：`tests/ui-regression.cjs`、`tests/workspace-runtime.cjs`、`tests/archive-return.cjs`、`tests/workspace-handoff.cjs`、`tests/production-smoke.cjs`。使用隔离的桌面 IPC 夹具，不调用真实模型。
- `backend`: `go test ./...`、`go vet ./...`。
- `backend/python/codelens`: `python -m unittest discover -v`。
- `desktop/src-tauri`: `cargo test --lib --locked`。
- `scripts/test-workspace-release.ps1`: 启动发布包的后端和 Python 服务，以临时目录验证真实索引、项目绑定、任务记录以及重启后的结果读取。
- `scripts/build.ps1` 生成 Windows 发布包；`scripts/create-shortcuts.ps1 -Desktop` 更新快捷方式。发布哈希记录在 `release/build-info.json`。
