"""
代码检索器

实现多策略的代码检索：
- 语义检索：基于向量相似度搜索
- 结构检索：基于知识图谱的关系查询
- 关键词检索：基于全文匹配
- 混合检索：结合多种策略的融合检索

检索结果经过排序、去重和上下文扩展。
"""

from typing import Optional, Dict, Any, List, Set, Tuple
from dataclasses import dataclass, field
from enum import Enum

from loguru import logger

from ..storage.vector_store import VectorStore, VectorDoc
from ..indexer.knowledge_graph import KnowledgeGraph, GraphNode
from ..parser.ast_extractor import EntityType


class SearchStrategy(Enum):
    """搜索策略"""
    SEMANTIC = "semantic"       # 纯向量语义搜索
    STRUCTURAL = "structural"   # 知识图谱结构搜索
    HYBRID = "hybrid"           # 混合搜索
    KEYWORD = "keyword"         # 关键词搜索


@dataclass
class RetrievalResult:
    """检索结果"""
    content: str
    score: float
    source: str                    # 来源: semantic, structural, keyword
    entity_name: str = ""
    entity_type: str = ""
    file_path: str = ""
    line_number: int = 0
    context_before: str = ""       # 上文
    context_after: str = ""        # 下文
    related_entities: List[str] = field(default_factory=list)
    metadata: Dict[str, Any] = field(default_factory=dict)

    def to_prompt_context(self) -> str:
        """转换为可注入 LLM 的上下文字符串"""
        parts = []
        if self.entity_name:
            parts.append(f"// Entity: {self.entity_name} ({self.entity_type})")
        parts.append(f"// File: {self.file_path}:{self.line_number}")
        if self.context_before:
            parts.append(f"// Context before:\n{self.context_before}")
        parts.append(self.content)
        if self.context_after:
            parts.append(f"// Context after:\n{self.context_after}")
        return "\n".join(parts)


