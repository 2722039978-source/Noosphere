"""
LLM & Tool Validator — 模型调用与工具对话验证

提供 LLM API 连通性测试和工具链 I/O 验证功能。

验证流程:
1. LLM 连通性测试 — 向 API 发送测试消息，验证认证和响应格式
2. 工具链验证 — 向 Nebula/DevOps 发送测试请求，验证工具输入/输出
3. 端到端测试 — 联合验证：索引样本项目 → 问答 → 检查响应
"""

import json
import time
import urllib.request
import urllib.error
from typing import Optional, Dict, Any, List, Tuple
from dataclasses import dataclass, field
from datetime import datetime

from loguru import logger


# ============================================================
# 数据模型
# ============================================================

@dataclass
class ValidationResult:
    """单次验证结果"""
    test_name: str
    passed: bool
    latency_ms: float = 0.0
    details: Dict[str, Any] = field(default_factory=dict)
    error: Optional[str] = None
    suggestions: List[str] = field(default_factory=list)

    def to_dict(self) -> Dict[str, Any]:
        return {
            "test_name": self.test_name,
            "passed": self.passed,
            "latency_ms": round(self.latency_ms, 1),
            "details": self.details,
            "error": self.error,
            "suggestions": self.suggestions,
        }


@dataclass
class ValidationReport:
    """完整的验证报告"""
    timestamp: str
    overall_status: str  # ok / partial / failed
    total_tests: int = 0
    passed_tests: int = 0
    failed_tests: int = 0
    results: List[ValidationResult] = field(default_factory=list)
    summary: str = ""

    def to_dict(self) -> Dict[str, Any]:
        return {
            "timestamp": self.timestamp,
            "overall_status": self.overall_status,
            "total_tests": self.total_tests,
            "passed_tests": self.passed_tests,
            "failed_tests": self.failed_tests,
            "results": [r.to_dict() for r in self.results],
            "summary": self.summary,
        }


# ============================================================
# LLM Validator
# ============================================================

