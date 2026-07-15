"""
LSM-Tree KV 存储模块

基于 LSM-Tree (Log-Structured Merge-Tree) 的键值存储，
用于高效存储和检索项目索引数据和分析结果。

支持多种后端：
- LevelDB (via plyvel)
- RocksDB (via python-rocksdb)
- SQLite LSM 扩展
- 内置纯 Python LSM 实现（回退方案）

LSM-Tree 特性：
- 高写入吞吐量（顺序写入 WAL）
- 分层合并策略（Leveled Compaction）
- 范围查询支持
- 内存 MemTable + 磁盘 SSTable 架构
"""

import os
import json
import pickle
import sqlite3
import struct
from pathlib import Path
from typing import Optional, Dict, Any, List, Iterator, Tuple, Union
from dataclasses import dataclass, field
from collections import OrderedDict

from loguru import logger


@dataclass
class LSMConfig:
    """LSM 存储配置"""
    # 存储引擎: "auto", "leveldb", "rocksdb", "sqlite_lsm", "builtin"
    engine: str = "auto"
    db_path: str = "./data/lsm_store"
    # MemTable 最大大小 (MB)
    memtable_size_mb: int = 64
    # 每层文件数量上限
    max_files_per_level: int = 10
    # 压缩级别数
    num_levels: int = 7
    # 是否使用 Bloom Filter
    use_bloom_filter: bool = True
    # 缓存大小 (MB)
    cache_size_mb: int = 256
    # 写入缓冲区大小 (MB)
    write_buffer_mb: int = 64
    # 是否启用压缩
    compression: bool = True
    # 是否同步写入
    sync_write: bool = False


