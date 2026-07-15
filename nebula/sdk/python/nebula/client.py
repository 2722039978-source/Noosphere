"""
Nebula Agent Python Client
"""
from typing import Optional, List, Dict, Any
from dataclasses import dataclass, field
import requests
import time

from .memory import Memory, SearchOptions, SearchResult, MemoryType


@dataclass
class StatsResponse:
    session_count: int = 0
    total_memories: int = 0
    episodic_count: int = 0
    semantic_count: int = 0
    vector_count: int = 0
    cache_hit_rate: float = 0.0
    uptime: str = ""


class Client:
    """Nebula Agent REST API 客户端"""

    def __init__(self, base_url: str = "http://localhost:8730", timeout: int = 30):
        self.base_url = base_url.rstrip("/")
        self.session = requests.Session()
        self.session.headers.update({"Content-Type": "application/json"})
        self.session.timeout = timeout

    # ─── Health ───

    def health(self) -> Dict[str, Any]:
        """健康检查"""
        return self._get("/health")

    # ─── Stats ───

    def stats(self) -> StatsResponse:
        """获取服务器统计"""
        data = self._get("/api/v1/stats")
        eng = data.get("engine_stats", {})
        return StatsResponse(
            session_count=data.get("session_count", 0),
            total_memories=eng.get("total_memories", 0),
            episodic_count=eng.get("episodic_count", 0),
            semantic_count=eng.get("semantic_count", 0),
            vector_count=eng.get("vector_count", 0),
            cache_hit_rate=eng.get("cache_hit_rate", 0.0),
            uptime=eng.get("uptime", ""),
        )

    # ─── Session ───

    def create_session(self, session_id: str = None) -> str:
        """创建会话"""
        body = {}
        if session_id:
            body["session_id"] = session_id
        resp = self._post("/api/v1/sessions", body)
        return resp.get("session_id", "")

    def delete_session(self, session_id: str) -> bool:
        """删除会话"""
        resp = self._delete(f"/api/v1/sessions/{session_id}")
        return resp.get("deleted", False)

    # ─── Memory CRUD ───

    def remember(
        self,
        session_id: str,
        content: str,
        mem_type: str = "episodic",
        importance: float = 0.5,
        tags: List[str] = None,
        metadata: Dict[str, str] = None,
        ttl_seconds: int = 0,
    ) -> str:
        """存储记忆，返回 memory ID"""
        body = {
            "content": content,
            "type": mem_type,
            "importance": importance,
            "tags": tags or [],
        }
        if metadata:
            body["metadata"] = metadata
        if ttl_seconds > 0:
            body["ttl_seconds"] = ttl_seconds

        resp = self._post(f"/api/v1/sessions/{session_id}/memories", body)
        return resp.get("id", "")

    def recall(self, session_id: str, memory_id: str) -> Optional[Dict]:
        """召回记忆"""
        return self._get(f"/api/v1/sessions/{session_id}/memories/{memory_id}")

    def forget(self, session_id: str, memory_id: str) -> bool:
        """遗忘记忆"""
        resp = self._delete(f"/api/v1/sessions/{session_id}/memories/{memory_id}")
        return resp.get("deleted", False)

    # ─── Search ───

    def search(
        self,
        session_id: str,
        query: str,
        top_k: int = 10,
        strategy: str = "hybrid",
        memory_types: List[str] = None,
        threshold: float = 0.0,
    ) -> List[Dict]:
        """检索记忆"""
        body = {
            "query": query,
            "top_k": top_k,
            "strategy": strategy,
            "threshold": threshold,
        }
        if memory_types:
            body["memory_types"] = memory_types

        resp = self._post(f"/api/v1/sessions/{session_id}/search", body)
        return resp.get("results", [])

    # ─── Batch ───

    def batch(self, session_id: str) -> "BatchContext":
        """批量操作上下文管理器"""
        return BatchContext(self, session_id)

    # ─── Internal HTTP ───

    def _get(self, path: str) -> Dict:
        resp = self.session.get(f"{self.base_url}{path}")
        resp.raise_for_status()
        return resp.json()

    def _post(self, path: str, body: Dict) -> Dict:
        resp = self.session.post(f"{self.base_url}{path}", json=body)
        resp.raise_for_status()
        return resp.json()

    def _delete(self, path: str) -> Dict:
        resp = self.session.delete(f"{self.base_url}{path}")
        resp.raise_for_status()
        return resp.json()


class BatchContext:
    """批量操作上下文"""

    def __init__(self, client: Client, session_id: str):
        self.client = client
        self.session_id = session_id
        self._ops = []

    def remember(self, content: str, mem_type: str = "episodic", **kwargs):
        """添加存储操作到批处理"""
        self._ops.append({
            "content": content,
            "type": mem_type,
            **kwargs,
        })

    def __enter__(self):
        return self

    def __exit__(self, *args):
        for op in self._ops:
            self.client.remember(self.session_id, **op)


class MemoryClient:
    """高级 Memory Client（类型安全）"""

    def __init__(self, client: Client, session_id: str):
        self._client = client
        self.session_id = session_id

    def remember(
        self,
        content: str,
        mem_type: MemoryType = MemoryType.EPISODIC,
        importance: float = 0.5,
        tags: List[str] = None,
    ) -> str:
        return self._client.remember(
            self.session_id, content, mem_type.value, importance, tags
        )

    def recall(self, memory_id: str) -> Optional[Dict]:
        return self._client.recall(self.session_id, memory_id)

    def forget(self, memory_id: str) -> bool:
        return self._client.forget(self.session_id, memory_id)

    def search(
        self,
        query: str,
        top_k: int = 10,
        strategy: str = "hybrid",
    ) -> List[Dict]:
        return self._client.search(self.session_id, query, top_k, strategy)
