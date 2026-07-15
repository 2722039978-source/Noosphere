# 贡献指南

感谢你对 Noosphere 的兴趣！本项目由三个可独立运行的服务组成，欢迎任何形式的贡献。

## 开发环境

| 服务 | 语言 | 要求 | 本地启动 |
|------|------|------|---------|
| `codelens/` | Python | 3.10+ | `pip install -r requirements.txt && python -m src.main serve` |
| `nebula/` | Go | 1.26+ | `go run ./cmd/nebula-server --memory --port 8730` |
| `devops/` | Go | 1.26+ | `go run ./cmd/devops-server --port 8740` |

或者直接 `docker compose up -d` 启动全部。

## 提交流程

1. Fork 本仓库并创建特性分支：`git checkout -b feat/your-feature`
2. 提交信息遵循 [Conventional Commits](https://www.conventionalcommits.org/zh-hans/)：
   - `feat(codelens): 支持 Kotlin 解析`
   - `fix(nebula): 修复 HNSW 并发写崩溃`
   - `docs: 补充 API 示例`
3. 确保测试通过：
   ```bash
   # CodeLens
   cd codelens && python -m pytest tests/ -v
   # Nebula / DevOps
   cd nebula && go test ./...
   cd devops && go test ./...
   ```
4. 发起 Pull Request，描述清楚**动机**和**改动范围**。

## 代码规范

- **Go**：`gofmt` 格式化；错误显式返回，不使用 `panic`；导出符号必须有注释。
- **Python**：遵循 PEP 8；公共函数需要 docstring；类型注解优先。
- **安全红线**：任何密钥、Token 严禁出现在代码或配置中——一律走环境变量。CI 会拒绝包含 `sk-` 模式密钥的提交。

## 报告问题

提 Issue 时请附上：

- 复现步骤与期望行为
- 服务日志（`docker compose logs <service>`）
- 环境信息（OS / Docker 版本 / 部署方式）

## 行为准则

保持友善与专业。我们欢迎所有背景的贡献者。
