# Noosphere 开发指南

## 环境要求

- Node.js 20 或更高版本（前端单元测试使用 Node.js 24）
- Go 1.26.5（遵循 backend/go.mod）
- Rust 1.88 或更高版本，使用 MSVC 工具链
- Python 3.10 或更高版本
- Microsoft C++ Build Tools，包含“使用 C++ 的桌面开发”（Desktop development with C++）工作负载和 Windows 10 或 11 SDK
- Microsoft Edge WebView2 Runtime（Windows 10/11 通常已经具备，精简或受管理的系统需确认）

Windows 依赖安装见 [Tauri 官方前置要求](https://v2.tauri.app/start/prerequisites/#windows)。仅安装 SDK 不会提供完整的 MSVC 编译和链接工具；请在 Build Tools 安装器中选择上述 C++ 工作负载。安装 Rust 时选用 `*-pc-windows-msvc` 默认主机工具链，当前 x64 桌面程序对应 `x86_64-pc-windows-msvc`。安装后重开终端，可用 `rustc -vV` 的 `host` 字段和 `rustup show active-toolchain` 核对；`scripts/preflight.ps1` 检查工具和依赖是否可发现，完整编译及链接仍由构建验证。

## 常用命令

本轮验证使用 Node 24.20.0、npm 11.19.0、Go 1.26.5、Rust 1.98.1、Python 3.14.7。统一使用 npm lockfile；旧 pnpm 文件已保存在本轮源码备份中。CodeLens 直接依赖已固定版本，测试依赖见 `requirements-dev.txt`。

```powershell
npm --prefix frontend ci
npm --prefix desktop ci
python -m venv .local/venv
.local/venv/Scripts/python.exe -m pip install -r backend/python/codelens/requirements-dev.txt
./scripts/preflight.ps1 -RequireDesktop
```

`common.ps1` 优先使用 PATH；缺少 Go/Rust 时可找到 `.local/tools/` 内的本地工具链，不修改系统 PATH 或 Windows 安全策略。Python 虚拟环境不会自动复制进安装包；配置 `modules.codelens.python_path` 为该解释器绝对路径，或者用分发目录的 `setup.bat` 安装运行依赖。

```powershell
# 完整桌面开发
powershell -ExecutionPolicy Bypass -File scripts\dev.ps1

# 前端类型检查和生产构建
cd frontend
npm run build

# Go 测试
cd backend
go test ./...

# 更新开发产物，完全不修改 release/
powershell -ExecutionPolicy Bypass -File scripts\build.ps1 -SkipTauri

# 便携版 / 便携版及 NSIS 安装包
powershell -ExecutionPolicy Bypass -File scripts\build.ps1
powershell -ExecutionPolicy Bypass -File scripts\build.ps1 -BundleNsis

# 使用临时夹具验证脚本，不触碰真实 release 或用户配置
powershell -ExecutionPolicy Bypass -File scripts\test-scripts.ps1
```

## 配置

默认配置模板位于 `backend/configs/config.yaml`，构建时暂存为 `desktop/src-tauri/config.defaults.yaml`。安装包只分发默认模板；实际配置在运行时的数据目录中初始化一次。便携版保留 `release/config.yaml`，安装版默认使用 `%LOCALAPPDATA%\com.noosphere.desktop`。开发模式同样使用独立数据目录，或通过绝对路径的 `NOOSPHERE_DATA_DIR` 指定测试目录。设置页显示当前实际目录。

模型配置以 `backend/models.yaml.example` 为模板，实际 `models.yaml` 可能包含本地密钥，不应提交或复制进安装资源。初始化仅创建空模型配置，不复制开发机模型。参见 [启动修复与验证记录](STARTUP_RELIABILITY.md)。

Go sidecar 要求每次运行的 bearer token，桌面端自动生成和注入；Python worker 使用独立凭证。CORS 不是权限边界，不应把凭证放进浏览器持久化或日志。生产环境继续监听回环地址。

独立后端开发时，在当前进程设置临时凭证（不写入配置文件）：

```powershell
$env:NOOSPHERE_BACKEND_TOKEN=[guid]::NewGuid().ToString('N')+[guid]::NewGuid().ToString('N')
Set-Location backend
go run ./cmd/server -config configs/config.yaml
# 调用者需在 Authorization 中提供 Bearer <同一凭证>。
```

隔离集成测试会启动真实 Go/Python 子进程，使用临时数据目录、随机端口和空模型配置，不调用付费模型：

```powershell
# 在项目根目录设置后进入 backend；go 必须在 PATH 上。
$env:NOOSPHERE_TEST_PYTHON=(Resolve-Path .local/venv/Scripts/python.exe).Path
Set-Location backend
go test -tags integration -v ./tests/integration
```

## 前端约定

- 新页面放在 `frontend/src/features/<feature>`，并在 `app/routes.tsx` 中使用懒加载。
- 全局后端状态通过 `BackendProvider` 获取，不在页面内增加独立健康轮询。
- 跨页面本地状态使用小而明确的 Zustand store；页面临时状态保留在组件内。
- 设计令牌只在 `src/index.css` 中维护，优先复用 `components/ui` 中已有组件。
- 窄屏必须验证全局导航、上下文侧栏、主内容和输入区均无横向溢出。

## 后端约定

- `cmd/server` 只做装配，不放业务规则。
- API 路由在各模块注册，并在 `cmd/server/router.go` 维护总表。
- 所有长时间任务接收 `context.Context`，退出时响应服务器关闭信号。
- 新增浏览器可调用接口时补充 CORS、输入限制、错误结构和测试。
- SSE 路由不能依赖全局固定写超时。
