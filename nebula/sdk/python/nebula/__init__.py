"""
Nebula Agent Python SDK
=======================
轻量级嵌入式 AI Agent Memory Engine 的 Python 客户端。

使用方式:
    import nebula

    # 通过 REST API 连接
    client = nebula.Client("http://localhost:8730")

    # 存储记忆
    client.remember("my-agent", "用户偏好 Python", "episodic", importance=0.8)

    # 语义检索
    results = client.search("my-agent", "编程语言", top_k=5)

    # 批量操作
    with client.batch("my-agent") as batch:
        batch.remember("事实1", "semantic")
        batch.remember("事实2", "semantic")
"""

from .client import Client, MemoryClient
from .memory import Memory, MemoryType, SearchOptions, SearchResult

__version__ = "0.1.0"
__all__ = ["Client", "MemoryClient", "Memory", "MemoryType", "SearchOptions", "SearchResult"]
