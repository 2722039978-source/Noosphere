"""
代码知识图谱

构建代码实体间的关系图谱，支持：
- 调用关系图 (Call Graph)
- 继承关系图 (Inheritance Graph)
- 依赖关系图 (Dependency Graph)
- 模块包含关系 (Containment Graph)

基于 networkx 实现图存储和查询，支持导出为可视化格式。
"""

import json
from typing import Optional, Dict, Any, List, Set, Tuple, Iterator
from dataclasses import dataclass, field
from pathlib import Path

import networkx as nx
from loguru import logger

from ..parser.ast_extractor import CodeEntity, Relation, EntityType, RelationType, CodeLocation


@dataclass
class GraphNode:
    """知识图谱节点"""
    id: str
    name: str
    entity_type: EntityType
    language: str = ""
    file_path: str = ""
    start_line: int = 0
    signature: str = ""
    docstring: str = ""
    complexity: int = 0
    properties: Dict[str, Any] = field(default_factory=dict)

    def to_dict(self) -> Dict[str, Any]:
        return {
            "id": self.id,
            "name": self.name,
            "entity_type": self.entity_type.value,
            "language": self.language,
            "file_path": self.file_path,
            "start_line": self.start_line,
            "signature": self.signature,
            "docstring": self.docstring,
            "complexity": self.complexity,
            "properties": self.properties,
        }


@dataclass
class GraphEdge:
    """知识图谱边"""
    source: str
    target: str
    relation_type: RelationType
    source_file: str = ""
    target_file: str = ""
    weight: float = 1.0
    properties: Dict[str, Any] = field(default_factory=dict)

    def to_dict(self) -> Dict[str, Any]:
        return {
            "source": self.source,
            "target": self.target,
            "relation_type": self.relation_type.value,
            "source_file": self.source_file,
            "target_file": self.target_file,
            "weight": self.weight,
            "properties": self.properties,
        }


