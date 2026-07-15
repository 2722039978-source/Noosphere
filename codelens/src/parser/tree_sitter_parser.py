"""
Tree-sitter 多语言代码解析器

基于 Tree-sitter 实现多语言（Python, JavaScript, TypeScript,
Java, C/C++, Go, Rust）的代码解析，提供统一的 AST 接口。
"""

import re
from enum import Enum
from pathlib import Path
from typing import Optional, Dict, Any, List, Tuple
from dataclasses import dataclass, field

from loguru import logger


class Language(Enum):
    """支持的语言枚举"""
    PYTHON = "python"
    JAVASCRIPT = "javascript"
    TYPESCRIPT = "typescript"
    JAVA = "java"
    CPP = "cpp"
    GO = "go"
    RUST = "rust"
    UNKNOWN = "unknown"

    @classmethod
    def from_extension(cls, ext: str) -> "Language":
        """根据文件扩展名推断语言"""
        mapping = {
            ".py": cls.PYTHON,
            ".pyi": cls.PYTHON,
            ".js": cls.JAVASCRIPT,
            ".jsx": cls.JAVASCRIPT,
            ".mjs": cls.JAVASCRIPT,
            ".ts": cls.TYPESCRIPT,
            ".tsx": cls.TYPESCRIPT,
            ".java": cls.JAVA,
            ".c": cls.CPP,
            ".cpp": cls.CPP,
            ".cc": cls.CPP,
            ".cxx": cls.CPP,
            ".h": cls.CPP,
            ".hpp": cls.CPP,
            ".go": cls.GO,
            ".rs": cls.RUST,
        }
        return mapping.get(ext.lower(), cls.UNKNOWN)

    @classmethod
    def from_filename(cls, filename: str) -> "Language":
        """根据文件名推断语言"""
        ext = Path(filename).suffix
        lang = cls.from_extension(ext)
        if lang == cls.UNKNOWN:
            # 特殊文件名检测
            name = Path(filename).name.lower()
            if name in ("makefile", "cmakelists.txt"):
                return cls.UNKNOWN
        return lang


@dataclass
class ParseResult:
    """解析结果"""
    file_path: str
    language: Language
    ast_root: Optional[Any] = None       # Tree-sitter 根节点
    parse_time_ms: float = 0.0
    errors: List[str] = field(default_factory=list)
    node_count: int = 0
    lines_of_code: int = 0

    @property
    def success(self) -> bool:
        return len(self.errors) == 0