class BuiltinLSM:
    """
    内置的纯 Python LSM-Tree 实现

    基于 SQLite 模拟 LSM-Tree 的分层存储行为：
    - Level 0: 内存中的 MemTable (OrderedDict)
    - Level 1-N: SQLite 表中的分层数据

    使用 WAL 模式保证写入持久性。
    """

    def __init__(self, db_path: str, config: LSMConfig):
        self.db_path = db_path
        self.config = config
        self._memtable: OrderedDict = OrderedDict()
        self._memtable_max_size = config.memtable_size_mb * 1024 * 1024
        self._conn: Optional[sqlite3.Connection] = None
        self._setup_database()

    def _setup_database(self):
        """初始化数据库"""
        os.makedirs(self.db_path, exist_ok=True)
        db_file = os.path.join(self.db_path, "lsm_store.db")

        self._conn = sqlite3.connect(db_file, check_same_thread=False)
        self._conn.execute("PRAGMA journal_mode=WAL")
        self._conn.execute("PRAGMA synchronous=NORMAL")
        self._conn.execute("PRAGMA cache_size=-{}".format(
            self.config.cache_size_mb * 1024
        ))
        self._conn.execute("PRAGMA mmap_size={}".format(
            self.config.cache_size_mb * 1024 * 1024
        ))

        # 创建分层存储表
        self._conn.execute("""
            CREATE TABLE IF NOT EXISTS lsm_sstables (
                level INTEGER NOT NULL,
                key BLOB NOT NULL,
                value BLOB NOT NULL,
                timestamp INTEGER NOT NULL DEFAULT (strftime('%s','now')),
                PRIMARY KEY (level, key)
            )
        """)
        self._conn.execute("""
            CREATE INDEX IF NOT EXISTS idx_lsm_key
            ON lsm_sstables(key)
        """)
        self._conn.execute("""
            CREATE TABLE IF NOT EXISTS lsm_metadata (
                key TEXT PRIMARY KEY,
                value TEXT NOT NULL
            )
        """)
        self._conn.commit()

    def put(self, key: Union[str, bytes], value: Union[str, bytes, dict, list]) -> bool:
        """写入键值对"""
        key_bytes = key.encode("utf-8") if isinstance(key, str) else key
        value_bytes = self._serialize(value)

        # 写入 MemTable
        self._memtable[key_bytes] = value_bytes
        # 同时记录 key 和 value 的粗略大小
        self._memtable_max_size -= len(key_bytes) + len(value_bytes)

        # MemTable 满了就 flush
        if self._memtable_max_size <= 0:
            self._flush_memtable()

        return True

    def get(self, key: Union[str, bytes]) -> Optional[Any]:
        """读取键值对"""
        key_bytes = key.encode("utf-8") if isinstance(key, str) else key

        # 先在 MemTable 中查找
        if key_bytes in self._memtable:
            return self._deserialize(self._memtable[key_bytes])

        # 再在 SSTable 中查找
        cursor = self._conn.execute(
            "SELECT value FROM lsm_sstables WHERE key = ? ORDER BY level LIMIT 1",
            (key_bytes,)
        )
        row = cursor.fetchone()
        if row:
            return self._deserialize(row[0])

        return None

    def delete(self, key: Union[str, bytes]) -> bool:
        """删除键值对"""
        key_bytes = key.encode("utf-8") if isinstance(key, str) else key

        # 从 MemTable 中删除
        self._memtable.pop(key_bytes, None)

        # 在 SSTable 中标记为删除（写入空值 tombstone）
        cursor = self._conn.execute(
            "DELETE FROM lsm_sstables WHERE key = ?",
            (key_bytes,)
        )
        self._conn.commit()
        return cursor.rowcount > 0

    def range_query(
        self,
        start_key: Union[str, bytes],
        end_key: Union[str, bytes],
        limit: int = 100,
    ) -> List[Tuple[bytes, Any]]:
        """范围查询"""
        start_bytes = start_key.encode("utf-8") if isinstance(start_key, str) else start_key
        end_bytes = end_key.encode("utf-8") if isinstance(end_key, str) else end_key

        results = []

        # 从 MemTable 中查询
        for key, value in self._memtable.items():
            if start_bytes <= key <= end_bytes:
                results.append((key, self._deserialize(value)))
                if len(results) >= limit:
                    return results[:limit]

        # 从 SSTable 中查询
        cursor = self._conn.execute(
            "SELECT key, value FROM lsm_sstables "
            "WHERE key >= ? AND key <= ? "
            "ORDER BY key LIMIT ?",
            (start_bytes, end_bytes, limit - len(results))
        )
        for row in cursor:
            results.append((row[0], self._deserialize(row[1])))

        return results[:limit]

    def scan(self, prefix: Union[str, bytes], limit: int = 100) -> List[Tuple[bytes, Any]]:
        """前缀扫描"""
        prefix_bytes = prefix.encode("utf-8") if isinstance(prefix, str) else prefix

        # 构造前缀范围
        end_bytes = prefix_bytes[:-1] + bytes([prefix_bytes[-1] + 1]) if prefix_bytes else b"\xff"

        return self.range_query(prefix_bytes, end_bytes, limit)

    def _flush_memtable(self):
        """将 MemTable 写入 SSTable"""
        if not self._memtable:
            return

        logger.debug(f"Flush MemTable: {len(self._memtable)} entries")

        # 批量写入 Level 0
        data = [(0, k, v) for k, v in self._memtable.items()]
        self._conn.executemany(
            "INSERT OR REPLACE INTO lsm_sstables (level, key, value) VALUES (?, ?, ?)",
            data
        )
        self._conn.commit()

        # 清空 MemTable
        self._memtable.clear()
        self._memtable_max_size = self.config.memtable_size_mb * 1024 * 1024

        # 触发合并
        self._maybe_compact()

    def _maybe_compact(self):
        """检查是否需要执行分层合并"""
        for level in range(self.config.num_levels - 1):
            cursor = self._conn.execute(
                "SELECT COUNT(*) FROM lsm_sstables WHERE level = ?", (level,)
            )
            count = cursor.fetchone()[0]
            if count > self.config.max_files_per_level:
                self._compact_level(level)
                break

    def _compact_level(self, level: int):
        """执行单层合并"""
        logger.debug(f"Compacting level {level}")

        # 将当前 level 的数据合并到下一层
        self._conn.execute("""
            INSERT OR REPLACE INTO lsm_sstables (level, key, value, timestamp)
            SELECT ?, key, value, timestamp
            FROM lsm_sstables
            WHERE level = ?
        """, (level + 1, level))

        self._conn.execute(
            "DELETE FROM lsm_sstables WHERE level = ?", (level,)
        )
        self._conn.commit()

    def put_batch(self, items: List[Tuple[Union[str, bytes], Any]]) -> bool:
        """批量写入"""
        for key, value in items:
            self.put(key, value)
        self._flush_memtable()
        return True

    def get_metadata(self, key: str) -> Optional[str]:
        """获取元数据"""
        cursor = self._conn.execute(
            "SELECT value FROM lsm_metadata WHERE key = ?", (key,)
        )
        row = cursor.fetchone()
        return row[0] if row else None

    def set_metadata(self, key: str, value: str):
        """设置元数据"""
        self._conn.execute(
            "INSERT OR REPLACE INTO lsm_metadata (key, value) VALUES (?, ?)",
            (key, value)
        )
        self._conn.commit()

    @property
    def stats(self) -> Dict[str, Any]:
        """获取存储统计"""
        cursor = self._conn.execute(
            "SELECT level, COUNT(*) FROM lsm_sstables GROUP BY level"
        )
        level_counts = {f"level_{row[0]}": row[1] for row in cursor}
        return {
            "memtable_size": len(self._memtable),
            "memtable_max": self.config.memtable_size_mb * 1024 * 1024,
            "levels": level_counts,
        }

    @staticmethod
    def _serialize(value: Any) -> bytes:
        """序列化值"""
        if isinstance(value, bytes):
            return value
        if isinstance(value, str):
            return value.encode("utf-8")
        return pickle.dumps(value)

    @staticmethod
    def _deserialize(data: bytes) -> Any:
        """反序列化值"""
        try:
            return data.decode("utf-8")
        except UnicodeDecodeError:
            try:
                return pickle.loads(data)
            except Exception:
                return data

    def close(self):
        """关闭存储"""
        self._flush_memtable()
        if self._conn:
            self._conn.commit()
            self._conn.close()
            self._conn = None

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        self.close()


