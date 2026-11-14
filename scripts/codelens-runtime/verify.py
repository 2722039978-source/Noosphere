"""Verify the native modules in the shipped CodeLens interpreter."""

import struct
import sys

import main
import pydantic_core
from ast_parser import get_analyzer


SNIPPETS = {
    ".go": "package sample\nfunc example() {}",
    ".py": "def example():\n    return 1",
    ".js": "function example() {}",
    ".jsx": "const Example = () => <div />;",
    ".ts": "const example: number = 1;",
    ".tsx": "const Example = () => <div />;",
    ".java": "class Example { void example() {} }",
}


def verify():
    if not (sys.platform == "win32" and sys.version_info[:2] == (3, 14) and struct.calcsize("P") == 8):
        raise RuntimeError("CodeLens runtime must be Windows x64 CPython 3.14")
    if main.app is None or not pydantic_core.__version__:
        raise RuntimeError("FastAPI or Pydantic Core could not be loaded")
    analyzer = get_analyzer()
    for extension, sample in SNIPPETS.items():
        language = main.LANGUAGES[extension]
        tree = analyzer.parsers[language].parse(sample.encode("utf-8"))
        if tree.root_node.has_error or not tree.root_node.named_child_count:
            raise RuntimeError(f"CodeLens grammar {language} could not parse {extension} source")
    print(f"CodeLens {sys.version.split()[0]} verified across {len(SNIPPETS)} source types")


if __name__ == "__main__":
    verify()
