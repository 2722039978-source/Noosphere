# Noosphere 加固交付记录

实施日期：2026-09-27 至 2026-09-28。实际工作目录为 `E:\project\Noosphere（agent项目）\new`；旧的 `E:\project\Noosphere\new` 不存在。

## 本轮范围

保留 React / Tauri / Go / Python 架构，补齐核心链路与持久化、安全、测试，不把项目描述为全语言理解或自动修复平台。

- Nebula：共享 Unicode/CJK 分词；索引原文与业务记录一起持久化；删除同步更新向量与关键词索引；恢复会话；修复搜索结果 JSON 与 Rust 的 `key` 契约。向量缓存可重建，模型与图一起切换；指定项目时只对该命名空间评分。
- CodeLens：安装缺失的五种 Tree-sitter grammar；修正 UTF-8 字节偏移；以真实包含关系定位调用者，使用限定符区分同名符号；增量复用未变文件。60 行源码分块附相对路径和行号，通过 Go 代理原子替换项目记录，删除的文件不再检索。
- 对话 RAG：当前选中项目独立持久化；不检索其他项目；上下文有来源、截断提示及字符预算。聊天开关改称“规划辅助”，实际只读工具执行放在 DevOps 诊断面板。
- DevOps：结构化 `{thought, action, args}`，工具/参数白名单，超时、取消、最多 5 步的 UI 入口，显示执行结果。用模拟模型验证真实工具分派链路，不调用付费模型。
- 本地服务：桌面生成进程级 bearer token；Go 为 Python worker 生成另一枚 token；两个边界均拒绝缺失或错误凭证。分析文件必须位于本次选择的项目根目录内。
- 聊天存储：按会话写 IndexedDB，事务同时提交顺序与活动会话；旧 localStorage 原文保留。增加 JSON 导出、校验后合并导入、失败重试；同 ID 本机数据优先。大于 20 MiB 的合法累计历史可重启恢复及重新导入。
- 工程：修复 npm lockfile，统一 npm；Python 直接依赖固定版本；增加环境预检，本地工具链回退；打包排除 Python 测试源码。

## 已执行验证

| 层级 | 命令 / 行为 | 结果 |
|---|---|---|
| 环境 | `scripts/preflight.ps1 -RequireDesktop` | 通过；不等同编译/运行验证 |
| Go | `go test ./... -count=1` | 全部包通过 |
| Go/Python 集成 | `go test -tags integration -v ./tests/integration` | 真实子进程：鉴权、索引→中文检索→删除后重索引通过 |
| Python | `python -m unittest discover -s backend/python/codelens -p 'test_*.py' -v` | 6 项通过 |
| Rust | `cargo test --offline --locked --manifest-path desktop/src-tauri/Cargo.toml --lib` | 25 项通过 |
| 前端单元 | `npm test` | 42 项通过 |
| TypeScript | `npx tsc --noEmit` | 通过 |
| 生产资产 | `npm run build` | 通过；Three.js 懒加载场景块约 599 KB，保留体积警告 |
| 页面回归 | `npm run test:ui` | 14 项通过，包括备份下载/导入/重启、诊断 trace、配额失败、取消和窄屏 |
| CSP | `npm run test:production` | 生产资源、CSP、场景隔离通过 |
| 启动反馈页面 | `npm run test:startup` | 7 项通过 |
| 打包脚本 | `scripts/test-scripts.ps1` | 40 项隔离夹具检查通过 |

页面测试环境：Playwright + 已安装 Edge，入口 `http://127.0.0.1:1420/`，1440×960、900×600、390px 手机宽度。Browser plugin not available。页面非空、无框架错误遮罩、无相关运行时错误；已检查备份页、诊断结果及手机对话截图。浏览器使用模拟 Tauri IPC，因此不能代替真实桌面桥接或 WebView 验证。

截图与浏览器结果位于 `%TEMP%\noosphere-hardening-qa`；编译日志位于 `.local/qa/hardening`。不上传或打包这些测试数据。

## 独立复核与回归修复

一次只读独立复核曾被额度阻断，恢复后完成。以下问题均补充失败回归，再实现修复：

