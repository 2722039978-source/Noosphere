# Noosphere Windows 桌面 · RhineLabUI 适配

日期：2026-10-06。

## 界面与来源

参考用户提供的 [LBEILC/RhineLabUI](https://github.com/LBEILC/RhineLabUI) 本地源码 `RhineLabUI/RhineLabUI`。三维阵列、档案盒模型、材质、运动和灯光以其实现为基础，适配在 `frontend/src/features/home/scene`。字体及附带声明保留在 `frontend/public/fonts`。原参考的资源与内容说明见其 README；本说明仅记录来源，不变更原资源的权利或许可。

产品保留 Noosphere 品牌和真实模块：AI 对话、CodeLens、Nebula、DevOps、Gateway。档案阵列中的重复盒体是导航视觉元素；没有复制演示研究记录或模拟身份验证。新的桌面图标由原有核心与轨道意象延伸，SVG 源为 `frontend/public/noosphere-icon.svg`，由 Tauri icon 命令生成 Windows ICO 和各尺寸 PNG。

默认进入三维档案；业务页面采用统一暖灰底色、细线分区、档案编号及模块强调色。AI 会话的已有持久化和流式请求生命周期保留。设置可切换默认入口、画质、启动过渡；WebGL 不可用或系统减少动态效果时提供静态入口。

## 性能与修复

- 三维场景独立按需加载，业务页面不创建场景。静止后停止绘制，后台窗口停止动画，卸载时释放监听、渲染器、纹理和几何体。
- 自动模式根据渲染 CPU 耗时降级；不能保证识别全部 GPU 瓶颈，另有手动节能和二维模式。精细模式像素比上限 1.5，节能模式上限 1。
- 修复空会话初次打开时向下滚动、启动过渡期间底层控件抢焦点、场景被透明布局拦截点击，以及窄视口档案容器不能实际滚动。
- 模型表单提供明确标签、提交中禁用和错误保留；网关保存失败回滚内存配置、原子替换磁盘配置、不回传密钥；指定不存在或禁用模型时返回错误。
- 桌面实测发现默认系统 Python 缺少 CodeLens 依赖，增加相对于资源目录的本地虚拟环境查找，明确配置仍保持最高优先级。

## 验证入口

- `frontend`: `npm test`、`npm run build`。
- 在本地开发服务上执行 `tests/ui-regression.cjs`、`tests/rhine-ui.cjs`、`tests/backend-startup-feedback.cjs`，使用隔离存储和模拟 IPC。覆盖业务流式对话、页面布局、档案交互、WebGL 失败、偏好保存、窄屏实际滚动、配置保存失败与后端恢复反馈。
- `tests/production-smoke.cjs` 验证构建资产在桌面 CSP 下加载，及场景卸载、按需加载。
- `backend`: `go test ./...`，包含网关配置与 CodeLens Python 查找回归。
- Windows 成品由 `scripts/build.ps1` 生成；独立启动检查验证真实 Go / Python 服务与进程退出清理，原用户配置不作为测试夹具修改。

开发工具可能提示 Three.js 懒加载包超过 500 KB；场景已按需隔离。真实第三方模型调用需要用户配置模型，本次没有用真实 API 密钥发起付费请求。便携版与新 NSIS 安装包是不同产物，是否生成安装包以 `release/build-info.json` 的 `installers` 为准。

## 2026-10-07 清晰度、动效与性能修订

用户实测反馈：三维与界面层级不够清楚，业务和详情页面的动效不足，操作卡顿。本轮保留 Windows 桌面产品和档案美术方向。

- 三维改为直接抗锯齿渲染，移除全场景 SSAO/景深合成；背景档案采用磨砂表面近似，抽出的档案保留透射材质。原生像素比按像素预算约束，节能模式不再固定压至 1 倍。
- 模型拾取改用档案包围盒，避免鼠标移动逐三角形求交；悬停渲染窗口缩短为 500 ms，拖动/切换保持完整过渡。自动画质采样实际帧间隔，包含 GPU 阻塞带来的延迟。
- 首页前景内容有独立浅色底层，功能和说明不再直接混在三维纹理里；业务页恢复卡片边界与层级，辅助文字和控件字号提高。
- 所有工作区保留小面积 CSS 轨道动效，后台暂停、系统减少动态效果时停止；页面、内容分区、表单和详情按操作播放过渡。三维休眠不影响这些轻量动效。
- CodeLens 增加编号工作流程、可展开代码片段和转入文件分析的操作；搜索失败与无结果区分。Gateway 提供默认模型摘要、模型表单展开反馈；Nebula 元数据和 DevOps 工具轨迹采用统一详情层级。
- 同机 Edge / Intel Arc A580、1280×800、精细模式档案切换测量：原版约106次绘制、1075万三角形/帧；修订后约48次、535万/帧。两轮 p95 帧间隔均约17–18ms，不能将渲染工作量减半等同于帧率翻倍；不同显卡与桌面负载仍需实际体验。
- 验证：archive-profile.cjs、render-budget.test.mjs、refinement-ui.cjs，以及既有 UI、档案、生产 CSP 和启动反馈回归。开发 UI 测试使用模拟 IPC，不发送真实模型请求。Browser skill 未提供，采用仓库已有 Playwright 测试入口；最终仍交付重新编译的 Windows 程序。

2026-10-07 02:59 CST：新 Windows EXE 发布完成，校验当前 ArchiveCanvas-BlzcjPad.js 与 index-CMp-5ndK.css 已内嵌。隔离启动检查确认 Go + CodeLens 就绪、鉴权检查与退出清理通过。桌面快捷方式已刷新。只读最终审查无确认的重要回归。成品 SHA256：9C0FBF9B6BDCA3BB598F3F5432111CFCA596291F0895DF35AC6C7896F9C83A64。

## 2026-10-08：冷启动、档案交接与 DeepSeek 接入

- 冷启动按第一次打开场景取样，原实现存在约 1.2 秒的单次主线程阻塞。Three r183 会把 PCFSoftShadowMap 替换为 PCFShadowMap，使旧预编译变体失效；现直接使用支持的阴影模式，同时预编译主缓冲与线性透射缓冲变体。材质插值保持 shader feature 开关稳定。场景完成准备后才显示，轻量开场与 WebGL 初始化错开。
- 远处阵列以原模型各表面的边界生成低多边形几何体，抽出的档案保留原始模型。相同 1280×800 场景，稳态单帧处理量由约 530 万三角形降至约 11 万。首次驱动/环境准备仍有耗时，这不是“瞬时启动”的承诺。
- 首页进入模块先展开选中档案，场景横向退出，档案封面铺开后交给对应工作区；目标模块同时预加载。返回首页保留最后选择。减少动态效果时直接导航；离开后停止渲染、释放资源。正在 compileAsync 的场景先移除界面，待编译结束再释放材质，避免 Three 内部轮询异常。
- Gateway 参考 CC Switch 的供应商预设、协议/地址/模型组合与默认切换方式，提供 DeepSeek 官方预设、编辑、连接测试和默认模型切换。当前官方目录为 deepseek-flash / deepseek-v4-pro，地址 https://api.deepseek.com。使用现有 Chat Completions 网关，不修改其他应用的配置。
- DeepSeek 专用 provider 显式请求非思考模式，输出正文。凭证只经原生 IPC 交给本机后端，保存使用 AES-GCM 和 Windows Credential Manager 中的主密钥；加密不可用时拒绝保存。编辑留空保留原密钥，默认切换保留生成参数；provider 缓存隔离配置 ID 和凭证版本。上游错误在写入 UI/日志前脱敏，包含流式错误与读取边界。
- 本轮增加 provider 切换、密钥保留、生成参数保留、DeepSeek 请求/流式格式、错误脱敏回归，以及页面交接/预设/连接反馈/编译中卸载的浏览器回归。测试使用合成凭证；真实连接验证只发送短测试内容。

参考：[CC Switch 官方接入说明](https://github.com/farion1231/cc-switch/blob/main/docs/guides/codex-deepseek-routing-guide-en.md)、[DeepSeek Chat Completions](https://api-docs.deepseek.com/zh-cn/api/create-chat-completion/)、[DeepSeek 思考模式](https://api-docs.deepseek.com/zh-cn/guides/thinking_mode/)。