class CodeRetriever:
    """
    代码检索器

    结合语义搜索和结构搜索的多策略检索器。
    支持检索结果的上下文扩展、去重和重排序。

    使用示例:
        retriever = CodeRetriever(vector_store, knowledge_graph)
        results = retriever.retrieve(
            "how does authentication work?",
            strategy=SearchStrategy.HYBRID,
            top_k=10,
        )
        for r in results:
            print(f"{r.entity_name}: {r.content[:100]}...")
    """

    def __init__(
        self,
        vector_store: VectorStore,
        knowledge_graph: KnowledgeGraph,
        source_code_map: Optional[Dict[str, str]] = None,
    ):
        """
        初始化检索器

        Args:
            vector_store: 向量存储
            knowledge_graph: 知识图谱
            source_code_map: 文件路径 -> 源代码的映射（用于上下文扩展）
        """
        self.vector_store = vector_store
        self.kg = knowledge_graph
        self.source_code_map = source_code_map or {}

    def retrieve(
        self,
        query: str,
        strategy: SearchStrategy = SearchStrategy.HYBRID,
        top_k: int = 10,
        language_filter: Optional[str] = None,
        entity_type_filter: Optional[str] = None,
        file_filter: Optional[str] = None,
        expand_context: bool = True,
    ) -> List[RetrievalResult]:
        """
        多策略检索

        Args:
            query: 查询文本
            strategy: 搜索策略
            top_k: 返回结果数
            language_filter: 语言过滤
            entity_type_filter: 实体类型过滤
            file_filter: 文件过滤
            expand_context: 是否扩展上下文

        Returns:
            排序后的检索结果
        """
        results: List[RetrievalResult] = []

        if strategy == SearchStrategy.SEMANTIC:
            results = self._semantic_search(
                query, top_k * 2, language_filter, entity_type_filter
            )
        elif strategy == SearchStrategy.STRUCTURAL:
            results = self._structural_search(
                query, top_k * 2
            )
        elif strategy == SearchStrategy.KEYWORD:
            results = self._keyword_search(
                query, top_k * 2
            )
        elif strategy == SearchStrategy.HYBRID:
            # 并行执行多种搜索
            semantic_results = self._semantic_search(
                query, top_k, language_filter, entity_type_filter
            )
            structural_results = self._structural_search(query, top_k)
            keyword_results = self._keyword_search(query, top_k)

            # 合并去重
            results = self._merge_results(
                [semantic_results, structural_results, keyword_results],
                top_k,
            )

        # 文件过滤
        if file_filter:
            results = [r for r in results if file_filter in r.file_path]

        # 扩展上下文
        if expand_context:
            results = [self._expand_context(r) for r in results]

        # 截断
        return results[:top_k]

    def retrieve_for_entity(
        self,
        entity_name: str,
        include_related: bool = True,
    ) -> List[RetrievalResult]:
        """
        获取指定实体的检索结果

        Args:
            entity_name: 实体名称
            include_related: 是否包含相关实体

        Returns:
            检索结果
        """
        node = self.kg.get_node(entity_name)
        if node is None:
            return []

        results = []

        # 实体自身的代码
        code = self._get_source_at_location(node.file_path, node.start_line)
        if code:
            results.append(RetrievalResult(
                content=code,
                score=1.0,
                source="structural",
                entity_name=node.name,
                entity_type=node.entity_type.value if isinstance(node.entity_type, EntityType) else node.entity_type,
                file_path=node.file_path,
                line_number=node.start_line,
            ))

        # 相关实体
        if include_related:
            callees = self.kg.get_callees(entity_name)
            for callee in callees[:5]:
                callee_code = self._get_source_at_location(callee.file_path, callee.start_line)
                if callee_code:
                    results.append(RetrievalResult(
                        content=callee_code,
                        score=0.8,
                        source="structural",
                        entity_name=callee.name,
                        entity_type=callee.entity_type.value if isinstance(callee.entity_type, EntityType) else callee.entity_type,
                        file_path=callee.file_path,
                        line_number=callee.start_line,
                        related_entities=[entity_name],
                    ))

        return results

    def retrieve_call_chain_context(self, function_name: str, max_depth: int = 3) -> str:
        """
        获取函数调用链的上下文（用于 QA 注入）

        Args:
            function_name: 函数名称
            max_depth: 最大深度

        Returns:
            调用链上下文文本
        """
        chains = self.kg.get_call_chain(function_name, max_depth)
        if not chains:
            return f"No call chain found for '{function_name}'"

        context_parts = [f"# Call Chain for '{function_name}':"]

        for i, chain in enumerate(chains[:5]):
            context_parts.append(f"\n## Path {i + 1}:")
            context_parts.append(" → ".join(chain))

            # 为链上每个函数添加上下文
            for func_name in chain[:max_depth]:
                node = self.kg.get_node(func_name)
                if node:
                    code = self._get_source_at_location(node.file_path, node.start_line)
                    if code:
                        context_parts.append(f"\n### {func_name}")
                        context_parts.append(f"```\n{code}\n```")

        return "\n".join(context_parts)

    # ---- 搜索策略实现 ----

    def _semantic_search(
        self,
        query: str,
        top_k: int,
        language: Optional[str] = None,
        entity_type: Optional[str] = None,
    ) -> List[RetrievalResult]:
        """语义向量搜索"""
        docs = self.vector_store.search_code(query, top_k, language, entity_type)

        results = []
        for doc in docs:
            results.append(RetrievalResult(
                content=doc.content,
                score=doc.score,
                source="semantic",
                entity_name=doc.metadata.get("name", ""),
                entity_type=doc.metadata.get("type", ""),
                file_path=doc.metadata.get("file", ""),
                line_number=doc.metadata.get("start_line", 0),
            ))

        return results

    def _structural_search(self, query: str, top_k: int) -> List[RetrievalResult]:
        """知识图谱结构搜索"""
        # 在知识图谱中搜索匹配的节点
        nodes = self.kg.search_nodes(query)

        results = []
        for node in nodes[:top_k]:
            code = self._get_source_at_location(node.file_path, node.start_line)
            if code:
                results.append(RetrievalResult(
                    content=code,
                    score=0.9 if query.lower() in node.name.lower() else 0.5,
                    source="structural",
                    entity_name=node.name,
                    entity_type=node.entity_type.value if isinstance(node.entity_type, EntityType) else node.entity_type,
                    file_path=node.file_path,
                    line_number=node.start_line,
                ))

        # 按名称匹配度排序
        results.sort(key=lambda r: r.score, reverse=True)
        return results[:top_k]

    def _keyword_search(self, query: str, top_k: int) -> List[RetrievalResult]:
        """关键词搜索（在源代码中直接搜索）"""
        if not self.source_code_map:
            return []

        keywords = query.lower().split()
        results = []
        scored = []

        for file_path, source in self.source_code_map.items():
            source_lower = source.lower()
            score = sum(1 for kw in keywords if kw in source_lower)
            if score > 0:
                scored.append((file_path, source, score))

        # 按分数排序
        scored.sort(key=lambda x: x[2], reverse=True)

        for file_path, source, score in scored[:top_k]:
            # 提取匹配的行周围的内容
            lines = source.split("\n")
            # 找最佳匹配行
            best_line = 0
            best_match = 0
            for i, line in enumerate(lines):
                match_count = sum(1 for kw in keywords if kw in line.lower())
                if match_count > best_match:
                    best_match = match_count
                    best_line = i

            # 提取上下文
            start = max(0, best_line - 5)
            end = min(len(lines), best_line + 6)
            context = "\n".join(lines[start:end])

            results.append(RetrievalResult(
                content=context,
                score=score / max(len(keywords), 1),
                source="keyword",
                file_path=file_path,
                line_number=best_line + 1,
            ))

        return results

    def _merge_results(
        self,
        result_groups: List[List[RetrievalResult]],
        top_k: int,
    ) -> List[RetrievalResult]:
        """
        合并多个搜索结果，去重并重排序

        使用加权融合策略：
        - 语义搜索: 权重 0.5
        - 结构搜索: 权重 0.3
        - 关键词搜索: 权重 0.2
        """
        weights = {"semantic": 0.5, "structural": 0.3, "keyword": 0.2}
        merged: Dict[str, RetrievalResult] = {}
        seen_content: Set[str] = set()

        for group in result_groups:
            for result in group:
                # 基于内容哈希去重
                content_hash = result.content[:100]
                if content_hash in seen_content:
                    continue
                seen_content.add(content_hash)

                # 加权
                weight = weights.get(result.source, 0.1)
                result.score *= weight

                key = f"{result.file_path}:{result.line_number}"
                if key in merged:
                    if result.score > merged[key].score:
                        merged[key] = result
                else:
                    merged[key] = result

        # 排序
        sorted_results = sorted(
            merged.values(), key=lambda r: r.score, reverse=True
        )

        return sorted_results[:top_k]

    def _expand_context(self, result: RetrievalResult, context_lines: int = 10) -> RetrievalResult:
        """扩展检索结果的上下文"""
        if not result.file_path or result.line_number <= 0:
            return result

        source = self.source_code_map.get(result.file_path)
        if source is None:
            # 尝试从文件读取
            try:
                from pathlib import Path
                source = Path(result.file_path).read_text(encoding="utf-8", errors="ignore")
                self.source_code_map[result.file_path] = source
            except Exception:
                return result

        lines = source.split("\n")
        line_idx = result.line_number - 1

        # 上文
        before_start = max(0, line_idx - context_lines)
        result.context_before = "\n".join(lines[before_start:line_idx])

        # 下文
        after_end = min(len(lines), line_idx + context_lines + 1)
        result.context_after = "\n".join(lines[line_idx + 1:after_end])

        return result

    def _get_source_at_location(self, file_path: str, line_number: int, context: int = 15) -> str:
        """获取指定位置的源代码片段"""
        source = self.source_code_map.get(file_path)
        if source is None:
            try:
                from pathlib import Path
                source = Path(file_path).read_text(encoding="utf-8", errors="ignore")
                self.source_code_map[file_path] = source
            except Exception:
                return ""

        lines = source.split("\n")
        start = max(0, line_number - context)
        end = min(len(lines), line_number + context)
        return "\n".join(lines[start:end])

    def set_source_code_map(self, source_map: Dict[str, str]):
        """设置源代码映射"""
        self.source_code_map.update(source_map)

    def load_source_files(self, file_paths: List[str]):
        """批量加载源文件"""
        from pathlib import Path

        for fp in file_paths:
            if fp not in self.source_code_map:
                try:
                    self.source_code_map[fp] = Path(fp).read_text(encoding="utf-8", errors="ignore")
                except Exception:
                    logger.warning(f"无法读取: {fp}")

        logger.info(f"加载 {len(self.source_code_map)} 个源文件到检索器")
