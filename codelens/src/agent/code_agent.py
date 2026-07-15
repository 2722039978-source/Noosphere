"""
CodeLens 智能代码分析 Agent

核心 Agent 模块，协调各个子系统完成代码分析任务：
- 代码理解与导航
- 问题诊断与定位
- 代码审查
- 重构建议
- 自动化分析流水线
"""

import time
from typing import Optional, Dict, Any, List, Callable, Union
from dataclasses import dataclass, field
from enum import Enum
from pathlib import Path

from loguru import logger

from ..parser.tree_sitter_parser import TreeSitterParser, Language
from ..parser.ast_extractor import ASTExtractor, CodeEntity, EntityType
from ..indexer.index_builder import IndexBuilder, ProjectIndex
from ..indexer.knowledge_graph import KnowledgeGraph
from ..storage.lsm_store import LSMStore, LSMConfig
from ..storage.vector_store import VectorStore
from ..rag.embeddings import CodeEmbedder
from ..rag.retriever import CodeRetriever, SearchStrategy
from ..rag.qa_engine import QACodeEngine, QAQuery, QAResponse, QueryType


class AgentAction(Enum):
    """Agent 可以执行的操作"""
    INDEX_PROJECT = "index_project"           # 索引项目
    ANALYZE_FILE = "analyze_file"             # 分析文件
    EXPLAIN_CODE = "explain_code"             # 解释代码
    TRACE_CALLS = "trace_calls"               # 追踪调用链
    ANALYZE_IMPACT = "analyze_impact"         # 影响分析
    FIND_BUGS = "find_bugs"                   # 查找问题
    GENERATE_DOCS = "generate_docs"           # 生成文档
    REVIEW_CODE = "review_code"               # 代码审查
    SEARCH_CODEBASE = "search_codebase"       # 搜索代码库


@dataclass
class AgentConfig:
    """Agent 配置"""
    project_root: str = "."
    # 解析配置
    languages: Optional[List[Language]] = None
    exclude_dirs: Optional[List[str]] = None
    max_file_size_mb: float = 10.0
    # 存储配置
    lsm_config: Optional[LSMConfig] = None
    # 嵌入配置
    embedding_model: str = "all-MiniLM-L6-v2"
    # 并行处理
    parallel: bool = True
    max_workers: int = 4
    # LLM 客户端
    llm_client: Optional[Any] = None


@dataclass
class AgentResult:
    """Agent 执行结果"""
    action: AgentAction
    success: bool
    data: Any = None
    error: Optional[str] = None
    execution_time_ms: float = 0.0
    metadata: Dict[str, Any] = field(default_factory=dict)


