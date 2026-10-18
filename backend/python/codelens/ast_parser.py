"""AST parser using tree-sitter for Go, Python, TypeScript, JavaScript, Java."""
from pathlib import Path
from tree_sitter import Language, Parser, Node

LANGUAGES = {
    ".go":   "go",
    ".py":   "python",
    ".ts":   "typescript",
    ".tsx":  "tsx",
    ".js":   "javascript",
    ".jsx":  "javascript",
    ".java": "java",
}

def _build_lang():
    import tree_sitter_go, tree_sitter_python, tree_sitter_javascript
    import tree_sitter_typescript, tree_sitter_java
    go_lib = tree_sitter_go.language()
    py_lib = tree_sitter_python.language()
    js_lib = tree_sitter_javascript.language()
    ts_lib = tree_sitter_typescript.language_typescript()
    tsx_lib = tree_sitter_typescript.language_tsx()
    java_lib = tree_sitter_java.language()
    return {
        "go":         Language(go_lib),
        "python":     Language(py_lib),
        "javascript": Language(js_lib),
        "typescript": Language(ts_lib),
        "tsx":        Language(tsx_lib),
        "java":       Language(java_lib),
    }

LANG_CACHE = None

def get_languages():
    global LANG_CACHE
    if LANG_CACHE is None:
        LANG_CACHE = _build_lang()
    return LANG_CACHE

class ASTAnalyzer:
    def __init__(self):
        self.parsers = {}
        langs = get_languages()
        for name, lang in langs.items():
            self.parsers[name] = Parser(lang)

    def analyze_file(self, filepath: str, source=None):
        ext = Path(filepath).suffix.lower()
        lang_name = LANGUAGES.get(ext)
        if not lang_name or lang_name not in self.parsers:
            return None
        try:
            if source is None:
                source = Path(filepath).read_bytes()
        except:
            return None
        tree = self.parsers[lang_name].parse(source)
        root = tree.root_node
        entities = []
        imports = []
        self._extract_entities(root, source, lang_name, entities)
        self._extract_imports(root, source, lang_name, imports)
        calls = self._extract_calls(root, source, lang_name)
        return {
            "file":      filepath,
            "language":  lang_name,
            "lines":     len(source.splitlines()),
            "entities":  entities,
            "imports":   imports,
            "calls":     calls,
        }

    def _extract_entities(self, node, source, lang, entities, depth=0):
        if depth > 300:
            return
        kind = node.type
        name = None
        entity_type = None

        if kind in ("function_declaration", "method_declaration", "func_literal",
                    "function_definition", "method_definition",
                    "constructor_declaration"):
            entity_type = "function"
        elif kind in ("class_declaration", "class_definition"):
            entity_type = "class"
        elif kind in ("type_declaration", "type_spec",
                      "interface_declaration", "interface_definition"):
            entity_type = "interface"
        elif kind in ("enum_declaration", "enum_definition"):
            entity_type = "enum"
        elif kind in ("variable_declaration", "var_spec", "short_var_declaration",
                      "field_declaration", "local_variable_declaration"):
            entity_type = "variable"

        if entity_type:
            name = self._get_node_name(node, source)
            if name:
                entities.append({
                    "name":      name,
                    "type":      entity_type,
                    "line":      node.start_point[0] + 1,
                    "end_line":  node.end_point[0] + 1,
                    "start_byte": node.start_byte,
                    "end_byte": node.end_byte,
                    "scope": self._scope(node, source),
                })

        if kind == 'variable_declarator':
            value = node.child_by_field_name('value')
            name = self._get_node_name(node, source)
            if name and value is not None and value.type in ('arrow_function', 'function_expression'):
                entities.append({'name': name, 'type': 'function', 'line': node.start_point[0]+1,
                                 'end_line': node.end_point[0]+1, 'start_byte': node.start_byte,
                                 'end_byte': node.end_byte, 'scope': self._scope(node, source)})

        for child in node.children:
            self._extract_entities(child, source, lang, entities, depth + 1)

    def _extract_imports(self, node, source, lang, imports, depth=0):
        if depth > 200:
            return
        kind = node.type
        if kind in ("import_declaration", "import_statement",
                    "import_from_statement", "import",
                    "package_declaration", "using_directive"):
            text = source[node.start_byte:node.end_byte].decode('utf8', errors='replace')
            imports.append({"raw": text.strip(), "line": node.start_point[0] + 1})
        for child in node.children:
            self._extract_imports(child, source, lang, imports, depth + 1)

    def _extract_calls(self, node, source, lang, depth=0):
        calls = []
        if depth > 300:
            return calls
        kind = node.type
        if kind in ("call_expression", "call"):
            func_node = node.child_by_field_name("function")
            if func_node:
                name = source[func_node.start_byte:func_node.end_byte].decode('utf8', errors='replace')
                if name:
                    calls.append({
                        "name":       name,
                        "line":       node.start_point[0] + 1,
                        "start_byte": node.start_byte,
                        "arguments":  node.child_count - 1,
                    })
        elif kind == "method_invocation":
            name_node = node.child_by_field_name("name")
            obj_node = node.child_by_field_name("object")
            if name_node:
                full = source[name_node.start_byte:name_node.end_byte].decode('utf8', errors='replace')
                if obj_node:
                    full = source[obj_node.start_byte:obj_node.end_byte].decode('utf8', errors='replace') + "." + full
                calls.append({
                    "name": full,
                    "line": node.start_point[0] + 1,
                    "start_byte": node.start_byte,
                })
        for child in node.children:
            calls.extend(self._extract_calls(child, source, lang, depth + 1))
        return calls

    def _get_node_name(self, node, source):
        name_node = node.child_by_field_name("name")
        if name_node:
            return source[name_node.start_byte:name_node.end_byte].decode('utf8', errors='replace')
        # Fallback: try declarations list for variable declarations
        if node.type in ("variable_declaration", "var_spec",
                         "field_declaration", "local_variable_declaration"):
            for child in node.children:
                if child.type == "variable_declarator":
                    n = child.child_by_field_name("name")
                    if n:
                        return source[n.start_byte:n.end_byte].decode('utf8', errors='replace')
        return None

    def _scope(self, node, source):
        names = []
        parent = node.parent
        while parent:
            if parent.type in ('function_definition', 'function_declaration', 'class_definition', 'class_declaration', 'method_definition', 'method_declaration'):
                name = self._get_node_name(parent, source)
                if name:
                    names.append(name)
            parent = parent.parent
        return '.'.join(reversed(names))

_analyzer = None

def get_analyzer():
    global _analyzer
    if _analyzer is None:
        _analyzer = ASTAnalyzer()
    return _analyzer