class LLMValidator:
    """
    LLM 连通性验证器

    测试 LLM API 是否可达、认证是否有效、响应格式是否正确。

    使用示例:
        from src.workspace.manager import WorkspaceManager

        wm = WorkspaceManager("../../workspace")
        config = wm.get_provider_config("deepseek")

        validator = LLMValidator()
        result = validator.test_connectivity(config)
        print(f"状态: {result.passed}, 延迟: {result.latency_ms}ms")
    """

    # 不同 provider 类型的 API 路径
    CHAT_COMPLETIONS_PATH = "/chat/completions"

    def test_connectivity(self, provider_config) -> ValidationResult:
        """
        测试 LLM 提供商连通性

        发送一个简单的测试消息，验证:
        1. API 可达性（网络连接）
        2. 认证有效性（API Key）
        3. 响应格式正确性

        Args:
            provider_config: LLMProviderConfig 实例

        Returns:
            ValidationResult
        """
        start_time = time.time()

        try:
            # 构建请求
            base_url = provider_config.base_url.rstrip("/")
            url = f"{base_url}{self.CHAT_COMPLETIONS_PATH}"

            messages = [
                {"role": "system", "content": "You are a helpful assistant. Reply concisely."},
                {"role": "user", "content": "Say 'Hello, Noosphere!' in exactly these words."},
            ]

            body = json.dumps({
                "model": provider_config.model,
                "messages": messages,
                "max_tokens": 50,
                "temperature": 0.0,
                "stream": False,
            }).encode("utf-8")

            headers = {
                "Content-Type": "application/json",
                "Authorization": f"Bearer {provider_config.api_key}",
            }

            # 针对 Anthropic 的特殊处理
            if provider_config.provider_type == "anthropic":
                url = f"{base_url}/messages"
                body = json.dumps({
                    "model": provider_config.model,
                    "system": "You are a helpful assistant. Reply concisely.",
                    "messages": [{"role": "user", "content": "Say 'Hello, Noosphere!'"}],
                    "max_tokens": 50,
                }).encode("utf-8")
                headers["x-api-key"] = provider_config.api_key
                headers["anthropic-version"] = "2023-06-01"

            req = urllib.request.Request(url, data=body, headers=headers)

            # 发送请求
            with urllib.request.urlopen(req, timeout=30) as resp:
                data = json.loads(resp.read().decode("utf-8"))
                latency = (time.time() - start_time) * 1000

                # 解析响应
                response_text = self._extract_response_text(data, provider_config.provider_type)
                tokens = self._extract_token_count(data, provider_config.provider_type)

                return ValidationResult(
                    test_name=f"LLM Connectivity ({provider_config.name})",
                    passed=True,
                    latency_ms=latency,
                    details={
                        "provider": provider_config.name,
                        "type": provider_config.provider_type,
                        "model": provider_config.model,
                        "base_url": base_url,
                        "response_preview": response_text[:200],
                        "tokens_used": tokens,
                        "status_code": resp.status,
                    },
                )

        except urllib.error.HTTPError as e:
            latency = (time.time() - start_time) * 1000
            error_body = ""
            try:
                error_body = e.read().decode("utf-8")[:500] if e.fp else str(e)
            except Exception:
                error_body = str(e)

            suggestions = []
            if e.code == 401:
                suggestions = [
                    "API Key 无效或已过期，请检查 llm_config.yaml 中的 api_key",
                    "确认 API Key 有正确的权限",
                    "尝试在提供商后台重新生成 API Key",
                ]
            elif e.code == 403:
                suggestions = [
                    "API Key 没有访问该模型的权限",
                    "检查账户余额是否充足",
                    "确认模型名称是否正确",
                ]
            elif e.code == 404:
                suggestions = [
                    f"API 端点不存在: {e.url}",
                    "检查 base_url 配置是否正确",
                    "确认 API 路径拼写无误",
                ]
            elif e.code == 429:
                suggestions = [
                    "请求频率超限，稍后重试",
                    "检查 API 配额限制",
                ]
            elif e.code >= 500:
                suggestions = [
                    "LLM 服务端错误，稍后重试",
                    "检查提供商状态页面",
                ]

            return ValidationResult(
                test_name=f"LLM Connectivity ({provider_config.name})",
                passed=False,
                latency_ms=latency,
                error=f"HTTP {e.code}: {error_body[:300]}",
                suggestions=suggestions,
            )

        except urllib.error.URLError as e:
            latency = (time.time() - start_time) * 1000
            return ValidationResult(
                test_name=f"LLM Connectivity ({provider_config.name})",
                passed=False,
                latency_ms=latency,
                error=f"网络错误: {e.reason}",
                suggestions=[
                    "检查网络连接",
                    f"确认 base_url ({provider_config.base_url}) 是否可达",
                    "如果使用本地模型（Ollama），确认服务是否在运行",
                ],
            )

        except Exception as e:
            latency = (time.time() - start_time) * 1000
            return ValidationResult(
                test_name=f"LLM Connectivity ({provider_config.name})",
                passed=False,
                latency_ms=latency,
                error=str(e)[:500],
                suggestions=["检查配置是否正确", "查看日志获取详细信息"],
            )

    def test_all_providers(self, llm_config: Dict) -> ValidationReport:
        """
        测试所有已启用的提供商

        Args:
            llm_config: load_llm_config() 返回的完整配置

        Returns:
            ValidationReport
        """
        results = []
        providers = llm_config.get("providers", [])

        for p in providers:
            if not p.get("enabled", True):
                continue
            if not p.get("api_key") or "your-api-key" in p.get("api_key", "").lower():
                results.append(ValidationResult(
                    test_name=f"LLM ({p.get('name', 'unknown')})",
                    passed=False,
                    error="API Key 未配置（仍是占位符）",
                    suggestions=[f"编辑 workspace/llm_config.yaml 填入 {p.get('name')} 的 API Key"],
                ))
                continue

            from .manager import LLMProviderConfig
            provider_config = LLMProviderConfig(
                name=p.get("name", ""),
                provider_type=p.get("type", "deepseek"),
                api_key=p.get("api_key", ""),
                base_url=p.get("base_url", "https://api.deepseek.com"),
                model=p.get("model", "deepseek-v4-pro"),
            )

            result = self.test_connectivity(provider_config)
            results.append(result)

        passed = sum(1 for r in results if r.passed)
        total = len(results)

        if total == 0:
            overall = "failed"
            summary = "没有已配置的 LLM 提供商，请在 workspace/llm_config.yaml 中配置"
        elif passed == total:
            overall = "ok"
            summary = f"全部 {total} 个 LLM 提供商连通性正常"
        else:
            overall = "partial"
            summary = f"{passed}/{total} 个 LLM 提供商连通性正常，{total - passed} 个失败"

        return ValidationReport(
            timestamp=datetime.now().isoformat(),
            overall_status=overall,
            total_tests=total,
            passed_tests=passed,
            failed_tests=total - passed,
            results=results,
            summary=summary,
        )

    def _extract_response_text(self, data: Dict, provider_type: str) -> str:
        """从 API 响应中提取文本内容"""
        if provider_type == "anthropic":
            if "content" in data and isinstance(data["content"], list):
                return "".join(
                    block.get("text", "")
                    for block in data["content"]
                    if block.get("type") == "text"
                )
        else:
            # OpenAI 兼容格式
            if "choices" in data and len(data["choices"]) > 0:
                return data["choices"][0].get("message", {}).get("content", "")
        return ""

    def _extract_token_count(self, data: Dict, provider_type: str) -> int:
        """从 API 响应中提取 token 用量"""
        if "usage" in data:
            return data["usage"].get("total_tokens", 0)
        return 0