class KnowledgeGraph:
    """
    代码知识图谱

    用于存储和查询代码实体之间的多维关系。支持：
    - 按实体名称、类型、文件路径查询节点
    - 按关系类型查询边
    - 调用链追踪 (Call Chain Analysis)
    - 影响分析 (Impact Analysis)
    - 导出为 JSON 或 HTML 可视化

    使用示例:
        kg = KnowledgeGraph()
        kg.add_entity(entity)
        kg.add_relation(relation)

        # 查询某个函数的调用链
        callers = kg.get_callers("my_function")
        callees = kg.get_callees("my_function")

        # 查询某个类的继承链
        hierarchy = kg.get_inheritance_chain("MyClass")
    """

    def __init__(self, name: str = "code-knowledge-graph"):
        """
        初始化知识图谱

        Args:
            name: 图谱名称
        """
        self.name = name
        self.graph = nx.DiGraph(name=name)
        self._node_index: Dict[str, str] = {}   # entity_name -> node_id
        self._file_index: Dict[str, List[str]] = {}  # file_path -> [node_ids]
        self._type_index: Dict[EntityType, List[str]] = {}  # entity_type -> [node_ids]

    # ---- 构建方法 ----

    def add_entity(self, entity: CodeEntity) -> str:
        """
        添加代码实体到图谱

        Args:
            entity: 代码实体

        Returns:
            node_id: 生成的节点 ID
        """
        node_id = self._generate_node_id(entity)
        node = GraphNode(
            id=node_id,
            name=entity.name,
            entity_type=entity.type,
            language=entity.language.value,
            file_path=entity.location.file_path,
            start_line=entity.location.start_line,
            signature=entity.signature,
            docstring=entity.docstring or "",
            complexity=entity.complexity,
        )

        self.graph.add_node(node_id, **node.to_dict())

        # 更新索引
        self._node_index[entity.name] = node_id
        self._file_index.setdefault(entity.location.file_path, []).append(node_id)
        self._type_index.setdefault(entity.type, []).append(node_id)

        return node_id

    def add_relation(self, relation: Relation):
        """添加关系到图谱"""
        source_id = self._node_index.get(relation.source)
        target_id = self._node_index.get(relation.target)

        if source_id is None:
            source_id = relation.source
            self.graph.add_node(source_id, name=relation.source,
                              entity_type=EntityType.UNKNOWN.value)
            self._node_index[relation.source] = source_id

        if target_id is None:
            target_id = relation.target
            self.graph.add_node(target_id, name=relation.target,
                              entity_type=EntityType.UNKNOWN.value)
            self._node_index[relation.target] = target_id

        edge = GraphEdge(
            source=source_id,
            target=target_id,
            relation_type=relation.type,
            source_file=relation.source_location.file_path if relation.source_location else "",
            target_file=relation.target_location.file_path if relation.target_location else "",
        )

        self.graph.add_edge(source_id, target_id, **edge.to_dict())

    def add_entities_batch(self, entities: List[CodeEntity]) -> List[str]:
        """批量添加实体"""
        return [self.add_entity(e) for e in entities]

    def add_relations_batch(self, relations: List[Relation]):
        """批量添加关系"""
        for r in relations:
            self.add_relation(r)

    # ---- 查询方法 ----

    def get_node(self, name: str) -> Optional[GraphNode]:
        """按名称获取节点"""
        node_id = self._node_index.get(name)
        if node_id and node_id in self.graph.nodes:
            data = self.graph.nodes[node_id]
            return GraphNode(**data)
        return None

    def get_nodes_by_type(self, entity_type: EntityType) -> List[GraphNode]:
        """按实体类型获取所有节点"""
        node_ids = self._type_index.get(entity_type, [])
        return [
            GraphNode(**self.graph.nodes[nid])
            for nid in node_ids if nid in self.graph.nodes
        ]

    def get_nodes_by_file(self, file_path: str) -> List[GraphNode]:
        """按文件路径获取所有节点"""
        node_ids = self._file_index.get(file_path, [])
        return [
            GraphNode(**self.graph.nodes[nid])
            for nid in node_ids if nid in self.graph.nodes
        ]

    def search_nodes(self, query: str, case_sensitive: bool = False) -> List[GraphNode]:
        """搜索节点（按名称模糊匹配）"""
        results = []
        query_lower = query if case_sensitive else query.lower()
        for name, node_id in self._node_index.items():
            name_cmp = name if case_sensitive else name.lower()
            if query_lower in name_cmp:
                if node_id in self.graph.nodes:
                    results.append(GraphNode(**self.graph.nodes[node_id]))
        return results

    # ---- 关系查询 ----

    def get_callers(self, function_name: str) -> List[GraphNode]:
        """获取调用该函数的所有函数"""
        node_id = self._node_index.get(function_name)
        if node_id is None:
            return []
        predecessors = self.graph.predecessors(node_id)
        return [
            GraphNode(**self.graph.nodes[pid])
            for pid in predecessors if pid in self.graph.nodes
        ]

    def get_callees(self, function_name: str) -> List[GraphNode]:
        """获取该函数调用的所有函数"""
        node_id = self._node_index.get(function_name)
        if node_id is None:
            return []
        successors = self.graph.successors(node_id)
        return [
            GraphNode(**self.graph.nodes[sid])
            for sid in successors if sid in self.graph.nodes
        ]

    def get_call_chain(self, start_name: str, max_depth: int = 10) -> List[List[str]]:
        """
        获取从指定函数开始的完整调用链

        Args:
            start_name: 起始函数名称
            max_depth: 最大深度

        Returns:
            调用链路径列表，每条路径是函数名序列
        """
        node_id = self._node_index.get(start_name)
        if node_id is None:
            return []

        chains = []
        visited = set()

        def dfs(current: str, path: List[str], depth: int):
            if depth > max_depth or current in visited:
                chains.append(path.copy())
                return
            visited.add(current)
            path.append(self.graph.nodes[current].get("name", current))
            successors = list(self.graph.successors(current))
            if not successors:
                chains.append(path.copy())
            else:
                for succ in successors:
                    dfs(succ, path, depth + 1)
            path.pop()
            visited.discard(current)

        dfs(node_id, [], 0)
        return chains

    def get_inheritance_chain(self, class_name: str) -> List[str]:
        """
        获取类的继承链（从基类到当前类）

        Returns:
            继承链中的类名列表（从顶层基类到当前类）
        """
        node_id = self._node_index.get(class_name)
        if node_id is None:
            return [class_name]

        chain = []
        visited = set()
        current = node_id

        while current and current not in visited:
            visited.add(current)
            chain.append(self.graph.nodes[current].get("name", current))
            # 查找继承边
            predecessors = list(self.graph.predecessors(current))
            inherits_from = None
            for pred in predecessors:
                edge_data = self.graph.get_edge_data(pred, current)
                if edge_data and edge_data.get("relation_type") == RelationType.INHERITS.value:
                    inherits_from = pred
                    break
            current = inherits_from

        # 反转使基类在前
        chain.reverse()
        return chain

    def get_dependents(self, entity_name: str) -> List[GraphNode]:
        """获取依赖该实体的所有实体（反向依赖）"""
        node_id = self._node_index.get(entity_name)
        if node_id is None:
            return []
        predecessors = self.graph.predecessors(node_id)
        return [
            GraphNode(**self.graph.nodes[pid])
            for pid in predecessors if pid in self.graph.nodes
        ]

    def get_dependencies(self, entity_name: str) -> List[GraphNode]:
        """获取该实体依赖的所有实体（正向依赖）"""
        node_id = self._node_index.get(entity_name)
        if node_id is None:
            return []
        successors = self.graph.successors(node_id)
        return [
            GraphNode(**self.graph.nodes[sid])
            for sid in successors if sid in self.graph.nodes
        ]

    def get_impact_analysis(self, entity_name: str, max_depth: int = 5) -> Dict[str, Any]:
        """
        影响分析：修改某个实体会影响哪些其他实体

        Args:
            entity_name: 实体名称
            max_depth: 最大深度

        Returns:
            影响分析结果，包含直接和间接受影响的实体
        """
        node_id = self._node_index.get(entity_name)
        if node_id is None:
            return {"entity": entity_name, "direct": [], "indirect": [], "total": 0}

        direct = []
        indirect = []
        visited = {node_id}
        queue = [(node_id, 1)]

        while queue:
            current, depth = queue.pop(0)
            if depth > max_depth:
                continue
            for pred in self.graph.predecessors(current):
                if pred not in visited:
                    visited.add(pred)
                    node_data = self.graph.nodes[pred]
                    node_info = {
                        "name": node_data.get("name", pred),
                        "type": node_data.get("entity_type", "unknown"),
                        "file": node_data.get("file_path", ""),
                        "depth": depth,
                    }
                    if depth == 1:
                        direct.append(node_info)
                    else:
                        indirect.append(node_info)
                    queue.append((pred, depth + 1))

        return {
            "entity": entity_name,
            "direct": direct,
            "indirect": indirect,
            "total": len(direct) + len(indirect),
        }

    def get_module_summary(self, file_path: str) -> Dict[str, Any]:
        """获取模块摘要"""
        nodes = self.get_nodes_by_file(file_path)
        return {
            "file_path": file_path,
            "total_entities": len(nodes),
            "functions": [n.name for n in nodes if n.entity_type == EntityType.FUNCTION],
            "classes": [n.name for n in nodes if n.entity_type == EntityType.CLASS],
            "imports": [n.name for n in nodes if n.entity_type == EntityType.IMPORT],
            "avg_complexity": sum(n.complexity for n in nodes) / max(len(nodes), 1),
        }

    # ---- 导出方法 ----

    def to_dict(self) -> Dict[str, Any]:
        """导出为字典"""
        nodes = []
        for node_id in self.graph.nodes:
            data = self.graph.nodes[node_id]
            nodes.append(data)

        edges = []
        for u, v, data in self.graph.edges(data=True):
            edges.append({
                "source": u,
                "target": v,
                "relation_type": data.get("relation_type", "unknown"),
                "weight": data.get("weight", 1.0),
            })

        return {
            "name": self.name,
            "node_count": self.graph.number_of_nodes(),
            "edge_count": self.graph.number_of_edges(),
            "nodes": nodes,
            "edges": edges,
        }

    def to_json(self, file_path: Optional[str] = None) -> str:
        """导出为 JSON"""
        data = self.to_dict()
        json_str = json.dumps(data, indent=2, ensure_ascii=False)
        if file_path:
            Path(file_path).write_text(json_str, encoding="utf-8")
            logger.info(f"知识图谱已导出到: {file_path}")
        return json_str

    def to_html(self, file_path: str):
        """
        导出为交互式 HTML 可视化

        使用 pyvis 生成可交互的网络图。
        """
        try:
            from pyvis.network import Network

            net = Network(height="800px", width="100%", directed=True, notebook=False)

            # 类型到颜色的映射
            color_map = {
                EntityType.FUNCTION.value: "#4CAF50",
                EntityType.METHOD.value: "#8BC34A",
                EntityType.CLASS.value: "#2196F3",
                EntityType.INTERFACE.value: "#00BCD4",
                EntityType.STRUCT.value: "#009688",
                EntityType.VARIABLE.value: "#FF9800",
                EntityType.IMPORT.value: "#9E9E9E",
                EntityType.UNKNOWN.value: "#607D8B",
            }

            for node_id in self.graph.nodes:
                data = self.graph.nodes[node_id]
                label = data.get("name", node_id)
                entity_type = data.get("entity_type", "unknown")
                color = color_map.get(entity_type, "#607D8B")
                title = (
                    f"Type: {entity_type}\n"
                    f"File: {data.get('file_path', 'N/A')}\n"
                    f"Line: {data.get('start_line', 'N/A')}"
                )
                net.add_node(node_id, label=label, color=color, title=title)

            for u, v, data in self.graph.edges(data=True):
                rel_type = data.get("relation_type", "unknown")
                net.add_edge(u, v, title=rel_type, arrows="to")

            net.show(file_path)
            logger.info(f"知识图谱可视化已导出到: {file_path}")

        except ImportError:
            logger.warning("pyvis 未安装，无法生成 HTML 可视化。请运行: pip install pyvis")

    # ---- 统计方法 ----

    @property
    def stats(self) -> Dict[str, Any]:
        """获取图谱统计信息"""
        type_counts = {}
        for entity_type, node_ids in self._type_index.items():
            type_counts[entity_type.value] = len(node_ids)

        return {
            "name": self.name,
            "total_nodes": self.graph.number_of_nodes(),
            "total_edges": self.graph.number_of_edges(),
            "type_distribution": type_counts,
            "files_indexed": len(self._file_index),
            "density": nx.density(self.graph),
            "is_connected": nx.is_weakly_connected(self.graph),
        }

    # ---- 内部方法 ----

    def _generate_node_id(self, entity: CodeEntity) -> str:
        """生成唯一的节点 ID"""
        # 使用 文件路径::实体名::实体类型 作为唯一标识
        return f"{entity.location.file_path}::{entity.name}::{entity.type.value}"

    def __len__(self) -> int:
        return self.graph.number_of_nodes()

    def __contains__(self, name: str) -> bool:
        return name in self._node_index