class CodeLensAgent:
    """
    CodeLens 智能代码分析 Agent

    协调解析器、索引器、知识图谱、向量存储和问答引擎
    完成各种代码分析任务。

    使用示例:
        agent = CodeLensAgent(AgentConfig(project_root="/path/to/project"))

        # 索引项目
        result = agent.execute(AgentAction.INDEX_PROJECT)

        # 分析文件
        result = agent.execute(AgentAction.ANALYZE_FILE, file_path="src/main.py")

        # 追踪调用链
        result = agent.execute(AgentAction.TRACE_CALLS, entity_name="authenticate")

        # 代码库问答
        answer = agent.ask("How does the authentication system work?")
    """

    def __init__(self, config: Optional[AgentConfig] = None):
        """
        初始化 Agent

        Args:
            config: Agent 配置
        """
        self.config = config or AgentConfig()

        # 初始化子系统
        self.parser = TreeSitterParser(self.config.languages)
        self.extractor = ASTExtractor()
        self.index_builder = IndexBuilder(
            parser=self.parser,
            extractor=self.extractor,
            exclude_dirs=set(self.config.exclude_dirs or []),
            max_file_size_mb=self.config.max_file_size_mb,
            parallel=self.config.parallel,
            max_workers=self.config.max_workers,
        )

        # LSM 存储
        lsm_cfg = self.config.lsm_config or LSMConfig()
        self.lsm_store = LSMStore(lsm_cfg)

        # 向量存储
        self.vector_store = VectorStore(
            embedding_model=self.config.embedding_model,
        )

        # 这些在索引完成后再初始化
        self._project_index: Optional[ProjectIndex] = None
        self._knowledge_graph: Optional[KnowledgeGraph] = None
        self._retriever: Optional[CodeRetriever] = None
        self._qa_engine: Optional[QACodeEngine] = None
        self._embedder: Optional[CodeEmbedder] = None

        self._initialized = False

    # ---- 公共 API ----

    def execute(
        self,
        action: AgentAction,
        **kwargs,
    ) -> AgentResult:
        """
        执行指定的 Agent 操作

        Args:
            action: 操作类型
            **kwargs: 操作参数

        Returns:
            AgentResult: 执行结果
        """
        start_time = time.time()

        handlers: Dict[AgentAction, Callable] = {
            AgentAction.INDEX_PROJECT: self._handle_index_project,
            AgentAction.ANALYZE_FILE: self._handle_analyze_file,
            AgentAction.EXPLAIN_CODE: self._handle_explain_code,
            AgentAction.TRACE_CALLS: self._handle_trace_calls,
            AgentAction.ANALYZE_IMPACT: self._handle_analyze_impact,
            AgentAction.FIND_BUGS: self._handle_find_bugs,
            AgentAction.GENERATE_DOCS: self._handle_generate_docs,
            AgentAction.SEARCH_CODEBASE: self._handle_search_codebase,
        }

        handler = handlers.get(action)
        if handler is None:
            return AgentResult(
                action=action,
                success=False,
                error=f"Unknown action: {action}",
            )

        try:
            data = handler(**kwargs)
            elapsed = (time.time() - start_time) * 1000
            return AgentResult(
                action=action,
                success=True,
                data=data,
                execution_time_ms=elapsed,
            )
        except Exception as e:
            elapsed = (time.time() - start_time) * 1000
            logger.error(f"执行 {action.value} 失败: {e}")
            return AgentResult(
                action=action,
                success=False,
                error=str(e),
                execution_time_ms=elapsed,
            )

    def ask(self, question: str, **kwargs) -> QAResponse:
        """
        代码库问答

        Args:
            question: 问题
            **kwargs: QAQuery 的其他参数

        Returns:
            QAResponse: 回答
        """
        self._ensure_initialized()

        query = QAQuery(
            question=question,
            **kwargs,
        )
        return self._qa_engine.ask(query)

    def ask_stream(self, question: str, **kwargs):
        """流式问答"""
        self._ensure_initialized()

        query = QAQuery(
            question=question,
            **kwargs,
        )
        yield from self._qa_engine.ask_with_stream(query)

    # ---- 操作处理器 ----

    def _handle_index_project(self, project_root: Optional[str] = None, **kwargs) -> ProjectIndex:
        """索引整个项目"""
        root = project_root or self.config.project_root
        logger.info(f"开始索引项目: {root}")

        self._project_index = self.index_builder.build_index(root)
        self._knowledge_graph = self._project_index.knowledge_graph

        # 初始化嵌入
        all_entities = []
        for entities in self._project_index.entities.values():
            all_entities.extend(entities)

        self._embedder = CodeEmbedder(self.vector_store)
        self._embedder.embed_entities(all_entities)

        # 加载源文件到检索器
        self._retriever = CodeRetriever(
            self.vector_store,
            self._knowledge_graph,
        )
        self._retriever.load_source_files(list(self._project_index.files.keys()))

        # 初始化 QA 引擎
        self._qa_engine = QACodeEngine(
            retriever=self._retriever,
            knowledge_graph=self._knowledge_graph,
            llm_client=self.config.llm_client,
        )

        # 保存索引到 LSM 存储
        with self.lsm_store:
            self.lsm_store.save_index(self._project_index.to_dict())

        self._initialized = True
        logger.info(f"项目索引完成: {self._project_index.total_files} 文件")

        return self._project_index

    def _handle_analyze_file(self, file_path: str, **kwargs) -> Dict[str, Any]:
        """分析单个文件"""
        self._ensure_initialized()

        entities, relations = self.extractor.extract_from_file(file_path, self.parser)
        module_summary = self._knowledge_graph.get_module_summary(file_path)

        return {
            "file_path": file_path,
            "language": Language.from_filename(file_path).value,
            "entities": [
                {
                    "name": e.name,
                    "type": e.type.value,
                    "line": e.location.start_line,
                    "signature": e.signature,
                    "complexity": e.complexity,
                }
                for e in entities
            ],
            "relations": [
                {
                    "source": r.source,
                    "target": r.target,
                    "type": r.type.value,
                }
                for r in relations
            ],
            "summary": module_summary,
        }

    def _handle_explain_code(self, code: str, language: str = "python", **kwargs) -> str:
        """解释代码"""
        self._ensure_initialized()

        lang = Language(language) if language in [l.value for l in Language] else Language.UNKNOWN
        parse_result = self.parser.parse_code(code, lang)
        entities, relations = self.extractor.extract(parse_result)

        query = QAQuery(
            question=f"Explain this code:\n```{language}\n{code}\n```",
            query_type=QueryType.CODE_EXPLANATION,
        )
        response = self._qa_engine.ask(query)
        return response.answer

    def _handle_trace_calls(self, entity_name: str, max_depth: int = 10, **kwargs) -> Dict[str, Any]:
        """追踪函数调用链"""
        self._ensure_initialized()

        call_chain = self._knowledge_graph.get_call_chain(entity_name, max_depth)
        callers = self._knowledge_graph.get_callers(entity_name)
        callees = self._knowledge_graph.get_callees(entity_name)

        return {
            "entity": entity_name,
            "callers": [
                {"name": c.name, "file": c.file_path, "line": c.start_line}
                for c in callers
            ],
            "callees": [
                {"name": c.name, "file": c.file_path, "line": c.start_line}
                for c in callees
            ],
            "call_chains": call_chain[:10],  # 最多返回 10 条路径
        }

    def _handle_analyze_impact(self, entity_name: str, **kwargs) -> Dict[str, Any]:
        """影响分析"""
        self._ensure_initialized()

        return self._knowledge_graph.get_impact_analysis(entity_name)

    def _handle_find_bugs(self, file_path: Optional[str] = None, **kwargs) -> List[Dict[str, Any]]:
        """查找潜在的代码问题"""
        self._ensure_initialized()

        issues = []

        if file_path:
            files_to_check = [file_path]
        else:
            files_to_check = list(self._project_index.files.keys()) if self._project_index else []

        for fp in files_to_check:
            entities, _ = self.extractor.extract_from_file(fp, self.parser)
            for entity in entities:
                # 检测高复杂度
                if entity.complexity > 15:
                    issues.append({
                        "type": "high_complexity",
                        "severity": "warning",
                        "file": fp,
                        "entity": entity.name,
                        "line": entity.location.start_line,
                        "message": f"High cyclomatic complexity ({entity.complexity})",
                        "suggestion": "Consider breaking this into smaller functions.",
                    })

                # 检测缺少文档
                if entity.type in (EntityType.FUNCTION, EntityType.METHOD, EntityType.CLASS):
                    if not entity.docstring:
                        issues.append({
                            "type": "missing_docs",
                            "severity": "info",
                            "file": fp,
                            "entity": entity.name,
                            "line": entity.location.start_line,
                            "message": f"Missing documentation for {entity.type.value}",
                            "suggestion": "Add a docstring describing the purpose and usage.",
                        })

                # 检测过长的参数列表
                if len(entity.parameters) > 7:
                    issues.append({
                        "type": "too_many_params",
                        "severity": "warning",
                        "file": fp,
                        "entity": entity.name,
                        "line": entity.location.start_line,
                        "message": f"Too many parameters ({len(entity.parameters)})",
                        "suggestion": "Consider grouping parameters into a data class or dictionary.",
                    })

        return issues

    def _handle_generate_docs(self, file_path: Optional[str] = None, **kwargs) -> str:
        """生成文档"""
        from .doc_generator import DocGenerator

        doc_gen = DocGenerator(self.parser, self.extractor)
        if file_path:
            return doc_gen.generate_file_doc(file_path)
        elif self._project_index:
            return doc_gen.generate_project_doc(
                self.config.project_root,
                list(self._project_index.files.keys()),
            )
        return ""

    def _handle_search_codebase(self, query: str, top_k: int = 10, **kwargs) -> List[Dict[str, Any]]:
        """搜索代码库"""
        self._ensure_initialized()

        results = self._retriever.retrieve(
            query,
            strategy=SearchStrategy.HYBRID,
            top_k=top_k,
        )

        return [
            {
                "content": r.content[:500],
                "score": r.score,
                "file": r.file_path,
                "line": r.line_number,
                "entity": r.entity_name,
                "type": r.entity_type,
            }
            for r in results
        ]

    def _handle_review_code(self, file_path: str, **kwargs) -> Dict[str, Any]:
        """代码审查"""
        self._ensure_initialized()

        entities, relations = self.extractor.extract_from_file(file_path, self.parser)

        # 结构分析
        structure_issues = []
        for entity in entities:
            if entity.complexity > 20:
                structure_issues.append(f"High complexity in {entity.name}: {entity.complexity}")
            if entity.type in (EntityType.FUNCTION, EntityType.METHOD) and not entity.docstring:
                structure_issues.append(f"Missing docstring: {entity.name}")

        # 依赖分析
        dependency_issues = []
        for relation in relations:
            if relation.type == RelationType.IMPORTS:
                pass  # 可扩展：检测循环依赖等

        return {
            "file": file_path,
            "total_entities": len(entities),
            "total_relations": len(relations),
            "structure_issues": structure_issues,
            "dependency_issues": dependency_issues,
            "score": max(0, 100 - len(structure_issues) * 5 - len(dependency_issues) * 3),
        }

    # ---- 辅助方法 ----

    def _ensure_initialized(self):
        """确保 Agent 已初始化"""
        if not self._initialized:
            if self._project_index is None:
                # 尝试从 LSM 存储加载
                try:
                    with self.lsm_store:
                        saved = self.lsm_store.load_index()
                        if saved.get("metadata"):
                            logger.info("从 LSM 存储加载索引...")
                            self._handle_index_project()
                            return
                except Exception:
                    pass

                # 自动索引
                logger.info("自动索引项目...")
                self._handle_index_project()

    def get_project_summary(self) -> Dict[str, Any]:
        """获取项目摘要"""
        self._ensure_initialized()

        return {
            "project_root": self.config.project_root,
            "total_files": self._project_index.total_files if self._project_index else 0,
            "total_entities": self._project_index.total_entities if self._project_index else 0,
            "languages": list(self._project_index.languages_detected) if self._project_index else [],
            "kg_stats": self._knowledge_graph.stats if self._knowledge_graph else {},
            "vector_store_stats": self.vector_store.stats,
        }

    def get_knowledge_graph(self) -> Optional[KnowledgeGraph]:
        """获取知识图谱（用于高级查询）"""
        self._ensure_initialized()
        return self._knowledge_graph

    def export_knowledge_graph(self, output_path: str, format: str = "json"):
        """导出知识图谱"""
        self._ensure_initialized()

        if format == "json":
            self._knowledge_graph.to_json(output_path)
        elif format == "html":
            self._knowledge_graph.to_html(output_path)
        else:
            raise ValueError(f"Unsupported format: {format}")

    def close(self):
        """关闭 Agent，释放资源"""
        if self.lsm_store:
            self.lsm_store.close()
        logger.info("Agent 已关闭")
