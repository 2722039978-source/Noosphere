"""
代码嵌入模块

将代码片段转换为语义向量，支持：
- 函数级嵌入
- 类级嵌入
- 文件级嵌入
- 文档字符串嵌入
- 代码+文档联合嵌入
"""

from typing import Optional, Dict, Any, List, Tuple
from dataclasses import dataclass

from loguru import logger

from ..parser.ast_extractor import CodeEntity, EntityType
from ..storage.vector_store import VectorStore, VectorDoc


@dataclass
class EmbeddingConfig:
    """嵌入配置"""
    model_name: str = "all-MiniLM-L6-v2"
    include_docstring: bool = True
    include_signature: bool = True
    include_body: bool = False      # 是否包含函数体（大模型适用）
    max_body_lines: int = 20
    chunk_size: int = 1000
    chunk_overlap: int = 200


class CodeEmbedder:
    """
    代码嵌入器

    将代码实体转换为向量嵌入，用于语义搜索和检索。

    支持多种嵌入策略：
    - 签名嵌入：仅使用函数/类签名
    - 签名+文档嵌入：签名和文档字符串
    - 完整嵌入：签名、文档和部分实现
    - 上下文嵌入：包含所在文件和模块信息

    使用示例:
        embedder = CodeEmbedder(vector_store)
        embedder.embed_entities(entities)

        # 语义搜索
        results = vector_store.search_code("find database connection functions")
    """

    def __init__(
        self,
        vector_store: VectorStore,
        config: Optional[EmbeddingConfig] = None,
    ):
        """
        初始化代码嵌入器

        Args:
            vector_store: 向量存储实例
            config: 嵌入配置
        """
        self.vector_store = vector_store
        self.config = config or EmbeddingConfig()

    def embed_entity(self, entity: CodeEntity) -> Optional[str]:
        """
        嵌入单个代码实体

        Args:
            entity: 代码实体

        Returns:
            文档 ID，失败返回 None
        """
        text = self._entity_to_text(entity)
        if not text.strip():
            return None

        doc_id = f"{entity.location.file_path}::{entity.name}::{entity.type.value}"
        doc = VectorDoc(
            id=doc_id,
            content=text,
            metadata={
                "name": entity.name,
                "type": entity.type.value,
                "language": entity.language.value,
                "file": entity.location.file_path,
                "start_line": entity.location.start_line,
                "signature": entity.signature,
                "complexity": entity.complexity,
            },
        )

        success = self.vector_store.add_documents([doc])
        return doc_id if success else None

    def embed_entities(self, entities: List[CodeEntity]) -> int:
        """
        批量嵌入代码实体

        Args:
            entities: 实体列表

        Returns:
            成功嵌入的实体数
        """
        if not entities:
            return 0

        documents = []
        for entity in entities:
            text = self._entity_to_text(entity)
            if not text.strip():
                continue

            documents.append(VectorDoc(
                id=f"{entity.location.file_path}::{entity.name}::{entity.type.value}",
                content=text,
                metadata={
                    "name": entity.name,
                    "type": entity.type.value,
                    "language": entity.language.value,
                    "file": entity.location.file_path,
                    "start_line": entity.location.start_line,
                    "signature": entity.signature,
                    "complexity": entity.complexity,
                },
            ))

        if documents:
            self.vector_store.add_documents(documents)
            logger.info(f"嵌入 {len(documents)} 个代码实体")

        return len(documents)

    def embed_file_context(self, file_path: str, entities: List[CodeEntity]) -> Optional[str]:
        """
        嵌入文件级上下文

        创建文件的整体描述，包含所有顶层实体。

        Args:
            file_path: 文件路径
            entities: 文件中的实体列表

        Returns:
            文档 ID
        """
        parts = [f"# File: {file_path}\n"]

        # 按类型分组
        imports = [e for e in entities if e.type == EntityType.IMPORT]
        functions = [e for e in entities if e.type == EntityType.FUNCTION]
        classes = [e for e in entities if e.type == EntityType.CLASS]
        variables = [e for e in entities if e.type == EntityType.VARIABLE]

        if imports:
            parts.append("## Imports")
            for e in imports[:20]:
                parts.append(f"- {e.name}")

        if classes:
            parts.append("\n## Classes")
            for e in classes:
                parts.append(f"- class {e.name}")
                if e.docstring:
                    parts.append(f"  {e.docstring[:200]}")

        if functions:
            parts.append("\n## Functions")
            for e in functions:
                parts.append(f"- {e.signature or e.name}")
                if e.docstring:
                    parts.append(f"  {e.docstring[:200]}")

        if variables:
            parts.append("\n## Variables")
            for e in variables[:20]:
                parts.append(f"- {e.name}")

        text = "\n".join(parts)
        doc = VectorDoc(
            id=f"__file_context__:{file_path}",
            content=text,
            metadata={
                "type": "file_context",
                "file": file_path,
                "entity_count": len(entities),
            },
        )

        self.vector_store.add_documents([doc])
        return doc.id

    def embed_code_snippet(self, code: str, file_path: str, language: str) -> List[str]:
        """
        嵌入原始代码片段（自动分块）

        Args:
            code: 源代码
            file_path: 文件路径
            language: 编程语言

        Returns:
            文档 ID 列表
        """
        lines = code.split("\n")
        chunks = []
        for i in range(0, len(lines), self.config.chunk_size - self.config.chunk_overlap):
            chunk = "\n".join(lines[i:i + self.config.chunk_size])
            chunks.append(chunk)

        documents = []
        for j, chunk in enumerate(chunks):
            doc_id = f"__snippet__:{file_path}:chunk{j}"
            documents.append(VectorDoc(
                id=doc_id,
                content=f"```{language}\n{chunk}\n```",
                metadata={
                    "type": "code_snippet",
                    "file": file_path,
                    "language": language,
                    "chunk_index": j,
                    "total_chunks": len(chunks),
                },
            ))

        if documents:
            self.vector_store.add_documents(documents)

        return [d.id for d in documents]

    # ---- 内部方法 ----

    def _entity_to_text(self, entity: CodeEntity) -> str:
        """将实体转换为嵌入文本"""
        parts = []

        # 类型和名称
        parts.append(f"[{entity.type.value.upper()}] {entity.name}")

        # 签名
        if self.config.include_signature and entity.signature:
            parts.append(entity.signature)

        # 参数
        if entity.parameters:
            param_strs = []
            for p in entity.parameters[:10]:
                if p.get("type"):
                    param_strs.append(f"{p['name']}: {p['type']}")
                else:
                    param_strs.append(p["name"])
            parts.append(f"Parameters: {', '.join(param_strs)}")

        # 返回类型
        if entity.return_type:
            parts.append(f"Returns: {entity.return_type}")

        # 文档字符串
        if self.config.include_docstring and entity.docstring:
            # 清理文档字符串
            doc = entity.docstring.strip().strip('"').strip("'")
            parts.append(f"Documentation: {doc}")

        # 修饰符
        if entity.modifiers:
            parts.append(f"Modifiers: {', '.join(entity.modifiers)}")

        # 基类
        if entity.base_classes:
            parts.append(f"Inherits from: {', '.join(entity.base_classes)}")

        # 装饰器
        if entity.decorators:
            parts.append(f"Decorators: {', '.join(entity.decorators)}")

        # 函数体（可选）
        if self.config.include_body and entity.body_summary:
            body_lines = entity.body_summary.split("\n")[:self.config.max_body_lines]
            parts.append(f"Body:\n" + "\n".join(body_lines))

        # 复杂度
        if entity.complexity > 0:
            parts.append(f"Complexity: {entity.complexity}")

        # 位置信息
        parts.append(f"Defined in: {entity.location}")

        return "\n".join(parts)
