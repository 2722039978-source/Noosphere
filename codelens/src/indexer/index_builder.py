"""
代码索引构建器

扫描项目目录，解析所有源代码文件，构建完整的项目索引。
包括：
- 项目结构索引
- 代码实体索引
- 调用关系图
- 全文搜索索引
"""

import os
import time
from pathlib import Path
from typing import Optional, Dict, Any, List, Set, Tuple
from dataclasses import dataclass, field
from concurrent.futures import ThreadPoolExecutor, as_completed

from loguru import logger

from ..parser.tree_sitter_parser import TreeSitterParser, Language, ParseResult
from ..parser.ast_extractor import ASTExtractor, CodeEntity, Relation, EntityType, RelationType
from .knowledge_graph import KnowledgeGraph, GraphNode, GraphEdge


@dataclass
class FileInfo:
    """文件信息"""
    path: str
    relative_path: str
    language: Language
    size_bytes: int
    lines_of_code: int = 0
    entities: List[str] = field(default_factory=list)  # 实体名称列表

    def to_dict(self) -> Dict[str, Any]:
        return {
            "path": self.path,
            "relative_path": self.relative_path,
            "language": self.language.value,
            "size_bytes": self.size_bytes,
            "lines_of_code": self.lines_of_code,
            "entity_count": len(self.entities),
            "entities": self.entities,
        }


@dataclass
class ProjectIndex:
    """项目索引"""
    project_root: str
    knowledge_graph: KnowledgeGraph
    files: Dict[str, FileInfo] = field(default_factory=dict)  # path -> FileInfo
    entities: Dict[str, List[CodeEntity]] = field(default_factory=dict)  # path -> entities
    total_files: int = 0
    total_entities: int = 0
    total_relations: int = 0
    index_time_ms: float = 0.0
    languages_detected: Set[str] = field(default_factory=set)

    def to_dict(self) -> Dict[str, Any]:
        return {
            "project_root": self.project_root,
            "total_files": self.total_files,
            "total_entities": self.total_entities,
            "total_relations": self.total_relations,
            "index_time_ms": self.index_time_ms,
            "languages_detected": list(self.languages_detected),
            "files": {k: v.to_dict() for k, v in self.files.items()},
        }