# ============================================================
# Tool Validator
# ============================================================

class ToolValidator:
    """
    工具链对话验证器

    向各服务发送测试请求，验证工具调用的输入/输出是否正确。

    测试项目:
    - Nebula: 会话创建 → 记忆存储 → 语义检索 → 对话
    - DevOps: 工具列表 → 系统信息 → 诊断
    - CodeLens: 健康检查 → QA 问答 → 搜索
    """

    def __init__(
        self,
        nebula_url: str = "http://localhost:8730",
        devops_url: str = "http://localhost:8740",
        codelens_url: str = "http://localhost:8765",
    ):
        self.services = {
            "nebula": nebula_url.rstrip("/"),
            "devops": devops_url.rstrip("/"),
            "codelens": codelens_url.rstrip("/"),
        }

    def validate_all(self, services: List[str] = None) -> ValidationReport:
        """
        验证所有服务的工具链

        Args:
            services: 要验证的服务列表，默认全部

        Returns:
            ValidationReport
        """
        if services is None:
            services = ["nebula", "devops", "codelens"]

        results = []
        for svc in services:
            validator_method = getattr(self, f"_validate_{svc}", None)
            if validator_method:
                svc_results = validator_method()
                results.extend(svc_results)
            else:
                results.append(ValidationResult(
                    test_name=f"Service: {svc}",
                    passed=False,
                    error=f"未知服务: {svc}",
                ))

        passed = sum(1 for r in results if r.passed)
        total = len(results)

        if passed == total:
            overall = "ok"
            summary = f"全部 {total} 项工具链测试通过"
        elif passed > 0:
            overall = "partial"
            summary = f"{passed}/{total} 项工具链测试通过"
        else:
            overall = "failed"
            summary = f"全部 {total} 项工具链测试失败，请检查服务是否启动"

        return ValidationReport(
            timestamp=datetime.now().isoformat(),
            overall_status=overall,
            total_tests=total,
            passed_tests=passed,
            failed_tests=total - passed,
            results=results,
            summary=summary,
        )

    def _validate_nebula(self) -> List[ValidationResult]:
        """验证 Nebula 的工具链"""
        results = []
        base = self.services["nebula"]

        # 1. 健康检查
        results.append(self._http_get_test(
            name="Nebula: 健康检查",
            url=f"{base}/health",
            expected_status=200,
        ))

        # 2. 创建会话
        session_id = f"validator-{int(time.time())}"
        results.append(self._http_post_test(
            name="Nebula: 创建会话",
            url=f"{base}/api/v1/sessions",
            body={"session_id": session_id},
            expected_status=201,
        ))

        # 3. 存储记忆
        results.append(self._http_post_test(
            name="Nebula: 存储记忆",
            url=f"{base}/api/v1/sessions/{session_id}/memories",
            body={
                "content": "Noosphere 验证测试记忆",
                "type": "episodic",
                "importance": 0.8,
                "tags": ["test", "validation"],
            },
            expected_status=201,
        ))

        # 4. 检索记忆
        results.append(self._http_post_test(
            name="Nebula: 语义检索",
            url=f"{base}/api/v1/sessions/{session_id}/search",
            body={
                "query": "验证测试",
                "top_k": 3,
                "strategy": "hybrid",
            },
            expected_status=200,
            validate_response=lambda d: "results" in d and "count" in d,
        ))

        # 5. 引擎统计
        results.append(self._http_get_test(
            name="Nebula: 引擎统计",
            url=f"{base}/api/v1/stats",
            expected_status=200,
        ))

        return results

    def _validate_devops(self) -> List[ValidationResult]:
        """验证 DevOps 的工具链"""
        results = []
        base = self.services["devops"]

        # 1. 健康检查
        results.append(self._http_get_test(
            name="DevOps: 健康检查",
            url=f"{base}/health",
            expected_status=200,
        ))

        # 2. 系统状态
        results.append(self._http_get_test(
            name="DevOps: 系统状态",
            url=f"{base}/api/v1/devops/status",
            expected_status=200,
        ))

        # 3. 工具列表
        results.append(self._http_get_test(
            name="DevOps: 工具列表",
            url=f"{base}/api/v1/devops/tools",
            expected_status=200,
            validate_response=lambda d: "tools" in d,
        ))

        # 4. 系统指标
        results.append(self._http_get_test(
            name="DevOps: 系统指标",
            url=f"{base}/api/v1/devops/metrics",
            expected_status=200,
        ))

        # 5. 诊断（基本查询）
        results.append(self._http_post_test(
            name="DevOps: 诊断查询",
            url=f"{base}/api/v1/devops/diagnose",
            body={
                "query": "系统状态检查",
                "session_id": "validator",
            },
            expected_status=200,
        ))

        return results

    def _validate_codelens(self) -> List[ValidationResult]:
        """验证 CodeLens 的工具链"""
        results = []
        base = self.services["codelens"]

        # 1. 健康检查
        results.append(self._http_get_test(
            name="CodeLens: 健康检查",
            url=f"{base}/api/v1/health",
            expected_status=200,
        ))

        # 2. 系统状态
        results.append(self._http_get_test(
            name="CodeLens: 系统状态",
            url=f"{base}/api/v1/status",
            expected_status=200,
        ))

        # 3. 代码搜索（不依赖索引）
        results.append(self._http_post_test(
            name="CodeLens: 代码搜索",
            url=f"{base}/api/v1/search",
            body={"query": "test", "top_k": 3},
            expected_status=200,
        ))

        # 4. 代码解释
        results.append(self._http_post_test(
            name="CodeLens: 代码解释",
            url=f"{base}/api/v1/analyze/explain?code=print('hello')&language=python",
            body={},
            expected_status=200,
        ))

        return results

    # ─── HTTP 辅助方法 ───

    def _http_get_test(
        self,
        name: str,
        url: str,
        expected_status: int = 200,
        validate_response=None,
    ) -> ValidationResult:
        """执行 HTTP GET 测试"""
        start = time.time()
        try:
            req = urllib.request.Request(url)
            with urllib.request.urlopen(req, timeout=10) as resp:
                latency = (time.time() - start) * 1000
                data = json.loads(resp.read().decode("utf-8"))

                if resp.status != expected_status:
                    return ValidationResult(
                        test_name=name,
                        passed=False,
                        latency_ms=latency,
                        error=f"期望状态码 {expected_status}，实际 {resp.status}",
                        details={"url": url, "response": str(data)[:300]},
                    )

                if validate_response and not validate_response(data):
                    return ValidationResult(
                        test_name=name,
                        passed=False,
                        latency_ms=latency,
                        error="响应格式不符合预期",
                        details={"url": url, "response": str(data)[:300]},
                    )

                return ValidationResult(
                    test_name=name,
                    passed=True,
                    latency_ms=latency,
                    details={"url": url, "status": resp.status},
                )

        except urllib.error.HTTPError as e:
            latency = (time.time() - start) * 1000
            return ValidationResult(
                test_name=name,
                passed=False,
                latency_ms=latency,
                error=f"HTTP {e.code}",
                details={"url": url},
                suggestions=["检查服务是否正常启动", f"确认端口是否正确"],
            )
        except urllib.error.URLError as e:
            latency = (time.time() - start) * 1000
            return ValidationResult(
                test_name=name,
                passed=False,
                latency_ms=latency,
                error=f"连接失败: {e.reason}",
                details={"url": url},
                suggestions=[f"确认服务是否在 {url.split('/')[2]} 运行"],
            )
        except Exception as e:
            return ValidationResult(
                test_name=name,
                passed=False,
                error=str(e)[:300],
            )

    def _http_post_test(
        self,
        name: str,
        url: str,
        body: Dict,
        expected_status: int = 200,
        validate_response=None,
    ) -> ValidationResult:
        """执行 HTTP POST 测试"""
        start = time.time()
        try:
            json_body = json.dumps(body).encode("utf-8")
            req = urllib.request.Request(
                url,
                data=json_body,
                headers={"Content-Type": "application/json"},
            )
            with urllib.request.urlopen(req, timeout=10) as resp:
                latency = (time.time() - start) * 1000
                data = json.loads(resp.read().decode("utf-8"))

                if resp.status != expected_status:
                    return ValidationResult(
                        test_name=name,
                        passed=False,
                        latency_ms=latency,
                        error=f"期望状态码 {expected_status}，实际 {resp.status}",
                        details={"url": url, "response": str(data)[:300]},
                    )

                if validate_response and not validate_response(data):
                    return ValidationResult(
                        test_name=name,
                        passed=False,
                        latency_ms=latency,
                        error="响应格式不符合预期",
                        details={"url": url, "response": str(data)[:300]},
                    )

                return ValidationResult(
                    test_name=name,
                    passed=True,
                    latency_ms=latency,
                    details={"url": url, "status": resp.status},
                )

        except urllib.error.HTTPError as e:
            latency = (time.time() - start) * 1000
            return ValidationResult(
                test_name=name,
                passed=False,
                latency_ms=latency,
                error=f"HTTP {e.code}",
                details={"url": url},
                suggestions=["检查服务是否正常启动"],
            )
        except urllib.error.URLError as e:
            latency = (time.time() - start) * 1000
            return ValidationResult(
                test_name=name,
                passed=False,
                latency_ms=latency,
                error=f"连接失败: {e.reason}",
                details={"url": url},
                suggestions=[f"确认服务是否在 {url.split('/')[2]} 运行"],
            )
        except Exception as e:
            return ValidationResult(
                test_name=name,
                passed=False,
                error=str(e)[:300],
            )


