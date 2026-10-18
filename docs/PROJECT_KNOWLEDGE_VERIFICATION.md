# 方案二交付验证

交付复核日期：2026-10-11（Asia/Shanghai）。功能与使用说明见 [project-knowledge.md](project-knowledge.md)。

## 发布文件

- Windows 桌面：`release/Noosphere.exe`
- 配套后端：`release/bin/noosphere-backend.exe`
- Python 资源：`release/codelens-python/`，发布文件逐项哈希与源码一致。
- 构建时间：2026-10-10 00:05:50（Asia/Shanghai），以 `release/build-info.json` 的 UTC 时间为准。
- 桌面 SHA-256：`4FA4CBF66AE2F0DF7966652E588A7BF1AB0D5919ED231974693426FDCDC42C5A`
- 后端 SHA-256：`7C19CBC183D713E383AC4FD332A0603DCB5ABC52A093528CFC8859E2E55E0DF1`
- 桌面 exe 与 Cargo release 产物哈希一致，两个 exe 的哈希均与构建清单一致。
- 项目内 `Noosphere.lnk` 与用户桌面 `Noosphere.lnk` 均指向 `release/Noosphere.exe`，工作目录为 `release`，图标取自该 exe。
- 此次交付为便携桌面发布包，没有生成新的 NSIS 安装器。

## 验证结果

| 范围 | 结果 |
|---|---|
| Go 全套 | 144 项通过，`go vet ./...` 通过 |
| Python | 12 项通过 |
| 前端单元/运行时 | 58 项通过 |
| Rust 桌面端 | 28 项通过 |
| 知识界面 Playwright | 5 组通过，控制台无应用错误 |
| 原有界面、工作区、返回动画、模块切换、生产 CSP | 33 组通过 |
| 构建脚本隔离夹具 | 40 项通过 |
| 打包后真实后端与 Python | 10 项通过，含重启恢复 |
| 前端生产构建 | TypeScript 与 Vite 成功 |

独立审查发现的 5 项问题均先用失败测试复现，再修复并复验：替代目标 revision 校验、失效文件来源允许停用、所有记忆的项目根目录归属、等待发布锁期间取消、符号切分重复扫描。没有遗留的审查待修项。

## 打包后的 10 项验证

`scripts/test-workspace-release.ps1` 实际启动已发布的后端和 Python 子进程，使用独立临时目录、随机端口、本地哈希向量和空模型列表，未使用真实模型或用户项目数据。

1. 打包后启动及工作区创建。
2. 真实 Python 解析与持久化项目绑定。
3. 任务事件及轻量任务列表。
4. 后端重启后已完成结果保留。
5. 符号索引策略和源码版本。
6. 未修改的重复索引不计算新向量、不重建图。
7. 待确认记忆不注入，代码来源可追溯。
8. 确认记忆后进入上下文。
9. 替代关系及旧 revision 冲突在重启后仍有效。
10. 修改源码后从上下文排除，并给出提示。

最终夹具结果：`C:\Users\sakiko\AppData\Local\Temp\noosphere-release-workspaces-c65ea8266ffc4bc4a3ee8e0ec8eead70\result.json`。

## 测试边界

- 渲染验证使用隔离的 Playwright 与模拟 Tauri IPC；发布验证使用真实后端、Python 和磁盘存储。没有对最终 exe 做人工点击全流程测试。
- 未调用真实 DeepSeek 或其他付费聊天/embedding 服务；不声称已经验证真实模型生成时的引用遵循率。
- 未注入操作系统磁盘故障或强制断电；已测试取消、嵌入失败和进程重启。
- 性能数据来自文档中的小型夹具，不代表大型真实项目端到端响应。
- Vite 仍提示既有三维场景分包超过 500 kB；该场景延迟加载，生产测试验证聊天不加载场景资源。Rust 的 ProcessJob 字段与 Python 测试库弃用提示是非阻断警告。
