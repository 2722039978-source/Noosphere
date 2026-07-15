"""
向量存储模块

用于存储代码片段的向量嵌入，支持语义相似度搜索。
基于 ChromaDB 实现，支持持久化存储和高效检索。
"""

import os
import struct
import hashlib
from pathlib import Path
from typing import Optional, Dict, Any, List, Tuple, Union
from dataclasses import dataclass, field

from loguru import logger


@dataclass
class VectorDoc:
    """向量文档"""
    id: str
    content: str
    metadata: Dict[str, Any] = field(default_factory=dict)
    embedding: Optional[List[float]] = None
    score: float = 0.0

    def to_dict(self) -> Dict[str, Any]:
        return {
            "id": self.id,
            "content": self.content,
            "metadata": self.metadata,
            "score": self.score,
        }


class VectorStore:
    """
    向量存储

    基于 ChromaDB 的向量存储实现，用于代码片段的语义检索。

    特性：
    - 持久化存储
    - 多种嵌入模型支持
    - 元数据过滤
    - 批量操作
    - 相似度搜索

    使用示例:
        store = VectorStore(persist_path="./data/vectors")

        # 添加文档
        store.add_documents([
            VectorDoc(id="1", content="def foo(): pass",
                     metadata={"type": "function", "file": "main.py"})
        ])

        # 语义搜索
        results = store.search("find all database functions", top_k=5)
    """

    def __init__(
        self,
        persist_path: str = "./data/vector_store",
        embedding_model: str = "all-MiniLM-L6-v2",
        collection_name: str = "code_snippets",
    ):
        """
        初始化向量存储

        Args:
            persist_path: 持久化路径
            embedding_model: 嵌入模型名称
            collection_name: 集合名称
        """
        self.persist_path = persist_path
        self.embedding_model_name = embedding_model
        self.collection_name = collection_name
        self._embedding_model = None
        self._client = None
        self._collection = None
        self._fallback_mode = False

        self._init_store()

    def _init_store(self):
        """初始化向量存储后端"""
        os.makedirs(self.persist_path, exist_ok=True)

        # 尝试使用 ChromaDB
        try:
            import chromadb
            from chromadb.config import Settings

            self._client = chromadb.PersistentClient(
                path=self.persist_path,
                settings=Settings(anonymized_telemetry=False),
            )

            # 获取或创建集合
            try:
                self._collection = self._client.get_collection(
                    name=self.collection_name
                )
                logger.info(f"加载已有集合: {self.collection_name}")
            except Exception:
                self._collection = self._client.create_collection(
                    name=self.collection_name,
                    metadata={"hnsw:space": "cosine"},
                )
                logger.info(f"创建新集合: {self.collection_name}")

            self._fallback_mode = False
            logger.info("使用 ChromaDB 向量存储后端")

        except ImportError:
            logger.warning("ChromaDB 未安装，使用内置向量存储")
            self._fallback_mode = True
            self._init_fallback()
        except Exception as e:
            logger.warning(f"ChromaDB 初始化失败: {e}，使用内置向量存储")
            self._fallback_mode = True
            self._init_fallback()

    def _init_fallback(self):
        """初始化内置回退向量存储"""
        import json

        self._fallback_data_file = os.path.join(self.persist_path, "vectors.json")
        self._fallback_index_file = os.path.join(self.persist_path, "index.json")

        # 加载已有数据
        if os.path.exists(self._fallback_data_file):
            with open(self._fallback_data_file, "r", encoding="utf-8") as f:
                self._fallback_docs: Dict[str, Dict] = json.load(f)
        else:
            self._fallback_docs: Dict[str, Dict] = {}

        self._fallback_embeddings: Dict[str, List[float]] = {}

    def _get_embedding_model(self):
        """懒加载嵌入模型"""
        if self._embedding_model is not None:
            return self._embedding_model

        try:
            from sentence_transformers import SentenceTransformer
            self._embedding_model = SentenceTransformer(self.embedding_model_name)
            logger.info(f"加载嵌入模型: {self.embedding_model_name}")
        except ImportError:
            logger.warning("sentence-transformers 未安装，使用简单哈希嵌入")
            self._embedding_model = "hash"
        except Exception as e:
            logger.warning(f"嵌入模型加载失败: {e}，使用简单哈希嵌入")
            self._embedding_model = "hash"

        return self._embedding_model

    def _compute_embedding(self, text: str) -> List[float]:
        """计算文本嵌入"""
        model = self._get_embedding_model()

        if model == "hash":
            # 简单哈希嵌入（回退方案）
            hash_bytes = hashlib.sha256(text.encode()).digest()
            # 转为 128 维浮点向量
            embedding = []
            for i in range(0, 32, 4):
                val = struct.unpack('f', hash_bytes[i:i+4])[0]
                embedding.append(val)
            # 归一化
            import math
            norm = math.sqrt(sum(v * v for v in embedding))
            return [v / max(norm, 1e-8) for v in embedding]
        else:
            return model.encode(text).tolist()

    def _compute_embeddings_batch(self, texts: List[str]) -> List[List[float]]:
        """批量计算嵌入"""
        model = self._get_embedding_model()

        if model == "hash":
            return [self._compute_embedding(t) for t in texts]
        else:
            return model.encode(texts).tolist()

    # ---- 公共 API ----

    def add_documents(self, documents: List[VectorDoc]) -> bool:
        """
        添加文档到向量存储

        Args:
            documents: 文档列表

        Returns:
            是否成功
        """
        if not documents:
            return True

        try:
            texts = [doc.content for doc in documents]
            ids = [doc.id for doc in documents]
            metadatas = [doc.metadata for doc in documents]
            embeddings = self._compute_embeddings_batch(texts)

            if self._fallback_mode:
                return self._add_fallback(documents, embeddings)

            # ChromaDB 方式
            self._collection.add(
                ids=ids,
                documents=texts,
                metadatas=metadatas,
                embeddings=embeddings,
            )
            logger.debug(f"添加 {len(documents)} 个文档到向量存储")
            return True

        except Exception as e:
            logger.error(f"添加文档失败: {e}")
            return False

    def search(
        self,
        query: str,
        top_k: int = 10,
        filter_metadata: Optional[Dict[str, str]] = None,
    ) -> List[VectorDoc]:
        """
        语义搜索

        Args:
            query: 搜索查询
            top_k: 返回结果数
            filter_metadata: 元数据过滤条件

        Returns:
            匹配的文档列表（按相似度降序）
        """
        try:
            query_embedding = self._compute_embedding(query)

            if self._fallback_mode:
                return self._search_fallback(query_embedding, top_k, filter_metadata)

            # ChromaDB 方式
            where_filter = filter_metadata if filter_metadata else None
            results = self._collection.query(
                query_embeddings=[query_embedding],
                n_results=top_k,
                where=where_filter,
            )

            docs = []
            if results and results["ids"] and results["ids"][0]:
                for i, doc_id in enumerate(results["ids"][0]):
                    doc = VectorDoc(
                        id=doc_id,
                        content=results["documents"][0][i] if results["documents"] else "",
                        metadata=results["metadatas"][0][i] if results["metadatas"] else {},
                        score=1 - results["distances"][0][i] if results["distances"] else 0,
                    )
                    docs.append(doc)

            return docs

        except Exception as e:
            logger.error(f"搜索失败: {e}")
            return []

    def delete(self, doc_ids: List[str]) -> bool:
        """删除文档"""
        try:
            if self._fallback_mode:
                for doc_id in doc_ids:
                    self._fallback_docs.pop(doc_id, None)
                    self._fallback_embeddings.pop(doc_id, None)
                self._save_fallback()
                return True

            self._collection.delete(ids=doc_ids)
            return True
        except Exception as e:
            logger.error(f"删除文档失败: {e}")
            return False

    def get(self, doc_ids: List[str]) -> List[VectorDoc]:
        """获取指定 ID 的文档"""
        try:
            if self._fallback_mode:
                return [
                    VectorDoc(
                        id=doc_id,
                        content=self._fallback_docs[doc_id]["content"],
                        metadata=self._fallback_docs[doc_id].get("metadata", {}),
                    )
                    for doc_id in doc_ids
                    if doc_id in self._fallback_docs
                ]

            results = self._collection.get(ids=doc_ids)
            docs = []
            if results and results["ids"]:
                for i, doc_id in enumerate(results["ids"]):
                    docs.append(VectorDoc(
                        id=doc_id,
                        content=results["documents"][i] if results["documents"] else "",
                        metadata=results["metadatas"][i] if results["metadatas"] else {},
                    ))
            return docs

        except Exception as e:
            logger.error(f"获取文档失败: {e}")
            return []

    def count(self) -> int:
        """获取文档总数"""
        try:
            if self._fallback_mode:
                return len(self._fallback_docs)
            return self._collection.count()
        except Exception:
            return 0

    def clear(self):
        """清空所有文档"""
        try:
            if self._fallback_mode:
                self._fallback_docs = {}
                self._fallback_embeddings = {}
                self._save_fallback()
                return

            # 删除并重建集合
            self._client.delete_collection(self.collection_name)
            self._collection = self._client.create_collection(
                name=self.collection_name,
                metadata={"hnsw:space": "cosine"},
            )
        except Exception as e:
            logger.error(f"清空向量存储失败: {e}")

    # ---- 面向代码的特殊方法 ----

    def index_code_snippets(
        self,
        snippets: List[Dict[str, Any]],
        chunk_size: int = 1000,
        chunk_overlap: int = 200,
    ) -> int:
        """
        索引代码片段

        自动将长代码分块并添加到向量存储。

        Args:
            snippets: 代码片段列表，每个包含 'code', 'file', 'name', 'type'
            chunk_size: 分块大小
            chunk_overlap: 分块重叠

        Returns:
            添加的文档总数
        """
        documents = []
        for snippet in snippets:
            code = snippet.get("code", "")
            chunks = self._chunk_code(code, chunk_size, chunk_overlap)
            for i, chunk in enumerate(chunks):
                doc_id = f"{snippet.get('file', 'unknown')}:{snippet.get('name', 'unknown')}:chunk{i}"
                documents.append(VectorDoc(
                    id=doc_id,
                    content=chunk,
                    metadata={
                        "file": snippet.get("file", ""),
                        "name": snippet.get("name", ""),
                        "type": snippet.get("type", ""),
                        "language": snippet.get("language", ""),
                        "chunk_index": i,
                        "total_chunks": len(chunks),
                    },
                ))

        self.add_documents(documents)
        return len(documents)

    def search_code(
        self,
        query: str,
        top_k: int = 10,
        language: Optional[str] = None,
        entity_type: Optional[str] = None,
    ) -> List[VectorDoc]:
        """
        搜索代码片段

        Args:
            query: 搜索查询
            top_k: 返回结果数
            language: 过滤语言
            entity_type: 过滤实体类型

        Returns:
            匹配的代码片段
        """
        filter_meta = {}
        if language:
            filter_meta["language"] = language
        if entity_type:
            filter_meta["type"] = entity_type

        return self.search(query, top_k, filter_meta if filter_meta else None)

    # ---- 回退存储方法 ----

    def _add_fallback(self, documents: List[VectorDoc], embeddings: List[List[float]]) -> bool:
        """回退存储添加"""
        import json

        for doc, emb in zip(documents, embeddings):
            self._fallback_docs[doc.id] = {
                "content": doc.content,
                "metadata": doc.metadata,
            }
            self._fallback_embeddings[doc.id] = emb

        self._save_fallback()
        return True

    def _search_fallback(
        self,
        query_embedding: List[float],
        top_k: int,
        filter_metadata: Optional[Dict[str, str]],
    ) -> List[VectorDoc]:
        """回退存储搜索"""
        import math

        def cosine_similarity(a, b):
            dot = sum(x * y for x, y in zip(a, b))
            norm_a = math.sqrt(sum(x * x for x in a))
            norm_b = math.sqrt(sum(y * y for y in b))
            return dot / (norm_a * norm_b + 1e-8)

        scores = []
        for doc_id, emb in self._fallback_embeddings.items():
            doc_data = self._fallback_docs.get(doc_id, {})
            # 元数据过滤
            if filter_metadata:
                meta = doc_data.get("metadata", {})
                match = all(meta.get(k) == v for k, v in filter_metadata.items())
                if not match:
                    continue

            score = cosine_similarity(query_embedding, emb)
            scores.append((doc_id, score))

        scores.sort(key=lambda x: x[1], reverse=True)
        top_results = scores[:top_k]

        docs = []
        for doc_id, score in top_results:
            doc_data = self._fallback_docs.get(doc_id, {})
            docs.append(VectorDoc(
                id=doc_id,
                content=doc_data.get("content", ""),
                metadata=doc_data.get("metadata", {}),
                score=score,
            ))

        return docs

    def _save_fallback(self):
        """保存回退数据到磁盘"""
        import json

        with open(self._fallback_data_file, "w", encoding="utf-8") as f:
            json.dump(self._fallback_docs, f, ensure_ascii=False, indent=2)

        # 嵌入向量单独存储（可能很大）
        with open(self._fallback_index_file, "w", encoding="utf-8") as f:
            json.dump(self._fallback_embeddings, f, ensure_ascii=False)

    @staticmethod
    def _chunk_code(code: str, chunk_size: int, overlap: int) -> List[str]:
        """将代码分成多个重叠的块"""
        lines = code.split("\n")
        chunks = []
        i = 0
        while i < len(lines):
            chunk_lines = lines[i:i + chunk_size]
            chunks.append("\n".join(chunk_lines))
            i += chunk_size - overlap
            if i <= 0:  # 防止无限循环
                i = chunk_size
        return chunks

    @property
    def stats(self) -> Dict[str, Any]:
        """获取存储统计"""
        return {
            "collection": self.collection_name,
            "document_count": self.count(),
            "embedding_model": self.embedding_model_name,
            "persist_path": self.persist_path,
            "backend": "fallback" if self._fallback_mode else "chromadb",
        }
