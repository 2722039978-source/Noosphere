# GitHub 协作与 CI

仓库入口：[2722039978-source/Noosphere](https://github.com/2722039978-source/Noosphere)。本页说明项目介绍、问题反馈、代码评审和自动检查的分工；本机开发命令见[开发指南](DEVELOPMENT.md)，贡献流程见[贡献指南](../CONTRIBUTING.md)。

## 各入口的用途

| 入口 | 用途 | 维护内容 |
|---|---|---|
| [README](../README.md) | 首次了解项目、查看界面示例、找到源码构建和使用文档 | 保持模块、依赖、启动方式与源码一致，区分历史验证记录和当前状态 |
| [仓库首页的 About](https://github.com/2722039978-source/Noosphere) | 在仓库列表和搜索中快速识别项目 | 使用简短项目介绍、相关主题和项目入口；详细能力与限制以 README 为准 |
| [Issues](https://github.com/2722039978-source/Noosphere/issues) | 提交缺陷、需求和使用问题 | 使用对应表单填写场景、复现、环境和脱敏证据，关联重复或相关问题 |
| [Pull Requests](https://github.com/2722039978-source/Noosphere/pulls) | 评审具体改动与验证结果 | 使用 PR 模板描述问题、最终行为、影响范围、测试和限制，关联 Issue |
| [Actions](https://github.com/2722039978-source/Noosphere/actions) | 查看工作流、当前提交的日志和检查结果 | 以对应 commit 的运行结果为准，失败时查看具体作业和步骤 |

README 的 CI badge 链接到 [CI 工作流](https://github.com/2722039978-source/Noosphere/actions/workflows/ci.yml)，用于查看其报告的状态。仓库存在工作流文件不等于已在 GitHub 成功运行，历史本机验证记录也不等于当前提交的 CI 结果。

## 当前 CI 覆盖范围

工作流定义位于 [`.github/workflows/ci.yml`](../.github/workflows/ci.yml)。触发方式为向 `main` 推送、提交或更新 PR，以及在 Actions 中手动运行。工作流的仓库权限为 `contents: read`，并对同一 PR 或 ref 取消被新运行替代的旧运行。

| 作业 | 环境 | 实际检查 |
|---|---|---|
| `frontend` / Frontend tests and build | Ubuntu，Node.js 24 | `npm ci`、`npm test`、`npm run build`；测试当前前端单元用例，并执行 TypeScript 检查和 Vite 生产构建 |
| `backend` / Go tests and vet (Windows) | Windows，Go 版本读取 `backend/go.mod` | `go test -count=1 ./...`、`go vet ./...` |
| `codelens` / Python CodeLens tests | Ubuntu，Python 3.14 | 安装 `requirements-dev.txt` 中的固定依赖，执行 `python -m unittest discover -s backend/python/codelens -p 'test_*.py' -v` |
| `scripts` / PowerShell script fixtures (Windows) | Windows，PowerShell | 执行 `scripts/test-scripts.ps1`，在临时目录中使用模拟编译器验证构建和开发脚本行为 |

当前前端 `npm test` 包含 60 个单元测试用例，覆盖流式取消、会话存储与搜索、工作区归属、代码上下文、任务观察和导航等现有逻辑；数量随源码变动，以对应提交的测试输出为准。`npm run build` 验证可生成前端生产资源。

这些检查使用合成夹具和本地模拟服务，不需要配置模型密钥。Windows 脚本夹具检查资源组装、已有数据保留及失败处理等脚本逻辑，模拟编译器生成的文件不构成真实桌面构建结果。

## 需要另行验证的部分

- **Rust 和 Tauri**：当前工作流没有运行 `cargo test`、编译桌面壳或启动 Tauri。相关改动应在 Windows 本机补充 `cargo test --lib --locked` 及所需运行验证。
- **完整 Windows 打包**：当前工作流没有执行 `scripts/build.ps1` 或 NSIS 打包，也没有生成、签名或上传可分发的桌面程序。需要在本机运行完整构建，核对 `release/build-info.json`、资源、启动及数据目录行为。
- **界面交互**：当前工作流没有运行 `frontend/tests/` 中的浏览器回归脚本。响应式布局、桌面 IPC、窗口行为、后台进程生命周期与升级路径需要按改动补充检查。
- **真实模型与系统操作**：当前工作流不使用真实模型账户、不验证外部 Provider 的实时行为，也不验证用户机器上的服务启停或真实项目写入。人工验证应记录环境、模型/Provider、执行证据和仍未覆盖的部分，并遵守逐项审批边界。
- **带标记的集成测试**：当前 Go 作业执行默认测试集，不包含需要 `-tags integration` 的 Go/Python 子进程集成测试；相关入口见[开发指南](DEVELOPMENT.md)。

PR 中应列出本次实际执行的检查；未覆盖项直接说明，不把前端构建或脚本夹具通过等同于完整桌面可用。现有专项验证记录可帮助定位测试入口，但其中的日期、机器和产物只代表当时记录的验证。

## 本机构建与公开发布

`scripts/build.ps1` 将完整构建产物组装到本机 `release/`；增加 `-BundleNsis` 可生成本机 NSIS 安装包。该目录包含运行配置及可能产生的本地数据，默认不进入 Git。生成安装包不会自动创建 GitHub Release。

公开发布需要另外核对版本、实际构建产物及校验值，并在 [GitHub Releases](https://github.com/2722039978-source/Noosphere/releases) 明确发布对应文件。当前 CI 没有 Release 发布作业；本页不将本地生成的安装包视为已经公开可下载的版本。

## 协作时保护本机信息

Issue、PR、Actions 日志和公开附件中不要包含实际模型配置、API Key、bearer token、私人会话或用户项目内容。提交示例使用无效占位值，复现使用临时目录和模拟服务。提交前检查暂存区；具体目录和验证要求见[贡献指南](../CONTRIBUTING.md#保护本机配置与数据)。
