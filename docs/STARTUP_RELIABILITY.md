# 启动修复与验证记录

日期：2026-09-09。

## 当前交付状态

代码修复和前端验证已完成；新 Go 后端已编译到 `desktop/src-tauri/bin/noosphere-backend.exe`。**新的 Rust 桌面程序和 NSIS 安装包尚未生成，完整桌面进程测试尚未执行。**

`release/Noosphere.exe` 仍是 2026-08-21 的旧文件；本轮没有发布到 release，也没有覆盖其模型、配置、数据、日志。根目录启动器会检查完整构建生成的 `build-info.json`，当前会提示需先构建，避免误开旧版。完整构建成功后才用于日常启动。

## 修复内容

- 桌面资源从 EXE 目录、Tauri 资源目录解析；开发构建额外支持源码暂存目录。后端、配置和 CodeLens 脚本均使用绝对路径。
- 便携版沿用 EXE 旁已有配置；安装版在应用本地数据目录首次初始化配置。支持绝对路径 `NOOSPHERE_DATA_DIR`；已有文件不被初始化覆盖。
- 统一后端生命周期管理，限制重复启动，启动失败和异常退出自动重试。关闭时设置永久停止标志，并由 Windows Job Object 管理自有进程树。
- 单实例插件先于其他插件注册，再次打开时显示已有窗口。
- 启动前检查 8080 是否空闲；健康响应携带每次启动的实例标识。不接管或终止占用端口的其他程序；API 和流式请求在本应用后端就绪前被拒绝。
- 底栏和设置显示启动错误、实际数据目录及共享重试按钮。恢复连接后重新加载模型、会话和指标。缺失资源恢复后可再次请求初始化。
- CodeLens 接收明确脚本路径和配置端口，隐藏 Python 控制台，给健康请求设置超时，识别提前退出，启动失败时终止并回收子进程。模块状态保留实际失败原因。
- 构建从 Cargo 本次 JSON 输出定位真实产物，支持自定义 `CARGO_TARGET_DIR`；不把旧产物当成本次成功结果。`-SkipTauri` 完全不修改 release。只有完整请求成功后才发布；旧 EXE、后端及安装包归档，用户配置和数据保留；写入含时间和实际 EXE SHA256 的构建清单供核对。
- Tauri 资源清单包含后端、Python 服务、默认配置和依赖安装脚本；不包含本机模型配置。项目内安装 Tauri CLI，NSIS 构建使用锁文件。

## 已执行验证

| 验证 | 结果 |
| --- | --- |
| PowerShell 隔离脚本测试 | 40 项通过：任意 CWD、路径含空格、自定义 target、失败不发布、保留用户数据、空模型初始化、旧产物归档及清单指向真实 EXE 等 |
| Go `go test ./...` | 全部通过；CodeLens 真实辅助子进程测试覆盖健康启动、配置端口、提前退出回收、重复停止和端口冲突 |
| 新后端编译 | 通过，开发暂存产物已更新 |
| 前端单元测试 | 33 项通过 |
| 前端 TypeScript 和生产构建 | 通过；Three.js 档案场景仍独立按需加载，存在大 chunk 提示 |
| Edge / Playwright 交互回归 | 12 组通过，覆盖流式、取消、跨路由、持久化、七路由、窄屏及深色主题 |
| 启动反馈 UI 隔离回归 | 7 组通过，覆盖旧状态兼容、原生错误、数据路径、共享重试、自动恢复、390px 和失败后重试 |
| 生产资产及 CSP 检查 | 通过，GLB 加载、档案导航及聊天页不加载 3D 场景 |
| 新 Rust 生命周期和路径测试 | 已添加 10 项，尚未运行；完整 Rust 编译被系统策略拦截 |
| 新版完整桌面和安装包实测 | 未执行，不能声称通过 |

浏览器验证使用独立存储和模拟 Tauri IPC，不读取真实模型配置，也不调用真实模型服务。浏览器插件没有可用控制入口，使用本地 Playwright 和 Edge 验证。

## 当前阻塞及证据

Windows `Microsoft-Windows-CodeIntegrity/Operational` 日志中出现事件 **3077**：

- 20:56:30，Rust `serde_core` 构建辅助 EXE 被拒绝执行，系统错误码 **4551**。
- 21:00:10 及随后复核，`zerovec_derive-990559629fa7acbd.dll` 被拒绝由 rustc 加载，Cargo 表面报 `E0463: can't find crate for zerovec_derive`。
- 策略 ID：`{0283ac0f-fff1-49ae-ada1-8a933130cad6}`。操作系统说明文件不符合签名要求或违反代码完整性策略。

工具自动审批另外拒绝了一条新后端的隔离进程测试命令，理由仅为 `blocked by policy`；该命令没有执行。这与上面的 Windows 代码完整性拦截是两项不同限制。

没有修改系统安全策略、关闭防护或为绕过拦截而更改二进制。依赖源码缓存不完整的问题已通过恢复原始 crate 包修复；当前剩余错误有明确系统策略证据，不应继续当作缺包处理。

## 后续完成条件

由用户或系统管理员允许可信的本地构建产物，并在允许执行进程测试的环境中继续：

1. 运行 Rust 单元测试，修复任何实际编译或测试错误。
2. 执行 `scripts/build.ps1 -BundleNsis`，验证本次主程序及安装包真实生成，确认旧文件归档和用户数据保留。
3. 在合成配置、空模型和独立 WebView 存储中，实际验证新版 EXE 从无关目录启动、重复打开仅一个实例、关闭无后端及 Python 残留、8080 冲突和资源恢复后的重试。
4. 验证安装布局的资源齐全、首次配置写入可写目录，再更新本记录的交付状态。

## 常用命令

```powershell
npm --prefix frontend ci
npm --prefix desktop ci
powershell -ExecutionPolicy Bypass -File scripts\test-scripts.ps1
cargo test --manifest-path desktop/src-tauri/Cargo.toml --locked --lib
powershell -ExecutionPolicy Bypass -File scripts\build.ps1 -BundleNsis
```

前端测试：在 `frontend` 中执行 `npm test`、`npm run build`；启动 Vite 后执行 `npm run test:ui`、`npm run test:startup`。生产资源验证执行 `npm run test:production`。浏览器测试需要 Playwright，可用 `PLAYWRIGHT_MODULE` 指向已安装模块，`QA_OUTPUT` 指定截图输出目录。
