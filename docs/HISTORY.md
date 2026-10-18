# Noosphere 演进记录

## 0.5.0

项目由多个独立 AI 工具收敛为 Windows 优先的本地桌面工作台。Tauri v2 统一桌面入口，Go 后端整合 Gateway、Nebula 和 DevOps，Python worker 保留 CodeLens 的 Tree-sitter 分析能力。

## 2026 年 9 月架构优化

- 保留 `frontend`、`backend`、`desktop` 原有目录边界。
- 将前端后端状态与健康轮询收敛到单例 Provider。
- 按工作区拆分前端路由产物。
- 重构聊天工作台的桌面和窄屏布局。
- 增加本地附件上下文与 Agent 模式交互。
- 限制 Go sidecar 的浏览器来源并启用 Tauri CSP。
- 补齐架构、API、开发和 Word 版架构文档。
