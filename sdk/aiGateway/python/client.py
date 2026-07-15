"""
AI Gateway Python SDK

统一的 LLM 调用客户端 —— 所有子项目通过此 SDK 调用 AI Gateway，
Gateway 再根据用户配置路由到具体的模型提供商。

使用示例:
    from ai_gateway import GatewayClient

    client = GatewayClient("http://localhost:8800", project="codelens")

    # 聊天
    resp = client.chat([
        {"role": "user", "content": "Hello, how are you?"}
    ])

    # 流式聊天
    for chunk in client.stream_chat("Tell me a story"):
        print(chunk, end="", flush=True)

    # Embedding
    embeddings = client.embedding(["text to embed"])

    # 获取可用模型
    models = client.list_models()
"""

import json
import time
import urllib.request
import urllib.error
from typing import Optional, Dict, Any, List, Iterator, Union


class GatewayClient:
    """AI Gateway 统一调用客户端"""

    def __init__(self, gateway_url: str = "http://localhost:8800", project: str = ""):
        """
        初始化 Gateway 客户端

        Args:
            gateway_url: AI Gateway 服务地址
            project: 项目标识（codelens / nebula / devops）
        """
        self.base_url = gateway_url.rstrip("/")
        self.project = project
        self._timeout = 120

    # ─── 核心 API ───

    def chat(
        self,
        messages: List[Dict[str, str]],
        model_id: str = "",
        system_prompt: str = "",
        temperature: float = 0.0,
        max_tokens: int = 4096,
        session_id: str = "",
    ) -> Dict[str, Any]:
        """
        发送聊天请求

        Args:
            messages: 消息列表 [{"role": "user", "content": "..."}]
            model_id: 指定模型 ID（空则使用默认模型）
            system_prompt: 系统提示词
            temperature: 温度参数
            max_tokens: 最大输出 token
            session_id: 会话 ID

        Returns:
            {"id": "...", "model": "...", "content": "...", "tokens_used": N, "latency_ms": N.N}
        """
        body = {
            "messages": messages,
            "project": self.project,
        }
        if model_id:
            body["model_id"] = model_id
        if system_prompt:
            body["system_prompt"] = system_prompt
        if temperature:
            body["temperature"] = temperature
        if max_tokens:
            body["max_tokens"] = max_tokens
        if session_id:
            body["session_id"] = session_id

        return self._post("/api/v1/gateway/chat", body)

    def stream_chat(
        self,
        prompt: str,
        model_id: str = "",
        system_prompt: str = "",
        history: List[Dict[str, str]] = None,
    ) -> Iterator[str]:
        """
        流式聊天（生成器）

        Args:
            prompt: 用户消息
            model_id: 指定模型 ID
            system_prompt: 系统提示词
            history: 历史消息

        Yields:
            文本片段
        """
        messages = []
        if history:
            messages.extend(history)
        messages.append({"role": "user", "content": prompt})

        body = {
            "messages": messages,
            "project": self.project,
            "stream": True,
        }
        if model_id:
            body["model_id"] = model_id
        if system_prompt:
            body["system_prompt"] = system_prompt

        # Streaming via SSE
        req_body = json.dumps(body).encode("utf-8")
        req = urllib.request.Request(
            f"{self.base_url}/api/v1/gateway/chat/stream",
            data=req_body,
            headers={"Content-Type": "application/json"},
        )

        try:
            with urllib.request.urlopen(req, timeout=self._timeout) as resp:
                remainder = ""
                while True:
                    chunk = resp.read(4096)
                    if not chunk:
                        break
                    remainder += chunk.decode("utf-8")
                    while "\n\n" in remainder:
                        idx = remainder.index("\n\n")
                        line = remainder[:idx]
                        remainder = remainder[idx + 2:]
                        if line.startswith("data: "):
                            data = line[6:]
                            if data == "[DONE]":
                                return
                            yield data
        except urllib.error.HTTPError as e:
            raise RuntimeError(f"Gateway stream error: HTTP {e.code}")

    def vision(
        self,
        prompt: str,
        image_urls: List[str] = None,
        image_base64: List[str] = None,
        model_id: str = "",
    ) -> Dict[str, Any]:
        """
        视觉/图像理解

        Args:
            prompt: 文本提示
            image_urls: 图片 URL 列表
            image_base64: Base64 编码的图片列表
            model_id: 指定模型 ID

        Returns:
            同 chat() 响应格式
        """
        body = {
            "prompt": prompt,
            "project": self.project,
        }
        if image_urls:
            body["image_urls"] = image_urls
        if image_base64:
            body["image_base64"] = image_base64
        if model_id:
            body["model_id"] = model_id

        return self._post("/api/v1/gateway/vision", body)

    def embedding(
        self,
        texts: Union[str, List[str]],
        model_id: str = "",
    ) -> List[List[float]]:
        """
        文本嵌入

        Args:
            texts: 单个文本或文本列表
            model_id: 指定模型 ID

        Returns:
            嵌入向量列表
        """
        if isinstance(texts, str):
            texts = [texts]

        body = {
            "input": texts,
            "project": self.project,
        }
        if model_id:
            body["model_id"] = model_id

        result = self._post("/api/v1/gateway/embedding", body)
        return result.get("embeddings", [])

    # ─── 管理 API ───

    def list_models(self) -> List[Dict]:
        """获取所有已配置的模型"""
        return self._get("/api/v1/gateway/models").get("models", [])

    def test_model(self, model_id: str) -> Dict[str, Any]:
        """测试模型连通性"""
        return self._post(f"/api/v1/gateway/models/{model_id}/test", {})

    def get_stats(self) -> Dict[str, Any]:
        """获取使用统计"""
        return self._get("/api/v1/gateway/stats")

    def get_logs(self, project: str = "", limit: int = 50) -> List[Dict]:
        """获取调用日志"""
        url = f"/api/v1/gateway/logs?limit={limit}"
        if project:
            url += f"&project={project}"
        return self._get(url).get("logs", [])

    def import_api_doc(self, content: str, format: str, name: str = "") -> Dict:
        """
        导入 API 文档

        Args:
            content: 文档内容（JSON/Markdown 文本）
            format: 格式（swagger / openapi / markdown）
            name: 文档名称
        """
        return self._post("/api/v1/gateway/docs/import", {
            "content": content,
            "format": format,
            "name": name,
        })

    def get_docs(self) -> List[Dict]:
        """获取已导入的 API 文档列表"""
        return self._get("/api/v1/gateway/docs").get("docs", [])

    def health(self) -> bool:
        """检查 Gateway 是否在线"""
        try:
            data = self._get("/health")
            return data.get("status") == "ok"
        except Exception:
            return False

    # ─── 内部方法 ───

    def _get(self, path: str) -> Dict[str, Any]:
        """HTTP GET 请求"""
        try:
            req = urllib.request.Request(f"{self.base_url}{path}")
            with urllib.request.urlopen(req, timeout=self._timeout) as resp:
                return json.loads(resp.read().decode("utf-8"))
        except urllib.error.HTTPError as e:
            raise RuntimeError(f"Gateway HTTP {e.code}: {e.reason}")
        except urllib.error.URLError as e:
            raise RuntimeError(f"Gateway unreachable: {e.reason}")

    def _post(self, path: str, body: Dict) -> Dict[str, Any]:
        """HTTP POST 请求"""
        try:
            data = json.dumps(body).encode("utf-8")
            req = urllib.request.Request(
                f"{self.base_url}{path}",
                data=data,
                headers={"Content-Type": "application/json"},
            )
            with urllib.request.urlopen(req, timeout=self._timeout) as resp:
                return json.loads(resp.read().decode("utf-8"))
        except urllib.error.HTTPError as e:
            body_text = e.read().decode("utf-8")[:500] if e.fp else str(e)
            raise RuntimeError(f"Gateway HTTP {e.code}: {body_text}")
        except urllib.error.URLError as e:
            raise RuntimeError(f"Gateway unreachable: {e.reason}")


# ─── 便捷函数 ───

_default_client = None

def init(gateway_url: str = "http://localhost:8800", project: str = ""):
    """初始化全局 Gateway 客户端"""
    global _default_client
    _default_client = GatewayClient(gateway_url, project)


def chat(messages, **kwargs) -> Dict[str, Any]:
    """使用默认客户端发送聊天请求"""
    if _default_client is None:
        init()
    return _default_client.chat(messages, **kwargs)


def stream_chat(prompt: str, **kwargs) -> Iterator[str]:
    """使用默认客户端发送流式聊天请求"""
    if _default_client is None:
        init()
    return _default_client.stream_chat(prompt, **kwargs)


def embedding(texts, **kwargs) -> List[List[float]]:
    """使用默认客户端获取嵌入"""
    if _default_client is None:
        init()
    return _default_client.embedding(texts, **kwargs)