class LSMStore:
    """
    LSM-Tree KV 存储统一接口

    自动选择最佳的后端存储引擎，回退到内置实现。

    使用示例:
        store = LSMStore(LSMConfig(db_path="./data/store"))
        with store:
            store.put("project:name", "MyProject")
            store.put("entity:main", {"type": "function", "file": "main.py"})

            value = store.get("entity:main")
            print(value)

            # 前缀扫描
            entities = store.scan("entity:")
    """

    def __init__(self, config: Optional[LSMConfig] = None):
        """
        初始化 LSM 存储

        Args:
            config: LSM 配置，None 使用默认配置
        """
        self.config = config or LSMConfig()
        self._backend = None
        self._setup_backend()

    def _setup_backend(self):
        """选择并初始化后端存储引擎"""
        engine = self.config.engine

        # 尝试 LevelDB
        if engine in ("auto", "leveldb"):
            try:
                import plyvel
                os.makedirs(self.config.db_path, exist_ok=True)
                self._backend = plyvel.DB(
                    self.config.db_path,
                    create_if_missing=True,
                )
                logger.info("使用 LevelDB 后端")
                return
            except ImportError:
                logger.debug("LevelDB 不可用，尝试其他后端")
            except Exception as e:
                logger.warning(f"LevelDB 初始化失败: {e}")

        # 尝试 RocksDB
        if engine in ("auto", "rocksdb"):
            try:
                import rocksdb
                os.makedirs(self.config.db_path, exist_ok=True)
                opts = rocksdb.Options()
                opts.create_if_missing = True
                self._backend = rocksdb.DB(
                    self.config.db_path, opts
                )
                logger.info("使用 RocksDB 后端")
                return
            except ImportError:
                logger.debug("RocksDB 不可用，回退到内置实现")
            except Exception as e:
                logger.warning(f"RocksDB 初始化失败: {e}")

        # 回退到内置实现
        self._backend = BuiltinLSM(self.config.db_path, self.config)
        logger.info("使用内置 LSM-Tree 实现 (SQLite-based)")

    def put(self, key: str, value: Any) -> bool:
        """写入键值对"""
        return self._backend.put(key, value)

    def get(self, key: str) -> Optional[Any]:
        """获取键对应的值"""
        return self._backend.get(key)

    def delete(self, key: str) -> bool:
        """删除键值对"""
        return self._backend.delete(key)

    def put_batch(self, items: List[Tuple[str, Any]]) -> bool:
        """批量写入"""
        return self._backend.put_batch(items)

    def scan(self, prefix: str, limit: int = 100) -> List[Tuple[str, Any]]:
        """前缀扫描"""
        raw_results = self._backend.scan(prefix, limit)
        return [
            (k.decode("utf-8") if isinstance(k, bytes) else k, v)
            for k, v in raw_results
        ]

    def range_query(self, start: str, end: str, limit: int = 100) -> List[Tuple[str, Any]]:
        """范围查询"""
        raw_results = self._backend.range_query(start, end, limit)
        return [
            (k.decode("utf-8") if isinstance(k, bytes) else k, v)
            for k, v in raw_results
        ]

    def save_index(self, index_data: Dict[str, Any]):
        """保存项目索引"""
        # 存储项目元数据
        self.put("__index__:metadata", {
            "project_root": index_data.get("project_root"),
            "total_files": index_data.get("total_files"),
            "total_entities": index_data.get("total_entities"),
            "total_relations": index_data.get("total_relations"),
            "index_time_ms": index_data.get("index_time_ms"),
        })

        # 存储文件信息
        files = index_data.get("files", {})
        for file_path, file_info in files.items():
            self.put(f"__index__:file:{file_path}", file_info)

        # 存储知识图谱
        kg_data = index_data.get("knowledge_graph", {})
        if hasattr(kg_data, 'to_dict'):
            kg_dict = kg_data.to_dict()
            for node in kg_dict.get("nodes", []):
                self.put(f"__index__:node:{node.get('id', '')}", node)
            for edge in kg_dict.get("edges", []):
                key = f"__index__:edge:{edge.get('source')}->{edge.get('target')}"
                self.put(key, edge)

    def load_index(self) -> Dict[str, Any]:
        """加载项目索引"""
        metadata = self.get("__index__:metadata") or {}

        files = {}
        for key, value in self.scan("__index__:file:", limit=10000):
            file_path = key.replace("__index__:file:", "")
            files[file_path] = value

        return {
            "metadata": metadata,
            "files": files,
        }

    def save_analysis_result(self, analysis_id: str, result: Dict[str, Any]):
        """保存分析结果"""
        self.put(f"__analysis__:{analysis_id}", result)

    def get_analysis_result(self, analysis_id: str) -> Optional[Dict[str, Any]]:
        """获取分析结果"""
        return self.get(f"__analysis__:{analysis_id}")

    def save_entity(self, entity: Dict[str, Any]):
        """保存单个实体"""
        entity_id = entity.get("id", entity.get("name", ""))
        self.put(f"__entity__:{entity_id}", entity)

    def get_entity(self, entity_id: str) -> Optional[Dict[str, Any]]:
        """获取单个实体"""
        return self.get(f"__entity__:{entity_id}")

    @property
    def stats(self) -> Dict[str, Any]:
        """获取存储统计"""
        if hasattr(self._backend, 'stats'):
            return self._backend.stats
        return {"engine": self.config.engine}

    def close(self):
        """关闭存储"""
        if hasattr(self._backend, 'close'):
            self._backend.close()

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        self.close()
