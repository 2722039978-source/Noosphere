"""
AST 信息提取模块

从 Tree-sitter AST 中提取结构化的代码实体信息，包括：
- 函数/方法定义及其参数、返回值
- 类/接口定义及其继承关系
- 变量定义及类型注解
- 导入/依赖关系
- 函数调用关系
"""

from enum import Enum
from typing import Optional, Dict, Any, List, Set, Tuple
from dataclasses import dataclass, field
from pathlib import Path

from loguru import logger

from .tree_sitter_parser import TreeSitterParser, ParseResult, Language


class EntityType(Enum):
    """代码实体类型"""
    FUNCTION = "function"
    METHOD = "method"
    CLASS = "class"
    INTERFACE = "interface"
    STRUCT = "struct"
    ENUM = "enum"
    VARIABLE = "variable"
    CONSTANT = "constant"
    IMPORT = "import"
    MODULE = "module"
    TYPE_ALIAS = "type_alias"
    DECORATOR = "decorator"
    ANNOTATION = "annotation"
    COMMENT = "comment"
    UNKNOWN = "unknown"


class RelationType(Enum):
    """实体关系类型"""
    CALLS = "calls"             # 函数调用
    INHERITS = "inherits"       # 继承
    IMPLEMENTS = "implements"   # 接口实现
    IMPORTS = "imports"         # 导入
    CONTAINS = "contains"       # 包含 (类包含方法)
    REFERENCES = "references"   # 引用变量
    DECORATES = "decorates"     # 装饰器
    PARAM_OF = "param_of"       # 是...的参数
    RETURNS = "returns"         # 返回值类型
    DEPENDS_ON = "depends_on"   # 依赖


@dataclass
class CodeLocation:
    """代码位置信息"""
    file_path: str
    start_line: int
    end_line: int
    start_col: int = 0
    end_col: int = 0

    def __str__(self) -> str:
        return f"{self.file_path}:{self.start_line}-{self.end_line}"


@dataclass
class CodeEntity:
    """代码实体"""
    name: str
    type: EntityType
    location: CodeLocation
    language: Language = Language.UNKNOWN
    modifiers: List[str] = field(default_factory=list)  # public, static, async, etc.
    signature: str = ""           # 函数签名
    parameters: List[Dict[str, str]] = field(default_factory=list)
    return_type: Optional[str] = None
    docstring: Optional[str] = None
    decorators: List[str] = field(default_factory=list)
    base_classes: List[str] = field(default_factory=list)
    body_summary: str = ""        # 函数体摘要
    complexity: int = 0           # 圈复杂度
    metadata: Dict[str, Any] = field(default_factory=dict)

    # 关系的目标名称列表
    calls: List[str] = field(default_factory=list)
    imports: List[str] = field(default_factory=list)
    references: List[str] = field(default_factory=list)

    def __hash__(self):
        return hash((self.name, str(self.location), self.type.value))

    def __str__(self) -> str:
        return f"{self.type.value}:{self.name} @ {self.location}"


@dataclass
class Relation:
    """实体间关系"""
    source: str          # 源实体名称
    target: str          # 目标实体名称
    type: RelationType
    source_location: Optional[CodeLocation] = None
    target_location: Optional[CodeLocation] = None
    metadata: Dict[str, Any] = field(default_factory=dict)

    def __str__(self) -> str:
        return f"{self.source} --[{self.type.value}]--> {self.target}"


