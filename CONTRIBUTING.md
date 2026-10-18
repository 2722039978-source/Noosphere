# 参与 Noosphere Desktop 开发

Noosphere Desktop 是 Windows 优先的本地 AI 桌面工作台。修改前先阅读 [README](README.md)、[开发指南](docs/DEVELOPMENT.md) 和[全栈架构](docs/ARCHITECTURE.md)，确认改动所属模块、数据目录与验证方式。

## 获取源码与准备环境

有仓库写权限时可直接创建分支；通过 Fork 贡献时，将下面的地址换成自己的 Fork，并向原仓库提交 PR。

```powershell
git clone https://github.com/2722039978-source/Noosphere.git
Set-Location Noosphere
git switch -c fix/describe-the-change
```

完整桌面开发需要 Node.js 20+、Go 1.26.5、Rust 1.88+（MSVC 工具链）、Python 3.10+、Microsoft C++ Build Tools（“使用 C++ 的桌面开发”及 Windows 10/11 SDK）和 WebView2 Runtime。前端自动测试使用 Node.js 24，建议使用相同版本。安装步骤、版本依据、工具链查找规则与独立后端的临时凭证用法见[开发指南](docs/DEVELOPMENT.md)。

在仓库根目录安装锁定的依赖并执行预检：

```powershell
npm --prefix frontend ci
npm --prefix desktop ci
python -m venv .local/venv
.local/venv/Scripts/python.exe -m pip install -r backend/python/codelens/requirements-dev.txt
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/preflight.ps1 -RequireDesktop
```

源码克隆不包含 `release/`、本机模型配置或生成的快捷方式。使用 `scripts/dev.ps1` 启动桌面开发模式；需要便携程序时运行 `scripts/build.ps1`。完整命令见 [README 的开发与构建说明](README.md#开发)。预检只检查可发现的工具与依赖，完整编译和运行需要另外验证。

## 修改范围

- 前端页面放在 `frontend/src/features/`，复用现有路由、`BackendProvider`、状态管理和 UI 组件。界面改动验证桌面与窄屏布局，保留流式输出、取消和会话持久化行为。
- Go 业务放在 `backend/internal/` 对应模块；`backend/cmd/server` 负责装配。接口变更同步更新 [API 参考](docs/API.md)，长任务保留取消、关闭和错误处理。
- CodeLens 的 Python 服务位于 `backend/python/codelens/`，Go 适配器位于 `backend/internal/codelens/`。修改接口时核对两端协议。
- 桌面壳位于 `desktop/src-tauri/`；启动、构建与资源暂存脚本位于 `scripts/`。修改这些位置时说明对数据目录、子进程管理和打包的影响。
- 项目检索和 Agent 改动应核对[工作区隔离](docs/workspace-runtime.md)、[只读调用边界](docs/executable-agent.md)及[逐项审批与恢复规则](docs/agent-reviewed-actions.md)。

一个 PR 聚焦一个可说明、可验证的问题。避免在功能修复中混入无关格式化、重构或依赖升级；需要变更锁文件时说明原因，使用对应包管理器更新。

## 提交前验证

下面的命令从仓库根目录开始执行，对应当前 CI 的基础检查：

```powershell
# 前端单元测试、类型检查和生产构建
npm --prefix frontend test
npm --prefix frontend run build

# Go 单元测试与静态检查
Push-Location backend
go test -count=1 ./...
go vet ./...
Pop-Location

# Python CodeLens 单元测试
.local/venv/Scripts/python.exe -m unittest discover -s backend/python/codelens -p 'test_*.py' -v

# Windows 启动/构建脚本的隔离夹具
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/test-scripts.ps1

# 提交内容与空白检查
git diff --check
git status --short
git diff
git diff --cached
```

按实际影响补充验证：Rust 改动在 `desktop/src-tauri/` 执行 `cargo test --lib --locked`；桌面或脚本行为改动验证完整 Windows 构建；界面改动附截图与交互检查结果；模型协议改动说明使用的模拟服务或经明确授权的真实模型验证。更多隔离集成测试与浏览器回归入口见[开发指南](docs/DEVELOPMENT.md)及[工作区验证说明](docs/workspace-runtime.md#验证入口)。

在 PR 中写清实际执行的命令、结果和未执行的项目。当前 CI 范围见 [GitHub 协作与 CI](docs/github-workflow.md)；单元测试通过不能代替桌面打包、运行或真实模型验证。

## 保护本机配置与数据

提交前检查实际 diff 和暂存区。不要提交 API Key、模型凭证、后端 bearer token、`.env`、实际 `models.yaml`、本机配置、会话导出、日志、数据库或用户项目内容。配置示例使用 `backend/models.yaml.example`，默认模板使用 `backend/configs/config.yaml`；示例中的值应为无效占位值。

`release/`、`.local/`、生成的 Tauri 资源和快捷方式由 [.gitignore](.gitignore) 排除。不要为了提交构建结果而强制加入这些目录。测试使用临时项目、空模型配置和独立数据目录；需要隔离桌面运行时，可把 `NOOSPHERE_DATA_DIR` 设置为测试目录的绝对路径。

Issue、PR 截图和日志同样需要去除密钥、个人路径、会话内容及其他私人信息。Agent 操作审批和 Windows 服务测试只应在可信、可恢复的测试环境中进行，真实模型请求按所选 Provider 计费。

## Issue 与 Pull Request 流程

1. 先在 [Issues](https://github.com/2722039978-source/Noosphere/issues) 和 [PR 列表](https://github.com/2722039978-source/Noosphere/pulls) 检查是否已有相同问题。问题反馈填写复现步骤、预期和实际行为、环境及脱敏证据；需求说明具体使用场景。
2. 在独立分支完成修改与相关验证，同时更新受影响的文档。
3. 提交到自己的分支或 Fork 后，向原仓库的 `main` 发起 PR，使用仓库的 PR 模板说明问题、最终行为、影响范围、验证结果与限制；有相关 Issue 时附链接。
4. 在 [Actions](https://github.com/2722039978-source/Noosphere/actions) 或 PR 的 Checks 查看当前提交结果，处理失败项并回应评审反馈。补充提交后重新核对相应检查，不能用旧提交的结果代表新代码。

GitHub 各入口、检查范围及本地打包与公开发布的区别见 [GitHub 协作说明](docs/github-workflow.md)。