# ============================================================
# 端到端验证
# ============================================================

class EndToEndValidator:
    """
    端到端验证器

    联合验证完整的分析流水线:
    1. 索引样本项目
    2. 执行代码问答
    3. 检查 LLM 调用是否正常
    4. 检查工具链输入/输出
    """

    def __init__(self, agent_instance=None):
        """
        初始化

        Args:
            agent_instance: CodeLensAgent 实例（可选）
        """
        self.agent = agent_instance

    def run_e2e(self, project_path: str = None) -> ValidationReport:
        """
        运行端到端验证

        Args:
            project_path: 要测试的项目路径（可选，默认使用 workspace 中的第一个项目）

        Returns:
            ValidationReport
        """
        results = []

        # 1. 检查 LLM 配置
        try:
            from ..config import is_configured, get_model, get_api_key
            if is_configured():
                results.append(ValidationResult(
                    test_name="E2E: LLM 配置检查",
                    passed=True,
                    details={
                        "model": get_model(),
                        "api_key": get_api_key()[:8] + "..." + get_api_key()[-4:],
                    },
                ))
            else:
                results.append(ValidationResult(
                    test_name="E2E: LLM 配置检查",
                    passed=False,
                    error="LLM API Key 未正确配置",
                    suggestions=["编辑 workspace/llm_config.yaml", "或设置 DEEPSEEK_API_KEY 环境变量"],
                ))
        except ImportError:
            results.append(ValidationResult(
                test_name="E2E: LLM 配置检查",
                passed=False,
                error="无法加载配置模块",
            ))

        # 2. 检查 CodeLens Agent 状态
        if self.agent:
            try:
                summary = self.agent.get_project_summary()
                results.append(ValidationResult(
                    test_name="E2E: CodeLens Agent 状态",
                    passed=summary.get("total_files", 0) > 0,
                    details=summary,
                ))
            except Exception as e:
                results.append(ValidationResult(
                    test_name="E2E: CodeLens Agent 状态",
                    passed=False,
                    error=str(e),
                ))

        # 3. 测试问答
        if self.agent and self.agent._initialized:
            try:
                response = self.agent.ask("What is the purpose of this project?")
                results.append(ValidationResult(
                    test_name="E2E: 代码问答测试",
                    passed=bool(response.answer),
                    details={
                        "answer_preview": response.answer[:200],
                        "sources_count": len(response.sources),
                        "tokens_used": response.tokens_used,
                    },
                ))
            except Exception as e:
                results.append(ValidationResult(
                    test_name="E2E: 代码问答测试",
                    passed=False,
                    error=str(e),
                ))

        passed = sum(1 for r in results if r.passed)
        total = len(results)

        return ValidationReport(
            timestamp=datetime.now().isoformat(),
            overall_status="ok" if passed == total else ("partial" if passed > 0 else "failed"),
            total_tests=total,
            passed_tests=passed,
            failed_tests=total - passed,
            results=results,
            summary=f"{passed}/{total} 项端到端测试通过",
        )
