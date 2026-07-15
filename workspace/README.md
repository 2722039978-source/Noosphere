# 🗂 Noosphere Workspace

> **把项目文件夹拖进来，Noosphere 自动识别、索引、分析。**
>
> 这是 Noosphere 的"空白画布"——你只需要把想分析的项目放到 `projects/` 下，
> 配置好你的 LLM API Key，剩下的交给系统。

---

## 快速开始

### 1. 配置 LLM

```bash
cp llm_config.example.yaml llm_config.yaml
# 编辑 llm_config.yaml，填入你的 API Key
```

支持的 LLM 提供商：
| 提供商 | type | 获取 API Key |
|--------|------|-------------|
| DeepSeek | `deepseek` | https://platform.deepseek.com/api_keys |
| OpenAI | `openai` | https://platform.openai.com/api-keys |
| Anthropic | `anthropic` | https://console.anthropic.com |
| Ollama（本地） | `openai_compat` | 本地运行 `ollama serve` |
| 其他兼容接口 | `openai_compat` | vLLM / LocalAI / One API 等 |

### 2. 放入项目

```
workspace/
├── llm_config.yaml          ← 你的 LLM 配置
├── projects/                ← 👈 把项目文件夹拖到这里
│   ├── my-go-project/
│   ├── my-python-service/
│   └── my-frontend-app/
└── README.md
```

### 3. 运行分析

启动 Noosphere 后，通过 API 或 Web 控制台触发分析：

```bash
# 扫描 workspace 中的所有项目
curl -X POST http://localhost:8765/api/v1/workspace/scan

# 查看已发现的项目
curl http://localhost:8765/api/v1/workspace/projects

# 分析某个项目
curl -X POST http://localhost:8765/api/v1/workspace/projects/my-go-project/analyze
```

或者打开 Web 控制台 → **Workspace** 面板 → 点击 **扫描与分析**。

---

## 项目识别

Noosphere 会自动识别：
- **编程语言**（30+ 种语言检测）
- **框架**（Go modules / npm / pip / Cargo 等）
- **项目结构**（入口文件、目录组织）
- **代码风格**（命名规范、错误处理模式）
- **依赖关系**（调用链、继承图、导入图）

分析结果会自动存入 Nebula 记忆引擎和 CodeLens 知识图谱。

---

## 验证与诊断

### 检查 LLM 连通性

```bash
curl -X POST http://localhost:8765/api/v1/workspace/validate/llm \
  -H "Content-Type: application/json" \
  -d '{"provider": "deepseek"}'
```

返回示例：
```json
{
  "provider": "deepseek",
  "status": "ok",
  "model": "deepseek-v4-pro",
  "latency_ms": 342,
  "tokens_used": 15,
  "test_response": "Hello! I am DeepSeek V4, ready to help..."
}
```

### 检查工具链对话

```bash
curl -X POST http://localhost:8765/api/v1/workspace/validate/tools \
  -H "Content-Type: application/json" \
  -d '{"service": "all"}'
```

会依次测试三个服务的工具输入/输出是否正确。

---

## 目录约定

| 路径 | 用途 |
|------|------|
| `workspace/projects/` | 待分析的项目（每个子文件夹 = 一个项目） |
| `workspace/llm_config.yaml` | LLM API 配置（已被 .gitignore 排除） |
| `workspace/llm_config.example.yaml` | 配置模板（可提交到 Git） |
