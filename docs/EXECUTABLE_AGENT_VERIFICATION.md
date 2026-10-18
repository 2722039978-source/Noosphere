# 方案三交付验证

验证日期：2026-10-11（Asia/Shanghai）。范围为方案三第一阶段：结构化工具调用的只读项目 Agent。使用说明见 [executable-agent.md](executable-agent.md)。

## 发布产物

- 桌面：`release/Noosphere.exe`，配套后端：`release/bin/noosphere-backend.exe`。
- 构建时间：2026-10-11 16:43:35（Asia/Shanghai），清单 UTC 为 `2026-10-11T08:43:35.7478975Z`。
- 桌面 SHA-256：`479B63943F303FB6CBB93BF06526D467C920EE1FDE5C65BA810B6EAA19BFDCC0`。
- 后端 SHA-256：`A1397D66966DBA6BD05FD19C392999F6A089AB30C8ECFC577CE119C185D1CCEB`。
- 两个文件的哈希均与 `release/build-info.json` 一致；桌面 exe 与 Cargo release 产物一致。
- 项目 `Noosphere.lnk` 和 `C:\Users\sakiko\Desktop\Noosphere.lnk` 均指向该桌面 exe，工作目录为 release，图标来自 exe。
- 5 个发布 Python 资源逐项哈希与源码一致。交付便携发布包，没有生成新的 NSIS 安装器。

## 已执行检查

| 检查 | 结果 |
|---|---|
| Go 全套与新增协议测试 | 通过；`go vet ./...` 通过 |
| 前端单元/运行时测试 | 60 项通过 |
| Python CodeLens | 12 项通过 |
| Rust 桌面端 | 28 项通过 |
| 构建脚本隔离夹具 | 40 项通过 |
| Agent 界面 Playwright | 5 组通过，零应用控制台错误 |
| 原有聊天/工作区/知识/模块切换/返回动画 | 37 组通过 |
| 生产资源、原生 CSP、场景隔离 | 1 组通过 |
| TypeScript、Vite、Cargo release、Go build | 成功 |
| 发布后 Agent 后端实测 | 5 项通过 |
| 发布后真实 Python 索引与知识库 | 10 项通过 |

## Agent 界面回归

`frontend/tests/executable-agent.cjs` 使用隔离 Playwright、真实 React 和模拟 Tauri IPC：

1. 聊天提交持久化 Agent 任务，展示来源文件、行号与哈希。
2. 重开已完成记录不重放任务；主动重新生成保留 Agent 模式。
3. 提交尚未返回时点击停止，等待注册确认后取消准确的后台任务；原聊天消息保留任务链接。
4. 执行中重开页面，能读取随后完成的后台结论，提交次数不增加。
5. DevOps 离页继续运行，任务中心仍可查看失败时已产生的工具证据。

独立审查发现并修复第 3、4 项对应的缺陷。第 4 项先观察到失败（等待已保存结论超时），修复后通过。另有后端回归先发现旧上下文引用未重新验证，现已统一走项目根目录读取并校验哈希。无遗留重要审查项。

## 打包后的 Agent 联调

`scripts/test-agent-release.py` 实际启动发布后端，使用临时项目、独立 SQLite、随机本机端口和本地模拟 DeepSeek 协议服务器，验证：

1. 模型返回工具调用 → 真实读取文件 → 带相同 call ID 回传 → 模型给出引用结论；源码 SHA-256 正确。
2. 工具开始、工具结果和来源聊天 ID 持久保存。
3. 等待模型时取消，迟到的工具调用不执行。
4. 模型响应期间更换工作区项目目录，后续工具不执行。
5. 强制终止后端后重启：已完成证据不变，进行中任务标为中断，无新模型请求。

结果：`C:\Users\sakiko\AppData\Local\Temp\noosphere-agent-release-08h2ri68\result.json`。

原有工作区/索引/记忆发布联调：`scripts/test-workspace-release.ps1`，10 项通过。
结果：`C:\Users\sakiko\AppData\Local\Temp\noosphere-release-workspaces-23eec510f6414cfc93e327ab8f32e76a\result.json`。

## 验证边界

- 未使用真实 DeepSeek 或其他付费 API；验证的是协议、实际后端执行及失败处理，不宣称已验证真实模型在用户项目上的推理质量。
- 界面通过浏览器中的桌面 IPC 夹具测试，发布验证直接运行真实后端/Python；未对最终桌面窗口做人工点击全流程测试。本轮未使用 Computer Use。
- 未扩展文件写入、任意命令、测试运行和服务变更能力。
- 既有三维场景分包仍有 Vite 大于 500 kB 提示；生产测试验证聊天不加载场景。Rust ProcessJob 的未读字段和 Python 测试库弃用提示为非阻断警告。