class ASTExtractor:
    """
    AST 信息提取器

    从解析后的 AST 中提取结构化的代码实体和关系信息。
    支持多语言，提供统一的实体表示。

    使用示例:
        extractor = ASTExtractor()
        entities, relations = extractor.extract(parse_result)
        for entity in entities:
            print(f"{entity.type.value}: {entity.name}")
    """

    def __init__(self):
        self._entity_cache: Dict[str, List[CodeEntity]] = {}

    # ---- 公共 API ----

    def extract(self, parse_result: ParseResult) -> Tuple[List[CodeEntity], List[Relation]]:
        """
        从解析结果中提取所有实体和关系

        Args:
            parse_result: Tree-sitter 解析结果

        Returns:
            (实体列表, 关系列表)
        """
        if not parse_result.success or parse_result.ast_root is None:
            return [], []

        entities: List[CodeEntity] = []
        relations: List[Relation] = []

        # 根据语言类型选择不同的提取策略
        extractors = {
            Language.PYTHON: self._extract_python,
            Language.JAVASCRIPT: self._extract_javascript,
            Language.TYPESCRIPT: self._extract_typescript,
            Language.JAVA: self._extract_java,
            Language.CPP: self._extract_cpp,
            Language.GO: self._extract_go,
            Language.RUST: self._extract_rust,
        }

        extractor = extractors.get(parse_result.language, self._extract_generic)
        entities, relations = extractor(parse_result)

        # 缓存提取结果
        self._entity_cache[parse_result.file_path] = entities

        logger.debug(
            f"提取 {parse_result.file_path}: "
            f"{len(entities)} 实体, {len(relations)} 关系"
        )

        return entities, relations

    def extract_from_file(self, file_path: str, parser: TreeSitterParser) -> Tuple[List[CodeEntity], List[Relation]]:
        """直接从文件提取实体和关系"""
        result = parser.parse_file(file_path)
        return self.extract(result)

    # ---- 通用 AST 遍历工具 ----

    def _walk(self, node: Any, node_type: str) -> List[Any]:
        """遍历 AST 查找指定类型的节点"""
        results = []
        if node.type == node_type:
            results.append(node)
        for child in node.children:
            results.extend(self._walk(child, node_type))
        return results

    def _find_all(self, node: Any, node_types: List[str]) -> List[Any]:
        """查找所有匹配类型的子节点"""
        results = []
        if node.type in node_types:
            results.append(node)
        for child in node.children:
            results.extend(self._find_all(child, node_types))
        return results

    def _get_text(self, node: Any, source: bytes) -> str:
        """获取节点的源代码文本"""
        return source[node.start_byte:node.end_byte].decode("utf-8")

    def _get_child_text(self, node: Any, field_name: str, source: bytes) -> Optional[str]:
        """获取指定字段的子节点文本"""
        child = node.child_by_field_name(field_name)
        if child:
            return self._get_text(child, source)
        return None

    def _make_location(self, node: Any, file_path: str) -> CodeLocation:
        """创建代码位置"""
        return CodeLocation(
            file_path=file_path,
            start_line=node.start_point[0] + 1,
            end_line=node.end_point[0] + 1,
            start_col=node.start_point[1],
            end_col=node.end_point[1],
        )

    # ---- 语言特定的提取器 ----

    def _extract_python(self, result: ParseResult) -> Tuple[List[CodeEntity], List[Relation]]:
        """提取 Python 代码实体和关系"""
        entities: List[CodeEntity] = []
        relations: List[Relation] = []
        root = result.ast_root
        # tree-sitter 0.26+ returns bytes directly; older versions return str
        raw_text = root.text if hasattr(root, 'text') else b""
        source = raw_text if isinstance(raw_text, bytes) else raw_text.encode("utf-8")
        file_path = result.file_path

        # 需要用 source bytes 重建
        import pathlib
        source_bytes = pathlib.Path(file_path).read_bytes()

        # 提取函数定义
        func_nodes = self._find_all(root, ["function_definition"])
        for node in func_nodes:
            name_node = node.child_by_field_name("name")
            if name_node is None:
                continue
            name = self._get_text(name_node, source_bytes)

            # 提取参数
            params = []
            params_node = node.child_by_field_name("parameters")
            if params_node:
                for child in params_node.children:
                    if child.type == "identifier":
                        params.append({
                            "name": self._get_text(child, source_bytes),
                            "type": None
                        })
                    elif child.type == "typed_parameter":
                        param_name = child.child_by_field_name("name")
                        param_type = child.child_by_field_name("type")
                        if param_name:
                            params.append({
                                "name": self._get_text(param_name, source_bytes) if param_name else "",
                                "type": self._get_text(param_type, source_bytes) if param_type else None
                            })

            # 提取返回类型
            return_type_node = node.child_by_field_name("return_type")
            return_type = self._get_text(return_type_node, source_bytes) if return_type_node else None

            # 提取装饰器
            decorators = []
            # 查找父节点中的 decorated_definition
            parent = node.parent
            if parent and parent.type == "decorated_definition":
                for child in parent.children:
                    if child.type == "decorator":
                        decorators.append(self._get_text(child, source_bytes))

            # 提取 docstring
            docstring = None
            body = node.child_by_field_name("body")
            if body and body.children:
                first = body.children[0]
                if first.type == "expression_statement":
                    str_child = first.children[0] if first.children else None
                    if str_child and str_child.type == "string":
                        docstring = self._get_text(str_child, source_bytes)

            entity = CodeEntity(
                name=name,
                type=EntityType.FUNCTION,
                location=self._make_location(node, file_path),
                language=Language.PYTHON,
                modifiers=["async"] if any(c.type == "async" for c in node.children) else [],
                signature=self._get_text(node, source_bytes).split(":")[0] if ":" in self._get_text(node, source_bytes) else "",
                parameters=params,
                return_type=return_type,
                docstring=docstring,
                decorators=decorators,
                body_summary=self._summarize_body(body, source_bytes) if body else "",
                complexity=self._compute_complexity(node),
            )
            entities.append(entity)

        # 提取类定义
        class_nodes = self._find_all(root, ["class_definition"])
        for node in class_nodes:
            name_node = node.child_by_field_name("name")
            if name_node is None:
                continue
            name = self._get_text(name_node, source_bytes)

            # 提取基类
            bases = []
            bases_node = node.child_by_field_name("superclasses")
            if bases_node:
                for child in bases_node.children:
                    if child.type == "identifier":
                        bases.append(self._get_text(child, source_bytes))
                    elif child.type == "attribute":
                        bases.append(self._get_text(child, source_bytes))

            # 提取 docstring
            docstring = None
            body = node.child_by_field_name("body")
            if body and body.children:
                for child in body.children:
                    if child.type == "expression_statement":
                        str_child = child.children[0] if child.children else None
                        if str_child and str_child.type == "string":
                            docstring = self._get_text(str_child, source_bytes)
                            break

            entity = CodeEntity(
                name=name,
                type=EntityType.CLASS,
                location=self._make_location(node, file_path),
                language=Language.PYTHON,
                base_classes=bases,
                docstring=docstring,
            )
            entities.append(entity)

            # 添加继承关系
            for base in bases:
                relations.append(Relation(
                    source=name, target=base,
                    type=RelationType.INHERITS,
                    source_location=self._make_location(node, file_path),
                ))

            # 提取类方法
            for child in body.children if body else []:
                if child.type == "function_definition":
                    method_name_node = child.child_by_field_name("name")
                    if method_name_node:
                        method_name = self._get_text(method_name_node, source_bytes)
                        relations.append(Relation(
                            source=name, target=method_name,
                            type=RelationType.CONTAINS,
                            source_location=self._make_location(node, file_path),
                        ))

        # 提取函数调用关系
        call_nodes = self._find_all(root, ["call"])
        current_func = self._find_enclosing_function
        for node in call_nodes:
            func_child = node.child_by_field_name("function")
            if func_child:
                if func_child.type == "identifier":
                    called_name = self._get_text(func_child, source_bytes)
                    # 找到调用所在的函数
                    enclosing = self._find_enclosing_function(node)
                    if enclosing:
                        enc_name = self._get_text(enclosing.child_by_field_name("name"), source_bytes) if enclosing.child_by_field_name("name") else None
                        if enc_name:
                            relations.append(Relation(
                                source=enc_name, target=called_name,
                                type=RelationType.CALLS,
                                source_location=self._make_location(enclosing, file_path),
                                target_location=self._make_location(node, file_path),
                            ))

        # 提取导入关系
        import_nodes = self._find_all(root, ["import_statement", "import_from_statement"])
        for node in import_nodes:
            if node.type == "import_statement":
                for child in node.children:
                    if child.type == "dotted_name":
                        module = self._get_text(child, source_bytes)
                        entities.append(CodeEntity(
                            name=module, type=EntityType.IMPORT,
                            location=self._make_location(node, file_path),
                            language=Language.PYTHON,
                        ))
                        relations.append(Relation(
                            source=file_path, target=module,
                            type=RelationType.IMPORTS,
                        ))
            elif node.type == "import_from_statement":
                module_node = node.child_by_field_name("module_name")
                module = self._get_text(module_node, source_bytes) if module_node else ""
                for child in node.children:
                    if child.type == "dotted_name" and child != module_node:
                        imported = self._get_text(child, source_bytes)
                        entities.append(CodeEntity(
                            name=f"{module}.{imported}" if module else imported,
                            type=EntityType.IMPORT,
                            location=self._make_location(node, file_path),
                            language=Language.PYTHON,
                        ))

        return entities, relations

    def _extract_javascript(self, result: ParseResult) -> Tuple[List[CodeEntity], List[Relation]]:
        """提取 JavaScript 代码实体"""
        return self._extract_generic(result, "javascript")

    def _extract_typescript(self, result: ParseResult) -> Tuple[List[CodeEntity], List[Relation]]:
        """提取 TypeScript 代码实体"""
        return self._extract_generic(result, "typescript")

    def _extract_java(self, result: ParseResult) -> Tuple[List[CodeEntity], List[Relation]]:
        """提取 Java 代码实体"""
        return self._extract_generic(result, "java")

    def _extract_cpp(self, result: ParseResult) -> Tuple[List[CodeEntity], List[Relation]]:
        """提取 C/C++ 代码实体"""
        return self._extract_generic(result, "cpp")

    def _extract_go(self, result: ParseResult) -> Tuple[List[CodeEntity], List[Relation]]:
        """提取 Go 代码实体"""
        return self._extract_generic(result, "go")

    def _extract_rust(self, result: ParseResult) -> Tuple[List[CodeEntity], List[Relation]]:
        """提取 Rust 代码实体"""
        return self._extract_generic(result, "rust")

    def _extract_generic(self, result: ParseResult, lang_hint: str = "") -> Tuple[List[CodeEntity], List[Relation]]:
        """
        通用提取器 - 基于 AST 节点类型模式匹配

        当没有特定语言的提取器时使用此方法。
        """
        entities: List[CodeEntity] = []
        relations: List[Relation] = []
        root = result.ast_root
        file_path = result.file_path

        if root is None:
            return entities, relations

        # AST 节点类型到实体类型的映射模式
        node_patterns = [
            (["function_definition", "function_declaration", "function_item",
              "method_definition", "method_declaration", "arrow_function"],
             EntityType.FUNCTION),
            (["class_definition", "class_declaration", "class_specifier"],
             EntityType.CLASS),
            (["interface_declaration", "interface_definition"],
             EntityType.INTERFACE),
            (["struct_item", "struct_declaration"],
             EntityType.STRUCT),
            (["variable_declaration", "variable_declarator",
              "assignment", "let_declaration", "const_declaration"],
             EntityType.VARIABLE),
            (["import_statement", "import_declaration",
              "import_from_statement", "use_declaration"],
             EntityType.IMPORT),
        ]

        # 递归遍历
        def traverse(node: Any, depth: int = 0, parent_entity: Optional[str] = None):
            if depth > 100:  # 防止过深递归
                return

            for node_types, entity_type in node_patterns:
                if node.type in node_types:
                    # 尝试获取名称
                    name = self._extract_name(node)
                    if name:
                        entity = CodeEntity(
                            name=name,
                            type=entity_type,
                            location=self._make_location(node, file_path),
                            language=result.language,
                        )
                        entities.append(entity)

                        if parent_entity:
                            relations.append(Relation(
                                source=parent_entity, target=name,
                                type=RelationType.CONTAINS,
                            ))
                        parent_entity = name
                    break

            # 处理调用关系
            if node.type in ("call_expression", "call", "function_call"):
                called = self._extract_name(node)
                if called and parent_entity:
                    relations.append(Relation(
                        source=parent_entity, target=called,
                        type=RelationType.CALLS,
                    ))

            for child in node.children:
                traverse(child, depth + 1, parent_entity)

        traverse(root)
        return entities, relations

    # ---- 辅助方法 ----

    def _extract_name(self, node: Any) -> Optional[str]:
        """从 AST 节点提取名称"""
        if hasattr(node, 'type') and node.type == "identifier":
            if hasattr(node, 'text'):
                return node.text.decode("utf-8") if isinstance(node.text, bytes) else node.text
        name_field = node.child_by_field_name("name") if hasattr(node, 'child_by_field_name') else None
        if name_field:
            if hasattr(name_field, 'text'):
                return name_field.text.decode("utf-8") if isinstance(name_field.text, bytes) else name_field.text
        return None

    def _find_enclosing_function(self, node: Any) -> Optional[Any]:
        """查找包含当前节点的最近函数/方法"""
        current = node.parent if hasattr(node, 'parent') else None
        while current:
            if hasattr(current, 'type') and current.type in (
                "function_definition", "function_declaration", "function_item",
                "method_definition", "method_declaration", "arrow_function"
            ):
                return current
            current = current.parent if hasattr(current, 'parent') else None
        return None

    def _summarize_body(self, body_node: Any, source: bytes) -> str:
        """生成函数体摘要"""
        if body_node is None:
            return ""
        text = self._get_text(body_node, source)
        lines = text.split("\n")
        # 取前5行作为摘要
        return "\n".join(lines[:5]) + ("..." if len(lines) > 5 else "")

    def _compute_complexity(self, node: Any) -> int:
        """计算圈复杂度 (McCabe)"""
        if node is None:
            return 0
        complexity = 1  # 基础复杂度
        branches = [
            "if_statement", "elif_clause", "else_clause",
            "for_statement", "while_statement",
            "try_statement", "except_clause",
            "and", "or", "case_clause",
            "match", "switch_statement", "case_statement",
            "conditional_expression", "ternary_expression",
        ]
        for child_type in branches:
            complexity += len(self._find_all(node, [child_type]))
        return complexity

    def get_cached_entities(self, file_path: str) -> List[CodeEntity]:
        """获取缓存的实体"""
        return self._entity_cache.get(file_path, [])

    def clear_cache(self):
        """清空实体缓存"""
        self._entity_cache.clear()