1. 累计聊天历史超过单次导入限制导致无法启动：拆分状态校验，不序列化整个库验证；取消人为的终身容量/备份回导上限。合法大文件测试、配额失败和并发快照测试通过。
2. 远程向量重建阻塞 30 秒桌面就绪期限：先提供本地可用服务，后台重建；总预算 2 分钟，HTTP 与关闭信号支持取消；完成后才切换图。慢服务启动测试通过。
3. 空会话元数据被送往 embedding 接口：空索引文本不生成向量。拒绝空输入的测试提供者不再阻断模型切换。
4. WAL 撕裂尾部吞掉下一次已确认写入：重放记录字节偏移，修剪不完整且未确认的尾部；完整坏记录和读取错误不再静默跳过。双重恢复及损坏记录拒绝测试通过。
5. SST 发布前丢弃旧持久数据：临时文件写完、flush、sync、关闭与验证后再发布；Windows 使用 write-through 名称移动，其他平台同步目录；然后才处理 WAL / 旧表。注入发布失败时，不留下最终路径的残缺文件。
6. 将复核中的“格式合法但维数矛盾的缓存”提升为启动可靠性问题：丢弃不一致缓存并重建，不允许它阻止权威数据打开。恢复测试通过。

另外修复了关闭后排队的 compaction 仍打开新 SST 文件、导致 Windows 临时目录被占用的问题；复用混合并发读写测试验证。

## 决策与剩余边界

- 此目录没有 Git 元数据，因此不创建仓库、不伪造提交。源码备份在 `.local/backups/hardening-20260927-230754`；被移除的两份 pnpm 文件可以从那里恢复。代价是本轮没有 Git commit 级回滚。
- 使用有界 60 行源码窗口，不宣称 AST 级完整语义切块。单个文件 1 MB、项目 30 MB / 5000 文件；超限报错并保留旧索引。超长窗口内容有截断提示。
- 选中项目路径由经过鉴权的本机调用者指定，文件分析校验规范化后的根目录包含关系；这不是对可信本机进程的操作系统沙箱。
- 指定项目的向量检索采用命名空间内精确评分，优先保证小项目召回；项目规模增大时，CPU 成本高于 ANN 候选过滤。
- 备份必须能恢复自身导出的历史，因此取消固定大小上限；仍先完整校验再合并。极大的 JSON 导入/导出需要内存，尚未做流式备份。
- 延后小项：Python 类方法里的未限定名称解析还不是完整 Python 作用域分析，少数类/模块同名函数可能产生错误边；其他不确定跨文件/动态调用记录为 unresolved。调用图不能当作完备证明。
- 没有调用真实付费模型、没有验证生产 embedding 质量，没有做真实断电测试；同步与原子发布是代码及故障注入验证，不承诺硬件层绝对防丢。
- Python `httpx` 测试适配存在上游弃用提示；当前测试通过。Rust `ProcessJob` 字段保持句柄所有权而未直接读取，存在 dead-code 提示。均非测试失败。

## 发布状态

`scripts/build.ps1 -BundleNsis` 成功完成，2026-09-28 01:05（北京时间）发布：

- `release/Noosphere.exe`：28,620,800 字节；SHA-256 `6AF3F031359835AA7EECF98235763165A10DF41CD3456E448944AD4136546F53`。
- `release/Noosphere Desktop_0.5.0_x64-setup.exe`：25,826,945 字节。
- 后端 SHA-256 `D7AF95B5008E0056A29444583839A837AA074C562D4179C2E5AE716C60FFCA35`。
- 精确构建信息在 `release/build-info.json`。旧程序和安装包由发布脚本留存在 `release/legacy/`；源码保留上述独立备份。

已用新 `release/Noosphere.exe` 做真实进程级 smoke：从无关工作目录启动，`NOOSPHERE_DATA_DIR` 指向新建的合成配置目录，`WEBVIEW2_USER_DATA_FOLDER` 也独立隔离，使用空模型配置和本地 Python venv。Go 与 CodeLens 就绪，未带凭证的 `/health` 返回 401；强制停止桌面进程后，Windows Job 清理了后端和 Python worker，无残留监听。

这不是全新 Windows 机器上的 NSIS 安装/卸载验收，也不是原生 WebView 的逐项点击验收。Computer Use 技能已读取，但本会话没有 `node_repl` 运行时，未绕过该限制做 Windows UI 自动化。便携版进程验证与浏览器页面验证分别报告。