class TreeSitterParser:
    """
    基于 Tree-sitter 的多语言代码解析器

    支持 Python, JavaScript, TypeScript, Java, C/C++, Go, Rust 代码的
    AST 解析，提供统一的解析接口。

    使用示例:
        parser = TreeSitterParser()
        result = parser.parse_file("src/main.py")
        if result.success:
            print(f"解析成功: {result.node_count} 个节点")
    """

    # 每种语言的核心查询模板
    QUERY_TEMPLATES: Dict[Language, Dict[str, str]] = {
        Language.PYTHON: {
            "functions": """
                (function_definition
                    name: (identifier) @function.name
                    parameters: (parameters) @function.params
                    body: (block) @function.body
                    return_type: (type)? @function.return_type
                )
                (decorated_definition
                    (function_definition
                        name: (identifier) @function.name
                    )
                )
            """,
            "classes": """
                (class_definition
                    name: (identifier) @class.name
                    superclasses: (argument_list)? @class.bases
                    body: (block) @class.body
                )
            """,
            "imports": """
                (import_statement
                    name: (dotted_name) @import.module
                )
                (import_from_statement
                    module_name: (dotted_name)? @import.from
                    name: (dotted_name) @import.name
                )
            """,
            "variables": """
                (assignment
                    left: (identifier) @var.name
                )
            """,
            "calls": """
                (call
                    function: (identifier) @call.name
                )
                (call
                    function: (attribute
                        attribute: (identifier) @call.method
                    )
                )
            """,
        },
        Language.JAVASCRIPT: {
            "functions": """
                (function_declaration
                    name: (identifier) @function.name
                    parameters: (formal_parameters) @function.params
                    body: (statement_block) @function.body
                )
                (arrow_function) @function.arrow
                (function) @function.anon
            """,
            "classes": """
                (class_declaration
                    name: (identifier) @class.name
                    body: (class_body) @class.body
                )
            """,
            "imports": """
                (import_statement
                    source: (string) @import.source
                )
                (import_statement
                    (import_clause
                        (named_imports) @import.named
                    )
                )
            """,
            "variables": """
                (variable_declarator
                    name: (identifier) @var.name
                )
            """,
            "calls": """
                (call_expression
                    function: (identifier) @call.name
                )
            """,
        },
        Language.TYPESCRIPT: {
            "functions": """
                (function_declaration
                    name: (identifier) @function.name
                )
                (method_definition
                    name: (property_identifier) @method.name
                )
            """,
            "classes": """
                (class_declaration
                    name: (type_identifier) @class.name
                )
            """,
            "interfaces": """
                (interface_declaration
                    name: (type_identifier) @interface.name
                )
            """,
            "imports": """
                (import_statement
                    source: (string) @import.source
                )
            """,
        },
        Language.JAVA: {
            "functions": """
                (method_declaration
                    name: (identifier) @method.name
                    parameters: (formal_parameters) @method.params
                    body: (block) @method.body
                )
            """,
            "classes": """
                (class_declaration
                    name: (identifier) @class.name
                    body: (class_body) @class.body
                )
                (interface_declaration
                    name: (identifier) @interface.name
                )
            """,
            "imports": """
                (import_declaration
                    (scoped_identifier) @import.path
                )
            """,
        },
        Language.CPP: {
            "functions": """
                (function_definition
                    declarator: (function_declarator
                        declarator: (identifier) @function.name
                    )
                )
            """,
            "classes": """
                (class_specifier
                    name: (type_identifier) @class.name
                )
            """,
            "includes": """
                (preproc_include
                    path: (string_literal) @include.path
                )
            """,
        },
        Language.GO: {
            "functions": """
                (function_declaration
                    name: (identifier) @function.name
                    parameters: (parameter_list) @function.params
                )
                (method_declaration
                    name: (field_identifier) @method.name
                )
            """,
            "imports": """
                (import_declaration
                    path: (interpreted_string_literal) @import.path
                )
            """,
        },
        Language.RUST: {
            "functions": """
                (function_item
                    name: (identifier) @function.name
                    parameters: (parameters) @function.params
                )
            """,
            "structs": """
                (struct_item
                    name: (type_identifier) @struct.name
                )
            """,
            "impls": """
                (impl_item
                    type: (type_identifier) @impl.type
                )
            """,
            "imports": """
                (use_declaration
                    argument: (scoped_identifier) @use.path
                )
            """,
        },
    }

    def __init__(self, languages: Optional[List[Language]] = None):
        """
        初始化解析器

        Args:
            languages: 要加载的语言列表，None 表示加载所有支持的语言
        """
        self._parsers: Dict[Language, Any] = {}
        self._loaded = False
        self._target_languages = languages or list(Language)
        # 移除 UNKNOWN
        self._target_languages = [
            l for l in self._target_languages if l != Language.UNKNOWN
        ]

    def _ensure_loaded(self):
        """确保 Tree-sitter 语言库已加载"""
        if self._loaded:
            return

        try:
            import tree_sitter_python
            import tree_sitter_javascript
            import tree_sitter_typescript
            import tree_sitter_java
            import tree_sitter_cpp
            import tree_sitter_go
            import tree_sitter_rust
        except ImportError as e:
            logger.error(f"Tree-sitter 语言包未安装: {e}")
            logger.info("请运行: pip install tree-sitter-python tree-sitter-javascript ...")
            raise

        # 语言到库的映射
        lang_libs = {
            Language.PYTHON: tree_sitter_python,
            Language.JAVASCRIPT: tree_sitter_javascript,
            Language.TYPESCRIPT: tree_sitter_typescript,
            Language.JAVA: tree_sitter_java,
            Language.CPP: tree_sitter_cpp,
            Language.GO: tree_sitter_go,
            Language.RUST: tree_sitter_rust,
        }

        import tree_sitter

        # tree-sitter-typescript 0.23+ has language_typescript()/language_tsx() instead of language()
        # Handle special language function names
        _LANG_FN_MAP = {
            Language.TYPESCRIPT: "language_typescript",
        }

        for lang in self._target_languages:
            if lang in lang_libs:
                try:
                    lib = lang_libs[lang]
                    # Some packages use a non-standard function name
                    fn_name = _LANG_FN_MAP.get(lang, "language")
                    lang_capsule = getattr(lib, fn_name)()
                    # tree-sitter 0.26+: wrap PyCapsule in Language, assign to parser.language
                    ts_lang = tree_sitter.Language(lang_capsule)
                    parser = tree_sitter.Parser()
                    parser.language = ts_lang
                    self._parsers[lang] = parser
                    logger.debug(f"加载语言: {lang.value}")
                except Exception as e:
                    logger.warning(f"无法加载语言 {lang.value}: {e}")

        self._loaded = True
        logger.info(f"已加载 {len(self._parsers)} 种语言的解析器")

    def detect_language(self, file_path: str) -> Language:
        """检测文件的编程语言"""
        return Language.from_filename(file_path)

    def parse_file(self, file_path: str, encoding: str = "utf-8") -> ParseResult:
        """
        解析单个文件

        Args:
            file_path: 文件路径
            encoding: 文件编码

        Returns:
            ParseResult: 解析结果，包含 AST 和元信息
        """
        self._ensure_loaded()

        path = Path(file_path)
        language = self.detect_language(file_path)

        result = ParseResult(
            file_path=str(path),
            language=language,
        )

        if language == Language.UNKNOWN:
            result.errors.append(f"不支持的文件类型: {path.suffix}")
            return result

        if language not in self._parsers:
            result.errors.append(f"未加载语言解析器: {language.value}")
            return result

        try:
            import time
            start = time.time()

            with open(file_path, "r", encoding=encoding) as f:
                source_code = f.read()

            result.lines_of_code = len(source_code.splitlines())

            parser = self._parsers[language]
            tree = parser.parse(bytes(source_code, encoding))
            result.ast_root = tree.root_node

            # 统计节点数量
            def count_nodes(node):
                count = 1
                for child in node.children:
                    count += count_nodes(child)
                return count

            result.node_count = count_nodes(tree.root_node)
            result.parse_time_ms = (time.time() - start) * 1000

            logger.debug(
                f"解析 {path.name}: {result.node_count} 节点, "
                f"{result.lines_of_code} 行, {result.parse_time_ms:.2f}ms"
            )

        except SyntaxError as e:
            result.errors.append(f"语法错误: {e}")
        except UnicodeDecodeError as e:
            result.errors.append(f"编码错误: {e}")
        except Exception as e:
            result.errors.append(f"解析异常: {e}")

        return result

    def parse_code(self, source_code: str, language: Language) -> ParseResult:
        """
        解析源代码字符串

        Args:
            source_code: 源代码字符串
            language: 编程语言

        Returns:
            ParseResult: 解析结果
        """
        self._ensure_loaded()

        result = ParseResult(
            file_path="<memory>",
            language=language,
            lines_of_code=len(source_code.splitlines()),
        )

        if language not in self._parsers:
            result.errors.append(f"未加载语言解析器: {language.value}")
            return result

        try:
            import time
            start = time.time()

            parser = self._parsers[language]
            tree = parser.parse(bytes(source_code, "utf-8"))
            result.ast_root = tree.root_node

            def count_nodes(node):
                count = 1
                for child in node.children:
                    count += count_nodes(child)
                return count

            result.node_count = count_nodes(tree.root_node)
            result.parse_time_ms = (time.time() - start) * 1000

        except Exception as e:
            result.errors.append(f"解析异常: {e}")

        return result

    def get_parser(self, language: Language) -> Optional[Any]:
        """获取指定语言的解析器实例"""
        self._ensure_loaded()
        return self._parsers.get(language)

    @property
    def loaded_languages(self) -> List[Language]:
        """获取已加载的语言列表"""
        self._ensure_loaded()
        return list(self._parsers.keys())

    def get_query_templates(self, language: Language) -> Dict[str, str]:
        """获取指定语言的查询模板"""
        return self.QUERY_TEMPLATES.get(language, {})
