"""Parse small known samples with every CodeLens grammar in the active interpreter."""

import importlib
import sys
from pathlib import Path

sys.path.insert(0, str(Path.cwd()))
analyzer = importlib.import_module("ast_parser").get_analyzer()
samples = {
    "go": "package demo\nfunc sample() {}",
    "python": "def sample():\n    return 1",
    "javascript": "function sample() { return 1; }",
    "typescript": "function sample(): number { return 1; }",
    "tsx": "const Sample = () => <main />;",
    "java": "class Sample { int value() { return 1; } }",
}
assert analyzer.parsers.keys() == samples.keys()
for name, source in samples.items():
    tree = analyzer.parsers[name].parse(source.encode())
    assert not tree.root_node.has_error, (name, str(tree.root_node))
    print(f"PASS CodeLens grammar {name}")