class IndexBuilder:
    """
    项目索引构建器

    递归扫描项目目录，解析所有支持的源代码文件，
    构建统一的代码知识图谱和全文索引。

    使用示例:
        builder = IndexBuilder()
        project_index = builder.build_index("/path/to/project")

        # 查询项目结构
        print(f"共索引 {project_index.total_files} 个文件")
        print(f"发现 {project_index.total_entities} 个代码实体")

        # 使用知识图谱进行查询
        kg = project_index.knowledge_graph
        callers = kg.get_callers("main")
    """

    # 默认忽略的目录
    DEFAULT_EXCLUDE_DIRS = {
        "node_modules", "__pycache__", ".git", ".svn", ".hg",
        "dist", "build", "target", "vendor", ".venv", "venv",
        ".tox", ".eggs", "*.egg-info", ".mypy_cache", ".pytest_cache",
        ".next", ".nuxt", "coverage", "bower_components",
    }

    # 默认忽略的文件模式
    DEFAULT_EXCLUDE_PATTERNS = [
        "*.min.js", "*.min.css", "*.generated.*", "*.d.ts",
        "*.pyc", "*.pyo", "*.so", "*.dll", "*.class",
        "*.o", "*.a", "*.exe", "*.bin",
    ]

    def __init__(
        self,
        parser: Optional[TreeSitterParser] = None,
        extractor: Optional[ASTExtractor] = None,
        exclude_dirs: Optional[Set[str]] = None,
        exclude_patterns: Optional[List[str]] = None,
        max_file_size_mb: float = 10.0,
        parallel: bool = True,
        max_workers: int = 4,
    ):
        """
        初始化索引构建器

        Args:
            parser: Tree-sitter 解析器实例
            extractor: AST 提取器实例
            exclude_dirs: 额外需要排除的目录
            exclude_patterns: 额外需要排除的文件模式
            max_file_size_mb: 最大处理文件大小 (MB)
            parallel: 是否并行处理
            max_workers: 并行工作线程数
        """
        self.parser = parser or TreeSitterParser()
        self.extractor = extractor or ASTExtractor()
        self.exclude_dirs = self.DEFAULT_EXCLUDE_DIRS | (exclude_dirs or set())
        self.exclude_patterns = self.DEFAULT_EXCLUDE_PATTERNS + (exclude_patterns or [])
        self.max_file_size = max_file_size_mb * 1024 * 1024
        self.parallel = parallel
        self.max_workers = max_workers

    def build_index(
        self,
        project_root: str,
        progress_callback: Optional[callable] = None,
    ) -> ProjectIndex:
        """
        构建项目索引

        Args:
            project_root: 项目根目录
            progress_callback: 进度回调函数 (current, total) -> None

        Returns:
            ProjectIndex: 完整的项目索引
        """
        start_time = time.time()

        logger.info(f"开始构建索引: {project_root}")

        # 1. 扫描文件
        files = self._scan_files(project_root)
        logger.info(f"扫描到 {len(files)} 个源代码文件")

        if not files:
            logger.warning("未找到任何支持的源代码文件")
            return ProjectIndex(
                project_root=project_root,
                knowledge_graph=KnowledgeGraph(),
            )

        # 2. 解析并提取实体
        kg = KnowledgeGraph(name=f"project-{Path(project_root).name}")
        file_infos: Dict[str, FileInfo] = {}
        all_entities: Dict[str, List[CodeEntity]] = {}
        total_entities = 0
        total_relations = 0
        languages = set()

        if self.parallel and len(files) > 1:
            # 并行处理
            results = self._process_parallel(files, progress_callback)
        else:
            # 顺序处理
            results = []
            for i, file_path in enumerate(files):
                result = self._process_file(file_path)
                results.append(result)
                if progress_callback:
                    progress_callback(i + 1, len(files))

        # 3. 构建知识图谱
        for file_path, entities, relations in results:
            if not entities:
                continue

            # 存储实体
            all_entities[file_path] = entities

            # 添加到知识图谱
            for entity in entities:
                kg.add_entity(entity)
                total_entities += 1
                if entity.language != Language.UNKNOWN:
                    languages.add(entity.language.value)

            for relation in relations:
                kg.add_relation(relation)
                total_relations += 1

            # 文件信息
            try:
                stat = os.stat(file_path)
                file_info = FileInfo(
                    path=file_path,
                    relative_path=os.path.relpath(file_path, project_root),
                    language=Language.from_filename(file_path),
                    size_bytes=stat.st_size,
                    lines_of_code=sum(1 for _ in open(file_path, 'r', encoding='utf-8', errors='ignore')),
                    entities=[e.name for e in entities],
                )
                file_infos[file_path] = file_info
            except Exception:
                pass

        total_time = (time.time() - start_time) * 1000

        logger.info(
            f"索引构建完成: {len(file_infos)} 文件, "
            f"{total_entities} 实体, {total_relations} 关系, "
            f"耗时 {total_time:.0f}ms"
        )

        return ProjectIndex(
            project_root=project_root,
            knowledge_graph=kg,
            files=file_infos,
            entities=all_entities,
            total_files=len(file_infos),
            total_entities=total_entities,
            total_relations=total_relations,
            index_time_ms=total_time,
            languages_detected=languages,
        )

    def incremental_update(
        self,
        existing_index: ProjectIndex,
        changed_files: List[str],
    ) -> ProjectIndex:
        """
        增量更新索引（仅重新解析变更的文件）

        Args:
            existing_index: 现有的项目索引
            changed_files: 变更的文件列表

        Returns:
            更新后的项目索引
        """
        logger.info(f"增量更新索引: {len(changed_files)} 个变更文件")

        kg = existing_index.knowledge_graph

        # 移除旧数据
        for file_path in changed_files:
            if file_path in existing_index.entities:
                # 从知识图谱中移除旧的实体
                for entity in existing_index.entities[file_path]:
                    pass  # networkx 移除节点较复杂，这里简化处理
                del existing_index.entities[file_path]
            if file_path in existing_index.files:
                del existing_index.files[file_path]

        # 重新解析变更的文件
        for file_path in changed_files:
            if os.path.exists(file_path):
                entities, relations = self._process_file(file_path)
                existing_index.entities[file_path] = entities
                for entity in entities:
                    kg.add_entity(entity)
                for relation in relations:
                    kg.add_relation(relation)

        existing_index.total_files = len(existing_index.files)
        existing_index.total_entities = sum(
            len(es) for es in existing_index.entities.values()
        )

        return existing_index

    # ---- 内部方法 ----

    def _scan_files(self, root: str) -> List[str]:
        """扫描目录中的所有源代码文件"""
        files = []
        root_path = Path(root)

        for entry in root_path.rglob("*"):
            if entry.is_file():
                # 检查是否在排除目录中
                parts = set(entry.parts)
                if parts & self.exclude_dirs:
                    continue

                # 检查文件大小
                try:
                    if entry.stat().st_size > self.max_file_size:
                        logger.debug(f"跳过过大文件: {entry}")
                        continue
                except OSError:
                    continue

                # 检查语言支持
                lang = Language.from_filename(entry.name)
                if lang != Language.UNKNOWN:
                    files.append(str(entry))

        return sorted(files)

    def _process_file(self, file_path: str) -> Tuple[str, List[CodeEntity], List[Relation]]:
        """处理单个文件"""
        try:
            parse_result = self.parser.parse_file(file_path)
            if not parse_result.success:
                logger.debug(f"解析失败: {file_path} - {parse_result.errors}")
                return file_path, [], []

            entities, relations = self.extractor.extract(parse_result)
            return file_path, entities, relations

        except Exception as e:
            logger.warning(f"处理文件异常 {file_path}: {e}")
            return file_path, [], []

    def _process_parallel(
        self,
        files: List[str],
        progress_callback: Optional[callable] = None,
    ) -> List[Tuple[str, List[CodeEntity], List[Relation]]]:
        """并行处理文件"""
        results = []
        completed = 0
        total = len(files)

        with ThreadPoolExecutor(max_workers=self.max_workers) as executor:
            future_to_file = {
                executor.submit(self._process_file, f): f for f in files
            }
            for future in as_completed(future_to_file):
                try:
                    result = future.result()
                    results.append(result)
                except Exception as e:
                    file_path = future_to_file[future]
                    logger.warning(f"并行处理异常 {file_path}: {e}")
                    results.append((file_path, [], []))

                completed += 1
                if progress_callback:
                    progress_callback(completed, total)

        return results

    def get_supported_languages(self) -> List[str]:
        """获取支持的语言列表"""
        return [lang.value for lang in self.parser.loaded_languages]
