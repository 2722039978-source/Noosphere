"""
Nebula Agent — Memory 数据模型 (Python)
"""
from enum import Enum
from typing import Optional, List, Dict
from dataclasses import dataclass, field


class MemoryType(Enum):
    WORKING = "working"
    EPISODIC = "episodic"
    SEMANTIC = "semantic"
    PROCEDURAL = "procedural"


@dataclass
class Memory:
    id: str = ""
    session_id: str = ""
    type: str = "episodic"
    content: str = ""
    embedding: Optional[List[float]] = None
    tags: List[str] = field(default_factory=list)
    metadata: Dict[str, str] = field(default_factory=dict)
    importance: float = 0.5
    created_at: str = ""
    access_cnt: int = 0


@dataclass
class SearchOptions:
    query: str = ""
    top_k: int = 10
    strategy: str = "hybrid"
    memory_types: List[str] = field(default_factory=list)
    threshold: float = 0.0


@dataclass
class SearchResult:
    memory: Optional[Memory] = None
    score: float = 0.0
    reason: str = ""
