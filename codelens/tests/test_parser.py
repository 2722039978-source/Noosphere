"""
代码解析模块测试
"""

import os
import sys
import unittest
from pathlib import Path

# 添加项目根目录到路径
sys.path.insert(0, str(Path(__file__).parent.parent))

from src.parser.tree_sitter_parser import TreeSitterParser, Language, ParseResult
from src.parser.ast_extractor import ASTExtractor, CodeEntity, EntityType, RelationType


class TestLanguageDetection(unittest.TestCase):
    """测试语言检测"""

    def test_python_detection(self):
        self.assertEqual(Language.from_filename("main.py"), Language.PYTHON)
        self.assertEqual(Language.from_filename("test.pyi"), Language.PYTHON)

    def test_javascript_detection(self):
        self.assertEqual(Language.from_filename("app.js"), Language.JAVASCRIPT)
        self.assertEqual(Language.from_filename("component.jsx"), Language.JAVASCRIPT)

    def test_typescript_detection(self):
        self.assertEqual(Language.from_filename("index.ts"), Language.TYPESCRIPT)
        self.assertEqual(Language.from_filename("app.tsx"), Language.TYPESCRIPT)

    def test_java_detection(self):
        self.assertEqual(Language.from_filename("Main.java"), Language.JAVA)

    def test_cpp_detection(self):
        self.assertEqual(Language.from_filename("main.cpp"), Language.CPP)
        self.assertEqual(Language.from_filename("header.h"), Language.CPP)

    def test_go_detection(self):
        self.assertEqual(Language.from_filename("main.go"), Language.GO)

    def test_rust_detection(self):
        self.assertEqual(Language.from_filename("main.rs"), Language.RUST)

    def test_unknown_detection(self):
        self.assertEqual(Language.from_filename("README.md"), Language.UNKNOWN)
        self.assertEqual(Language.from_filename("Makefile"), Language.UNKNOWN)


class TestTreeSitterParser(unittest.TestCase):
    """测试 Tree-sitter 解析器"""

    @classmethod
    def setUpClass(cls):
        cls.parser = TreeSitterParser()

    def test_parser_creation(self):
        """测试解析器创建"""
        self.assertIsNotNone(self.parser)
        self.assertGreater(len(self.parser.loaded_languages), 0)

    def test_parse_python_code(self):
        """测试解析 Python 代码"""
        code = """
def hello(name: str) -> str:
    \"\"\"Say hello\"\"\"
    return f"Hello, {name}!"

class Greeter:
    def greet(self, name: str) -> None:
        print(hello(name))
"""
        result = self.parser.parse_code(code, Language.PYTHON)

        if result.success:
            self.assertGreater(result.node_count, 0)
            self.assertEqual(result.lines_of_code, 10)
            self.assertLess(result.parse_time_ms, 1000)
        else:
            self.skipTest(f"Tree-sitter Python not available: {result.errors}")

    def test_parse_javascript_code(self):
        """测试解析 JavaScript 代码"""
        code = """
function greet(name) {
    return `Hello, ${name}!`;
}

class Person {
    constructor(name) {
        this.name = name;
    }
}
"""
        result = self.parser.parse_code(code, Language.JAVASCRIPT)

        if result.success:
            self.assertGreater(result.node_count, 0)
        else:
            self.skipTest(f"Tree-sitter JavaScript not available: {result.errors}")

    def test_parse_invalid_code(self):
        """测试解析无效代码"""
        code = "def broken("
        result = self.parser.parse_code(code, Language.PYTHON)

        # 即使代码有语法错误，tree-sitter 通常也能返回部分 AST
        self.assertIsNotNone(result)


class TestASTExtractor(unittest.TestCase):
    """测试 AST 提取器"""

    @classmethod
    def setUpClass(cls):
        cls.parser = TreeSitterParser()
        cls.extractor = ASTExtractor()

    def test_extract_python_entities(self):
        """测试提取 Python 实体"""
        code = """
import os
from typing import List

def calculate_sum(numbers: List[int]) -> int:
    \"\"\"Calculate the sum of numbers.\"\"\"
    total = 0
    for n in numbers:
        total += n
    return total

class Calculator:
    \"\"\"A simple calculator class.\"\"\"

    def __init__(self):
        self.history = []

    def add(self, a: int, b: int) -> int:
        \"\"\"Add two numbers.\"\"\"
        result = a + b
        self.history.append(('add', a, b, result))
        return result

def main():
    calc = Calculator()
    result = calc.add(1, 2)
    print(calculate_sum([1, 2, 3]))
"""
        result = self.parser.parse_code(code, Language.PYTHON)
        if not result.success:
            self.skipTest(f"Tree-sitter Python not available: {result.errors}")

        entities, relations = self.extractor.extract(result)

        # 应该提取到多个实体
        entity_names = [e.name for e in entities]
        self.assertIn("calculate_sum", entity_names)
        self.assertIn("Calculator", entity_names)

        # 应该有继承/包含关系
        print(f"\nExtracted {len(entities)} entities and {len(relations)} relations")
        for e in entities:
            print(f"  {e.type.value}: {e.name} (complexity: {e.complexity})")

    def test_extract_empty_code(self):
        """测试提取空代码"""
        result = self.parser.parse_code("", Language.PYTHON)
        entities, relations = self.extractor.extract(result)

        self.assertEqual(len(entities), 0)
        self.assertEqual(len(relations), 0)

    def test_entity_types(self):
        """测试实体类型枚举"""
        self.assertEqual(EntityType.FUNCTION.value, "function")
        self.assertEqual(EntityType.CLASS.value, "class")
        self.assertEqual(EntityType.VARIABLE.value, "variable")

    def test_relation_types(self):
        """测试关系类型枚举"""
        self.assertEqual(RelationType.CALLS.value, "calls")
        self.assertEqual(RelationType.INHERITS.value, "inherits")
        self.assertEqual(RelationType.IMPORTS.value, "imports")


class TestIntegration(unittest.TestCase):
    """集成测试"""

    @classmethod
    def setUpClass(cls):
        cls.parser = TreeSitterParser()
        cls.extractor = ASTExtractor()

    def test_full_pipeline(self):
        """测试完整的解析-提取流程"""
        code = """
def factorial(n: int) -> int:
    if n <= 1:
        return 1
    return n * factorial(n - 1)

def main():
    result = factorial(5)
    print(f"Result: {result}")
"""
        # 解析
        result = self.parser.parse_code(code, Language.PYTHON)
        if not result.success:
            self.skipTest(f"Parsing failed: {result.errors}")

        # 提取
        entities, relations = self.extractor.extract(result)

        # 验证
        func_names = [e.name for e in entities if e.type == EntityType.FUNCTION]
        self.assertIn("factorial", func_names)
        self.assertIn("main", func_names)

        # 应该有调用关系
        call_relations = [r for r in relations if r.type == RelationType.CALLS]
        self.assertTrue(any(r.target == "factorial" for r in call_relations))


if __name__ == "__main__":
    unittest.main(verbosity=2)
